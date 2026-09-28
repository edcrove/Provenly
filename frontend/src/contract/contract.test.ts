import { mkdirSync, writeFileSync } from 'node:fs'
import path from 'node:path'

import { setupServer } from 'msw/node'
import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest'

import { createApiClient, type ApiClient } from '@/api/client'
import { db, handlers, resetDb } from '@/test/mockApi'

import { consumedOperations } from './consumed'
import { declaredStatuses, operations, validateRequest, validateResponse } from './spec'

/**
 * Contract gate (frontend): every response variant of every operation the
 * frontend consumes is exercised through the generated client against the MSW
 * handlers used by the integration tests; requests and responses are validated
 * against api/openapi.yaml.
 */
type Result = { response: Response }
interface Scenario {
  op: string
  status: number
  setup?: () => void
  call: (c: ApiClient) => Promise<Result>
}

const tc = { testCaseId: 153 }
const unknownTc = { testCaseId: 987654 }
const run = { testRunId: 7 }
const unknownRun = { testRunId: 987654 }
const fail = () => {
  db.failing = true
}

const scenarios: Scenario[] = [
  // Test cases
  {
    op: 'GET /api/v1/test-cases',
    status: 200,
    call: (c) => c.GET('/api/v1/test-cases', { params: { query: { page: 1, status: 'active' } } }),
  },
  {
    op: 'GET /api/v1/test-cases',
    status: 400,
    call: (c) => c.GET('/api/v1/test-cases', { params: { query: { page: 0 } } }),
  },
  { op: 'GET /api/v1/test-cases', status: 500, setup: fail, call: (c) => c.GET('/api/v1/test-cases') },
  {
    op: 'POST /api/v1/test-cases',
    status: 201,
    call: (c) => c.POST('/api/v1/test-cases', { body: { title: 'New', automated: true } }),
  },
  {
    op: 'POST /api/v1/test-cases',
    status: 400,
    call: (c) => c.POST('/api/v1/test-cases', { body: { title: ' ' } }),
  },
  {
    op: 'POST /api/v1/test-cases',
    status: 500,
    setup: fail,
    call: (c) => c.POST('/api/v1/test-cases', { body: { title: 'x' } }),
  },
  {
    op: 'GET /api/v1/test-cases/{testCaseId}',
    status: 200,
    call: (c) => c.GET('/api/v1/test-cases/{testCaseId}', { params: { path: tc } }),
  },
  {
    op: 'GET /api/v1/test-cases/{testCaseId}',
    status: 400,
    call: (c) => c.GET('/api/v1/test-cases/{testCaseId}', { params: { path: { testCaseId: 0 } } }),
  },
  {
    op: 'GET /api/v1/test-cases/{testCaseId}',
    status: 404,
    call: (c) => c.GET('/api/v1/test-cases/{testCaseId}', { params: { path: unknownTc } }),
  },
  {
    op: 'GET /api/v1/test-cases/{testCaseId}',
    status: 500,
    setup: fail,
    call: (c) => c.GET('/api/v1/test-cases/{testCaseId}', { params: { path: tc } }),
  },
  {
    op: 'PATCH /api/v1/test-cases/{testCaseId}',
    status: 200,
    call: (c) =>
      c.PATCH('/api/v1/test-cases/{testCaseId}', {
        params: { path: tc },
        body: { title: 'Edited', automated: false },
      }),
  },
  {
    op: 'PATCH /api/v1/test-cases/{testCaseId}',
    status: 400,
    call: (c) => c.PATCH('/api/v1/test-cases/{testCaseId}', { params: { path: tc }, body: {} }),
  },
  {
    op: 'PATCH /api/v1/test-cases/{testCaseId}',
    status: 404,
    call: (c) =>
      c.PATCH('/api/v1/test-cases/{testCaseId}', { params: { path: unknownTc }, body: { title: 'x' } }),
  },
  {
    op: 'PATCH /api/v1/test-cases/{testCaseId}',
    status: 500,
    setup: fail,
    call: (c) => c.PATCH('/api/v1/test-cases/{testCaseId}', { params: { path: tc }, body: { title: 'x' } }),
  },
  {
    op: 'POST /api/v1/test-cases/{testCaseId}/deprecate',
    status: 200,
    call: (c) => c.POST('/api/v1/test-cases/{testCaseId}/deprecate', { params: { path: tc } }),
  },
  {
    op: 'POST /api/v1/test-cases/{testCaseId}/deprecate',
    status: 400,
    call: (c) =>
      c.POST('/api/v1/test-cases/{testCaseId}/deprecate', { params: { path: { testCaseId: -1 } } }),
  },
  {
    op: 'POST /api/v1/test-cases/{testCaseId}/deprecate',
    status: 404,
    call: (c) => c.POST('/api/v1/test-cases/{testCaseId}/deprecate', { params: { path: unknownTc } }),
  },
  {
    op: 'POST /api/v1/test-cases/{testCaseId}/deprecate',
    status: 500,
    setup: fail,
    call: (c) => c.POST('/api/v1/test-cases/{testCaseId}/deprecate', { params: { path: tc } }),
  },
  {
    op: 'GET /api/v1/test-cases/{testCaseId}/results',
    status: 200,
    call: (c) =>
      c.GET('/api/v1/test-cases/{testCaseId}/results', { params: { path: tc, query: { page: 1 } } }),
  },
  {
    op: 'GET /api/v1/test-cases/{testCaseId}/results',
    status: 400,
    call: (c) =>
      c.GET('/api/v1/test-cases/{testCaseId}/results', { params: { path: tc, query: { pageSize: 500 } } }),
  },
  {
    op: 'GET /api/v1/test-cases/{testCaseId}/results',
    status: 404,
    call: (c) => c.GET('/api/v1/test-cases/{testCaseId}/results', { params: { path: unknownTc } }),
  },
  {
    op: 'GET /api/v1/test-cases/{testCaseId}/results',
    status: 500,
    setup: fail,
    call: (c) => c.GET('/api/v1/test-cases/{testCaseId}/results', { params: { path: tc } }),
  },
  // Steps
  {
    op: 'GET /api/v1/test-cases/{testCaseId}/steps',
    status: 200,
    call: (c) =>
      c.GET('/api/v1/test-cases/{testCaseId}/steps', { params: { path: tc, query: { pageSize: 100 } } }),
  },
  {
    op: 'GET /api/v1/test-cases/{testCaseId}/steps',
    status: 400,
    call: (c) => c.GET('/api/v1/test-cases/{testCaseId}/steps', { params: { path: tc, query: { page: 0 } } }),
  },
  {
    op: 'GET /api/v1/test-cases/{testCaseId}/steps',
    status: 404,
    call: (c) => c.GET('/api/v1/test-cases/{testCaseId}/steps', { params: { path: unknownTc } }),
  },
  {
    op: 'GET /api/v1/test-cases/{testCaseId}/steps',
    status: 500,
    setup: fail,
    call: (c) => c.GET('/api/v1/test-cases/{testCaseId}/steps', { params: { path: tc } }),
  },
  {
    op: 'POST /api/v1/test-cases/{testCaseId}/steps',
    status: 201,
    call: (c) =>
      c.POST('/api/v1/test-cases/{testCaseId}/steps', {
        params: { path: tc },
        body: { action: 'a', expectedResult: 'b' },
      }),
  },
  {
    op: 'POST /api/v1/test-cases/{testCaseId}/steps',
    status: 400,
    call: (c) =>
      c.POST('/api/v1/test-cases/{testCaseId}/steps', { params: { path: tc }, body: { action: ' ' } }),
  },
  {
    op: 'POST /api/v1/test-cases/{testCaseId}/steps',
    status: 404,
    call: (c) =>
      c.POST('/api/v1/test-cases/{testCaseId}/steps', { params: { path: unknownTc }, body: { action: 'a' } }),
  },
  {
    op: 'POST /api/v1/test-cases/{testCaseId}/steps',
    status: 500,
    setup: fail,
    call: (c) =>
      c.POST('/api/v1/test-cases/{testCaseId}/steps', { params: { path: tc }, body: { action: 'a' } }),
  },
  {
    op: 'PUT /api/v1/test-cases/{testCaseId}/steps/order',
    status: 200,
    call: (c) =>
      c.PUT('/api/v1/test-cases/{testCaseId}/steps/order', {
        params: { path: tc },
        body: { stepIds: [2, 1] },
      }),
  },
  {
    op: 'PUT /api/v1/test-cases/{testCaseId}/steps/order',
    status: 400,
    call: (c) =>
      c.PUT('/api/v1/test-cases/{testCaseId}/steps/order', { params: { path: tc }, body: { stepIds: [1] } }),
  },
  {
    op: 'PUT /api/v1/test-cases/{testCaseId}/steps/order',
    status: 404,
    call: (c) =>
      c.PUT('/api/v1/test-cases/{testCaseId}/steps/order', {
        params: { path: unknownTc },
        body: { stepIds: [] },
      }),
  },
  {
    op: 'PUT /api/v1/test-cases/{testCaseId}/steps/order',
    status: 500,
    setup: fail,
    call: (c) =>
      c.PUT('/api/v1/test-cases/{testCaseId}/steps/order', { params: { path: tc }, body: { stepIds: [] } }),
  },
  {
    op: 'PATCH /api/v1/test-cases/{testCaseId}/steps/{stepId}',
    status: 200,
    call: (c) =>
      c.PATCH('/api/v1/test-cases/{testCaseId}/steps/{stepId}', {
        params: { path: { ...tc, stepId: 1 } },
        body: { action: 'edited' },
      }),
  },
  {
    op: 'PATCH /api/v1/test-cases/{testCaseId}/steps/{stepId}',
    status: 400,
    call: (c) =>
      c.PATCH('/api/v1/test-cases/{testCaseId}/steps/{stepId}', {
        params: { path: { ...tc, stepId: 1 } },
        body: {},
      }),
  },
  {
    op: 'PATCH /api/v1/test-cases/{testCaseId}/steps/{stepId}',
    status: 404,
    call: (c) =>
      c.PATCH('/api/v1/test-cases/{testCaseId}/steps/{stepId}', {
        params: { path: { ...tc, stepId: 999 } },
        body: { action: 'x' },
      }),
  },
  {
    op: 'PATCH /api/v1/test-cases/{testCaseId}/steps/{stepId}',
    status: 500,
    setup: fail,
    call: (c) =>
      c.PATCH('/api/v1/test-cases/{testCaseId}/steps/{stepId}', {
        params: { path: { ...tc, stepId: 1 } },
        body: { action: 'x' },
      }),
  },
  {
    op: 'DELETE /api/v1/test-cases/{testCaseId}/steps/{stepId}',
    status: 204,
    call: (c) =>
      c.DELETE('/api/v1/test-cases/{testCaseId}/steps/{stepId}', { params: { path: { ...tc, stepId: 1 } } }),
  },
  {
    op: 'DELETE /api/v1/test-cases/{testCaseId}/steps/{stepId}',
    status: 400,
    call: (c) =>
      c.DELETE('/api/v1/test-cases/{testCaseId}/steps/{stepId}', { params: { path: { ...tc, stepId: 0 } } }),
  },
  {
    op: 'DELETE /api/v1/test-cases/{testCaseId}/steps/{stepId}',
    status: 404,
    call: (c) =>
      c.DELETE('/api/v1/test-cases/{testCaseId}/steps/{stepId}', {
        params: { path: { ...tc, stepId: 999 } },
      }),
  },
  {
    op: 'DELETE /api/v1/test-cases/{testCaseId}/steps/{stepId}',
    status: 500,
    setup: fail,
    call: (c) =>
      c.DELETE('/api/v1/test-cases/{testCaseId}/steps/{stepId}', { params: { path: { ...tc, stepId: 1 } } }),
  },
  // Parse errors
  {
    op: 'GET /api/v1/test-runs/{testRunId}/parse-errors',
    status: 200,
    setup: () => {
      db.parseErrors[7] = [
        { index: 0, testName: '', message: 'no name', persisted: false, severity: 'error' },
      ]
    },
    call: (c) =>
      c.GET('/api/v1/test-runs/{testRunId}/parse-errors', { params: { path: run, query: { page: 1 } } }),
  },
  {
    op: 'GET /api/v1/test-runs/{testRunId}/parse-errors',
    status: 400,
    call: (c) =>
      c.GET('/api/v1/test-runs/{testRunId}/parse-errors', { params: { path: run, query: { page: 0 } } }),
  },
  {
    op: 'GET /api/v1/test-runs/{testRunId}/parse-errors',
    status: 404,
    call: (c) => c.GET('/api/v1/test-runs/{testRunId}/parse-errors', { params: { path: unknownRun } }),
  },
  {
    op: 'GET /api/v1/test-runs/{testRunId}/parse-errors',
    status: 500,
    setup: fail,
    call: (c) => c.GET('/api/v1/test-runs/{testRunId}/parse-errors', { params: { path: run } }),
  },
  // Runs
  {
    op: 'GET /api/v1/test-runs',
    status: 200,
    call: (c) => c.GET('/api/v1/test-runs', { params: { query: { page: 1 } } }),
  },
  {
    op: 'GET /api/v1/test-runs',
    status: 400,
    call: (c) => c.GET('/api/v1/test-runs', { params: { query: { pageSize: 0 } } }),
  },
  { op: 'GET /api/v1/test-runs', status: 500, setup: fail, call: (c) => c.GET('/api/v1/test-runs') },
  {
    op: 'GET /api/v1/test-runs/{testRunId}',
    status: 200,
    call: (c) => c.GET('/api/v1/test-runs/{testRunId}', { params: { path: run } }),
  },
  {
    op: 'GET /api/v1/test-runs/{testRunId}',
    status: 400,
    call: (c) => c.GET('/api/v1/test-runs/{testRunId}', { params: { path: { testRunId: 0 } } }),
  },
  {
    op: 'GET /api/v1/test-runs/{testRunId}',
    status: 404,
    call: (c) => c.GET('/api/v1/test-runs/{testRunId}', { params: { path: unknownRun } }),
  },
  {
    op: 'GET /api/v1/test-runs/{testRunId}',
    status: 500,
    setup: fail,
    call: (c) => c.GET('/api/v1/test-runs/{testRunId}', { params: { path: run } }),
  },
  {
    op: 'GET /api/v1/test-runs/{testRunId}/results',
    status: 200,
    call: (c) =>
      c.GET('/api/v1/test-runs/{testRunId}/results', {
        params: { path: run, query: { status: 'failed', correlation: 'valid' } },
      }),
  },
  {
    op: 'GET /api/v1/test-runs/{testRunId}/results',
    status: 400,
    call: (c) =>
      c.GET('/api/v1/test-runs/{testRunId}/results', { params: { path: run, query: { page: 0 } } }),
  },
  {
    op: 'GET /api/v1/test-runs/{testRunId}/results',
    status: 404,
    call: (c) => c.GET('/api/v1/test-runs/{testRunId}/results', { params: { path: unknownRun } }),
  },
  {
    op: 'GET /api/v1/test-runs/{testRunId}/results',
    status: 500,
    setup: fail,
    call: (c) => c.GET('/api/v1/test-runs/{testRunId}/results', { params: { path: run } }),
  },
  {
    op: 'GET /api/v1/test-runs/{testRunId}/summary',
    status: 200,
    call: (c) => c.GET('/api/v1/test-runs/{testRunId}/summary', { params: { path: run } }),
  },
  {
    op: 'GET /api/v1/test-runs/{testRunId}/summary',
    status: 400,
    call: (c) => c.GET('/api/v1/test-runs/{testRunId}/summary', { params: { path: { testRunId: 0 } } }),
  },
  {
    op: 'GET /api/v1/test-runs/{testRunId}/summary',
    status: 404,
    call: (c) => c.GET('/api/v1/test-runs/{testRunId}/summary', { params: { path: unknownRun } }),
  },
  {
    op: 'GET /api/v1/test-runs/{testRunId}/summary',
    status: 500,
    setup: fail,
    call: (c) => c.GET('/api/v1/test-runs/{testRunId}/summary', { params: { path: run } }),
  },
]

const server = setupServer(...handlers)
const captured: Request[] = []
const responses: Response[] = []
server.events.on('request:start', ({ request }) => {
  captured.push(request.clone())
})
server.events.on('response:mocked', ({ response }) => {
  responses.push(response.clone())
})
const evidence = new Set<string>()
const client = createApiClient('http://localhost')
const ops = new Map(operations().map((o) => [o.key, o]))

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
beforeEach(() => {
  resetDb()
  captured.length = 0
  responses.length = 0
})
afterAll(() => {
  server.close()
  const out = path.resolve(import.meta.dirname, '../../coverage/contract')
  mkdirSync(out, { recursive: true })
  writeFileSync(
    path.join(out, 'evidence.json'),
    JSON.stringify(
      { side: 'frontend', consumed: consumedOperations(), validated: [...evidence].sort() },
      null,
      2,
    ),
  )
})

describe('frontend contract', () => {
  it('every consumed operation exists in the contract and every declared variant has a scenario', () => {
    const consumed = consumedOperations()
    expect(consumed.length).toBeGreaterThan(0)
    for (const key of consumed) {
      const op = ops.get(key)
      expect(op, `${key} is not in api/openapi.yaml`).toBeDefined()
      for (const status of declaredStatuses(op!)) {
        expect(
          scenarios.some((s) => s.op === key && s.status === status),
          `${key} -> ${status} has no scenario`,
        ).toBe(true)
      }
    }
  })

  it('the validators reject contract violations', async () => {
    const op = ops.get('GET /api/v1/test-cases/{testCaseId}')!
    const json = (body: unknown, status = 200, type = 'application/json') =>
      new Response(JSON.stringify(body), { status, headers: { 'Content-Type': type } })
    expect(await validateResponse(op, json({ id: 'x' }))).not.toEqual([])
    expect(await validateResponse(op, json({}, 418))).not.toEqual([])
    expect(await validateResponse(op, json({ code: 'not_found' }, 404, 'application/json'))).not.toEqual([])
    expect(await validateRequest(new Request('http://localhost/api/v1/nope'))).not.toEqual([])
    expect(await validateRequest(new Request('http://localhost/api/v1/test-cases?page=0&x=1'))).toHaveLength(
      2,
    )
    const badBody = new Request('http://localhost/api/v1/test-cases', {
      method: 'POST',
      body: JSON.stringify({ id: 1 }),
    })
    expect(await validateRequest(badBody)).not.toEqual([])
  })

  it.each(scenarios.map((s) => [`${s.op} -> ${s.status}`, s] as const))('%s', async (_, s) => {
    s.setup?.()
    const op = ops.get(s.op)!
    const { response } = await s.call(client)
    expect(response.status).toBe(s.status)
    expect(captured).toHaveLength(1)
    if (s.status < 300) expect(await validateRequest(captured[0])).toEqual([])
    expect(responses).toHaveLength(1)
    expect(await validateResponse(op, responses[0])).toEqual([])
    evidence.add(`${op.operation.operationId} ${s.status}`)
  })
})
