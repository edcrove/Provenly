import { http, HttpResponse, type JsonBodyType } from 'msw'

import type {
  ApiKey,
  Invitation,
  ParseError,
  Project,
  User,
  TestCase,
  TestCaseResult,
  TestResult,
  TestRun,
  TestRunSummary,
  TestStep,
} from '@/api/client'
import type { MemberRole } from '@/lib/roles'

import { invitation, project, summary, testCase, testResult, testRun, testStep, user } from './fixtures'

/**
 * In-memory implementation of the Provenly REST API for MSW. It is used by the
 * Integration tests (components against a mocked API) and its responses are
 * validated against api/openapi.yaml by the Contract tests.
 */
export interface MockDb {
  users: User[]
  passwords: Record<string, string>
  invitations: Invitation[]
  /** Tokens of the invitations created in this mock, by invitation id. */
  invitationTokens: Record<string, number>
  /** Project roles of non-admin users. */
  members: { projectId: number; userId: number; role: MemberRole; since: string }[]
  /** CI API keys by project id. */
  apiKeys: (ApiKey & { projectId: number })[]
  /** The signed-in user's id (the session cookie), or null when signed out. */
  session: number | null
  projects: Project[]
  testCases: TestCase[]
  steps: TestStep[]
  runs: TestRun[]
  results: TestResult[]
  summaries: Record<number, TestRunSummary>
  parseErrors: Record<number, ParseError[]>
  /** When true every handler answers 500 (server failure), except who is signed in. */
  failing: boolean
  /** When true "who is signed in" answers 500 too. */
  failingMe: boolean
  nextId: number
}

export function seed(): MockDb {
  const tc = testCase()
  return {
    users: [user()],
    passwords: { admin: 'correct horse' },
    invitations: [],
    invitationTokens: {},
    session: 1,
    members: [],
    apiKeys: [],
    projects: [project()],
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
    failingMe: false,
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
  401: 'Unauthorized',
  403: 'Forbidden',
  404: 'Not Found',
  409: 'Conflict',
  412: 'Precondition Failed',
  415: 'Unsupported Media Type',
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

/** A test case response with its version as the ETag (optimistic locking). */
function tagged(body: JsonBodyType | null, tc: TestCase, status = 200) {
  const headers = { ETag: `"${tc.version}"` }
  return body === null
    ? new HttpResponse(null, { status, headers })
    : HttpResponse.json(body, { status, headers })
}

/** Like the server: an If-Match that no longer matches the test case's version is a 412. */
function stale(request: Request, tc: TestCase): Response | undefined {
  const header = request.headers.get('If-Match')
  if (!header || header.trim() === '*') return undefined
  const tags = header.split(',').map((t) => t.trim())
  if (tags.includes(`"${tc.version}"`)) return undefined
  return problem(412, 'precondition_failed', `test case ${tc.id} changed since you read it`)
}

function pathId(raw: string | readonly string[] | undefined): number | undefined {
  const n = Number(raw)
  return Number.isInteger(n) && n >= 1 ? n : undefined
}

const BASE = '*/api/v1'

type Handler = Parameters<typeof http.get>[1]

/** Wraps a handler with the shared failure switch. */
/** Wraps a handler that works without a session (sign-in, sign-out, accepting an invitation). */
const publicGuard =
  (fn: Handler): Handler =>
  (info) =>
    db.failing ? problem(500, 'internal_error', 'an unexpected error occurred') : fn(info)

/** Wraps a handler that needs a session, like every other API operation. */
const guard =
  (fn: Handler): Handler =>
  (info) =>
    db.session === null && !db.failing
      ? problem(401, 'unauthorized', 'sign in to continue')
      : publicGuard(fn)(info)

const currentUser = () => db.users.find((u) => u.id === db.session)!
const session = (u: User) => ({ token: `token-${u.id}`, expiresAt: '2026-10-05T22:00:00Z', user: u })
/** The signed-in user's role in a project, like the server: admin everywhere, else the membership. */
const roleIn = (projectId: number): Project['myRole'] | undefined =>
  currentUser().isAdmin
    ? 'admin'
    : db.members.find((m) => m.projectId === projectId && m.userId === db.session)?.role
const memberRoleOk = (role: unknown): role is MemberRole =>
  role === 'maintainer' || role === 'member' || role === 'viewer'
const forbidden = () => problem(403, 'forbidden', 'only administrators can manage users and invitations')
const USERNAME = /^[a-z0-9][a-z0-9._-]{2,31}$/

/** Wraps a handler whose request body is JSON: other content types answer 415 like the server. */
const jsonGuard =
  (fn: Handler): Handler =>
  (info) =>
    info.request.headers.get('Content-Type')?.split(';')[0].trim().toLowerCase() === 'application/json'
      ? fn(info)
      : problem(415, 'unsupported_media_type', 'Content-Type must be application/json')

const rank = { viewer: 1, member: 2, maintainer: 3, admin: 4 }
/** Like the server: invisible projects are 404, a role below min is 403. */
function requireRole(projectId: number, min: MemberRole, missing: () => Response): Response | undefined {
  const role = roleIn(projectId)
  if (!role) return missing()
  if (rank[role] < rank[min]) return problem(403, 'forbidden', `this needs the ${min} role in the project`)
  return undefined
}

function findCase(
  raw: string | readonly string[] | undefined,
  min: MemberRole = 'viewer',
): TestCase | Response {
  const id = pathId(raw)
  if (id === undefined) return validation('testCaseId', 'must be a positive integer')
  const tc = db.testCases.find((t) => t.id === id)
  if (!tc) return notFound(`test case TC-${id}`)
  return requireRole(tc.projectId, min, () => notFound(`test case TC-${id}`)) ?? tc
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

const KEY = /^[A-Z][A-Z0-9]{1,9}$/

/** Resolves ?project=<KEY> like the server: absent is every project, malformed is 400, unknown is 404. */
function projectFilter(url: URL): Project | undefined | Response {
  if (!url.searchParams.has('project')) return undefined
  const key = url.searchParams.get('project') ?? ''
  if (!KEY.test(key)) return validation('project', 'must be a project key')
  return db.projects.find((p) => p.key === key) ?? notFound(`project ${key}`)
}

/** The wire form of a mock API key (the project is in the URL). */
function withoutProject({ projectId, ...k }: MockDb['apiKeys'][number]): ApiKey {
  void projectId
  return k
}

/** A project the signed-in user maintains (or administers), like the server: else 400, 404 or 403. */
function maintainedProject(raw: string | readonly string[] | undefined): Project | Response {
  if (!KEY.test(String(raw))) return validation('projectKey', 'must be a project key')
  const p = db.projects.find((x) => x.key === raw)
  if (!p) return notFound(`project ${String(raw)}`)
  return requireRole(p.id, 'maintainer', () => notFound(`project ${String(raw)}`)) ?? p
}

const stepsOf = (tcId: number) =>
  db.steps.filter((s) => s.testCaseId === tcId).sort((a, b) => a.position - b.position)

export const handlers = [
  http.get(
    `${BASE}/projects/:projectKey/members`,
    guard(({ params, request }) => {
      if (!KEY.test(String(params.projectKey))) return validation('projectKey', 'must be a project key')
      const p = db.projects.find((x) => x.key === params.projectKey)
      if (!p || !roleIn(p.id)) return notFound(`project ${String(params.projectKey)}`)
      const items = db.members
        .filter((m) => m.projectId === p.id)
        .map((m) => ({ user: db.users.find((u) => u.id === m.userId)!, role: m.role, since: m.since }))
        .sort((a, b) => a.user.username.localeCompare(b.user.username))
      return respond(pageOf(new URL(request.url), items))
    }),
  ),
  http.put(
    `${BASE}/projects/:projectKey/members/:username`,
    guard(
      jsonGuard(async ({ params, request }) => {
        if (!KEY.test(String(params.projectKey))) return validation('projectKey', 'must be a project key')
        const p = db.projects.find((x) => x.key === params.projectKey)
        if (!p || !roleIn(p.id)) return notFound(`project ${String(params.projectKey)}`)
        if (roleIn(p.id) !== 'admin' && roleIn(p.id) !== 'maintainer')
          return problem(403, 'forbidden', 'this needs the maintainer role in the project')
        const role = (await readBody(request))?.role
        if (!memberRoleOk(role)) return validation('role', 'must be one of maintainer, member, viewer')
        const u = db.users.find((x) => x.username === params.username)
        if (!u) return notFound(`user ${String(params.username)}`)
        db.members = db.members.filter((m) => !(m.projectId === p.id && m.userId === u.id))
        db.members.push({ projectId: p.id, userId: u.id, role, since: now() })
        return respond({ user: u, role, since: now() })
      }),
    ),
  ),
  http.delete(
    `${BASE}/projects/:projectKey/members/:username`,
    guard(({ params }) => {
      if (!KEY.test(String(params.projectKey))) return validation('projectKey', 'must be a project key')
      const p = db.projects.find((x) => x.key === params.projectKey)
      if (!p || !roleIn(p.id)) return notFound(`project ${String(params.projectKey)}`)
      if (roleIn(p.id) !== 'admin' && roleIn(p.id) !== 'maintainer')
        return problem(403, 'forbidden', 'this needs the maintainer role in the project')
      const u = db.users.find((x) => x.username === params.username)
      const before = db.members.length
      db.members = db.members.filter((m) => !(m.projectId === p.id && m.userId === u?.id))
      if (db.members.length === before) return notFound(`member ${String(params.username)}`)
      return new HttpResponse(null, { status: 204 })
    }),
  ),
  http.get(
    `${BASE}/projects/:projectKey/api-keys`,
    guard(({ params, request }) => {
      const p = maintainedProject(params.projectKey)
      if (p instanceof Response) return p
      const items = db.apiKeys
        .filter((k) => k.projectId === p.id)
        .sort((a, b) => b.id - a.id)
        .map(withoutProject)
      return respond(pageOf(new URL(request.url), items))
    }),
  ),
  http.post(
    `${BASE}/projects/:projectKey/api-keys`,
    guard(
      jsonGuard(async ({ params, request }) => {
        const p = maintainedProject(params.projectKey)
        if (p instanceof Response) return p
        const name = String((await readBody(request))?.name ?? '').trim()
        if (!name || name.length > 100) return validation('name', 'must be 1 to 100 characters')
        const id = db.nextId++
        const hex = id.toString(16).padStart(8, '0')
        const key = {
          id,
          projectId: p.id,
          name,
          prefix: `pvk_${hex}`,
          status: 'active' as const,
          createdBy: db.session!,
          createdAt: now(),
          lastUsedAt: null,
          revokedAt: null,
        }
        db.apiKeys.push(key)
        return respond({ apiKey: withoutProject(key), token: `pvk_${hex}_${'k'.repeat(43)}` }, 201)
      }),
    ),
  ),
  http.post(
    `${BASE}/projects/:projectKey/api-keys/:apiKeyId/revoke`,
    guard(({ params }) => {
      const id = pathId(params.apiKeyId)
      if (id === undefined) return validation('apiKeyId', 'must be a positive integer')
      const p = maintainedProject(params.projectKey)
      if (p instanceof Response) return p
      const k = db.apiKeys.find((x) => x.id === id && x.projectId === p.id)
      if (!k) return notFound(`API key ${id}`)
      if (k.revokedAt) return problem(409, 'conflict', `API key ${id} is already revoked`)
      Object.assign(k, { status: 'revoked', revokedAt: now() })
      return respond(withoutProject(k))
    }),
  ),
  http.post(
    `${BASE}/auth/login`,
    publicGuard(
      jsonGuard(async ({ request }) => {
        const body = await readBody(request)
        if (!body) return validation('body', 'must be a JSON object')
        const username = String(body.username ?? '')
          .trim()
          .toLowerCase()
        const u = db.users.find((x) => x.username === username)
        if (!u || db.passwords[username] !== body?.password)
          return problem(401, 'unauthorized', 'invalid username or password')
        db.session = u.id
        return respond(session(u))
      }),
    ),
  ),
  http.post(
    `${BASE}/auth/logout`,
    publicGuard(() => {
      db.session = null
      return new HttpResponse(null, { status: 204 })
    }),
  ),
  // Who is signed in keeps answering while db.failing breaks the pages' own requests.
  http.get(`${BASE}/auth/me`, () => {
    if (db.failingMe) return problem(500, 'internal_error', 'an unexpected error occurred')
    return db.session === null ? problem(401, 'unauthorized', 'sign in to continue') : respond(currentUser())
  }),
  http.post(
    `${BASE}/auth/password`,
    guard(
      jsonGuard(async ({ request }) => {
        const body = await readBody(request)
        const u = currentUser()
        const next = String(body?.newPassword ?? '')
        if (next.length < 10) return validation('newPassword', 'must be at least 10 characters')
        if (db.passwords[u.username] !== body?.currentPassword)
          return validation('currentPassword', 'is not your current password')
        db.passwords[u.username] = next
        return respond(session(u))
      }),
    ),
  ),
  http.get(
    `${BASE}/users`,
    guard(({ request }) =>
      currentUser().isAdmin ? respond(pageOf(new URL(request.url), db.users)) : forbidden(),
    ),
  ),
  http.get(
    `${BASE}/invitations`,
    guard(({ request }) =>
      currentUser().isAdmin
        ? respond(
            pageOf(
              new URL(request.url),
              [...db.invitations].sort((a, b) => b.id - a.id),
            ),
          )
        : forbidden(),
    ),
  ),
  http.post(
    `${BASE}/invitations`,
    guard(
      jsonGuard(async ({ request }) => {
        if (!currentUser().isAdmin) return forbidden()
        const body = await readBody(request)
        const email = typeof body?.email === 'string' && body.email.trim() ? body.email.trim() : null
        if (email && !/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(email))
          return validation('email', 'must be an email address')
        const grant = typeof body?.project === 'string' && body.project ? body.project : null
        const grantProject = grant ? db.projects.find((p) => p.key === grant) : undefined
        if (grant && !grantProject) return notFound(`project ${grant}`)
        if (grant && !memberRoleOk(body?.role))
          return validation('role', 'must be one of maintainer, member, viewer')
        const inv = invitation({
          id: ++db.nextId,
          projectId: grantProject?.id ?? null,
          projectRole: grantProject ? (body?.role as MemberRole) : null,
          email,
          note: String(body?.note ?? '').trim(),
          createdBy: currentUser().id,
          createdAt: now(),
        })
        const token = `invite-${inv.id}`
        db.invitations.push(inv)
        db.invitationTokens[token] = inv.id
        return respond({ invitation: inv, token }, 201)
      }),
    ),
  ),
  http.post(
    `${BASE}/invitations/accept`,
    publicGuard(
      jsonGuard(async ({ request }) => {
        const body = await readBody(request)
        const username = String(body?.username ?? '')
          .trim()
          .toLowerCase()
        const password = String(body?.password ?? '')
        if (!USERNAME.test(username)) return validation('username', 'must be 3 to 32 lower-case letters')
        if (password.length < 10) return validation('password', 'must be at least 10 characters')
        const inv = db.invitations.find((i) => i.id === db.invitationTokens[String(body?.token ?? '')])
        if (!inv || inv.status !== 'pending')
          return problem(404, 'not_found', 'invitation not found, expired, revoked or already used')
        if (db.users.some((u) => u.username === username))
          return problem(409, 'conflict', `username ${username} is taken`)
        const u = user({
          id: ++db.nextId,
          username,
          displayName: String(body?.displayName ?? '').trim(),
          email: (body?.email as string | null) || inv.email,
          isAdmin: false,
          createdAt: now(),
        })
        db.users.push(u)
        db.passwords[username] = password
        Object.assign(inv, { status: 'accepted', acceptedAt: now(), acceptedUserId: u.id })
        if (inv.projectId && inv.projectRole)
          db.members.push({ projectId: inv.projectId, userId: u.id, role: inv.projectRole, since: now() })
        db.session = u.id
        return respond(session(u), 201)
      }),
    ),
  ),
  http.post(
    `${BASE}/invitations/:invitationId/revoke`,
    guard(({ params }) => {
      if (!currentUser().isAdmin) return forbidden()
      const id = pathId(params.invitationId)
      if (id === undefined) return validation('invitationId', 'must be a positive integer')
      const inv = db.invitations.find((i) => i.id === id)
      if (!inv) return notFound(`invitation ${id}`)
      if (inv.status !== 'pending')
        return problem(409, 'conflict', `invitation ${id} was already accepted or revoked`)
      Object.assign(inv, { status: 'revoked', revokedAt: now() })
      return respond(inv)
    }),
  ),
  http.get(
    `${BASE}/projects`,
    guard(({ request }) =>
      respond(
        pageOf(
          new URL(request.url),
          db.projects
            .filter((p) => roleIn(p.id))
            .map((p) => ({ ...p, myRole: roleIn(p.id)! }))
            .sort((a, b) => a.key.localeCompare(b.key)),
        ),
      ),
    ),
  ),
  http.post(
    `${BASE}/projects`,
    guard(
      jsonGuard(async ({ request }) => {
        if (!currentUser().isAdmin) return problem(403, 'forbidden', 'administrators only')
        const body = await readBody(request)
        const key = String(body?.key ?? '')
          .trim()
          .toUpperCase()
        const name = String(body?.name ?? '').trim()
        if (!KEY.test(key)) return validation('key', 'must be a project key')
        if (!name) return validation('name', 'must not be empty')
        if (db.projects.some((p) => p.key === key))
          return problem(409, 'conflict', `project ${key} already exists`)
        const p = project({
          id: ++db.nextId,
          key,
          name,
          description: String(body?.description ?? ''),
          createdAt: now(),
          updatedAt: now(),
        })
        db.projects.push(p)
        return respond(p, 201)
      }),
    ),
  ),
  http.patch(
    `${BASE}/projects/:projectKey`,
    guard(
      jsonGuard(async ({ params, request }) => {
        const p = db.projects.find((x) => x.key === params.projectKey)
        if (!p) return notFound(`project ${String(params.projectKey)}`)
        const denied = requireRole(p.id, 'maintainer', () => notFound(`project ${String(params.projectKey)}`))
        if (denied) return denied
        const body = await readBody(request)
        if (!body || Object.keys(body).length === 0)
          return validation('body', 'at least one field is required')
        if (body.name !== undefined && !String(body.name).trim())
          return validation('name', 'must not be empty')
        Object.assign(p, body, { updatedAt: now() })
        return respond(p)
      }),
    ),
  ),
  http.get(
    `${BASE}/test-cases`,
    guard(({ request }) => {
      const url = new URL(request.url)
      const status = url.searchParams.get('status')
      if (status && status !== 'active' && status !== 'deprecated') return validation('status', 'invalid')
      const p = projectFilter(url)
      if (p instanceof Response) return p
      const items = db.testCases
        .filter((t) => (!status || t.status === status) && (!p || t.projectId === p.id))
        .sort((a, b) => b.id - a.id)
      return respond(pageOf(url, items))
    }),
  ),
  http.post(
    `${BASE}/test-cases`,
    guard(
      jsonGuard(async ({ request }) => {
        const body = await readBody(request)
        const title = typeof body?.title === 'string' ? body.title.trim() : ''
        if (!body || 'id' in body) return validation('body', 'unknown field "id"')
        if (!title) return validation('title', 'is required')
        const key = String(body.project ?? 'TC')
        if (!KEY.test(key)) return validation('project', 'must be a project key')
        const p = db.projects.find((x) => x.key === key)
        if (!p) return notFound(`project ${key}`)
        const denied = requireRole(p.id, 'member', () => notFound(`project ${key}`))
        if (denied) return denied
        const id = ++db.nextId
        const tc = testCase({
          id,
          projectId: p.id,
          projectKey: p.key,
          number: Math.max(0, ...db.testCases.filter((t) => t.projectId === p.id).map((t) => t.number)) + 1,
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
  ),
  http.get(
    `${BASE}/test-cases/:testCaseId`,
    guard(({ params }) => {
      const tc = findCase(params.testCaseId)
      return tc instanceof Response ? tc : tagged(tc, tc)
    }),
  ),
  http.patch(
    `${BASE}/test-cases/:testCaseId`,
    guard(
      jsonGuard(async ({ params, request }) => {
        const tc = findCase(params.testCaseId, 'member')
        if (tc instanceof Response) return tc
        const body = await readBody(request)
        if (!body || Object.keys(body).length === 0)
          return validation('body', 'at least one field is required')
        if (body.title !== undefined && !String(body.title).trim())
          return validation('title', 'must not be empty')
        const conflict = stale(request, tc)
        if (conflict) return conflict
        Object.assign(tc, body, { updatedAt: now(), version: tc.version + 1 })
        return tagged(tc, tc)
      }),
    ),
  ),
  http.post(
    `${BASE}/test-cases/:testCaseId/deprecate`,
    guard(({ params, request }) => {
      const tc = findCase(params.testCaseId, 'maintainer')
      if (tc instanceof Response) return tc
      const conflict = stale(request, tc)
      if (conflict) return conflict
      tc.status = 'deprecated'
      tc.deprecatedAt ??= now()
      tc.version++
      return tagged(tc, tc)
    }),
  ),
  http.post(
    `${BASE}/test-cases/:testCaseId/reactivate`,
    guard(({ params, request }) => {
      const tc = findCase(params.testCaseId, 'maintainer')
      if (tc instanceof Response) return tc
      const conflict = stale(request, tc)
      if (conflict) return conflict
      tc.status = 'active'
      tc.deprecatedAt = null
      tc.version++
      return tagged(tc, tc)
    }),
  ),
  http.get(
    `${BASE}/test-cases/:testCaseId/steps`,
    guard(({ params, request }) => {
      const tc = findCase(params.testCaseId)
      if (tc instanceof Response) return tc
      const page = pageOf(new URL(request.url), stepsOf(tc.id))
      return page instanceof Response ? page : tagged(page, tc)
    }),
  ),
  http.post(
    `${BASE}/test-cases/:testCaseId/steps`,
    guard(
      jsonGuard(async ({ params, request }) => {
        const tc = findCase(params.testCaseId, 'member')
        if (tc instanceof Response) return tc
        const body = await readBody(request)
        const action = typeof body?.action === 'string' ? body.action.trim() : ''
        if (!action) return validation('action', 'must not be empty')
        const conflict = stale(request, tc)
        if (conflict) return conflict
        const step = testStep({
          id: ++db.nextId,
          testCaseId: tc.id,
          position: stepsOf(tc.id).length + 1,
          action,
          expectedResult: String(body?.expectedResult ?? ''),
        })
        db.steps.push(step)
        tc.version++
        return tagged(step, tc, 201)
      }),
    ),
  ),
  http.put(
    `${BASE}/test-cases/:testCaseId/steps/order`,
    guard(
      jsonGuard(async ({ params, request }) => {
        const tc = findCase(params.testCaseId, 'member')
        if (tc instanceof Response) return tc
        const body = await readBody(request)
        const ids = Array.isArray(body?.stepIds) ? (body.stepIds as number[]) : []
        const current = stepsOf(tc.id)
        const valid = ids.length === current.length && current.every((s) => ids.includes(s.id))
        if (!valid) return validation('stepIds', 'must list every step of the test case exactly once')
        const conflict = stale(request, tc)
        if (conflict) return conflict
        ids.forEach((id, i) => {
          const step = current.find((s) => s.id === id)!
          step.position = i + 1
        })
        tc.version++
        return tagged({ items: stepsOf(tc.id) }, tc)
      }),
    ),
  ),
  http.patch(
    `${BASE}/test-cases/:testCaseId/steps/:stepId`,
    guard(
      jsonGuard(async ({ params, request }) => {
        const tc = findCase(params.testCaseId, 'member')
        if (tc instanceof Response) return tc
        const stepId = pathId(params.stepId)
        if (stepId === undefined) return validation('stepId', 'must be a positive integer')
        const body = await readBody(request)
        if (!body || Object.keys(body).length === 0)
          return validation('body', 'at least one field is required')
        if (body.action !== undefined && !String(body.action).trim())
          return validation('action', 'must not be empty')
        const step = stepsOf(tc.id).find((s) => s.id === stepId)
        if (!step) return notFound(`step ${stepId}`)
        const conflict = stale(request, tc)
        if (conflict) return conflict
        Object.assign(step, body, { updatedAt: now() })
        tc.version++
        return tagged(step, tc)
      }),
    ),
  ),
  http.delete(
    `${BASE}/test-cases/:testCaseId/steps/:stepId`,
    guard(({ params, request }) => {
      const tc = findCase(params.testCaseId, 'member')
      if (tc instanceof Response) return tc
      const stepId = pathId(params.stepId)
      if (stepId === undefined) return validation('stepId', 'must be a positive integer')
      const step = stepsOf(tc.id).find((s) => s.id === stepId)
      if (!step) return notFound(`step ${stepId}`)
      const conflict = stale(request, tc)
      if (conflict) return conflict
      db.steps = db.steps.filter((s) => s.id !== stepId)
      stepsOf(tc.id).forEach((s, i) => (s.position = i + 1))
      tc.version++
      return tagged(null, tc, 204)
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
    guard(({ request }) => {
      const url = new URL(request.url)
      const p = projectFilter(url)
      if (p instanceof Response) return p
      return respond(
        pageOf(
          url,
          db.runs.filter((r) => !p || r.projectId === p.id).sort((a, b) => b.id - a.id),
        ),
      )
    }),
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
