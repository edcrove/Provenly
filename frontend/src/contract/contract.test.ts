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

type Method = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
type AnyCall = (path: string, init: object) => Promise<Result>
const anyPath = { testCaseId: 153, testRunId: 7, stepId: 1, projectKey: 'TC', invitationId: 1 }

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

const scenarios: Scenario[] = [
  ...roleScenarios,
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
