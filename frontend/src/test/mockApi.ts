import { http, HttpResponse, type JsonBodyType } from 'msw'

import type {
  ParseError,
  TestCase,
  TestCaseResult,
  TestResult,
  TestRun,
  TestRunSummary,
  TestStep,
} from '@/api/client'

import { summary, testCase, testResult, testRun, testStep } from './fixtures'

/**
 * In-memory implementation of the Provenly REST API for MSW. It is used by the
 * Integration tests (components against a mocked API) and its responses are
 * validated against api/openapi.yaml by the Contract tests.
 */
export interface MockDb {
  testCases: TestCase[]
  steps: TestStep[]
  runs: TestRun[]
  results: TestResult[]
  summaries: Record<number, TestRunSummary>
  parseErrors: Record<number, ParseError[]>
  /** When true every handler answers 500 (server failure). */
  failing: boolean
  nextId: number
}

export function seed(): MockDb {
  const tc = testCase()
  return {
    testCases: [tc, testCase({ id: 154, title: 'Logout works' })],
    steps: [
      testStep({ id: 1, position: 1 }),
      testStep({ id: 2, position: 2, action: 'Submit credentials', expectedResult: '' }),
    ],
    runs: [testRun()],
    results: [
      testResult({ id: 1 }),
      testResult({
        id: 2,
        testName: 'login firefox',
        status: 'failed',
        errorMessage: 'boom',
        errorDetails: 'trace',
      }),
      testResult({
        id: 3,
        testCaseId: null,
        requestedTestCaseId: null,
        correlation: 'missing',
        testName: 'no id',
      }),
    ],
    summaries: { 7: summary() },
    parseErrors: {},
    failing: false,
    nextId: 1000,
  }
}

export const db: MockDb = seed()

export function resetDb() {
  Object.assign(db, seed())
}

const problem = (
  status: number,
  code: string,
  detail: string,
  errors?: { field: string; message: string }[],
) =>
  HttpResponse.json(
    { type: 'about:blank', title: statusText[status], status, code, detail, ...(errors ? { errors } : {}) },
    { status, headers: { 'Content-Type': 'application/problem+json' } },
  )

const statusText: Record<number, string> = {
  400: 'Bad Request',
  404: 'Not Found',
  500: 'Internal Server Error',
}

const validation = (field: string, message: string) =>
  problem(400, 'validation_error', 'request validation failed', [{ field, message }])
const notFound = (what: string) => problem(404, 'not_found', `${what} not found`)
const now = () => new Date().toISOString()

function pageOf<T>(url: URL, items: T[]): JsonBodyType | Response {
  const page = Number(url.searchParams.get('page') ?? '1')
  const pageSize = Number(url.searchParams.get('pageSize') ?? '20')
  if (!Number.isInteger(page) || page < 1) return validation('page', 'must be an integer >= 1')
  if (!Number.isInteger(pageSize) || pageSize < 1 || pageSize > 100)
    return validation('pageSize', 'must be 1..100')
  const start = (page - 1) * pageSize
  return {
    page,
    pageSize,
    totalItems: items.length,
    totalPages: Math.ceil(items.length / pageSize),
    items: items.slice(start, start + pageSize) as JsonBodyType,
  } as JsonBodyType
}

function respond(body: JsonBodyType | Response, status = 200) {
  return body instanceof Response ? body : HttpResponse.json(body, { status })
}

function pathId(raw: string | readonly string[] | undefined): number | undefined {
  const n = Number(raw)
  return Number.isInteger(n) && n >= 1 ? n : undefined
}

const BASE = '*/api/v1'

type Handler = Parameters<typeof http.get>[1]

/** Wraps a handler with the shared failure switch. */
const guard =
  (fn: Handler): Handler =>
  (info) =>
    db.failing ? problem(500, 'internal_error', 'an unexpected error occurred') : fn(info)

function findCase(raw: string | readonly string[] | undefined): TestCase | Response {
  const id = pathId(raw)
  if (id === undefined) return validation('testCaseId', 'must be a positive integer')
  return db.testCases.find((t) => t.id === id) ?? notFound(`test case TC-${id}`)
}

function findRun(raw: string | readonly string[] | undefined): TestRun | Response {
  const id = pathId(raw)
  if (id === undefined) return validation('testRunId', 'must be a positive integer')
  return db.runs.find((r) => r.id === id) ?? notFound(`test run ${id}`)
}

async function readBody(request: Request): Promise<Record<string, unknown> | undefined> {
  try {
    const body = await request.json()
    return typeof body === 'object' && body !== null && !Array.isArray(body) ? body : undefined
  } catch {
    return undefined
  }
}

const stepsOf = (tcId: number) =>
  db.steps.filter((s) => s.testCaseId === tcId).sort((a, b) => a.position - b.position)

export const handlers = [
  http.get(
    `${BASE}/test-cases`,
    guard(({ request }) => {
      const url = new URL(request.url)
      const status = url.searchParams.get('status')
      if (status && status !== 'active' && status !== 'deprecated') return validation('status', 'invalid')
      const items = db.testCases.filter((t) => !status || t.status === status).sort((a, b) => b.id - a.id)
      return respond(pageOf(url, items))
    }),
  ),
  http.post(
    `${BASE}/test-cases`,
    guard(async ({ request }) => {
      const body = await readBody(request)
      const title = typeof body?.title === 'string' ? body.title.trim() : ''
      if (!body || 'id' in body) return validation('body', 'unknown field "id"')
      if (!title) return validation('title', 'is required')
      const tc = testCase({
        id: ++db.nextId,
        title,
        description: String(body.description ?? ''),
        expectedResult: String(body.expectedResult ?? ''),
        automated: Boolean(body.automated),
        createdAt: now(),
        updatedAt: now(),
      })
      db.testCases.push(tc)
      return respond(tc, 201)
    }),
  ),
  http.get(
    `${BASE}/test-cases/:testCaseId`,
    guard(({ params }) => respond(findCase(params.testCaseId) as JsonBodyType)),
  ),
  http.patch(
    `${BASE}/test-cases/:testCaseId`,
    guard(async ({ params, request }) => {
      const tc = findCase(params.testCaseId)
      if (tc instanceof Response) return tc
      const body = await readBody(request)
      if (!body || Object.keys(body).length === 0) return validation('body', 'at least one field is required')
      if (body.title !== undefined && !String(body.title).trim())
        return validation('title', 'must not be empty')
      Object.assign(tc, body, { updatedAt: now() })
      return respond(tc)
    }),
  ),
  http.post(
    `${BASE}/test-cases/:testCaseId/deprecate`,
    guard(({ params }) => {
      const tc = findCase(params.testCaseId)
      if (tc instanceof Response) return tc
      tc.status = 'deprecated'
      tc.deprecatedAt ??= now()
      return respond(tc)
    }),
  ),
  http.get(
    `${BASE}/test-cases/:testCaseId/steps`,
    guard(({ params, request }) => {
      const tc = findCase(params.testCaseId)
      if (tc instanceof Response) return tc
      return respond(pageOf(new URL(request.url), stepsOf(tc.id)))
    }),
  ),
  http.post(
    `${BASE}/test-cases/:testCaseId/steps`,
    guard(async ({ params, request }) => {
      const tc = findCase(params.testCaseId)
      if (tc instanceof Response) return tc
      const body = await readBody(request)
      const action = typeof body?.action === 'string' ? body.action.trim() : ''
      if (!action) return validation('action', 'must not be empty')
      const step = testStep({
        id: ++db.nextId,
        testCaseId: tc.id,
        position: stepsOf(tc.id).length + 1,
        action,
        expectedResult: String(body?.expectedResult ?? ''),
      })
      db.steps.push(step)
      return respond(step, 201)
    }),
  ),
  http.put(
    `${BASE}/test-cases/:testCaseId/steps/order`,
    guard(async ({ params, request }) => {
      const tc = findCase(params.testCaseId)
      if (tc instanceof Response) return tc
      const body = await readBody(request)
      const ids = Array.isArray(body?.stepIds) ? (body.stepIds as number[]) : []
      const current = stepsOf(tc.id)
      const valid = ids.length === current.length && current.every((s) => ids.includes(s.id))
      if (!valid) return validation('stepIds', 'must list every step of the test case exactly once')
      ids.forEach((id, i) => {
        const step = current.find((s) => s.id === id)!
        step.position = i + 1
      })
      return respond({ items: stepsOf(tc.id) })
    }),
  ),
  http.patch(
    `${BASE}/test-cases/:testCaseId/steps/:stepId`,
    guard(async ({ params, request }) => {
      const tc = findCase(params.testCaseId)
      if (tc instanceof Response) return tc
      const stepId = pathId(params.stepId)
      if (stepId === undefined) return validation('stepId', 'must be a positive integer')
      const body = await readBody(request)
      if (!body || Object.keys(body).length === 0) return validation('body', 'at least one field is required')
      if (body.action !== undefined && !String(body.action).trim())
        return validation('action', 'must not be empty')
      const step = stepsOf(tc.id).find((s) => s.id === stepId)
      if (!step) return notFound(`step ${stepId}`)
      Object.assign(step, body, { updatedAt: now() })
      return respond(step)
    }),
  ),
  http.delete(
    `${BASE}/test-cases/:testCaseId/steps/:stepId`,
    guard(({ params }) => {
      const tc = findCase(params.testCaseId)
      if (tc instanceof Response) return tc
      const stepId = pathId(params.stepId)
      if (stepId === undefined) return validation('stepId', 'must be a positive integer')
      const step = stepsOf(tc.id).find((s) => s.id === stepId)
      if (!step) return notFound(`step ${stepId}`)
      db.steps = db.steps.filter((s) => s.id !== stepId)
      stepsOf(tc.id).forEach((s, i) => (s.position = i + 1))
      return new HttpResponse(null, { status: 204 })
    }),
  ),
  http.get(
    `${BASE}/test-cases/:testCaseId/results`,
    guard(({ params, request }) => {
      const tc = findCase(params.testCaseId)
      if (tc instanceof Response) return tc
      const items: TestCaseResult[] = db.results
        .filter((r) => r.testCaseId === tc.id)
        .sort((a, b) => b.id - a.id)
        .map((result) => ({ result, run: db.runs.find((r) => r.id === result.testRunId)! }))
      return respond(pageOf(new URL(request.url), items))
    }),
  ),
  http.get(
    `${BASE}/test-runs`,
    guard(({ request }) =>
      respond(
        pageOf(
          new URL(request.url),
          [...db.runs].sort((a, b) => b.id - a.id),
        ),
      ),
    ),
  ),
  http.get(
    `${BASE}/test-runs/:testRunId`,
    guard(({ params }) => respond(findRun(params.testRunId) as JsonBodyType)),
  ),
  http.get(
    `${BASE}/test-runs/:testRunId/results`,
    guard(({ params, request }) => {
      const run = findRun(params.testRunId)
      if (run instanceof Response) return run
      const url = new URL(request.url)
      const status = url.searchParams.get('status')
      const correlation = url.searchParams.get('correlation')
      if (status && !['passed', 'failed', 'error', 'skipped'].includes(status))
        return validation('status', 'invalid')
      const items = db.results.filter(
        (r) =>
          r.testRunId === run.id &&
          (!status || r.status === status) &&
          (!correlation || r.correlation === correlation),
      )
      return respond(pageOf(url, items))
    }),
  ),
  http.get(
    `${BASE}/test-runs/:testRunId/summary`,
    guard(({ params }) => {
      const run = findRun(params.testRunId)
      if (run instanceof Response) return run
      return respond(db.summaries[run.id] ?? summary({ testRunId: run.id }))
    }),
  ),
  http.get(
    `${BASE}/test-runs/:testRunId/parse-errors`,
    guard(({ params, request }) => {
      const run = findRun(params.testRunId)
      if (run instanceof Response) return run
      return respond(pageOf(new URL(request.url), db.parseErrors[run.id] ?? []))
    }),
  ),
]
