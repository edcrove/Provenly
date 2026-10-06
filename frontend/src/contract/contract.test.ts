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
const textPlain = { 'Content-Type': 'text/plain' }
const fail = () => {
  db.failing = true
}

const chk = { projectKey: 'CHK' }
const addChk = () => {
  db.projects.push({ ...db.projects[0], id: 2, key: 'CHK', name: 'Checkout' })
}

const signedOut = () => {
  db.session = null
}
const asMember = () => {
  db.users.push({ ...db.users[0], id: 2, username: 'ana', isAdmin: false })
  db.session = 2
}
const pending = () => {
  db.invitations.push({
    id: 5,
    email: null,
    note: '',
    status: 'pending',
    createdBy: 1,
    createdAt: '2026-10-05T10:00:00Z',
    expiresAt: '2026-10-12T10:00:00Z',
    acceptedAt: null,
    acceptedUserId: null,
    revokedAt: null,
    projectId: null,
    projectRole: null,
  })
  db.invitationTokens['invite-5'] = 5
}
const accepted = () => {
  pending()
  db.invitations[0].status = 'accepted'
}
const inv = { invitationId: 5 }
const acceptBody = (username = 'ana', token = 'invite-5') => ({
  token,
  username,
  displayName: 'Ana',
  password: 'a long password',
})
const login = { username: 'admin', password: 'correct horse' }
const passwords = { currentPassword: 'correct horse', newPassword: 'a brand new password' }

/** Ana: a viewer in TC (project 1). */
const asViewer = () => {
  asMember()
  db.members.push({ projectId: 1, userId: 2, role: 'viewer', since: '2026-10-05T10:00:00Z' })
}
const addAna = () => {
  db.users.push({ ...db.users[0], id: 2, username: 'ana', isAdmin: false })
}
const tcKey = { projectKey: 'TC' }
const ana = { projectKey: 'TC', username: 'ana' }

const withKey = () => {
  db.apiKeys.push({
    id: 9,
    projectId: 1,
    name: 'ci',
    prefix: 'pvk_00000009',
    status: 'active',
    createdBy: 1,
    createdAt: '2026-10-05T10:00:00Z',
    lastUsedAt: null,
    revokedAt: null,
  })
}
const key9 = { projectKey: 'TC', apiKeyId: 9 }
const outside154 = () => {
  db.summaries[7] = {
    ...db.summaries[7],
    outsideUniverse: 1,
    outsideUniverseTestCaseIds: [154],
    testCases: db.summaries[7].testCases.filter((c) => c.testCaseId !== 154),
  }
}
const amendBody = { testCaseId: 154, reason: 'marked manual by mistake' }
const amendmentScenarios: Scenario[] = [
  {
    op: 'GET /api/v1/test-runs/{testRunId}/amendments',
    status: 200,
    call: (c) => c.GET('/api/v1/test-runs/{testRunId}/amendments', { params: { path: run } }),
  },
  {
    op: 'GET /api/v1/test-runs/{testRunId}/amendments',
    status: 400,
    call: (c) =>
      c.GET('/api/v1/test-runs/{testRunId}/amendments', { params: { path: run, query: { page: 0 } } }),
  },
  {
    op: 'GET /api/v1/test-runs/{testRunId}/amendments',
    status: 404,
    call: (c) => c.GET('/api/v1/test-runs/{testRunId}/amendments', { params: { path: unknownRun } }),
  },
  {
    op: 'GET /api/v1/test-runs/{testRunId}/amendments',
    status: 500,
    setup: fail,
    call: (c) => c.GET('/api/v1/test-runs/{testRunId}/amendments', { params: { path: run } }),
  },
  {
    op: 'POST /api/v1/test-runs/{testRunId}/amendments',
    status: 201,
    setup: outside154,
    call: (c) =>
      c.POST('/api/v1/test-runs/{testRunId}/amendments', { params: { path: run }, body: amendBody }),
  },
  {
    op: 'POST /api/v1/test-runs/{testRunId}/amendments',
    status: 400,
    setup: outside154,
    call: (c) =>
      c.POST('/api/v1/test-runs/{testRunId}/amendments', {
        params: { path: run },
        body: { ...amendBody, reason: ' ' },
      }),
  },
  {
    op: 'POST /api/v1/test-runs/{testRunId}/amendments',
    status: 403,
    setup: () => {
      outside154()
      asViewer()
    },
    call: (c) =>
      c.POST('/api/v1/test-runs/{testRunId}/amendments', { params: { path: run }, body: amendBody }),
  },
  {
    op: 'POST /api/v1/test-runs/{testRunId}/amendments',
    status: 404,
    call: (c) =>
      c.POST('/api/v1/test-runs/{testRunId}/amendments', { params: { path: unknownRun }, body: amendBody }),
  },
  {
    op: 'POST /api/v1/test-runs/{testRunId}/amendments',
    status: 409,
    setup: () => {
      outside154()
      db.summaries[7].amendedTestCaseIds = [154]
    },
    call: (c) =>
      c.POST('/api/v1/test-runs/{testRunId}/amendments', { params: { path: run }, body: amendBody }),
  },
  {
    op: 'POST /api/v1/test-runs/{testRunId}/amendments',
    status: 415,
    call: (c) =>
      c.POST('/api/v1/test-runs/{testRunId}/amendments', {
        params: { path: run },
        body: amendBody,
        bodySerializer: JSON.stringify,
        headers: textPlain,
      }),
  },
  {
    op: 'POST /api/v1/test-runs/{testRunId}/amendments',
    status: 500,
    setup: fail,
    call: (c) =>
      c.POST('/api/v1/test-runs/{testRunId}/amendments', { params: { path: run }, body: amendBody }),
  },
]

const staleTag = { 'If-Match': '"99"' }
const step1 = { testCaseId: 153, stepId: 1 }
const staleScenarios: Scenario[] = [
  {
    op: 'PATCH /api/v1/test-cases/{testCaseId}',
    status: 412,
    call: (c) =>
      c.PATCH('/api/v1/test-cases/{testCaseId}', {
        params: { path: tc },
        body: { title: 'x' },
        headers: staleTag,
      }),
  },
  {
    op: 'POST /api/v1/test-cases/{testCaseId}/deprecate',
    status: 412,
    call: (c) =>
      c.POST('/api/v1/test-cases/{testCaseId}/deprecate', { params: { path: tc }, headers: staleTag }),
  },
  {
    op: 'POST /api/v1/test-cases/{testCaseId}/reactivate',
    status: 412,
    call: (c) =>
      c.POST('/api/v1/test-cases/{testCaseId}/reactivate', { params: { path: tc }, headers: staleTag }),
  },
  {
    op: 'POST /api/v1/test-cases/{testCaseId}/steps',
    status: 412,
    call: (c) =>
      c.POST('/api/v1/test-cases/{testCaseId}/steps', {
        params: { path: tc },
        body: { action: 'a' },
        headers: staleTag,
      }),
  },
  {
    op: 'PUT /api/v1/test-cases/{testCaseId}/steps/order',
    status: 412,
    call: (c) =>
      c.PUT('/api/v1/test-cases/{testCaseId}/steps/order', {
        params: { path: tc },
        body: { stepIds: [2, 1] },
        headers: staleTag,
      }),
  },
  {
    op: 'PATCH /api/v1/test-cases/{testCaseId}/steps/{stepId}',
    status: 412,
    call: (c) =>
      c.PATCH('/api/v1/test-cases/{testCaseId}/steps/{stepId}', {
        params: { path: step1 },
        body: { action: 'x' },
        headers: staleTag,
      }),
  },
  {
    op: 'DELETE /api/v1/test-cases/{testCaseId}/steps/{stepId}',
    status: 412,
    call: (c) =>
      c.DELETE('/api/v1/test-cases/{testCaseId}/steps/{stepId}', {
        params: { path: step1 },
        headers: staleTag,
      }),
  },
]

const apiKeyScenarios: Scenario[] = [
  {
    op: 'GET /api/v1/projects/{projectKey}/api-keys',
    status: 200,
    setup: withKey,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/api-keys', { params: { path: tcKey } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/api-keys',
    status: 400,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/api-keys', { params: { path: { projectKey: 'tc' } } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/api-keys',
    status: 403,
    setup: asViewer,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/api-keys', { params: { path: tcKey } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/api-keys',
    status: 404,
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/api-keys', { params: { path: { projectKey: 'NOPE' } } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/api-keys',
    status: 500,
    setup: fail,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/api-keys', { params: { path: tcKey } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/api-keys',
    status: 201,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/api-keys', { params: { path: tcKey }, body: { name: 'ci' } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/api-keys',
    status: 400,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/api-keys', { params: { path: tcKey }, body: { name: ' ' } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/api-keys',
    status: 403,
    setup: asViewer,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/api-keys', { params: { path: tcKey }, body: { name: 'ci' } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/api-keys',
    status: 404,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/api-keys', {
        params: { path: { projectKey: 'NOPE' } },
        body: { name: 'ci' },
      }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/api-keys',
    status: 415,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/api-keys', {
        params: { path: tcKey },
        body: { name: 'ci' },
        bodySerializer: JSON.stringify,
        headers: textPlain,
      }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/api-keys',
    status: 500,
    setup: fail,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/api-keys', { params: { path: tcKey }, body: { name: 'ci' } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/api-keys/{apiKeyId}/revoke',
    status: 200,
    setup: withKey,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/api-keys/{apiKeyId}/revoke', { params: { path: key9 } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/api-keys/{apiKeyId}/revoke',
    status: 400,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/api-keys/{apiKeyId}/revoke', {
        params: { path: { projectKey: 'TC', apiKeyId: 0 } },
      }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/api-keys/{apiKeyId}/revoke',
    status: 403,
    setup: () => {
      withKey()
      asViewer()
    },
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/api-keys/{apiKeyId}/revoke', { params: { path: key9 } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/api-keys/{apiKeyId}/revoke',
    status: 404,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/api-keys/{apiKeyId}/revoke', { params: { path: key9 } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/api-keys/{apiKeyId}/revoke',
    status: 409,
    setup: () => {
      withKey()
      db.apiKeys[0] = { ...db.apiKeys[0], status: 'revoked', revokedAt: '2026-10-05T11:00:00Z' }
    },
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/api-keys/{apiKeyId}/revoke', { params: { path: key9 } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/api-keys/{apiKeyId}/revoke',
    status: 500,
    setup: fail,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/api-keys/{apiKeyId}/revoke', { params: { path: key9 } }),
  },
]

const roleScenarios: Scenario[] = [
  {
    op: 'POST /api/v1/projects',
    status: 403,
    setup: asViewer,
    call: (c) => c.POST('/api/v1/projects', { body: { key: 'WEB', name: 'w' } }),
  },
  {
    op: 'PATCH /api/v1/projects/{projectKey}',
    status: 403,
    setup: asViewer,
    call: (c) => c.PATCH('/api/v1/projects/{projectKey}', { params: { path: tcKey }, body: { name: 'x' } }),
  },
  {
    op: 'POST /api/v1/test-cases',
    status: 403,
    setup: asViewer,
    call: (c) => c.POST('/api/v1/test-cases', { body: { title: 'x' } }),
  },
  {
    op: 'PATCH /api/v1/test-cases/{testCaseId}',
    status: 403,
    setup: asViewer,
    call: (c) => c.PATCH('/api/v1/test-cases/{testCaseId}', { params: { path: tc }, body: { title: 'x' } }),
  },
  {
    op: 'POST /api/v1/test-cases/{testCaseId}/deprecate',
    status: 403,
    setup: asViewer,
    call: (c) => c.POST('/api/v1/test-cases/{testCaseId}/deprecate', { params: { path: tc } }),
  },
  {
    op: 'POST /api/v1/test-cases/{testCaseId}/reactivate',
    status: 403,
    setup: asViewer,
    call: (c) => c.POST('/api/v1/test-cases/{testCaseId}/reactivate', { params: { path: tc } }),
  },
  {
    op: 'POST /api/v1/test-cases/{testCaseId}/steps',
    status: 403,
    setup: asViewer,
    call: (c) =>
      c.POST('/api/v1/test-cases/{testCaseId}/steps', { params: { path: tc }, body: { action: 'a' } }),
  },
  {
    op: 'PUT /api/v1/test-cases/{testCaseId}/steps/order',
    status: 403,
    setup: asViewer,
    call: (c) =>
      c.PUT('/api/v1/test-cases/{testCaseId}/steps/order', {
        params: { path: tc },
        body: { stepIds: [1, 2] },
      }),
  },
  {
    op: 'PATCH /api/v1/test-cases/{testCaseId}/steps/{stepId}',
    status: 403,
    setup: asViewer,
    call: (c) =>
      c.PATCH('/api/v1/test-cases/{testCaseId}/steps/{stepId}', {
        params: { path: { ...tc, stepId: 1 } },
        body: { action: 'a' },
      }),
  },
  {
    op: 'DELETE /api/v1/test-cases/{testCaseId}/steps/{stepId}',
    status: 403,
    setup: asViewer,
    call: (c) =>
      c.DELETE('/api/v1/test-cases/{testCaseId}/steps/{stepId}', { params: { path: { ...tc, stepId: 1 } } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/members',
    status: 200,
    setup: asViewer,
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/members', { params: { path: tcKey, query: { page: 1 } } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/members',
    status: 400,
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/members', { params: { path: tcKey, query: { pageSize: 0 } } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/members',
    status: 404,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/members', { params: { path: chk } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/members',
    status: 500,
    setup: fail,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/members', { params: { path: tcKey } }),
  },
  ...(
    [
      [200, addAna, ana, { role: 'member' }],
      [400, addAna, ana, { role: 'owner' }],
      [403, asViewer, ana, { role: 'maintainer' }],
      [404, undefined, ana, { role: 'member' }],
      [
        409,
        () => {
          addAna()
          db.users[db.users.length - 1].deactivatedAt = '2026-10-06T10:00:00Z'
        },
        ana,
        { role: 'member' },
      ],
      [500, fail, ana, { role: 'member' }],
    ] as const
  ).map(([status, setup, path, body]): Scenario => ({
    op: 'PUT /api/v1/projects/{projectKey}/members/{username}',
    status,
    setup,
    call: (c) =>
      c.PUT('/api/v1/projects/{projectKey}/members/{username}', {
        params: { path },
        body: body as { role: 'member' },
      }),
  })),
  {
    op: 'PUT /api/v1/projects/{projectKey}/members/{username}',
    status: 415,
    call: (c) =>
      c.PUT('/api/v1/projects/{projectKey}/members/{username}', {
        params: { path: ana },
        body: { role: 'member' },
        headers: textPlain,
      }),
  },
  ...(
    [
      [204, asViewer, { projectKey: 'TC', username: 'ana' }, true],
      [400, undefined, { projectKey: 'tc', username: 'ana' }, false],
      [403, asViewer, ana, false],
      [404, undefined, ana, false],
      [500, fail, ana, false],
    ] as const
  ).map(([status, setup, path, asAdmin]): Scenario => ({
    op: 'DELETE /api/v1/projects/{projectKey}/members/{username}',
    status,
    setup: () => {
      setup?.()
      if (asAdmin) db.session = 1
    },
    call: (c) => c.DELETE('/api/v1/projects/{projectKey}/members/{username}', { params: { path } }),
  })),
  {
    op: 'POST /api/v1/invitations',
    status: 404,
    call: (c) => c.POST('/api/v1/invitations', { body: { project: 'NOPE', role: 'viewer' } }),
  },
]

const authScenarios: Scenario[] = [
  { op: 'POST /api/v1/auth/login', status: 200, call: (c) => c.POST('/api/v1/auth/login', { body: login }) },
  {
    op: 'POST /api/v1/auth/login',
    status: 400,
    call: (c) =>
      c.POST('/api/v1/auth/login', {
        body: { username: 'admin', password: 'x' },
        bodySerializer: () => '{"username":',
      }),
  },
  {
    op: 'POST /api/v1/auth/login',
    status: 429,
    setup: () => {
      db.loginFailures.admin = 5
    },
    call: (c) => c.POST('/api/v1/auth/login', { body: login }),
  },
  {
    op: 'POST /api/v1/auth/login',
    status: 401,
    call: (c) => c.POST('/api/v1/auth/login', { body: { ...login, password: 'wrong password' } }),
  },
  {
    op: 'POST /api/v1/auth/login',
    status: 415,
    call: (c) => c.POST('/api/v1/auth/login', { body: login, headers: textPlain }),
  },
  {
    op: 'POST /api/v1/auth/login',
    status: 500,
    setup: fail,
    call: (c) => c.POST('/api/v1/auth/login', { body: login }),
  },
  { op: 'POST /api/v1/auth/logout', status: 204, call: (c) => c.POST('/api/v1/auth/logout') },
  { op: 'GET /api/v1/auth/me', status: 200, call: (c) => c.GET('/api/v1/auth/me') },
  {
    op: 'GET /api/v1/auth/me',
    status: 500,
    setup: () => {
      db.failingMe = true
    },
    call: (c) => c.GET('/api/v1/auth/me'),
  },
  {
    op: 'POST /api/v1/auth/password',
    status: 200,
    call: (c) => c.POST('/api/v1/auth/password', { body: passwords }),
  },
  {
    op: 'POST /api/v1/auth/password',
    status: 400,
    call: (c) => c.POST('/api/v1/auth/password', { body: { ...passwords, currentPassword: 'wrong one!' } }),
  },
  {
    op: 'POST /api/v1/auth/password',
    status: 415,
    call: (c) => c.POST('/api/v1/auth/password', { body: passwords, headers: textPlain }),
  },
  {
    op: 'POST /api/v1/auth/password',
    status: 500,
    setup: fail,
    call: (c) => c.POST('/api/v1/auth/password', { body: passwords }),
  },
  {
    op: 'GET /api/v1/users',
    status: 200,
    call: (c) => c.GET('/api/v1/users', { params: { query: { page: 1 } } }),
  },
  {
    op: 'GET /api/v1/users',
    status: 400,
    call: (c) => c.GET('/api/v1/users', { params: { query: { page: 0 } } }),
  },
  { op: 'GET /api/v1/users', status: 403, setup: asMember, call: (c) => c.GET('/api/v1/users') },
  { op: 'GET /api/v1/users', status: 500, setup: fail, call: (c) => c.GET('/api/v1/users') },
  { op: 'GET /api/v1/invitations', status: 200, setup: pending, call: (c) => c.GET('/api/v1/invitations') },
  {
    op: 'GET /api/v1/invitations',
    status: 400,
    call: (c) => c.GET('/api/v1/invitations', { params: { query: { pageSize: 0 } } }),
  },
  { op: 'GET /api/v1/invitations', status: 403, setup: asMember, call: (c) => c.GET('/api/v1/invitations') },
  { op: 'GET /api/v1/invitations', status: 500, setup: fail, call: (c) => c.GET('/api/v1/invitations') },
  {
    op: 'POST /api/v1/invitations',
    status: 201,
    call: (c) => c.POST('/api/v1/invitations', { body: { email: 'bob@example.com', note: 'QA' } }),
  },
  {
    op: 'POST /api/v1/invitations',
    status: 400,
    call: (c) => c.POST('/api/v1/invitations', { body: { email: 'bob@example' } }),
  },
  {
    op: 'POST /api/v1/invitations',
    status: 403,
    setup: asMember,
    call: (c) => c.POST('/api/v1/invitations', { body: {} }),
  },
  {
    op: 'POST /api/v1/invitations',
    status: 415,
    call: (c) => c.POST('/api/v1/invitations', { body: {}, headers: textPlain }),
  },
  {
    op: 'POST /api/v1/invitations',
    status: 500,
    setup: fail,
    call: (c) => c.POST('/api/v1/invitations', { body: {} }),
  },
  ...(
    [
      [200, pending, inv],
      [400, undefined, { invitationId: 0 }],
      [403, asMember, inv],
      [404, undefined, inv],
      [409, accepted, inv],
      [500, fail, inv],
    ] as const
  ).map(([status, setup, path]): Scenario => ({
    op: 'POST /api/v1/invitations/{invitationId}/revoke',
    status,
    setup,
    call: (c) => c.POST('/api/v1/invitations/{invitationId}/revoke', { params: { path } }),
  })),
  ...(
    [
      [200, () => db.users.push({ ...db.users[0], id: 2, username: 'ana', isAdmin: false }), 'ana'],
      [401, signedOut, 'ana'],
      [403, asMember, 'admin'],
      [404, undefined, 'nobody'],
      [409, undefined, 'admin'],
      [500, fail, 'ana'],
    ] as const
  ).map(([status, setup, username]): Scenario => ({
    op: 'POST /api/v1/users/{username}/deactivate',
    status,
    setup,
    call: (c) => c.POST('/api/v1/users/{username}/deactivate', { params: { path: { username } } }),
  })),
  ...(
    [
      [200, undefined, 'admin'],
      [401, signedOut, 'admin'],
      [403, asMember, 'admin'],
      [404, undefined, 'nobody'],
      [500, fail, 'admin'],
    ] as const
  ).map(([status, setup, username]): Scenario => ({
    op: 'POST /api/v1/users/{username}/reactivate',
    status,
    setup,
    call: (c) => c.POST('/api/v1/users/{username}/reactivate', { params: { path: { username } } }),
  })),
  ...(
    [
      [201, undefined, 'admin'],
      [401, signedOut, 'admin'],
      [403, asMember, 'admin'],
      [404, undefined, 'nobody'],
      [
        409,
        () =>
          db.users.push({
            ...db.users[0],
            id: 2,
            username: 'ana',
            isAdmin: false,
            deactivatedAt: '2026-10-06T10:00:00Z',
          }),
        'ana',
      ],
      [500, fail, 'admin'],
    ] as const
  ).map(([status, setup, username]): Scenario => ({
    op: 'POST /api/v1/users/{username}/password-reset',
    status,
    setup,
    call: (c) => c.POST('/api/v1/users/{username}/password-reset', { params: { path: { username } } }),
  })),
  {
    op: 'POST /api/v1/password-reset',
    status: 200,
    setup: () => {
      db.resetTokens.tok = 'admin'
    },
    call: (c) => c.POST('/api/v1/password-reset', { body: { token: 'tok', password: 'a long password' } }),
  },
  {
    op: 'POST /api/v1/password-reset',
    status: 400,
    call: (c) => c.POST('/api/v1/password-reset', { body: { token: 'tok', password: 'short' } }),
  },
  {
    op: 'POST /api/v1/password-reset',
    status: 404,
    call: (c) => c.POST('/api/v1/password-reset', { body: { token: 'nope', password: 'a long password' } }),
  },
  {
    op: 'POST /api/v1/password-reset',
    status: 500,
    setup: fail,
    call: (c) => c.POST('/api/v1/password-reset', { body: { token: 'tok', password: 'a long password' } }),
  },
  {
    op: 'POST /api/v1/password-reset',
    status: 415,
    call: (c) =>
      c.POST('/api/v1/password-reset', { body: { token: 'nope', password: 'x' }, headers: textPlain }),
  },
  {
    op: 'POST /api/v1/invitations/accept',
    status: 201,
    setup: pending,
    call: (c) => c.POST('/api/v1/invitations/accept', { body: acceptBody() }),
  },
  {
    op: 'POST /api/v1/invitations/accept',
    status: 400,
    call: (c) => c.POST('/api/v1/invitations/accept', { body: acceptBody('x') }),
  },
  {
    op: 'POST /api/v1/invitations/accept',
    status: 404,
    call: (c) => c.POST('/api/v1/invitations/accept', { body: acceptBody('ana', 'nope') }),
  },
  {
    op: 'POST /api/v1/invitations/accept',
    status: 409,
    setup: pending,
    call: (c) => c.POST('/api/v1/invitations/accept', { body: acceptBody('admin') }),
  },
  {
    op: 'POST /api/v1/invitations/accept',
    status: 415,
    call: (c) => c.POST('/api/v1/invitations/accept', { body: acceptBody(), headers: textPlain }),
  },
  {
    op: 'POST /api/v1/invitations/accept',
    status: 500,
    setup: fail,
    call: (c) => c.POST('/api/v1/invitations/accept', { body: acceptBody() }),
  },
]

const risk = { projectKey: 'TC', dimensionKey: 'risk' }
const critical = { ...risk, valueKey: 'critical' }
const named = { key: 'browser', name: 'Browser' }

/** The usual failures of a maintainer-only taxonomy write: 403 for a viewer, 404, 415 and 500. */
function taxonomyWriteFailures(
  op: string,
  call: (c: ApiClient, headers?: Record<string, string>) => Promise<Result>,
) {
  return [
    { op, status: 403, setup: asViewer, call: (c: ApiClient) => call(c) },
    { op, status: 415, call: (c: ApiClient) => call(c, textPlain) },
    { op, status: 500, setup: fail, call: (c: ApiClient) => call(c) },
  ]
}

const taxonomyScenarios: Scenario[] = [
  {
    op: 'GET /api/v1/projects/{projectKey}/dimensions',
    status: 200,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/dimensions', { params: { path: tcKey } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/dimensions',
    status: 400,
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/dimensions', { params: { path: { projectKey: 'tc' } } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/dimensions',
    status: 404,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/dimensions', { params: { path: chk } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/dimensions',
    status: 500,
    setup: fail,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/dimensions', { params: { path: tcKey } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/dimensions',
    status: 201,
    call: (c) => c.POST('/api/v1/projects/{projectKey}/dimensions', { params: { path: tcKey }, body: named }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/dimensions',
    status: 400,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/dimensions', {
        params: { path: tcKey },
        body: { key: 'Browser', name: 'x' },
      }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/dimensions',
    status: 404,
    call: (c) => c.POST('/api/v1/projects/{projectKey}/dimensions', { params: { path: chk }, body: named }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/dimensions',
    status: 409,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/dimensions', {
        params: { path: tcKey },
        body: { key: 'risk', name: 'Risk' },
      }),
  },
  ...taxonomyWriteFailures('POST /api/v1/projects/{projectKey}/dimensions', (c, headers) =>
    c.POST('/api/v1/projects/{projectKey}/dimensions', { params: { path: tcKey }, body: named, headers }),
  ),
  {
    op: 'PATCH /api/v1/projects/{projectKey}/dimensions/{dimensionKey}',
    status: 200,
    call: (c) =>
      c.PATCH('/api/v1/projects/{projectKey}/dimensions/{dimensionKey}', {
        params: { path: risk },
        body: { name: 'Business risk', archived: true },
      }),
  },
  {
    op: 'PATCH /api/v1/projects/{projectKey}/dimensions/{dimensionKey}',
    status: 400,
    call: (c) =>
      c.PATCH('/api/v1/projects/{projectKey}/dimensions/{dimensionKey}', {
        params: { path: risk },
        body: {},
      }),
  },
  {
    op: 'PATCH /api/v1/projects/{projectKey}/dimensions/{dimensionKey}',
    status: 404,
    call: (c) =>
      c.PATCH('/api/v1/projects/{projectKey}/dimensions/{dimensionKey}', {
        params: { path: { ...risk, dimensionKey: 'browser' } },
        body: { archived: false },
      }),
  },
  ...taxonomyWriteFailures('PATCH /api/v1/projects/{projectKey}/dimensions/{dimensionKey}', (c, headers) =>
    c.PATCH('/api/v1/projects/{projectKey}/dimensions/{dimensionKey}', {
      params: { path: risk },
      body: { name: 'R' },
      headers,
    }),
  ),
  {
    op: 'POST /api/v1/projects/{projectKey}/dimensions/{dimensionKey}/values',
    status: 201,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/dimensions/{dimensionKey}/values', {
        params: { path: risk },
        body: { key: 'medium', name: 'Medium' },
      }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/dimensions/{dimensionKey}/values',
    status: 400,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/dimensions/{dimensionKey}/values', {
        params: { path: risk },
        body: { key: '-x', name: 'x' },
      }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/dimensions/{dimensionKey}/values',
    status: 404,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/dimensions/{dimensionKey}/values', {
        params: { path: { ...risk, dimensionKey: 'browser' } },
        body: { key: 'chrome', name: 'Chrome' },
      }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/dimensions/{dimensionKey}/values',
    status: 409,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/dimensions/{dimensionKey}/values', {
        params: { path: risk },
        body: { key: 'critical', name: 'Critical' },
      }),
  },
  ...taxonomyWriteFailures(
    'POST /api/v1/projects/{projectKey}/dimensions/{dimensionKey}/values',
    (c, headers) =>
      c.POST('/api/v1/projects/{projectKey}/dimensions/{dimensionKey}/values', {
        params: { path: risk },
        body: { key: 'medium', name: 'Medium' },
        headers,
      }),
  ),
  {
    op: 'PATCH /api/v1/projects/{projectKey}/dimensions/{dimensionKey}/values/{valueKey}',
    status: 200,
    call: (c) =>
      c.PATCH('/api/v1/projects/{projectKey}/dimensions/{dimensionKey}/values/{valueKey}', {
        params: { path: critical },
        body: { archived: true },
      }),
  },
  {
    op: 'PATCH /api/v1/projects/{projectKey}/dimensions/{dimensionKey}/values/{valueKey}',
    status: 400,
    call: (c) =>
      c.PATCH('/api/v1/projects/{projectKey}/dimensions/{dimensionKey}/values/{valueKey}', {
        params: { path: { ...critical, valueKey: 'Critical' } },
        body: { archived: true },
      }),
  },
  {
    op: 'PATCH /api/v1/projects/{projectKey}/dimensions/{dimensionKey}/values/{valueKey}',
    status: 404,
    call: (c) =>
      c.PATCH('/api/v1/projects/{projectKey}/dimensions/{dimensionKey}/values/{valueKey}', {
        params: { path: { ...critical, valueKey: 'blocker' } },
        body: { archived: true },
      }),
  },
  ...taxonomyWriteFailures(
    'PATCH /api/v1/projects/{projectKey}/dimensions/{dimensionKey}/values/{valueKey}',
    (c, headers) =>
      c.PATCH('/api/v1/projects/{projectKey}/dimensions/{dimensionKey}/values/{valueKey}', {
        params: { path: critical },
        body: { name: 'Blocker' },
        headers,
      }),
  ),
  // Tags and classification on test cases, and the list filters.
  {
    op: 'POST /api/v1/test-cases',
    status: 201,
    call: (c) =>
      c.POST('/api/v1/test-cases', {
        body: { title: 'Pay', tags: ['Smoke'], classification: { risk: 'critical' } },
      }),
  },
  {
    op: 'POST /api/v1/test-cases',
    status: 400,
    call: (c) => c.POST('/api/v1/test-cases', { body: { title: 'Pay', classification: { os: 'linux' } } }),
  },
  {
    op: 'PATCH /api/v1/test-cases/{testCaseId}',
    status: 200,
    call: (c) =>
      c.PATCH('/api/v1/test-cases/{testCaseId}', {
        params: { path: tc },
        body: { tags: ['api'], classification: { risk: 'high', feature: null } },
      }),
  },
  {
    op: 'PATCH /api/v1/test-cases/{testCaseId}',
    status: 400,
    call: (c) =>
      c.PATCH('/api/v1/test-cases/{testCaseId}', { params: { path: tc }, body: { tags: ['a b'] } }),
  },
  {
    op: 'GET /api/v1/test-cases',
    status: 200,
    call: (c) =>
      c.GET('/api/v1/test-cases', { params: { query: { tag: 'smoke', classification: 'risk:critical' } } }),
  },
  {
    op: 'GET /api/v1/test-cases',
    status: 400,
    call: (c) => c.GET('/api/v1/test-cases', { params: { query: { classification: 'risk' } } }),
  },
  {
    op: 'GET /api/v1/test-cases',
    status: 200,
    call: (c) => c.GET('/api/v1/test-cases', { params: { query: { key: 'TC-153' } } }),
  },
  {
    op: 'GET /api/v1/test-cases',
    status: 400,
    call: (c) => c.GET('/api/v1/test-cases', { params: { query: { key: 'tc-153' } } }),
  },
  // DEC-78: pickers search by key or title (?q=), and every catalog list is paged.
  {
    op: 'GET /api/v1/test-cases',
    status: 200,
    call: (c) => c.GET('/api/v1/test-cases', { params: { query: { project: 'TC', q: 'tc-153' } } }),
  },
  {
    op: 'GET /api/v1/test-cases',
    status: 400,
    call: (c) => c.GET('/api/v1/test-cases', { params: { query: { q: ' ' } } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/requirements',
    status: 400,
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/requirements', {
        params: { path: tcKey, query: { pageSize: 101 } },
      }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/issues',
    status: 400,
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/issues', { params: { path: tcKey, query: { page: 0 } } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/suites',
    status: 400,
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/suites', { params: { path: tcKey, query: { page: 0 } } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/dimensions',
    status: 400,
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/dimensions', { params: { path: tcKey, query: { pageSize: 0 } } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/webhooks',
    status: 400,
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/webhooks', { params: { path: tcKey, query: { page: 0 } } }),
  },
]

const smoke = { projectKey: 'TC', suiteKey: 'smoke' }
const release = { projectKey: 'TC', suiteKey: 'release' }
const withSuites = () => {
  db.suites.push(
    {
      projectId: 1,
      id: 900,
      members: [153],
      key: 'release',
      name: 'Release',
      description: '',
      kind: 'static',
      query: null,
      archivedAt: null,
      createdAt: '2026-10-05T10:00:00Z',
      updatedAt: '2026-10-05T10:00:00Z',
      caseCount: 0,
    },
    {
      projectId: 1,
      id: 901,
      members: [],
      key: 'smoke',
      name: 'Smoke',
      description: '',
      kind: 'query',
      query: { tag: 'smoke', classification: [] },
      archivedAt: null,
      createdAt: '2026-10-05T10:00:00Z',
      updatedAt: '2026-10-05T10:00:00Z',
      caseCount: 0,
    },
  )
}
const newSuite = { key: 'nightly', name: 'Nightly', kind: 'query' as const, query: { tag: 'nightly' } }

const suiteScenarios: Scenario[] = [
  {
    op: 'GET /api/v1/projects/{projectKey}/suites',
    status: 200,
    setup: withSuites,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/suites', { params: { path: tcKey } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/suites',
    status: 400,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/suites', { params: { path: { projectKey: 'tc' } } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/suites',
    status: 404,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/suites', { params: { path: chk } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/suites',
    status: 500,
    setup: fail,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/suites', { params: { path: tcKey } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/suites',
    status: 201,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/suites', {
        params: { path: tcKey },
        body: { key: 'release', name: 'Release', kind: 'static', testCaseIds: [153] },
      }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/suites',
    status: 400,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/suites', {
        params: { path: tcKey },
        body: { key: 'q', name: 'Q', kind: 'query', query: {} },
      }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/suites',
    status: 404,
    call: (c) => c.POST('/api/v1/projects/{projectKey}/suites', { params: { path: chk }, body: newSuite }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/suites',
    status: 409,
    setup: withSuites,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/suites', {
        params: { path: tcKey },
        body: { ...newSuite, key: 'smoke' },
      }),
  },
  ...taxonomyWriteFailures('POST /api/v1/projects/{projectKey}/suites', (c, headers) =>
    c.POST('/api/v1/projects/{projectKey}/suites', { params: { path: tcKey }, body: newSuite, headers }),
  ),
  {
    op: 'GET /api/v1/projects/{projectKey}/suites/{suiteKey}',
    status: 200,
    setup: withSuites,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/suites/{suiteKey}', { params: { path: release } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/suites/{suiteKey}',
    status: 400,
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/suites/{suiteKey}', {
        params: { path: { ...smoke, suiteKey: 'Bad' } },
      }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/suites/{suiteKey}',
    status: 404,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/suites/{suiteKey}', { params: { path: smoke } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/suites/{suiteKey}',
    status: 500,
    setup: fail,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/suites/{suiteKey}', { params: { path: smoke } }),
  },
  {
    op: 'PATCH /api/v1/projects/{projectKey}/suites/{suiteKey}',
    status: 200,
    setup: withSuites,
    call: (c) =>
      c.PATCH('/api/v1/projects/{projectKey}/suites/{suiteKey}', {
        params: { path: smoke },
        body: { archived: true, query: { tag: null, classification: ['risk:high'] } },
      }),
  },
  {
    op: 'PATCH /api/v1/projects/{projectKey}/suites/{suiteKey}',
    status: 400,
    setup: withSuites,
    call: (c) =>
      c.PATCH('/api/v1/projects/{projectKey}/suites/{suiteKey}', {
        params: { path: release },
        body: { query: { tag: 'x' } },
      }),
  },
  {
    op: 'PATCH /api/v1/projects/{projectKey}/suites/{suiteKey}',
    status: 404,
    call: (c) =>
      c.PATCH('/api/v1/projects/{projectKey}/suites/{suiteKey}', {
        params: { path: smoke },
        body: { name: 'x' },
      }),
  },
  ...taxonomyWriteFailures('PATCH /api/v1/projects/{projectKey}/suites/{suiteKey}', (c, headers) =>
    c.PATCH('/api/v1/projects/{projectKey}/suites/{suiteKey}', {
      params: { path: smoke },
      body: { name: 'S' },
      headers,
    }),
  ),
  {
    op: 'PUT /api/v1/projects/{projectKey}/suites/{suiteKey}/cases',
    status: 200,
    setup: withSuites,
    call: (c) =>
      c.PUT('/api/v1/projects/{projectKey}/suites/{suiteKey}/cases', {
        params: { path: release },
        body: { testCaseIds: [153, 154] },
      }),
  },
  {
    op: 'PUT /api/v1/projects/{projectKey}/suites/{suiteKey}/cases',
    status: 400,
    setup: withSuites,
    call: (c) =>
      c.PUT('/api/v1/projects/{projectKey}/suites/{suiteKey}/cases', {
        params: { path: release },
        body: { testCaseIds: [987654] },
      }),
  },
  {
    op: 'PUT /api/v1/projects/{projectKey}/suites/{suiteKey}/cases',
    status: 404,
    call: (c) =>
      c.PUT('/api/v1/projects/{projectKey}/suites/{suiteKey}/cases', {
        params: { path: release },
        body: { testCaseIds: [] },
      }),
  },
  ...taxonomyWriteFailures('PUT /api/v1/projects/{projectKey}/suites/{suiteKey}/cases', (c, headers) =>
    c.PUT('/api/v1/projects/{projectKey}/suites/{suiteKey}/cases', {
      params: { path: release },
      body: { testCaseIds: [] },
      headers,
    }),
  ),
  {
    op: 'GET /api/v1/test-cases',
    status: 200,
    setup: withSuites,
    call: (c) => c.GET('/api/v1/test-cases', { params: { query: { project: 'TC', suite: 'release' } } }),
  },
  {
    op: 'GET /api/v1/test-cases',
    status: 404,
    call: (c) => c.GET('/api/v1/test-cases', { params: { query: { project: 'TC', suite: 'nope' } } }),
  },
  {
    op: 'GET /api/v1/test-runs',
    status: 200,
    call: (c) => c.GET('/api/v1/test-runs', { params: { query: { suite: 'smoke' } } }),
  },
  {
    op: 'GET /api/v1/test-runs',
    status: 400,
    call: (c) => c.GET('/api/v1/test-runs', { params: { query: { suite: 'Smoke' } } }),
  },
]

const manualTcs = () => {
  db.testCases[1] = { ...db.testCases[1], automated: false }
}
const startManual = { project: 'TC', name: 'Release sign-off' }
/** A running manual run (id 900) expecting TC-153. */
const runningManual = () => {
  db.runs.push({ ...db.runs[0], id: 900, mode: 'manual', executionStatus: 'running', startedBy: 'admin' })
  db.summaries[900] = { ...db.summaries[7], testRunId: 900 }
}
const manualRun = { testRunId: 900 }

const manualScenarios: Scenario[] = [
  {
    op: 'POST /api/v1/test-runs/manual',
    status: 201,
    setup: manualTcs,
    call: (c) =>
      c.POST('/api/v1/test-runs/manual', { body: { ...startManual, scope: 'manual', branch: 'main' } }),
  },
  {
    op: 'POST /api/v1/test-runs/manual',
    status: 400,
    call: (c) => c.POST('/api/v1/test-runs/manual', { body: { project: 'TC', name: ' ' } }),
  },
  {
    op: 'POST /api/v1/test-runs/manual',
    status: 403,
    setup: asViewer,
    call: (c) => c.POST('/api/v1/test-runs/manual', { body: startManual }),
  },
  {
    op: 'POST /api/v1/test-runs/manual',
    status: 404,
    call: (c) => c.POST('/api/v1/test-runs/manual', { body: { ...startManual, project: 'NOPE' } }),
  },
  {
    op: 'POST /api/v1/test-runs/manual',
    status: 409,
    setup: () => {
      withSuites()
      db.suites[1].archivedAt = '2026-10-05T10:00:00Z'
    },
    call: (c) => c.POST('/api/v1/test-runs/manual', { body: { ...startManual, suite: 'smoke' } }),
  },
  {
    op: 'POST /api/v1/test-runs/manual',
    status: 415,
    call: (c) =>
      c.POST('/api/v1/test-runs/manual', {
        body: startManual,
        bodySerializer: JSON.stringify,
        headers: textPlain,
      }),
  },
  {
    op: 'POST /api/v1/test-runs/manual',
    status: 500,
    setup: fail,
    call: (c) => c.POST('/api/v1/test-runs/manual', { body: startManual }),
  },
  {
    op: 'POST /api/v1/test-runs/{testRunId}/manual-results',
    status: 201,
    setup: runningManual,
    call: (c) =>
      c.POST('/api/v1/test-runs/{testRunId}/manual-results', {
        params: { path: manualRun },
        body: { testCaseId: 153, status: 'failed', note: 'Button missing', failedStep: 2 },
      }),
  },
  {
    op: 'POST /api/v1/test-runs/{testRunId}/manual-results',
    status: 400,
    setup: runningManual,
    call: (c) =>
      c.POST('/api/v1/test-runs/{testRunId}/manual-results', {
        params: { path: manualRun },
        body: { testCaseId: 153, status: 'blocked' as 'error' },
      }),
  },
  {
    op: 'POST /api/v1/test-runs/{testRunId}/manual-results',
    status: 403,
    setup: () => {
      runningManual()
      asViewer()
    },
    call: (c) =>
      c.POST('/api/v1/test-runs/{testRunId}/manual-results', {
        params: { path: manualRun },
        body: { testCaseId: 153, status: 'passed' },
      }),
  },
  {
    op: 'POST /api/v1/test-runs/{testRunId}/manual-results',
    status: 404,
    call: (c) =>
      c.POST('/api/v1/test-runs/{testRunId}/manual-results', {
        params: { path: unknownRun },
        body: { testCaseId: 153, status: 'passed' },
      }),
  },
  {
    op: 'POST /api/v1/test-runs/{testRunId}/manual-results',
    status: 409,
    call: (c) =>
      c.POST('/api/v1/test-runs/{testRunId}/manual-results', {
        params: { path: run },
        body: { testCaseId: 153, status: 'passed' },
      }),
  },
  {
    op: 'POST /api/v1/test-runs/{testRunId}/manual-results',
    status: 415,
    setup: runningManual,
    call: (c) =>
      c.POST('/api/v1/test-runs/{testRunId}/manual-results', {
        params: { path: manualRun },
        body: { testCaseId: 153, status: 'passed' },
        bodySerializer: JSON.stringify,
        headers: textPlain,
      }),
  },
  {
    op: 'POST /api/v1/test-runs/{testRunId}/manual-results',
    status: 500,
    setup: fail,
    call: (c) =>
      c.POST('/api/v1/test-runs/{testRunId}/manual-results', {
        params: { path: manualRun },
        body: { testCaseId: 153, status: 'passed' },
      }),
  },
  {
    op: 'POST /api/v1/test-runs/{testRunId}/finish',
    status: 200,
    setup: runningManual,
    call: (c) =>
      c.POST('/api/v1/test-runs/{testRunId}/finish', {
        params: { path: manualRun },
        body: { status: 'completed' },
      }),
  },
  {
    op: 'POST /api/v1/test-runs/{testRunId}/finish',
    status: 400,
    setup: runningManual,
    call: (c) =>
      c.POST('/api/v1/test-runs/{testRunId}/finish', {
        params: { path: manualRun },
        body: { status: 'interrupted' as 'completed' },
      }),
  },
  {
    op: 'POST /api/v1/test-runs/{testRunId}/finish',
    status: 403,
    setup: () => {
      runningManual()
      asViewer()
    },
    call: (c) =>
      c.POST('/api/v1/test-runs/{testRunId}/finish', {
        params: { path: manualRun },
        body: { status: 'completed' },
      }),
  },
  {
    op: 'POST /api/v1/test-runs/{testRunId}/finish',
    status: 404,
    call: (c) =>
      c.POST('/api/v1/test-runs/{testRunId}/finish', {
        params: { path: unknownRun },
        body: { status: 'completed' },
      }),
  },
  {
    op: 'POST /api/v1/test-runs/{testRunId}/finish',
    status: 409,
    call: (c) =>
      c.POST('/api/v1/test-runs/{testRunId}/finish', {
        params: { path: run },
        body: { status: 'cancelled' },
      }),
  },
  {
    op: 'POST /api/v1/test-runs/{testRunId}/finish',
    status: 415,
    setup: runningManual,
    call: (c) =>
      c.POST('/api/v1/test-runs/{testRunId}/finish', {
        params: { path: manualRun },
        body: { status: 'completed' },
        bodySerializer: JSON.stringify,
        headers: textPlain,
      }),
  },
  {
    op: 'POST /api/v1/test-runs/{testRunId}/finish',
    status: 500,
    setup: fail,
    call: (c) =>
      c.POST('/api/v1/test-runs/{testRunId}/finish', {
        params: { path: manualRun },
        body: { status: 'completed' },
      }),
  },
]

const withRequirement = () => {
  db.requirements.push({
    projectId: 1,
    id: 950,
    provider: 'jira',
    externalId: 'PAY-12',
    title: 'Refunds',
    description: '',
    url: 'https://jira.test/PAY-12',
    providerStatus: 'In Progress',
    archivedAt: null,
    lastSyncedAt: '2026-10-05T10:00:00Z',
    createdAt: '2026-10-05T10:00:00Z',
    updatedAt: '2026-10-05T10:00:00Z',
    testCaseIds: [153],
    coverage: { status: 'uncovered', linked: 0, passed: 0, failed: 0, notRun: 0, testCases: [] },
  })
  db.latest[153] = 'failed'
}
const req950 = { projectKey: 'TC', requirementId: 950 }
const unknownReq = { projectKey: 'TC', requirementId: 987654 }

const requirementScenarios: Scenario[] = [
  {
    op: 'GET /api/v1/projects/{projectKey}/requirements',
    status: 200,
    setup: withRequirement,
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/requirements', {
        params: { path: tcKey, query: { testCase: 153 } },
      }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/requirements',
    status: 400,
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/requirements', {
        params: { path: tcKey, query: { testCase: 0 } },
      }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/requirements',
    status: 404,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/requirements', { params: { path: chk } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/requirements',
    status: 500,
    setup: fail,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/requirements', { params: { path: tcKey } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/requirements',
    status: 201,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/requirements', {
        params: { path: tcKey },
        body: { title: 'Pay by card', url: 'https://x.test' },
      }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/requirements',
    status: 400,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/requirements', { params: { path: tcKey }, body: { title: ' ' } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/requirements',
    status: 403,
    setup: asViewer,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/requirements', { params: { path: tcKey }, body: { title: 'x' } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/requirements',
    status: 404,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/requirements', { params: { path: chk }, body: { title: 'x' } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/requirements',
    status: 409,
    setup: withRequirement,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/requirements', {
        params: { path: tcKey },
        body: { title: 'x', provider: 'jira', externalId: 'PAY-12' },
      }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/requirements',
    status: 415,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/requirements', {
        params: { path: tcKey },
        body: { title: 'x' },
        headers: textPlain,
      }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/requirements',
    status: 500,
    setup: fail,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/requirements', { params: { path: tcKey }, body: { title: 'x' } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/requirements/{requirementId}',
    status: 200,
    setup: withRequirement,
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/requirements/{requirementId}', { params: { path: req950 } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/requirements/{requirementId}',
    status: 400,
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/requirements/{requirementId}', {
        params: { path: { projectKey: 'TC', requirementId: 0 } },
      }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/requirements/{requirementId}',
    status: 404,
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/requirements/{requirementId}', { params: { path: unknownReq } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/requirements/{requirementId}',
    status: 500,
    setup: fail,
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/requirements/{requirementId}', { params: { path: req950 } }),
  },
  {
    op: 'PATCH /api/v1/projects/{projectKey}/requirements/{requirementId}',
    status: 200,
    setup: withRequirement,
    call: (c) =>
      c.PATCH('/api/v1/projects/{projectKey}/requirements/{requirementId}', {
        params: { path: req950 },
        body: { archived: true },
      }),
  },
  {
    op: 'PATCH /api/v1/projects/{projectKey}/requirements/{requirementId}',
    status: 400,
    setup: withRequirement,
    call: (c) =>
      c.PATCH('/api/v1/projects/{projectKey}/requirements/{requirementId}', {
        params: { path: req950 },
        body: {},
      }),
  },
  {
    op: 'PATCH /api/v1/projects/{projectKey}/requirements/{requirementId}',
    status: 404,
    call: (c) =>
      c.PATCH('/api/v1/projects/{projectKey}/requirements/{requirementId}', {
        params: { path: unknownReq },
        body: { title: 'x' },
      }),
  },
  ...taxonomyWriteFailures('PATCH /api/v1/projects/{projectKey}/requirements/{requirementId}', (c, headers) =>
    c.PATCH('/api/v1/projects/{projectKey}/requirements/{requirementId}', {
      params: { path: req950 },
      body: { title: 'x' },
      headers,
    }),
  ).map((sc) => ({ ...sc, setup: sc.setup ? () => (withRequirement(), sc.setup!()) : withRequirement })),
  {
    op: 'PUT /api/v1/projects/{projectKey}/requirements/{requirementId}/test-cases',
    status: 200,
    setup: withRequirement,
    call: (c) =>
      c.PUT('/api/v1/projects/{projectKey}/requirements/{requirementId}/test-cases', {
        params: { path: req950 },
        body: { testCaseIds: [153, 154] },
      }),
  },
  {
    op: 'PUT /api/v1/projects/{projectKey}/requirements/{requirementId}/test-cases',
    status: 400,
    setup: withRequirement,
    call: (c) =>
      c.PUT('/api/v1/projects/{projectKey}/requirements/{requirementId}/test-cases', {
        params: { path: req950 },
        body: { testCaseIds: [987654] },
      }),
  },
  {
    op: 'PUT /api/v1/projects/{projectKey}/requirements/{requirementId}/test-cases',
    status: 404,
    call: (c) =>
      c.PUT('/api/v1/projects/{projectKey}/requirements/{requirementId}/test-cases', {
        params: { path: unknownReq },
        body: { testCaseIds: [] },
      }),
  },
  ...taxonomyWriteFailures(
    'PUT /api/v1/projects/{projectKey}/requirements/{requirementId}/test-cases',
    (c, headers) =>
      c.PUT('/api/v1/projects/{projectKey}/requirements/{requirementId}/test-cases', {
        params: { path: req950 },
        body: { testCaseIds: [] },
        headers,
      }),
  ).map((sc) => ({ ...sc, setup: sc.setup ? () => (withRequirement(), sc.setup!()) : withRequirement })),
]

const withIssue = () => {
  db.issues.push({
    projectId: 1,
    id: 960,
    provider: 'jira',
    externalId: 'PAY-12',
    title: 'Refund twice',
    description: '',
    url: 'https://jira.test/PAY-12',
    state: 'open',
    providerStatus: 'In Progress',
    closedAt: null,
    lastSyncedAt: '2026-10-05T10:00:00Z',
    createdAt: '2026-10-05T10:00:00Z',
    updatedAt: '2026-10-05T10:00:00Z',
    testCaseIds: [153],
    verification: { status: 'unlinked', testCases: [] },
  })
  db.latest[153] = 'failed'
}
const issue960 = { projectKey: 'TC', issueId: 960 }
const unknownIssue = { projectKey: 'TC', issueId: 987654 }

const issueScenarios: Scenario[] = [
  {
    op: 'GET /api/v1/projects/{projectKey}/issues',
    status: 200,
    setup: withIssue,
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/issues', {
        params: { path: tcKey, query: { testCase: 153 } },
      }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/issues',
    status: 400,
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/issues', {
        params: { path: tcKey, query: { testCase: 0 } },
      }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/issues',
    status: 404,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/issues', { params: { path: chk } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/issues',
    status: 500,
    setup: fail,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/issues', { params: { path: tcKey } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/issues',
    status: 201,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/issues', {
        params: { path: tcKey },
        body: { title: 'Crash', url: 'https://x.test' },
      }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/issues',
    status: 400,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/issues', { params: { path: tcKey }, body: { title: ' ' } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/issues',
    status: 403,
    setup: asViewer,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/issues', { params: { path: tcKey }, body: { title: 'x' } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/issues',
    status: 404,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/issues', { params: { path: chk }, body: { title: 'x' } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/issues',
    status: 409,
    setup: withIssue,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/issues', {
        params: { path: tcKey },
        body: { title: 'x', provider: 'jira', externalId: 'PAY-12' },
      }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/issues',
    status: 415,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/issues', {
        params: { path: tcKey },
        body: { title: 'x' },
        headers: textPlain,
      }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/issues',
    status: 500,
    setup: fail,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/issues', { params: { path: tcKey }, body: { title: 'x' } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/issues/{issueId}',
    status: 200,
    setup: withIssue,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/issues/{issueId}', { params: { path: issue960 } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/issues/{issueId}',
    status: 400,
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/issues/{issueId}', {
        params: { path: { projectKey: 'TC', issueId: 0 } },
      }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/issues/{issueId}',
    status: 404,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/issues/{issueId}', { params: { path: unknownIssue } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/issues/{issueId}',
    status: 500,
    setup: fail,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/issues/{issueId}', { params: { path: issue960 } }),
  },
  {
    op: 'PATCH /api/v1/projects/{projectKey}/issues/{issueId}',
    status: 200,
    setup: withIssue,
    call: (c) =>
      c.PATCH('/api/v1/projects/{projectKey}/issues/{issueId}', {
        params: { path: issue960 },
        body: { state: 'closed' },
      }),
  },
  {
    op: 'PATCH /api/v1/projects/{projectKey}/issues/{issueId}',
    status: 400,
    setup: withIssue,
    call: (c) =>
      c.PATCH('/api/v1/projects/{projectKey}/issues/{issueId}', {
        params: { path: issue960 },
        body: {},
      }),
  },
  {
    op: 'PATCH /api/v1/projects/{projectKey}/issues/{issueId}',
    status: 404,
    call: (c) =>
      c.PATCH('/api/v1/projects/{projectKey}/issues/{issueId}', {
        params: { path: unknownIssue },
        body: { title: 'x' },
      }),
  },
  ...taxonomyWriteFailures('PATCH /api/v1/projects/{projectKey}/issues/{issueId}', (c, headers) =>
    c.PATCH('/api/v1/projects/{projectKey}/issues/{issueId}', {
      params: { path: issue960 },
      body: { title: 'x' },
      headers,
    }),
  ).map((sc) => ({ ...sc, setup: sc.setup ? () => (withIssue(), sc.setup!()) : withIssue })),
  {
    op: 'PUT /api/v1/projects/{projectKey}/issues/{issueId}/test-cases',
    status: 200,
    setup: withIssue,
    call: (c) =>
      c.PUT('/api/v1/projects/{projectKey}/issues/{issueId}/test-cases', {
        params: { path: issue960 },
        body: { testCaseIds: [153, 154] },
      }),
  },
  {
    op: 'PUT /api/v1/projects/{projectKey}/issues/{issueId}/test-cases',
    status: 400,
    setup: withIssue,
    call: (c) =>
      c.PUT('/api/v1/projects/{projectKey}/issues/{issueId}/test-cases', {
        params: { path: issue960 },
        body: { testCaseIds: [987654] },
      }),
  },
  {
    op: 'PUT /api/v1/projects/{projectKey}/issues/{issueId}/test-cases',
    status: 404,
    call: (c) =>
      c.PUT('/api/v1/projects/{projectKey}/issues/{issueId}/test-cases', {
        params: { path: unknownIssue },
        body: { testCaseIds: [] },
      }),
  },
  ...taxonomyWriteFailures('PUT /api/v1/projects/{projectKey}/issues/{issueId}/test-cases', (c, headers) =>
    c.PUT('/api/v1/projects/{projectKey}/issues/{issueId}/test-cases', {
      params: { path: issue960 },
      body: { testCaseIds: [] },
      headers,
    }),
  ).map((sc) => ({ ...sc, setup: sc.setup ? () => (withIssue(), sc.setup!()) : withIssue })),
]

const qualityScenarios: Scenario[] = [
  {
    op: 'GET /api/v1/projects/{projectKey}/quality',
    status: 200,
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/quality', {
        params: { path: tcKey, query: { staleDays: 30, window: 50 } },
      }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/quality',
    status: 400,
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/quality', { params: { path: tcKey, query: { staleDays: 366 } } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/quality',
    status: 404,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/quality', { params: { path: chk } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/quality',
    status: 500,
    setup: fail,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/quality', { params: { path: tcKey } }),
  },
]

const liveRun7 = { testRunId: 7 }
const withLive = () => {
  db.runs[0] = { ...db.runs[0], mode: 'live', executionStatus: 'running' }
  db.live[7] = {
    reconciliation: 'pending',
    events: 1,
    lastSequence: 1,
    runFinished: false,
    waiting: 1,
    running: 1,
    finished: 0,
    testCases: [
      { testCaseId: 153, testCaseKey: 'TC-153', state: 'running' },
      { testCaseId: 154, testCaseKey: 'TC-154', state: 'waiting' },
    ],
    mismatches: [],
  }
}
const liveScenarios: Scenario[] = [
  {
    op: 'GET /api/v1/test-runs/{testRunId}/live',
    status: 200,
    setup: withLive,
    call: (c) => c.GET('/api/v1/test-runs/{testRunId}/live', { params: { path: liveRun7 } }),
  },
  {
    op: 'GET /api/v1/test-runs/{testRunId}/live',
    status: 400,
    call: (c) => c.GET('/api/v1/test-runs/{testRunId}/live', { params: { path: { testRunId: 0 } } }),
  },
  {
    op: 'GET /api/v1/test-runs/{testRunId}/live',
    status: 404,
    call: (c) => c.GET('/api/v1/test-runs/{testRunId}/live', { params: { path: { testRunId: 987654 } } }),
  },
  {
    op: 'GET /api/v1/test-runs/{testRunId}/live',
    status: 409,
    call: (c) => c.GET('/api/v1/test-runs/{testRunId}/live', { params: { path: liveRun7 } }),
  },
  {
    op: 'GET /api/v1/test-runs/{testRunId}/live',
    status: 500,
    setup: fail,
    call: (c) => c.GET('/api/v1/test-runs/{testRunId}/live', { params: { path: liveRun7 } }),
  },
]

type Method = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
type AnyCall = (path: string, init: object) => Promise<Result>
const anyPath = {
  testCaseId: 153,
  testRunId: 7,
  stepId: 1,
  projectKey: 'TC',
  invitationId: 1,
  dimensionKey: 'risk',
  valueKey: 'critical',
  suiteKey: 'smoke',
  requirementId: 1,
  issueId: 1,
  webhookId: 1,
}

/** Without a session every operation that declares 401 answers it (sign-in's own 401 is a wrong password). */
const signedOutScenarios = (): Scenario[] =>
  operations()
    .filter(
      (op) =>
        consumedOperations().includes(op.key) &&
        declaredStatuses(op).includes(401) &&
        op.key !== 'POST /api/v1/auth/login',
    )
    .map((op) => {
      const [method, path] = op.key.split(' ') as [Method, string]
      return {
        op: op.key,
        status: 401,
        setup: signedOut,
        call: (c) =>
          (c[method] as AnyCall)(path, {
            params: { path: anyPath },
            ...(method === 'GET' || method === 'DELETE' ? {} : { body: {} }),
          }),
      }
    })

const withWebhook = () => {
  db.webhooks.push({
    id: 31,
    projectId: 1,
    url: 'https://hooks.example.com/p',
    events: ['run.completed'],
    active: true,
    createdBy: 'admin',
    createdAt: '2026-10-05T10:00:00Z',
    updatedAt: '2026-10-05T10:00:00Z',
  })
}
const hook31 = { projectKey: 'TC', webhookId: 31 }
const hook404 = { projectKey: 'TC', webhookId: 987654 }
const nope = { projectKey: 'NOPE' }
const badKey = { projectKey: 'tc' }
const hookBody = { url: 'https://hooks.example.com/p', events: ['run.completed' as const] }
const withGitHub =
  (repository = 'acme/shop', tokenHint = '…1234') =>
  () => {
    db.github.push({
      projectId: 1,
      repository,
      labels: '',
      tokenHint,
      lastSyncedAt: null,
      lastError: null,
      updatedAt: '2026-10-05T10:00:00Z',
    })
  }
const ghBody = { repository: 'acme/shop', token: 'ghp_x' }
const integrationScenarios: Scenario[] = [
  // A malformed project key is a 400 before any lookup.
  {
    op: 'GET /api/v1/projects/{projectKey}/webhooks',
    status: 400,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/webhooks', { params: { path: badKey } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/github',
    status: 400,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/github', { params: { path: badKey } }),
  },
  {
    op: 'DELETE /api/v1/projects/{projectKey}/github',
    status: 400,
    call: (c) => c.DELETE('/api/v1/projects/{projectKey}/github', { params: { path: badKey } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/github/sync',
    status: 400,
    call: (c) => c.POST('/api/v1/projects/{projectKey}/github/sync', { params: { path: badKey } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/webhooks',
    status: 200,
    setup: withWebhook,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/webhooks', { params: { path: tcKey } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/webhooks',
    status: 403,
    setup: asViewer,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/webhooks', { params: { path: tcKey } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/webhooks',
    status: 404,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/webhooks', { params: { path: nope } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/webhooks',
    status: 500,
    setup: fail,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/webhooks', { params: { path: tcKey } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/webhooks',
    status: 201,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/webhooks', { params: { path: tcKey }, body: hookBody }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/webhooks',
    status: 400,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/webhooks', {
        params: { path: tcKey },
        body: { ...hookBody, url: 'nope' },
      }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/webhooks',
    status: 403,
    setup: asViewer,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/webhooks', { params: { path: tcKey }, body: hookBody }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/webhooks',
    status: 404,
    call: (c) => c.POST('/api/v1/projects/{projectKey}/webhooks', { params: { path: nope }, body: hookBody }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/webhooks',
    status: 415,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/webhooks', {
        params: { path: tcKey },
        body: hookBody,
        bodySerializer: JSON.stringify,
        headers: textPlain,
      }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/webhooks',
    status: 500,
    setup: fail,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/webhooks', { params: { path: tcKey }, body: hookBody }),
  },
  {
    op: 'PATCH /api/v1/projects/{projectKey}/webhooks/{webhookId}',
    status: 200,
    setup: withWebhook,
    call: (c) =>
      c.PATCH('/api/v1/projects/{projectKey}/webhooks/{webhookId}', {
        params: { path: hook31 },
        body: { active: false },
      }),
  },
  {
    op: 'PATCH /api/v1/projects/{projectKey}/webhooks/{webhookId}',
    status: 400,
    setup: withWebhook,
    call: (c) =>
      c.PATCH('/api/v1/projects/{projectKey}/webhooks/{webhookId}', { params: { path: hook31 }, body: {} }),
  },
  {
    op: 'PATCH /api/v1/projects/{projectKey}/webhooks/{webhookId}',
    status: 403,
    setup: () => {
      withWebhook()
      asViewer()
    },
    call: (c) =>
      c.PATCH('/api/v1/projects/{projectKey}/webhooks/{webhookId}', {
        params: { path: hook31 },
        body: { active: false },
      }),
  },
  {
    op: 'PATCH /api/v1/projects/{projectKey}/webhooks/{webhookId}',
    status: 404,
    call: (c) =>
      c.PATCH('/api/v1/projects/{projectKey}/webhooks/{webhookId}', {
        params: { path: hook404 },
        body: { active: false },
      }),
  },
  {
    op: 'PATCH /api/v1/projects/{projectKey}/webhooks/{webhookId}',
    status: 415,
    setup: withWebhook,
    call: (c) =>
      c.PATCH('/api/v1/projects/{projectKey}/webhooks/{webhookId}', {
        params: { path: hook31 },
        body: { active: false },
        bodySerializer: JSON.stringify,
        headers: textPlain,
      }),
  },
  {
    op: 'PATCH /api/v1/projects/{projectKey}/webhooks/{webhookId}',
    status: 500,
    setup: fail,
    call: (c) =>
      c.PATCH('/api/v1/projects/{projectKey}/webhooks/{webhookId}', {
        params: { path: hook31 },
        body: { active: false },
      }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/webhooks/{webhookId}/ping',
    status: 202,
    setup: withWebhook,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/webhooks/{webhookId}/ping', { params: { path: hook31 } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/webhooks/{webhookId}/ping',
    status: 400,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/webhooks/{webhookId}/ping', {
        params: { path: { projectKey: 'TC', webhookId: 0 } },
      }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/webhooks/{webhookId}/ping',
    status: 403,
    setup: () => {
      withWebhook()
      asViewer()
    },
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/webhooks/{webhookId}/ping', { params: { path: hook31 } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/webhooks/{webhookId}/ping',
    status: 404,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/webhooks/{webhookId}/ping', { params: { path: hook404 } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/webhooks/{webhookId}/ping',
    status: 500,
    setup: fail,
    call: (c) =>
      c.POST('/api/v1/projects/{projectKey}/webhooks/{webhookId}/ping', { params: { path: hook31 } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/webhooks/{webhookId}/deliveries',
    status: 200,
    setup: withWebhook,
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/webhooks/{webhookId}/deliveries', {
        params: { path: hook31, query: { page: 1 } },
      }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/webhooks/{webhookId}/deliveries',
    status: 400,
    setup: withWebhook,
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/webhooks/{webhookId}/deliveries', {
        params: { path: hook31, query: { page: 0 } },
      }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/webhooks/{webhookId}/deliveries',
    status: 403,
    setup: () => {
      withWebhook()
      asViewer()
    },
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/webhooks/{webhookId}/deliveries', { params: { path: hook31 } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/webhooks/{webhookId}/deliveries',
    status: 404,
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/webhooks/{webhookId}/deliveries', { params: { path: hook404 } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/webhooks/{webhookId}/deliveries',
    status: 500,
    setup: fail,
    call: (c) =>
      c.GET('/api/v1/projects/{projectKey}/webhooks/{webhookId}/deliveries', { params: { path: hook31 } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/github',
    status: 200,
    setup: withGitHub(),
    call: (c) => c.GET('/api/v1/projects/{projectKey}/github', { params: { path: tcKey } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/github',
    status: 403,
    setup: asViewer,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/github', { params: { path: tcKey } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/github',
    status: 404,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/github', { params: { path: tcKey } }),
  },
  {
    op: 'GET /api/v1/projects/{projectKey}/github',
    status: 500,
    setup: fail,
    call: (c) => c.GET('/api/v1/projects/{projectKey}/github', { params: { path: tcKey } }),
  },
  {
    op: 'PUT /api/v1/projects/{projectKey}/github',
    status: 200,
    call: (c) => c.PUT('/api/v1/projects/{projectKey}/github', { params: { path: tcKey }, body: ghBody }),
  },
  {
    op: 'PUT /api/v1/projects/{projectKey}/github',
    status: 400,
    call: (c) =>
      c.PUT('/api/v1/projects/{projectKey}/github', {
        params: { path: tcKey },
        body: { repository: 'acme/shop' },
      }),
  },
  {
    op: 'PUT /api/v1/projects/{projectKey}/github',
    status: 403,
    setup: asViewer,
    call: (c) => c.PUT('/api/v1/projects/{projectKey}/github', { params: { path: tcKey }, body: ghBody }),
  },
  {
    op: 'PUT /api/v1/projects/{projectKey}/github',
    status: 404,
    call: (c) => c.PUT('/api/v1/projects/{projectKey}/github', { params: { path: nope }, body: ghBody }),
  },
  {
    op: 'PUT /api/v1/projects/{projectKey}/github',
    status: 415,
    call: (c) =>
      c.PUT('/api/v1/projects/{projectKey}/github', {
        params: { path: tcKey },
        body: ghBody,
        bodySerializer: JSON.stringify,
        headers: textPlain,
      }),
  },
  {
    op: 'PUT /api/v1/projects/{projectKey}/github',
    status: 500,
    setup: fail,
    call: (c) => c.PUT('/api/v1/projects/{projectKey}/github', { params: { path: tcKey }, body: ghBody }),
  },
  {
    op: 'DELETE /api/v1/projects/{projectKey}/github',
    status: 204,
    setup: withGitHub(),
    call: (c) => c.DELETE('/api/v1/projects/{projectKey}/github', { params: { path: tcKey } }),
  },
  {
    op: 'DELETE /api/v1/projects/{projectKey}/github',
    status: 403,
    setup: asViewer,
    call: (c) => c.DELETE('/api/v1/projects/{projectKey}/github', { params: { path: tcKey } }),
  },
  {
    op: 'DELETE /api/v1/projects/{projectKey}/github',
    status: 404,
    call: (c) => c.DELETE('/api/v1/projects/{projectKey}/github', { params: { path: tcKey } }),
  },
  {
    op: 'DELETE /api/v1/projects/{projectKey}/github',
    status: 500,
    setup: fail,
    call: (c) => c.DELETE('/api/v1/projects/{projectKey}/github', { params: { path: tcKey } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/github/sync',
    status: 200,
    setup: withGitHub(),
    call: (c) => c.POST('/api/v1/projects/{projectKey}/github/sync', { params: { path: tcKey } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/github/sync',
    status: 403,
    setup: asViewer,
    call: (c) => c.POST('/api/v1/projects/{projectKey}/github/sync', { params: { path: tcKey } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/github/sync',
    status: 404,
    call: (c) => c.POST('/api/v1/projects/{projectKey}/github/sync', { params: { path: tcKey } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/github/sync',
    status: 409,
    setup: withGitHub('acme/shop', ''),
    call: (c) => c.POST('/api/v1/projects/{projectKey}/github/sync', { params: { path: tcKey } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/github/sync',
    status: 502,
    setup: withGitHub('acme/down'),
    call: (c) => c.POST('/api/v1/projects/{projectKey}/github/sync', { params: { path: tcKey } }),
  },
  {
    op: 'POST /api/v1/projects/{projectKey}/github/sync',
    status: 500,
    setup: fail,
    call: (c) => c.POST('/api/v1/projects/{projectKey}/github/sync', { params: { path: tcKey } }),
  },
]

const auditScenarios: Scenario[] = [
  {
    op: 'GET /api/v1/audit',
    status: 200,
    setup: () => {
      db.audit.push({
        id: 1,
        occurredAt: '2026-10-05T10:00:00Z',
        actor: 'admin',
        action: 'POST /api/v1/projects',
        path: '/api/v1/projects',
        project: 'TC',
        status: 201,
        summary: 'edited TC-1',
        testCase: 'TC-1',
        ip: '198.51.100.7',
        userAgent: 'curl/8',
      })
    },
    call: (c) =>
      c.GET('/api/v1/audit', {
        params: { query: { project: 'TC', actor: 'admin', testCase: 'TC-1', page: 1 } },
      }),
  },
  {
    op: 'GET /api/v1/audit',
    status: 400,
    call: (c) => c.GET('/api/v1/audit', { params: { query: { project: 'tc' } } }),
  },
  {
    op: 'GET /api/v1/audit',
    status: 400,
    call: (c) => c.GET('/api/v1/audit', { params: { query: { testCase: 'tc-1' } } }),
  },
  { op: 'GET /api/v1/audit', status: 403, setup: asMember, call: (c) => c.GET('/api/v1/audit') },
  { op: 'GET /api/v1/audit', status: 500, setup: fail, call: (c) => c.GET('/api/v1/audit') },
]

const scenarios: Scenario[] = [
  ...auditScenarios,
  ...integrationScenarios,
  ...roleScenarios,
  ...apiKeyScenarios,
  ...staleScenarios,
  ...amendmentScenarios,
  ...taxonomyScenarios,
  ...suiteScenarios,
  ...manualScenarios,
  ...requirementScenarios,
  ...issueScenarios,
  ...qualityScenarios,
  ...liveScenarios,
  ...authScenarios,
  ...signedOutScenarios(),
  // Projects
  {
    op: 'GET /api/v1/projects',
    status: 200,
    call: (c) => c.GET('/api/v1/projects', { params: { query: { page: 1, pageSize: 100 } } }),
  },
  {
    op: 'GET /api/v1/projects',
    status: 400,
    call: (c) => c.GET('/api/v1/projects', { params: { query: { page: 0 } } }),
  },
  { op: 'GET /api/v1/projects', status: 500, setup: fail, call: (c) => c.GET('/api/v1/projects') },
  {
    op: 'POST /api/v1/projects',
    status: 201,
    call: (c) => c.POST('/api/v1/projects', { body: { key: 'CHK', name: 'Checkout', description: 'cart' } }),
  },
  {
    op: 'POST /api/v1/projects',
    status: 400,
    call: (c) => c.POST('/api/v1/projects', { body: { key: '1X', name: 'x' } }),
  },
  {
    op: 'POST /api/v1/projects',
    status: 409,
    call: (c) => c.POST('/api/v1/projects', { body: { key: 'TC', name: 'again' } }),
  },
  {
    op: 'POST /api/v1/projects',
    status: 415,
    call: (c) => c.POST('/api/v1/projects', { body: { key: 'CHK', name: 'x' }, headers: textPlain }),
  },
  {
    op: 'POST /api/v1/projects',
    status: 500,
    setup: fail,
    call: (c) => c.POST('/api/v1/projects', { body: { key: 'CHK', name: 'x' } }),
  },
  {
    op: 'PATCH /api/v1/projects/{projectKey}',
    status: 200,
    setup: addChk,
    call: (c) =>
      c.PATCH('/api/v1/projects/{projectKey}', { params: { path: chk }, body: { name: 'Checkout v2' } }),
  },
  {
    op: 'PATCH /api/v1/projects/{projectKey}',
    status: 400,
    setup: addChk,
    call: (c) => c.PATCH('/api/v1/projects/{projectKey}', { params: { path: chk }, body: { name: ' ' } }),
  },
  {
    op: 'PATCH /api/v1/projects/{projectKey}',
    status: 404,
    call: (c) => c.PATCH('/api/v1/projects/{projectKey}', { params: { path: chk }, body: { name: 'x' } }),
  },
  {
    op: 'PATCH /api/v1/projects/{projectKey}',
    status: 415,
    call: (c) =>
      c.PATCH('/api/v1/projects/{projectKey}', {
        params: { path: chk },
        body: { name: 'x' },
        headers: textPlain,
      }),
  },
  {
    op: 'PATCH /api/v1/projects/{projectKey}',
    status: 500,
    setup: fail,
    call: (c) => c.PATCH('/api/v1/projects/{projectKey}', { params: { path: chk }, body: { name: 'x' } }),
  },
  {
    op: 'GET /api/v1/test-cases',
    status: 404,
    call: (c) => c.GET('/api/v1/test-cases', { params: { query: { project: 'NOPE' } } }),
  },
  {
    op: 'POST /api/v1/test-cases',
    status: 404,
    call: (c) => c.POST('/api/v1/test-cases', { body: { title: 'x', project: 'NOPE' } }),
  },
  {
    op: 'GET /api/v1/test-runs',
    status: 404,
    call: (c) => c.GET('/api/v1/test-runs', { params: { query: { project: 'NOPE' } } }),
  },
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
    op: 'POST /api/v1/test-cases/{testCaseId}/reactivate',
    status: 200,
    call: (c) => c.POST('/api/v1/test-cases/{testCaseId}/reactivate', { params: { path: tc } }),
  },
  {
    op: 'POST /api/v1/test-cases/{testCaseId}/reactivate',
    status: 400,
    call: (c) =>
      c.POST('/api/v1/test-cases/{testCaseId}/reactivate', { params: { path: { testCaseId: 0 } } }),
  },
  {
    op: 'POST /api/v1/test-cases/{testCaseId}/reactivate',
    status: 404,
    call: (c) => c.POST('/api/v1/test-cases/{testCaseId}/reactivate', { params: { path: unknownTc } }),
  },
  {
    op: 'POST /api/v1/test-cases/{testCaseId}/reactivate',
    status: 500,
    setup: fail,
    call: (c) => c.POST('/api/v1/test-cases/{testCaseId}/reactivate', { params: { path: tc } }),
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
        { index: 0, testName: '', message: 'no name', persisted: false, severity: 'error', shard: null },
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
    status: 200,
    setup: () => {
      db.runs[0] = { ...db.runs[0], mode: 'sharded', shards: { total: 2, received: [1, 2], missing: [] } }
      db.results = db.results.map((r, i) => ({ ...r, shard: (i % 2) + 1 }))
    },
    call: (c) =>
      c.GET('/api/v1/test-runs/{testRunId}/results', { params: { path: run, query: { shard: 2 } } }),
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
  // Non-JSON bodies on JSON operations (415).
  {
    op: 'POST /api/v1/test-cases',
    status: 415,
    call: (c) => c.POST('/api/v1/test-cases', { body: { title: 'x' }, headers: textPlain }),
  },
  {
    op: 'PATCH /api/v1/test-cases/{testCaseId}',
    status: 415,
    call: (c) =>
      c.PATCH('/api/v1/test-cases/{testCaseId}', {
        params: { path: tc },
        body: { title: 'x' },
        headers: textPlain,
      }),
  },
  {
    op: 'POST /api/v1/test-cases/{testCaseId}/steps',
    status: 415,
    call: (c) =>
      c.POST('/api/v1/test-cases/{testCaseId}/steps', {
        params: { path: tc },
        body: { action: 'x' },
        headers: textPlain,
      }),
  },
  {
    op: 'PUT /api/v1/test-cases/{testCaseId}/steps/order',
    status: 415,
    call: (c) =>
      c.PUT('/api/v1/test-cases/{testCaseId}/steps/order', {
        params: { path: tc },
        body: { stepIds: [1, 2] },
        headers: textPlain,
      }),
  },
  {
    op: 'PATCH /api/v1/test-cases/{testCaseId}/steps/{stepId}',
    status: 415,
    call: (c) =>
      c.PATCH('/api/v1/test-cases/{testCaseId}/steps/{stepId}', {
        params: { path: { ...tc, stepId: 1 } },
        body: { action: 'x' },
        headers: textPlain,
      }),
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
