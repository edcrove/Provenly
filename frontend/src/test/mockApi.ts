import { http, HttpResponse, type JsonBodyType } from 'msw'

import type {
  Amendment,
  ApiKey,
  AuditEvent,
  Dimension,
  GitHubConnection,
  Webhook,
  WebhookDelivery,
  Issue,
  LiveRun,
  Requirement,
  Suite,
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
  /** Failed sign-ins by username: five lock it (429), like the server's throttle. */
  loginFailures: Record<string, number>
  invitations: Invitation[]
  /** Tokens of the invitations created in this mock, by invitation id. */
  invitationTokens: Record<string, number>
  /** Password reset tokens: token -> username (used ones are deleted). */
  resetTokens: Record<string, string>
  /** Project roles of non-admin users. */
  members: { projectId: number; userId: number; role: MemberRole; since: string }[]
  /** CI API keys by project id. */
  apiKeys: (ApiKey & { projectId: number })[]
  /** Snapshot amendments (DEC-42), all runs. */
  amendments: Amendment[]
  /** Suites (with their members for static ones) by project id. */
  suites: (Suite & { projectId: number; id: number; members: number[] })[]
  /** Requirements by project id; latest is the latest result status of each test case (coverage). */
  requirements: (Requirement & { projectId: number })[]
  latest: Record<number, 'passed' | 'failed' | 'error' | 'skipped'>
  /** The latest conclusive result (passed, failed, error) and its run, when the latest one was skipped; else latest. */
  conclusive: Record<number, { status: 'passed' | 'failed' | 'error'; runId: number }>
  /** Live state of live runs, by run id (what GET /test-runs/{id}/live answers). */
  live: Record<number, LiveRun>
  /** Flaky test case counts the quality endpoint reports (by test case id). */
  flaky: Record<number, number>
  /** Issues by project id; their verification is recomputed from latest (skipped is inconclusive). */
  issues: (Issue & { projectId: number })[]
  /** Webhooks by project id (lastDelivery is derived from deliveries) and their deliveries. */
  webhooks: (Omit<Webhook, 'lastDelivery'> & { projectId: number })[]
  deliveries: WebhookDelivery[]
  /** GitHub connections by project id; repository acme/down answers 502 on sync, an empty tokenHint 409. */
  github: (GitHubConnection & { projectId: number })[]
  /** Audit events, oldest first (listed newest first). */
  audit: AuditEvent[]
  /** Classification dimensions by project id. */
  dimensions: (Dimension & { projectId: number })[]
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
    loginFailures: {},
    invitations: [],
    invitationTokens: {},
    resetTokens: {},
    session: 1,
    members: [],
    apiKeys: [],
    amendments: [],
    dimensions: seedDimensions(1),
    suites: [],
    requirements: [],
    latest: {},
    conclusive: {},
    issues: [],
    webhooks: [],
    deliveries: [],
    audit: [],
    github: [],
    flaky: {},
    live: {},
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

/** A reduced set of the built-in dimensions the server seeds in every project. */
export function seedDimensions(projectId: number): MockDb['dimensions'] {
  const value = (key: string, name: string) => ({ key, name, archivedAt: null })
  return [
    { projectId, key: 'feature', name: 'Feature', builtIn: true, archivedAt: null, values: [] },
    {
      projectId,
      key: 'risk',
      name: 'Risk',
      builtIn: true,
      archivedAt: null,
      values: [value('critical', 'Critical'), value('high', 'High'), value('low', 'Low')],
    },
  ]
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
  429: 'Too Many Requests',
  502: 'Bad Gateway',
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

// The API's message for a malformed project key (platform/projectkey).
const PROJECT_KEY_MESSAGE =
  'must be a project key: 2 to 10 upper-case letters or digits, starting with a letter (e.g. CHK)'
const KEY = /^[A-Z][A-Z0-9]{1,9}$/

/** Resolves ?project=<KEY> like the server: absent is every project, malformed is 400, unknown is 404. */
function projectFilter(url: URL): Project | undefined | Response {
  if (!url.searchParams.has('project')) return undefined
  const key = url.searchParams.get('project') ?? ''
  if (!KEY.test(key)) return validation('project', PROJECT_KEY_MESSAGE)
  return db.projects.find((p) => p.key === key) ?? notFound(`project ${key}`)
}

/** The wire form of a mock API key (the project is in the URL). */
function withoutProject({ projectId, ...k }: MockDb['apiKeys'][number]): ApiKey {
  void projectId
  return k
}

/** A project the signed-in user maintains (or administers), like the server: else 400, 404 or 403. */
function maintainedProject(raw: string | readonly string[] | undefined): Project | Response {
  if (!KEY.test(String(raw))) return validation('projectKey', PROJECT_KEY_MESSAGE)
  const p = db.projects.find((x) => x.key === raw)
  if (!p) return notFound(`project ${String(raw)}`)
  return requireRole(p.id, 'maintainer', () => notFound(`project ${String(raw)}`)) ?? p
}

const DIMENSION = /^[a-z][a-z0-9-]{0,29}$/
/** A project the user maintains, for integrations (any unknown or malformed key is 404, like the server). */
function integrationProject(raw: string | readonly string[] | undefined): Project | Response {
  if (typeof raw !== 'string' || !KEY.test(raw)) return validation('projectKey', PROJECT_KEY_MESSAGE)
  const p = db.projects.find((x) => x.key === raw)
  if (!p) return notFound(`project ${String(raw)}`)
  return requireRole(p.id, 'maintainer', () => notFound(`project ${String(raw)}`)) ?? p
}

function webhookOf(p: Project, raw: string | readonly string[] | undefined) {
  const id = pathId(raw)
  if (id === undefined) return validation('webhookId', 'must be a positive integer')
  return db.webhooks.find((w) => w.id === id && w.projectId === p.id) ?? notFound(`webhook ${id}`)
}

function webhookView({ projectId, ...w }: MockDb['webhooks'][number]): Webhook {
  void projectId
  const last = db.deliveries.filter((d) => d.webhookId === w.id).at(-1)
  return last ? { ...w, lastDelivery: last } : w
}

const WEBHOOK_URL = /^https?:\/\/[^\s/@]+/

function githubView({ projectId, ...c }: MockDb['github'][number]): GitHubConnection {
  void projectId
  return c
}

const VALUE = /^[a-z0-9][a-z0-9-]{0,29}$/
const TAG = /^[a-z0-9][a-z0-9._-]{0,39}$/
const PAIR = '[a-z][a-z0-9-]{0,29}:[a-z0-9][a-z0-9-]{0,29}'
const CLASSIFIED = new RegExp(`^${PAIR}(,${PAIR}){0,9}$`)

/** The wire form of a mock dimension (the project is in the URL). */
function dimensionDto({ projectId, ...d }: MockDb['dimensions'][number]): Dimension {
  void projectId
  return d
}

/** A project the signed-in user sees (viewer and up), like the server. */
function visibleProject(raw: string | readonly string[] | undefined): Project | Response {
  if (!KEY.test(String(raw))) return validation('projectKey', PROJECT_KEY_MESSAGE)
  const p = db.projects.find((x) => x.key === raw)
  if (!p || !roleIn(p.id)) return notFound(`project ${String(raw)}`)
  return p
}

/** A dimension of a project the user maintains: else 400, 403 or 404. */
function maintainedDimension(params: Record<string, string | readonly string[] | undefined>) {
  if (!DIMENSION.test(String(params.dimensionKey)))
    return validation('dimensionKey', 'must be a dimension key')
  const p = maintainedProject(params.projectKey)
  if (p instanceof Response) return p
  return (
    db.dimensions.find((d) => d.projectId === p.id && d.key === params.dimensionKey) ??
    notFound(`dimension ${String(params.dimensionKey)}`)
  )
}

/** Validates a key and name body like the server. */
function keyAndName(body: Record<string, unknown> | undefined, pattern: RegExp) {
  if (!body) return validation('body', 'malformed JSON body')
  if (!pattern.test(String(body.key))) return validation('key', 'is not a valid key')
  const name = typeof body.name === 'string' ? body.name.trim() : ''
  if (!name) return validation('name', 'must not be empty')
  return { key: String(body.key), name }
}

/** Applies a rename or archive body like the server. */
function renameOrArchive(
  target: { name: string; archivedAt: string | null },
  body: Record<string, unknown> | undefined,
) {
  if (!body || Object.keys(body).length === 0) return validation('body', 'at least one field is required')
  if (body.name !== undefined) {
    if (!String(body.name).trim()) return validation('name', 'must not be empty')
    target.name = String(body.name).trim()
  }
  if (body.archived === true) target.archivedAt ??= now()
  if (body.archived === false) target.archivedAt = null
  return undefined
}

/** Normalizes tags like the server, or answers 400. */
function normalizeTags(raw: unknown): string[] | Response {
  const tags = [...new Set((Array.isArray(raw) ? raw : []).map((t) => String(t).trim().toLowerCase()))].sort()
  const bad = tags.find((t) => !TAG.test(t))
  return bad === undefined ? tags : validation('tags', `"${bad}" is not a valid tag`)
}

/** Applies a classification change like the server (null clears; archived only when already current). */
function classify(tc: TestCase, raw: unknown): Response | undefined {
  const next = { ...tc.classification }
  for (const [dim, value] of Object.entries((raw ?? {}) as Record<string, string | null>)) {
    const d = db.dimensions.find((x) => x.projectId === tc.projectId && x.key === dim)
    if (!d) return validation(`classification.${dim}`, 'is not a dimension of the project')
    if (value === null) {
      delete next[dim]
      continue
    }
    const v = d.values.find((x) => x.key === value)
    if (!v) return validation(`classification.${dim}`, `"${value}" is not a value of ${d.name}`)
    if ((v.archivedAt || d.archivedAt) && tc.classification[dim] !== value)
      return validation(`classification.${dim}`, `"${value}" is archived`)
    next[dim] = value
  }
  tc.classification = next
  return undefined
}

/** The wire form of a mock suite (members only when reading one). */
function suiteDto({ projectId, id, members, ...su }: MockDb['suites'][number], withCases: boolean): Suite {
  void projectId
  void id
  return {
    ...su,
    caseCount: su.kind === 'static' ? members.length : 0,
    ...(withCases && su.kind === 'static' ? { testCaseIds: [...members].sort((a, b) => a - b) } : {}),
  }
}

/** A suite of a project the user sees (viewer) or maintains, like the server: else 400, 403 or 404. */
function findSuite(params: Record<string, string | readonly string[] | undefined>, maintain: boolean) {
  if (!DIMENSION.test(String(params.suiteKey))) return validation('suiteKey', 'must be a suite key')
  const p = maintain ? maintainedProject(params.projectKey) : visibleProject(params.projectKey)
  if (p instanceof Response) return p
  return (
    db.suites.find((x) => x.projectId === p.id && x.key === params.suiteKey) ??
    notFound(`suite ${String(params.suiteKey)}`)
  )
}

/** Validates a static suite's members like the server. */
function suiteMembers(projectId: number, raw: unknown): number[] | Response {
  const ids = [...new Set((Array.isArray(raw) ? raw : []).map(Number))]
  const missing = ids.filter((id) => !db.testCases.some((t) => t.id === id && t.projectId === projectId))
  return missing.length
    ? validation('testCaseIds', `not test cases of the project: ${missing.join(', ')}`)
    : ids
}

type Outcome = TestRunSummary['testCases'][number]

/** The summary of a manual run, recomputed from its test cases' latest results. */
/** The counts and percentages of a summary recomputed from its test cases, like the server. */
function recount(cases: Outcome[]): Partial<TestRunSummary> {
  const count = (st: string) => cases.filter((c) => c.status === st).length
  const executed = cases.length - count('untested')
  const pct = (n: number, of: number) => (of ? (n * 100) / of : 0)
  return {
    expectedTotal: cases.length,
    executedTotal: executed,
    counts: {
      untested: count('untested'),
      passed: count('passed'),
      failed: count('failed'),
      error: count('error'),
      skipped: count('skipped'),
    },
    percentOfExpected: {
      untested: pct(count('untested'), cases.length),
      passed: pct(count('passed'), cases.length),
      failed: pct(count('failed'), cases.length),
      error: pct(count('error'), cases.length),
      skipped: pct(count('skipped'), cases.length),
    },
    percentOfExecuted: {
      passed: pct(count('passed'), executed),
      failed: pct(count('failed'), executed),
      error: pct(count('error'), executed),
      skipped: pct(count('skipped'), executed),
    },
    executionPercent: pct(executed, cases.length),
    testCases: cases,
  }
}

/** The run outcome derived from its summary, like execution.Summary.Outcome on the server. */
function outcomeOf(s: TestRunSummary): TestRun['outcome'] {
  const c = s.counts
  const verdict: TestRun['outcome']['verdict'] =
    s.expectedTotal === 0
      ? 'no_tests'
      : c.failed + c.error > 0
        ? 'failed'
        : c.untested + c.skipped > 0
          ? 'incomplete'
          : 'passed'
  return {
    verdict,
    executed: s.executedTotal,
    passed: c.passed,
    failed: c.failed,
    error: c.error,
    skipped: c.skipped,
    untested: c.untested,
    passRate: s.percentOfExecuted.passed,
    flaky: s.testCases.filter((t) => t.flaky).length,
  }
}

function manualSummary(runId: number, cases: Outcome[]): TestRunSummary {
  return summary({
    testRunId: runId,
    snapshotTotal: cases.length,
    diagnostics: { missing: 0, malformed: 0, unknown: 0, deprecated: 0, wrongProject: 0, total: 0 },
    ...recount(cases),
  })
}

const RESULT_STATUSES = ['passed', 'failed', 'error', 'skipped']

/** The key of a test case, like the server's (null for an id no test case has). */
const keyOfCase = (id: number) => db.testCases.find((t) => t.id === id)?.key ?? null

/** pageOf plus the counts of a status over every item (not only the page's), like the server (DEC-78). */
function pageWithCounts<T>(url: URL, items: T[], field: string, statusOf: (item: T) => string) {
  const page = pageOf(url, items)
  if (page instanceof Response) return page
  const counts: Record<string, number> = {}
  for (const item of items) counts[statusOf(item)] = (counts[statusOf(item)] ?? 0) + 1
  return { ...(page as object), [field]: counts } as JsonBodyType
}
const PROVIDERS = ['provenly', 'jira', 'github', 'azure_devops']

/** A requirement with its coverage recomputed from db.latest, like the server. */
function requirementDto({ projectId, ...r }: MockDb['requirements'][number]): Requirement {
  void projectId
  const cases = r.testCaseIds.map((id) => ({
    testCaseId: id,
    testCaseKey: keyOfCase(id),
    status: db.latest[id] ?? null,
  }))
  const passed = cases.filter((c) => c.status === 'passed').length
  const failed = cases.filter((c) => c.status === 'failed' || c.status === 'error').length
  const status: Requirement['coverage']['status'] =
    cases.length === 0
      ? 'uncovered'
      : failed > 0
        ? 'failing'
        : passed === cases.length
          ? 'passing'
          : cases.every((c) => c.status === null)
            ? 'not_run'
            : 'partial'
  return {
    ...r,
    coverage: {
      status,
      linked: cases.length,
      passed,
      failed,
      notRun: cases.length - passed - failed,
      testCases: cases,
    },
  }
}

function findRequirement(params: Record<string, string | readonly string[] | undefined>, min: MemberRole) {
  const id = pathId(params.requirementId)
  if (id === undefined) return validation('requirementId', 'must be a positive integer')
  const p = visibleProject(params.projectKey)
  if (p instanceof Response) return p
  const denied = requireRole(p.id, min, () => notFound(`project ${p.key}`))
  if (denied) return denied
  return db.requirements.find((r) => r.projectId === p.id && r.id === id) ?? notFound(`requirement ${id}`)
}

type Verification = Issue['verification']['status']
const RANK: Verification[] = ['validated_fixed', 'not_reproducible', 'unverified', 'known_issue', 'reopen']

/** An issue with its verification recomputed from db.latest, like the server (DEC-8). */
function issueDto({ projectId, ...i }: MockDb['issues'][number]): Issue {
  void projectId
  const testCases = i.testCaseIds.map((id) => {
    const latest = db.latest[id]
    // The evidence is the latest conclusive result: a skipped run keeps the previous one (the server's
    // LatestConclusive), and only says the latest was inconclusive.
    const kept = db.conclusive[id]
    const evidence = latest === 'skipped' || latest === undefined ? (kept?.status ?? null) : latest
    const status: Issue['verification']['testCases'][number]['status'] =
      evidence === null
        ? 'unverified'
        : evidence === 'passed'
          ? i.state === 'open'
            ? 'not_reproducible'
            : 'validated_fixed'
          : i.state === 'open'
            ? 'known_issue'
            : 'reopen'
    return {
      testCaseId: id,
      testCaseKey: keyOfCase(id),
      status,
      evidence,
      evidenceRunId: evidence ? (latest === 'skipped' || latest === undefined ? kept!.runId : 1) : null,
      latestInconclusive: latest === 'skipped',
    }
  })
  const status = testCases.reduce<Verification>(
    (worst, c) => (RANK.indexOf(c.status) > RANK.indexOf(worst) ? c.status : worst),
    testCases.length ? 'validated_fixed' : 'unlinked',
  )
  return { ...i, verification: { status, testCases } }
}

function findIssue(params: Record<string, string | readonly string[] | undefined>, min: MemberRole) {
  const id = pathId(params.issueId)
  if (id === undefined) return validation('issueId', 'must be a positive integer')
  const p = visibleProject(params.projectKey)
  if (p instanceof Response) return p
  const denied = requireRole(p.id, min, () => notFound(`project ${p.key}`))
  if (denied) return denied
  return db.issues.find((i) => i.projectId === p.id && i.id === id) ?? notFound(`issue ${id}`)
}

const stepsOf = (tcId: number) =>
  db.steps.filter((s) => s.testCaseId === tcId).sort((a, b) => a.position - b.position)

export const handlers = [
  http.get(
    `${BASE}/test-runs/:testRunId/live`,
    guard(({ params }) => {
      const run = findRun(params.testRunId)
      if (run instanceof Response) return run
      const live = db.live[run.id]
      return live ? respond(live) : problem(409, 'conflict', `run ${run.id} is not a live run`)
    }),
  ),
  http.get(
    `${BASE}/projects/:projectKey/quality`,
    guard(({ params, request }) => {
      const q = new URL(request.url).searchParams
      const bounded = (name: string, fallback: number, max: number) => {
        const raw = q.get(name)
        if (raw === null) return fallback
        const n = Number(raw)
        return Number.isInteger(n) && n >= 1 && n <= max ? n : undefined
      }
      const staleDays = bounded('staleDays', 14, 365)
      if (staleDays === undefined) return validation('staleDays', 'must be 1 to 365')
      const window = bounded('window', 20, 200)
      if (window === undefined) return validation('window', 'must be 1 to 200')
      const p = visibleProject(params.projectKey)
      if (p instanceof Response) return p
      const active = db.testCases.filter((t) => t.projectId === p.id && t.status === 'active')
      const automated = active.filter((t) => t.automated).length
      // Last execution: the newest run with a result for the test case, like the server.
      const lastRun = (id: number) =>
        db.results
          .filter((r) => r.testCaseId === id)
          .map((r) => db.runs.find((run) => run.id === r.testRunId)?.createdAt ?? '')
          .sort()
          .at(-1)
      const never = active.filter((t) => lastRun(t.id) === undefined)
      const cutoff = Date.now() - staleDays * 24 * 3600 * 1000
      const stale = active
        .map((t) => ({ t, last: lastRun(t.id) }))
        .filter(
          (x): x is { t: (typeof active)[number]; last: string } =>
            x.last !== undefined && Date.parse(x.last) < cutoff,
        )
        .sort((a, b) => a.last.localeCompare(b.last))
      return respond({
        testCases: {
          active: active.length,
          automated,
          manual: active.length - automated,
          automationRate: active.length ? Math.round((automated * 10000) / active.length) / 100 : 0,
        },
        execution: {
          staleDays,
          neverExecuted: never.length,
          stale: stale.length,
          testCases: [
            ...never.map((t) => ({ testCaseId: t.id, testCaseKey: t.key, lastExecutedAt: null })),
            ...stale.map(({ t, last }) => ({ testCaseId: t.id, testCaseKey: t.key, lastExecutedAt: last })),
          ],
        },
        flaky: {
          window,
          testCases: Object.entries(db.flaky).map(([id, runs]) => ({
            testCaseId: Number(id),
            testCaseKey: `TC-${id}`,
            runs,
          })),
        },
      })
    }),
  ),
  http.get(
    `${BASE}/projects/:projectKey/issues`,
    guard(({ params, request }) => {
      const p = visibleProject(params.projectKey)
      if (p instanceof Response) return p
      const q = new URL(request.url).searchParams
      const raw = q.get('testCase')
      const testCase = raw === null ? undefined : pathId(raw)
      if (raw !== null && testCase === undefined) return validation('testCase', 'must be a positive integer')
      const state = q.get('state')
      if (state !== null && state !== 'open' && state !== 'closed')
        return validation('state', 'must be one of open, closed')
      const items = db.issues
        .filter(
          (i) =>
            i.projectId === p.id &&
            (testCase === undefined || i.testCaseIds.includes(testCase)) &&
            (state === null || i.state === state),
        )
        .sort((a, b) => b.id - a.id)
        .map(issueDto)
      return respond(
        pageWithCounts(new URL(request.url), items, 'verificationCounts', (i) => i.verification.status),
      )
    }),
  ),
  http.post(
    `${BASE}/projects/:projectKey/issues`,
    guard(
      jsonGuard(async ({ params, request }) => {
        const p = visibleProject(params.projectKey)
        if (p instanceof Response) return p
        const denied = requireRole(p.id, 'member', () => notFound(`project ${p.key}`))
        if (denied) return denied
        const body = await readBody(request)
        const title = typeof body?.title === 'string' ? body.title.trim() : ''
        if (!title) return validation('title', 'must not be empty')
        const provider = String(body?.provider ?? 'provenly')
        if (!PROVIDERS.includes(provider)) return validation('provider', 'is not a provider')
        const native = provider === 'provenly'
        const externalId = native
          ? `I-${db.issues.filter((i) => i.projectId === p.id && i.provider === 'provenly').length + 1}`
          : String(body?.externalId ?? '')
        if (!native && !/^[A-Za-z0-9][A-Za-z0-9._#/-]{0,99}$/.test(externalId))
          return validation('externalId', 'must be the id in the tracker')
        if (
          db.issues.some(
            (i) => i.projectId === p.id && i.provider === provider && i.externalId === externalId,
          )
        )
          return problem(409, 'conflict', `issue ${externalId} of ${provider} already exists`)
        const state = body?.state === 'closed' ? 'closed' : 'open'
        const i = {
          projectId: p.id,
          id: ++db.nextId,
          provider: provider as Issue['provider'],
          externalId,
          title,
          description: String(body?.description ?? ''),
          url: String(body?.url ?? ''),
          state: state as Issue['state'],
          providerStatus: String(body?.providerStatus ?? ''),
          closedAt: state === 'closed' ? now() : null,
          lastSyncedAt: null,
          createdAt: now(),
          updatedAt: now(),
          testCaseIds: [],
          verification: { status: 'unlinked' as const, testCases: [] },
        }
        db.issues.push(i)
        return respond(issueDto(i), 201)
      }),
    ),
  ),
  http.get(
    `${BASE}/projects/:projectKey/issues/:issueId`,
    guard(({ params }) => {
      const i = findIssue(params, 'viewer')
      return i instanceof Response ? i : respond(issueDto(i))
    }),
  ),
  http.patch(
    `${BASE}/projects/:projectKey/issues/:issueId`,
    guard(
      jsonGuard(async ({ params, request }) => {
        const i = findIssue(params, 'member')
        if (i instanceof Response) return i
        const body = await readBody(request)
        if (!body || Object.keys(body).length === 0)
          return validation('body', 'at least one field is required')
        if (body.state !== undefined && body.state !== 'open' && body.state !== 'closed')
          return validation('state', 'must be one of open, closed')
        if (body.state === 'closed') i.closedAt ??= now()
        if (body.state === 'open') i.closedAt = null
        if (body.state !== undefined) i.state = body.state as Issue['state']
        i.updatedAt = now()
        return respond(issueDto(i))
      }),
    ),
  ),
  http.put(
    `${BASE}/projects/:projectKey/issues/:issueId/test-cases`,
    guard(
      jsonGuard(async ({ params, request }) => {
        const i = findIssue(params, 'member')
        if (i instanceof Response) return i
        const body = await readBody(request)
        if (!Array.isArray(body?.testCaseIds)) return validation('testCaseIds', 'is required')
        const ids = suiteMembers(i.projectId, body.testCaseIds)
        if (ids instanceof Response) return ids
        i.testCaseIds = ids.sort((a, b) => a - b)
        return respond(issueDto(i))
      }),
    ),
  ),
  http.get(
    `${BASE}/projects/:projectKey/requirements`,
    guard(({ params, request }) => {
      const p = visibleProject(params.projectKey)
      if (p instanceof Response) return p
      const raw = new URL(request.url).searchParams.get('testCase')
      const testCase = raw === null ? undefined : pathId(raw)
      if (raw !== null && testCase === undefined) return validation('testCase', 'must be a positive integer')
      const items = db.requirements
        .filter((r) => r.projectId === p.id && (testCase === undefined || r.testCaseIds.includes(testCase)))
        .sort((a, b) => b.id - a.id)
        .map(requirementDto)
      const url = new URL(request.url)
      const page = pageWithCounts(url, items, 'coverageCounts', (r) => r.coverage.status)
      if (page instanceof Response) return page
      // Archived requirements are not counted.
      const counts: Record<string, number> = {}
      for (const r of items.filter((x) => !x.archivedAt))
        counts[r.coverage.status] = (counts[r.coverage.status] ?? 0) + 1
      return respond({ ...(page as object), coverageCounts: counts } as JsonBodyType)
    }),
  ),
  http.post(
    `${BASE}/projects/:projectKey/requirements`,
    guard(
      jsonGuard(async ({ params, request }) => {
        const p = visibleProject(params.projectKey)
        if (p instanceof Response) return p
        const denied = requireRole(p.id, 'member', () => notFound(`project ${p.key}`))
        if (denied) return denied
        const body = await readBody(request)
        const title = typeof body?.title === 'string' ? body.title.trim() : ''
        if (!title) return validation('title', 'must not be empty')
        const provider = String(body?.provider ?? 'provenly')
        if (!PROVIDERS.includes(provider)) return validation('provider', 'is not a provider')
        const native = provider === 'provenly'
        const externalId = native
          ? `R-${db.requirements.filter((r) => r.projectId === p.id && r.provider === 'provenly').length + 1}`
          : String(body?.externalId ?? '')
        if (!native && !/^[A-Za-z0-9][A-Za-z0-9._#/-]{0,99}$/.test(externalId))
          return validation('externalId', 'must be the id in the provider')
        if (
          db.requirements.some(
            (r) => r.projectId === p.id && r.provider === provider && r.externalId === externalId,
          )
        )
          return problem(409, 'conflict', `requirement ${externalId} of ${provider} already exists`)
        const r = {
          projectId: p.id,
          id: ++db.nextId,
          provider: provider as Requirement['provider'],
          externalId,
          title,
          description: String(body?.description ?? ''),
          url: String(body?.url ?? ''),
          providerStatus: String(body?.providerStatus ?? ''),
          archivedAt: null,
          lastSyncedAt: null,
          createdAt: now(),
          updatedAt: now(),
          testCaseIds: [],
          coverage: {
            status: 'uncovered' as const,
            linked: 0,
            passed: 0,
            failed: 0,
            notRun: 0,
            testCases: [],
          },
        }
        db.requirements.push(r)
        return respond(requirementDto(r), 201)
      }),
    ),
  ),
  http.get(
    `${BASE}/projects/:projectKey/requirements/:requirementId`,
    guard(({ params }) => {
      const r = findRequirement(params, 'viewer')
      return r instanceof Response ? r : respond(requirementDto(r))
    }),
  ),
  http.patch(
    `${BASE}/projects/:projectKey/requirements/:requirementId`,
    guard(
      jsonGuard(async ({ params, request }) => {
        const r = findRequirement(params, 'member')
        if (r instanceof Response) return r
        const body = await readBody(request)
        if (!body || Object.keys(body).length === 0)
          return validation('body', 'at least one field is required')
        if (body.title !== undefined && !String(body.title).trim())
          return validation('title', 'must not be empty')
        if (body.title !== undefined) r.title = String(body.title).trim()
        if (body.archived === true) r.archivedAt ??= now()
        if (body.archived === false) r.archivedAt = null
        r.updatedAt = now()
        return respond(requirementDto(r))
      }),
    ),
  ),
  http.put(
    `${BASE}/projects/:projectKey/requirements/:requirementId/test-cases`,
    guard(
      jsonGuard(async ({ params, request }) => {
        const r = findRequirement(params, 'member')
        if (r instanceof Response) return r
        const body = await readBody(request)
        if (!Array.isArray(body?.testCaseIds)) return validation('testCaseIds', 'is required')
        const ids = suiteMembers(r.projectId, body.testCaseIds)
        if (ids instanceof Response) return ids
        r.testCaseIds = ids.sort((a, b) => a - b)
        return respond(requirementDto(r))
      }),
    ),
  ),
  http.post(
    `${BASE}/test-runs/manual`,
    guard(
      jsonGuard(async ({ request }) => {
        const body = await readBody(request)
        const key = String(body?.project ?? '')
        if (!KEY.test(key)) return validation('project', PROJECT_KEY_MESSAGE)
        const name = typeof body?.name === 'string' ? body.name.trim() : ''
        if (!name) return validation('name', 'is required')
        const scope = body?.scope ?? 'manual'
        if (scope !== 'manual' && scope !== 'all') return validation('scope', 'must be one of manual, all')
        const p = db.projects.find((x) => x.key === key)
        if (!p) return notFound(`project ${key}`)
        const denied = requireRole(p.id, 'member', () => notFound(`project ${key}`))
        if (denied) return denied
        let suite: MockDb['suites'][number] | undefined
        if (body?.suite !== undefined) {
          suite = db.suites.find((x) => x.projectId === p.id && x.key === body.suite)
          if (!suite) return notFound(`suite ${String(body.suite)}`)
          if (suite.archivedAt) return problem(409, 'conflict', `suite ${suite.key} is archived`)
        }
        const expected = db.testCases.filter(
          (t) =>
            t.projectId === p.id &&
            t.status === 'active' &&
            (scope === 'all' || !t.automated) &&
            (!suite || suite.kind !== 'static' || suite.members.includes(t.id)) &&
            (!suite || suite.kind !== 'query' || !suite.query?.tag || t.tags.includes(suite.query.tag)),
        )
        const id = ++db.nextId
        const run = testRun({
          id,
          projectId: p.id,
          externalRunId: `manual:${id}:1`,
          provider: 'manual',
          providerRunId: String(id),
          pipeline: name,
          branch: String(body?.branch ?? ''),
          commit: '',
          executionStatus: 'running',
          mode: 'manual',
          startedBy: currentUser().username,
          suite: suite ? { key: suite.key, name: suite.name } : null,
          expectedCount: expected.length,
          resultCount: 0,
          startedAt: now(),
          completedAt: null,
          createdAt: now(),
          outcome: {
            verdict: expected.length ? 'incomplete' : 'no_tests',
            executed: 0,
            passed: 0,
            failed: 0,
            error: 0,
            skipped: 0,
            untested: expected.length,
            passRate: 0,
            flaky: 0,
          },
        })
        db.runs.push(run)
        db.summaries[id] = manualSummary(
          id,
          expected.map((t) => ({
            testCaseId: t.id,
            testCaseKey: t.key,
            status: 'untested',
            resultCount: 0,
            flaky: false,
          })),
        )
        return respond(run, 201)
      }),
    ),
  ),
  http.post(
    `${BASE}/test-runs/:testRunId/manual-results`,
    guard(
      jsonGuard(async ({ params, request }) => {
        const run = findRun(params.testRunId)
        if (run instanceof Response) return run
        const body = await readBody(request)
        if (!RESULT_STATUSES.includes(String(body?.status)))
          return validation('status', 'must be one of passed, failed, error, skipped')
        const denied = requireRole(run.projectId, 'member', () => notFound(`test run ${run.id}`))
        if (denied) return denied
        if (run.mode !== 'manual' || run.executionStatus !== 'running')
          return problem(409, 'conflict', `run ${run.id} takes no more results`)
        const sum = db.summaries[run.id]
        const outcome = sum.testCases.find((c) => c.testCaseId === body?.testCaseId)
        if (!outcome)
          return problem(
            409,
            'conflict',
            `test case ${String(body?.testCaseId)} is not expected in run ${run.id}`,
          )
        outcome.status = body!.status as Outcome['status']
        outcome.resultCount++
        db.summaries[run.id] = manualSummary(run.id, sum.testCases)
        const result = testResult({
          id: ++db.nextId,
          testRunId: run.id,
          testCaseId: outcome.testCaseId,
          testCaseKey: outcome.testCaseKey,
          requestedTestCaseId: outcome.testCaseKey,
          testName: outcome.testCaseKey ?? '',
          className: 'provenly-manual',
          suiteName: '',
          status: body!.status as TestResult['status'],
          errorMessage: String(body?.note ?? ''),
          attempt: outcome.resultCount,
          recordedBy: currentUser().username,
          failedStep: typeof body?.failedStep === 'number' ? body.failedStep : null,
          durationMs: null,
        })
        db.results.push(result)
        run.resultCount++
        run.outcome = outcomeOf(db.summaries[run.id])
        return respond(result, 201)
      }),
    ),
  ),
  http.post(
    `${BASE}/test-runs/:testRunId/finish`,
    guard(
      jsonGuard(async ({ params, request }) => {
        const run = findRun(params.testRunId)
        if (run instanceof Response) return run
        const status = (await readBody(request))?.status
        if (status !== 'completed' && status !== 'cancelled')
          return validation('status', 'must be one of completed, cancelled')
        const denied = requireRole(run.projectId, 'member', () => notFound(`test run ${run.id}`))
        if (denied) return denied
        if (run.mode !== 'manual' || run.executionStatus !== 'running')
          return problem(409, 'conflict', `run ${run.id} is ${run.executionStatus}`)
        run.executionStatus = status
        run.completedAt = now()
        return respond(run)
      }),
    ),
  ),
  http.get(
    `${BASE}/projects/:projectKey/suites`,
    guard(({ params, request }) => {
      const p = visibleProject(params.projectKey)
      if (p instanceof Response) return p
      const items = db.suites
        .filter((x) => x.projectId === p.id)
        .sort((a, b) => a.key.localeCompare(b.key))
        .map((x) => suiteDto(x, false))
      return respond(pageOf(new URL(request.url), items))
    }),
  ),
  http.post(
    `${BASE}/projects/:projectKey/suites`,
    guard(
      jsonGuard(async ({ params, request }) => {
        const p = maintainedProject(params.projectKey)
        if (p instanceof Response) return p
        const body = await readBody(request)
        const input = keyAndName(body, DIMENSION)
        if (input instanceof Response) return input
        const kind = body?.kind
        if (kind !== 'static' && kind !== 'query') return validation('kind', 'must be one of static, query')
        const query = (body?.query ?? null) as Suite['query']
        if (kind === 'query' && !query?.tag && !(query?.classification ?? []).length)
          return validation('query', 'a query suite needs a tag or a dimension:value pair')
        const members = suiteMembers(p.id, body?.testCaseIds)
        if (members instanceof Response) return members
        if (db.suites.some((x) => x.projectId === p.id && x.key === input.key))
          return problem(409, 'conflict', `suite ${input.key} already exists`)
        const su = {
          projectId: p.id,
          id: ++db.nextId,
          members: kind === 'static' ? members : [],
          ...input,
          description: String(body?.description ?? ''),
          kind,
          query:
            kind === 'query'
              ? { tag: query?.tag ?? null, classification: query?.classification ?? [] }
              : null,
          archivedAt: null,
          createdAt: now(),
          updatedAt: now(),
          caseCount: 0,
        } satisfies MockDb['suites'][number]
        db.suites.push(su)
        return respond(suiteDto(su, true), 201)
      }),
    ),
  ),
  http.get(
    `${BASE}/projects/:projectKey/suites/:suiteKey`,
    guard(({ params }) => {
      const su = findSuite(params, false)
      return su instanceof Response ? su : respond(suiteDto(su, true))
    }),
  ),
  http.patch(
    `${BASE}/projects/:projectKey/suites/:suiteKey`,
    guard(
      jsonGuard(async ({ params, request }) => {
        const su = findSuite(params, true)
        if (su instanceof Response) return su
        const body = await readBody(request)
        if (body?.query !== undefined && su.kind !== 'query')
          return validation('query', 'a static suite lists its test cases instead')
        const invalid = renameOrArchive(su, body)
        if (invalid) return invalid
        if (body?.query !== undefined) su.query = body.query as Suite['query']
        su.updatedAt = now()
        return respond(suiteDto(su, true))
      }),
    ),
  ),
  http.put(
    `${BASE}/projects/:projectKey/suites/:suiteKey/cases`,
    guard(
      jsonGuard(async ({ params, request }) => {
        const su = findSuite(params, true)
        if (su instanceof Response) return su
        const body = await readBody(request)
        if (!Array.isArray(body?.testCaseIds)) return validation('testCaseIds', 'is required')
        if (su.kind !== 'static')
          return validation('testCaseIds', 'a query suite selects its test cases with its query')
        const members = suiteMembers(su.projectId, body.testCaseIds)
        if (members instanceof Response) return members
        su.members = members
        return respond(suiteDto(su, true))
      }),
    ),
  ),
  http.get(
    `${BASE}/projects/:projectKey/dimensions`,
    guard(({ params, request }) => {
      const p = visibleProject(params.projectKey)
      if (p instanceof Response) return p
      return respond(
        pageOf(new URL(request.url), db.dimensions.filter((d) => d.projectId === p.id).map(dimensionDto)),
      )
    }),
  ),
  http.post(
    `${BASE}/projects/:projectKey/dimensions`,
    guard(
      jsonGuard(async ({ params, request }) => {
        const p = maintainedProject(params.projectKey)
        if (p instanceof Response) return p
        const input = keyAndName(await readBody(request), DIMENSION)
        if (input instanceof Response) return input
        if (db.dimensions.some((d) => d.projectId === p.id && d.key === input.key))
          return problem(409, 'conflict', `dimension ${input.key} already exists`)
        const d = { projectId: p.id, ...input, builtIn: false, archivedAt: null, values: [] }
        db.dimensions.push(d)
        return respond(dimensionDto(d), 201)
      }),
    ),
  ),
  http.patch(
    `${BASE}/projects/:projectKey/dimensions/:dimensionKey`,
    guard(
      jsonGuard(async ({ params, request }) => {
        const d = maintainedDimension(params)
        if (d instanceof Response) return d
        return renameOrArchive(d, await readBody(request)) ?? respond(dimensionDto(d))
      }),
    ),
  ),
  http.post(
    `${BASE}/projects/:projectKey/dimensions/:dimensionKey/values`,
    guard(
      jsonGuard(async ({ params, request }) => {
        const d = maintainedDimension(params)
        if (d instanceof Response) return d
        const input = keyAndName(await readBody(request), VALUE)
        if (input instanceof Response) return input
        if (d.values.some((v) => v.key === input.key))
          return problem(409, 'conflict', `${d.key} already has the value ${input.key}`)
        d.values.push({ ...input, archivedAt: null })
        return respond(dimensionDto(d), 201)
      }),
    ),
  ),
  http.patch(
    `${BASE}/projects/:projectKey/dimensions/:dimensionKey/values/:valueKey`,
    guard(
      jsonGuard(async ({ params, request }) => {
        if (!VALUE.test(String(params.valueKey))) return validation('valueKey', 'must be a value key')
        const d = maintainedDimension(params)
        if (d instanceof Response) return d
        const v = d.values.find((x) => x.key === params.valueKey)
        if (!v) return notFound(`value ${String(params.valueKey)} of dimension ${d.key}`)
        return renameOrArchive(v, await readBody(request)) ?? respond(dimensionDto(d))
      }),
    ),
  ),
  http.get(
    `${BASE}/projects/:projectKey/members`,
    guard(({ params, request }) => {
      if (!KEY.test(String(params.projectKey))) return validation('projectKey', PROJECT_KEY_MESSAGE)
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
        if (!KEY.test(String(params.projectKey))) return validation('projectKey', PROJECT_KEY_MESSAGE)
        const p = db.projects.find((x) => x.key === params.projectKey)
        if (!p || !roleIn(p.id)) return notFound(`project ${String(params.projectKey)}`)
        if (roleIn(p.id) !== 'admin' && roleIn(p.id) !== 'maintainer')
          return problem(403, 'forbidden', 'this needs the maintainer role in the project')
        const role = (await readBody(request))?.role
        if (!memberRoleOk(role)) return validation('role', 'must be one of maintainer, member, viewer')
        const u = db.users.find((x) => x.username === params.username)
        if (!u) return notFound(`user ${String(params.username)}`)
        if (u.deactivatedAt)
          return problem(409, 'conflict', `${u.username} is deactivated: reactivate the account first`)
        db.members = db.members.filter((m) => !(m.projectId === p.id && m.userId === u.id))
        db.members.push({ projectId: p.id, userId: u.id, role, since: now() })
        return respond({ user: u, role, since: now() })
      }),
    ),
  ),
  http.delete(
    `${BASE}/projects/:projectKey/members/:username`,
    guard(({ params }) => {
      if (!KEY.test(String(params.projectKey))) return validation('projectKey', PROJECT_KEY_MESSAGE)
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
        const id = ++db.nextId
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
  http.get(
    `${BASE}/audit`,
    guard(({ request }) => {
      const url = new URL(request.url)
      const project = url.searchParams.get('project')
      const actor = url.searchParams.get('actor')
      const testCase = url.searchParams.get('testCase')
      if (project !== null && !KEY.test(project)) return validation('project', PROJECT_KEY_MESSAGE)
      if (actor === '') return validation('actor', 'must not be empty')
      if (testCase !== null && !/^[A-Z][A-Z0-9]{1,9}-[1-9][0-9]{0,17}$/.test(testCase))
        return validation('testCase', 'must be a test case key (e.g. CHK-4)')
      if (!currentUser().isAdmin) return forbidden()
      const items = db.audit
        .filter(
          (e) =>
            (project === null || e.project === project) &&
            (actor === null || e.actor === actor) &&
            (testCase === null || e.testCase === testCase),
        )
        .reverse()
      return respond(pageOf(url, items))
    }),
  ),
  http.get(
    `${BASE}/projects/:projectKey/webhooks`,
    guard(({ params, request }) => {
      const p = integrationProject(params.projectKey)
      if (p instanceof Response) return p
      return respond(
        pageOf(new URL(request.url), db.webhooks.filter((w) => w.projectId === p.id).map(webhookView)),
      )
    }),
  ),
  http.post(
    `${BASE}/projects/:projectKey/webhooks`,
    guard(
      jsonGuard(async ({ params, request }) => {
        const body = await readBody(request)
        const url = String(body?.url ?? '')
        if (!WEBHOOK_URL.test(url)) return validation('url', 'must be an https URL without credentials')
        const p = integrationProject(params.projectKey)
        if (p instanceof Response) return p
        const w = {
          id: ++db.nextId,
          projectId: p.id,
          url,
          events: ['run.completed' as const],
          active: true,
          createdBy: db.users.find((u) => u.id === db.session)!.username,
          createdAt: now(),
          updatedAt: now(),
        }
        db.webhooks.push(w)
        return respond({ webhook: webhookView(w), secret: `whsec_${'0'.repeat(48)}` }, 201)
      }),
    ),
  ),
  http.patch(
    `${BASE}/projects/:projectKey/webhooks/:webhookId`,
    guard(
      jsonGuard(async ({ params, request }) => {
        const body = (await readBody(request)) ?? {}
        if (Object.keys(body).length === 0) return validation('body', 'at least one field is required')
        const p = integrationProject(params.projectKey)
        if (p instanceof Response) return p
        const w = webhookOf(p, params.webhookId)
        if (w instanceof Response) return w
        if (typeof body.active === 'boolean') w.active = body.active
        w.updatedAt = now()
        return respond(webhookView(w))
      }),
    ),
  ),
  http.post(
    `${BASE}/projects/:projectKey/webhooks/:webhookId/ping`,
    guard(({ params }) => {
      const p = integrationProject(params.projectKey)
      if (p instanceof Response) return p
      const w = webhookOf(p, params.webhookId)
      if (w instanceof Response) return w
      const d: WebhookDelivery = {
        id: ++db.nextId,
        webhookId: w.id,
        event: 'ping',
        status: 'pending',
        attempts: 0,
        nextAttemptAt: now(),
        lastStatusCode: null,
        lastError: null,
        createdAt: now(),
        completedAt: null,
        payload: { event: 'ping' },
      }
      db.deliveries.push(d)
      return respond(d, 202)
    }),
  ),
  http.get(
    `${BASE}/projects/:projectKey/webhooks/:webhookId/deliveries`,
    guard(({ params, request }) => {
      const p = integrationProject(params.projectKey)
      if (p instanceof Response) return p
      const w = webhookOf(p, params.webhookId)
      if (w instanceof Response) return w
      const items = db.deliveries.filter((d) => d.webhookId === w.id).reverse()
      return respond(pageOf(new URL(request.url), items))
    }),
  ),
  http.get(
    `${BASE}/projects/:projectKey/github`,
    guard(({ params }) => {
      const p = integrationProject(params.projectKey)
      if (p instanceof Response) return p
      const c = db.github.find((x) => x.projectId === p.id)
      return c ? respond(githubView(c)) : notFound(`GitHub connection of ${p.key}`)
    }),
  ),
  http.put(
    `${BASE}/projects/:projectKey/github`,
    guard(
      jsonGuard(async ({ params, request }) => {
        const body = (await readBody(request)) ?? {}
        const repository = String(body.repository ?? '')
        if (!/^[\w.-]+\/[\w.-]+$/.test(repository) || repository.split('/').some((x) => /^\.+$/.test(x)))
          return validation('repository', 'must be owner/name')
        const p = integrationProject(params.projectKey)
        if (p instanceof Response) return p
        const current = db.github.find((x) => x.projectId === p.id)
        const token = typeof body.token === 'string' ? body.token : undefined
        if (!current && !token) return validation('token', 'is required to connect')
        if (current && !token && current.repository.toLowerCase() !== repository.toLowerCase())
          return validation('token', 'is required to change the repository')
        const c = {
          projectId: p.id,
          repository,
          labels: String(body.labels ?? ''),
          tokenHint: token ? (token.length >= 12 ? `…${token.slice(-4)}` : '…') : current!.tokenHint,
          lastSyncedAt: current?.lastSyncedAt ?? null,
          lastError: null,
          updatedAt: now(),
        }
        db.github = [...db.github.filter((x) => x.projectId !== p.id), c]
        return respond(githubView(c))
      }),
    ),
  ),
  http.delete(
    `${BASE}/projects/:projectKey/github`,
    guard(({ params }) => {
      const p = integrationProject(params.projectKey)
      if (p instanceof Response) return p
      const before = db.github.length
      db.github = db.github.filter((x) => x.projectId !== p.id)
      if (db.github.length === before) return notFound(`GitHub connection of ${p.key}`)
      return new HttpResponse(null, { status: 204 })
    }),
  ),
  http.post(
    `${BASE}/projects/:projectKey/github/sync`,
    guard(({ params }) => {
      const p = integrationProject(params.projectKey)
      if (p instanceof Response) return p
      const c = db.github.find((x) => x.projectId === p.id)
      if (!c) return notFound(`GitHub connection of ${p.key}`)
      if (!c.tokenHint)
        return problem(409, 'conflict', 'the GitHub token cannot be read; connect again with a new token')
      if (c.repository === 'acme/down') {
        c.lastError = `GitHub answered 503 for ${c.repository}`
        return problem(502, 'upstream_error', c.lastError)
      }
      c.lastSyncedAt = now()
      c.lastError = null
      return respond({ created: 2, updated: 1 })
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
        if ((db.loginFailures[username] ?? 0) >= 5)
          return problem(
            429,
            'too_many_requests',
            'too many failed sign-ins for this username; try again in 15 minutes',
          )
        if (!u || db.passwords[username] !== body?.password || u.deactivatedAt) {
          db.loginFailures[username] = (db.loginFailures[username] ?? 0) + 1
          return problem(401, 'unauthorized', 'invalid username or password')
        }
        delete db.loginFailures[username]
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
        const tooShort = (p: string) => [...p].length < 10
        const tooLong = (p: string) => new TextEncoder().encode(p).length > 72
        if (tooShort(next)) return validation('newPassword', 'must be at least 10 characters')
        if (tooLong(next))
          return validation('newPassword', 'must be at most 72 bytes (letters outside ASCII count as 2 to 4)')
        if (db.passwords[u.username] !== body?.currentPassword)
          return validation('currentPassword', 'is not your current password')
        db.passwords[u.username] = next
        return respond(session(u))
      }),
    ),
  ),
  http.post(
    `${BASE}/users/:username/:action`,
    guard(({ params }) => {
      if (!currentUser().isAdmin) return forbidden()
      const u = db.users.find((x) => x.username === params.username)
      if (!u) return notFound(`user ${String(params.username)}`)
      if (params.action === 'deactivate') {
        if (u.id === currentUser().id)
          return problem(409, 'conflict', 'you cannot deactivate yourself: ask another administrator')
        u.deactivatedAt = u.deactivatedAt ?? now()
        for (const [t, name] of Object.entries(db.resetTokens))
          if (name === u.username) delete db.resetTokens[t]
        return respond(u)
      }
      if (params.action === 'reactivate') {
        u.deactivatedAt = null
        return respond(u)
      }
      if (u.deactivatedAt)
        return problem(409, 'conflict', `${u.username} is deactivated: reactivate the account first`)
      const token = `reset-${++db.nextId}`
      db.resetTokens[token] = u.username
      return respond({ username: u.username, token, expiresAt: now() }, 201)
    }),
  ),
  http.post(
    `${BASE}/password-reset`,
    publicGuard(
      jsonGuard(async ({ request }) => {
        const body = await readBody(request)
        const password = String(body?.password ?? '')
        if ([...password].length < 10) return validation('password', 'must be at least 10 characters')
        const username = db.resetTokens[String(body?.token ?? '')]
        const u = db.users.find((x) => x.username === username)
        if (!u || u.deactivatedAt)
          return problem(404, 'not_found', 'password reset link not found, expired or already used')
        delete db.resetTokens[String(body?.token)]
        db.passwords[u.username] = password
        db.session = u.id
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
        if ([...password].length < 10) return validation('password', 'must be at least 10 characters')
        if (new TextEncoder().encode(password).length > 72)
          return validation('password', 'must be at most 72 bytes (letters outside ASCII count as 2 to 4)')
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
        if (!KEY.test(key)) return validation('key', PROJECT_KEY_MESSAGE)
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
        db.dimensions.push(...seedDimensions(p.id))
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
      const tag = url.searchParams.get('tag')
      if (tag !== null && !TAG.test(tag)) return validation('tag', 'is not a valid tag')
      const classified = url.searchParams.get('classification')
      if (classified !== null && !CLASSIFIED.test(classified))
        return validation('classification', 'must be dimension:value pairs')
      const suiteKey = url.searchParams.get('suite')
      let suite: MockDb['suites'][number] | undefined
      if (suiteKey !== null) {
        if (!DIMENSION.test(suiteKey)) return validation('suite', 'must be a suite key')
        if (!p || tag !== null || classified !== null)
          return validation('suite', 'needs ?project= and cannot be combined with tag or classification')
        suite = db.suites.find((x) => x.projectId === p.id && x.key === suiteKey)
        if (!suite) return notFound(`suite ${suiteKey}`)
      }
      const inSuite = (t: TestCase) =>
        !suite ||
        (suite.kind === 'static'
          ? suite.members.includes(t.id)
          : (!suite.query?.tag || t.tags.includes(suite.query.tag)) &&
            (suite.query?.classification ?? []).every(
              (pair) => t.classification[pair.split(':')[0]] === pair.split(':')[1],
            ))
      const pairs = (classified ?? '').split(',').filter(Boolean)
      const tcKey = url.searchParams.get('key')
      if (tcKey !== null && !/^[A-Z][A-Z0-9]{1,9}-[1-9][0-9]{0,17}$/.test(tcKey))
        return validation('key', 'must be a test case key: <PROJECT>-<number> (e.g. CHK-12)')
      // ?q=: a title containing the text (any case), or a key or number, like the server (DEC-78).
      const search = url.searchParams.get('q')?.trim()
      if (search === '') return validation('q', 'must not be empty')
      const searchNumber = search?.match(/^(?:[A-Za-z][A-Za-z0-9]{1,9}-)?([1-9][0-9]{0,17})$/)?.[1]
      const found = (t: TestCase) =>
        search === undefined ||
        t.title.toLowerCase().includes(search.toLowerCase()) ||
        (searchNumber !== undefined && t.key.endsWith(`-${searchNumber}`))
      const items = db.testCases
        .filter(
          (t) =>
            found(t) &&
            (tcKey === null || t.key === tcKey) &&
            (!status || t.status === status) &&
            (!p || t.projectId === p.id) &&
            (tag === null || t.tags.includes(tag)) &&
            inSuite(t) &&
            pairs.every((pair) => `${pair.split(':')[0]}:${t.classification[pair.split(':')[0]]}` === pair),
        )
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
        if (!KEY.test(key)) return validation('project', PROJECT_KEY_MESSAGE)
        const p = db.projects.find((x) => x.key === key)
        if (!p) return notFound(`project ${key}`)
        const denied = requireRole(p.id, 'member', () => notFound(`project ${key}`))
        if (denied) return denied
        const tags = normalizeTags(body.tags)
        if (tags instanceof Response) return tags
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
          tags,
        })
        const invalid = classify(tc, body.classification)
        if (invalid) return invalid
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
        const { tags: rawTags, classification, ...content } = body
        const tags = rawTags === undefined ? tc.tags : normalizeTags(rawTags)
        if (tags instanceof Response) return tags
        const before = tc.classification
        const invalid = classify(tc, classification)
        if (invalid) {
          tc.classification = before
          return invalid
        }
        Object.assign(tc, content, { tags, updatedAt: now(), version: tc.version + 1 })
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
      const suite = url.searchParams.get('suite')
      if (suite !== null && !DIMENSION.test(suite)) return validation('suite', 'must be a suite key')
      return respond(
        pageOf(
          url,
          db.runs
            .filter((r) => (!p || r.projectId === p.id) && (suite === null || r.suite?.key === suite))
            .sort((a, b) => b.id - a.id),
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
      const shard = url.searchParams.get('shard')
      if (status && !['passed', 'failed', 'error', 'skipped'].includes(status))
        return validation('status', 'invalid')
      const items = db.results.filter(
        (r) =>
          r.testRunId === run.id &&
          (!status || r.status === status) &&
          (!correlation || r.correlation === correlation) &&
          (!shard || r.shard === Number(shard)),
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
    `${BASE}/test-runs/:testRunId/amendments`,
    guard(({ params, request }) => {
      const run = findRun(params.testRunId)
      if (run instanceof Response) return run
      return respond(
        pageOf(
          new URL(request.url),
          db.amendments.filter((a) => a.testRunId === run.id),
        ),
      )
    }),
  ),
  http.post(
    `${BASE}/test-runs/:testRunId/amendments`,
    guard(
      jsonGuard(async ({ params, request }) => {
        const run = findRun(params.testRunId)
        if (run instanceof Response) return run
        const denied = requireRole(run.projectId, 'maintainer', () => notFound(`test run ${run.id}`))
        if (denied) return denied
        const body = await readBody(request)
        const testCaseId = Number(body?.testCaseId)
        const reason = String(body?.reason ?? '').trim()
        if (!reason || reason.length > 500) return validation('reason', 'must be 1 to 500 characters')
        const s = (db.summaries[run.id] ??= summary({ testRunId: run.id }))
        if (s.amendedTestCaseIds.includes(testCaseId) || s.testCases.some((c) => c.testCaseId === testCaseId))
          return problem(409, 'conflict', `TC-ID ${testCaseId} is already in the universe of run ${run.id}`)
        if (!s.outsideUniverseTestCaseIds.includes(testCaseId))
          return validation('testCaseId', 'has no valid result in this run')
        const tc = db.testCases.find((t) => t.id === testCaseId)
        // Its logical result in this run: the highest attempt (the server reads it the same way).
        const latest = db.results
          .filter((r) => r.testRunId === run.id && r.testCaseId === testCaseId)
          .sort((a, b) => b.attempt - a.attempt)[0]
        const amendment: Amendment = {
          id: ++db.nextId,
          testRunId: run.id,
          testCaseId,
          testCaseKey: tc?.key ?? `TC-${testCaseId}`,
          amendedBy: db.session!,
          amendedByUsername: currentUser().username,
          reason,
          createdAt: now(),
        }
        db.amendments.push(amendment)
        const cases: Outcome[] = [
          ...s.testCases,
          {
            testCaseId,
            testCaseKey: amendment.testCaseKey,
            status: (latest?.status ?? 'passed') as Outcome['status'],
            resultCount: 1,
            flaky: false,
          },
        ]
        Object.assign(s, {
          outsideUniverse: Math.max(0, s.outsideUniverse - 1),
          outsideUniverseTestCaseIds: s.outsideUniverseTestCaseIds.filter((id) => id !== testCaseId),
          amendedTestCaseIds: [...s.amendedTestCaseIds, testCaseId].sort((a, b) => a - b),
          ...recount(cases),
        })
        Object.assign(run, {
          amendmentCount: run.amendmentCount + 1,
          expectedCount: run.expectedCount + 1,
          outcome: outcomeOf(s),
        })
        return respond(amendment, 201)
      }),
    ),
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
