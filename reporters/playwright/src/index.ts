// @provenly/playwright-reporter — the first native Provenly reporter (DEC-15).
//
// While Playwright runs, it starts a live run in Provenly and streams test.started / test.finished events; when the
// run ends it sends the authoritative JUnit report (one testcase per attempt, with the test's TC-ID and attempt as
// properties) through the normal ingestion, which completes the live run. Provenly being unreachable never fails
// the test run: the reporter logs and carries on.
//
// It has no dependencies: the Playwright types it uses are declared structurally below, so it works with any
// Playwright version that has the Reporter API (onBegin, onTestBegin, onTestEnd, onEnd).

/** The parts of a Playwright TestCase the reporter reads. */
export interface PwTestCase {
  id: string
  title: string
  titlePath(): string[]
  location: { file: string }
  annotations: { type: string; description?: string }[]
  tags?: string[]
}

/** The parts of a Playwright TestResult the reporter reads. */
export interface PwTestResult {
  retry: number
  status: 'passed' | 'failed' | 'timedOut' | 'skipped' | 'interrupted'
  duration: number
  startTime: Date
  errors: { message?: string; stack?: string }[]
}

/** The parts of a Playwright FullResult the reporter reads. */
export interface PwFullResult {
  status: 'passed' | 'failed' | 'timedout' | 'interrupted'
}

export interface ProvenlyReporterOptions {
  /** Base URL of Provenly (default PROVENLY_URL). Without it the reporter does nothing. */
  url?: string
  /** A project API key (default PROVENLY_API_KEY). */
  apiKey?: string
  /** Project key (default PROVENLY_PROJECT; else the API key's project). */
  project?: string
  /** Suite key the run executes (default PROVENLY_SUITE). */
  suite?: string
  provider?: string
  runId?: string
  runAttempt?: number
  pipeline?: string
  branch?: string
  commit?: string
  /** Stream live events while tests run (default true). */
  live?: boolean
  /** Events are sent in batches of this size (default 50) and when the run ends. */
  flushEvery?: number
  /** For tests: the fetch implementation, environment, clock and log. */
  fetch?: typeof fetch
  env?: Record<string, string | undefined>
  now?: () => Date
  log?: (message: string) => void
}

type Status = 'passed' | 'failed' | 'error' | 'skipped'

interface LiveEvent {
  eventId: string
  sequence: number
  type: 'test.started' | 'test.finished' | 'run.finished'
  testName?: string
  testCase?: string
  status?: Status
  occurredAt: string
  attempt?: number
}

interface Attempt {
  name: string
  className: string
  tcId: string
  attempt: number
  status: Status
  seconds: number
  message: string
  details: string
}

const TC_ID = /\b([A-Z][A-Z0-9]{1,9}-\d+)\b/g

/** The TC-ID a test declares: a `tc-id` annotation, a `@KEY-n` tag, or the last KEY-n in its title. */
export function tcIdOf(test: PwTestCase): string {
  const annotation = test.annotations.find((a) => a.type.toLowerCase() === 'tc-id' && a.description)
  if (annotation) return annotation.description!.trim()
  for (const tag of test.tags ?? []) {
    const m = /^@([A-Z][A-Z0-9]{1,9}-\d+)$/.exec(tag)
    if (m) return m[1]
  }
  const matches = [...test.title.matchAll(TC_ID)]
  return matches.length ? matches[matches.length - 1][1] : ''
}

/** Playwright's outcome as a Provenly result status. */
export function statusOf(status: PwTestResult['status']): Status {
  switch (status) {
    case 'passed':
      return 'passed'
    case 'skipped':
      return 'skipped'
    case 'interrupted':
      return 'error'
    default:
      return 'failed'
  }
}

const escapeXml = (s: string) =>
  s
    // eslint-disable-next-line no-control-regex
    .replace(/[\u0000-\u0008\u000B\u000C\u000E-\u001F]/g, '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')

/** The JUnit XML report of the attempts: one testcase per attempt, with its TC-ID and attempt as properties. */
export function junitXml(attempts: Attempt[], startedAt: Date): string {
  const cases = attempts.map((a) => {
    const props =
      `<properties>${a.tcId ? `<property name="tc-id" value="${escapeXml(a.tcId)}"/>` : ''}` +
      `<property name="attempt" value="${a.attempt}"/></properties>`
    const outcome =
      a.status === 'failed'
        ? `<failure message="${escapeXml(a.message)}">${escapeXml(a.details)}</failure>`
        : a.status === 'error'
          ? `<error message="${escapeXml(a.message)}">${escapeXml(a.details)}</error>`
          : a.status === 'skipped'
            ? '<skipped/>'
            : ''
    return `<testcase name="${escapeXml(a.name)}" classname="${escapeXml(a.className)}" time="${a.seconds.toFixed(3)}">${props}${outcome}</testcase>`
  })
  return (
    `<?xml version="1.0" encoding="UTF-8"?><testsuites><testsuite name="playwright" tests="${attempts.length}" ` +
    `timestamp="${startedAt.toISOString()}">${cases.join('')}</testsuite></testsuites>`
  )
}

export default class ProvenlyReporter {
  private readonly url: string
  private readonly apiKey: string
  private readonly opts: Required<Pick<ProvenlyReporterOptions, 'provider' | 'runId' | 'runAttempt' | 'live' | 'flushEvery'>> &
    ProvenlyReporterOptions
  private readonly fetch: typeof fetch
  private readonly now: () => Date
  private readonly log: (message: string) => void
  private runId = 0
  private sequence = 0
  private pending: LiveEvent[] = []
  private readonly attempts: Attempt[] = []
  private startedAt = new Date(0)
  private liveStart: Promise<void> = Promise.resolve()
  private sending: Promise<void> = Promise.resolve()

  constructor(options: ProvenlyReporterOptions = {}) {
    const env = options.env ?? process.env
    this.url = (options.url ?? env.PROVENLY_URL ?? '').replace(/\/+$/, '')
    this.apiKey = options.apiKey ?? env.PROVENLY_API_KEY ?? ''
    this.fetch = options.fetch ?? fetch
    this.now = options.now ?? (() => new Date())
    this.log = options.log ?? ((m) => console.warn(`[provenly] ${m}`))
    this.opts = {
      ...options,
      project: options.project ?? env.PROVENLY_PROJECT,
      suite: options.suite ?? env.PROVENLY_SUITE,
      provider: options.provider ?? (env.GITHUB_ACTIONS ? 'github' : 'local'),
      runId: options.runId ?? env.GITHUB_RUN_ID ?? String(this.now().getTime()),
      runAttempt: options.runAttempt ?? Number(env.GITHUB_RUN_ATTEMPT ?? 1),
      pipeline: options.pipeline ?? env.GITHUB_WORKFLOW,
      branch: options.branch ?? env.GITHUB_REF_NAME,
      commit: options.commit ?? env.GITHUB_SHA,
      live: options.live ?? true,
      flushEvery: options.flushEvery ?? 50,
    }
  }

  printsToStdio(): boolean {
    return false
  }

  private get enabled(): boolean {
    return this.url !== ''
  }

  private async call(method: string, path: string, body: string, contentType: string): Promise<Response> {
    const headers: Record<string, string> = { 'Content-Type': contentType }
    if (this.apiKey) headers.Authorization = `Bearer ${this.apiKey}`
    return this.fetch(`${this.url}${path}`, { method, headers, body })
  }

  onBegin(): void {
    this.startedAt = this.now()
    if (!this.enabled || !this.opts.live) return
    const body = JSON.stringify({
      project: this.opts.project,
      suite: this.opts.suite,
      provider: this.opts.provider,
      runId: this.opts.runId,
      runAttempt: this.opts.runAttempt,
      pipeline: this.opts.pipeline,
      branch: this.opts.branch,
      commit: this.opts.commit,
    })
    this.liveStart = this.call('POST', '/api/v1/test-runs/live', body, 'application/json')
      .then(async (res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        this.runId = ((await res.json()) as { id: number }).id
      })
      .catch((err: Error) => this.log(`live run not started (${err.message}); the final report is still sent`))
  }

  private event(type: LiveEvent['type'], key: string, test?: PwTestCase, status?: Status, attempt?: number): void {
    if (!this.enabled || !this.opts.live) return
    this.pending.push({
      eventId: key,
      sequence: ++this.sequence,
      type,
      testName: test?.titlePath().join(' › '),
      testCase: test ? tcIdOf(test) || undefined : undefined,
      status,
      occurredAt: this.now().toISOString(),
      attempt,
    })
    if (this.pending.length >= this.opts.flushEvery) this.flush()
  }

  /** Sends the pending events in order, once the live run started; failures are logged, never thrown. */
  private flush(): Promise<void> {
    const batch = this.pending
    this.pending = []
    this.sending = this.sending.then(async () => {
      await this.liveStart
      if (!this.runId || batch.length === 0) return
      try {
        const res = await this.call('POST', `/api/v1/test-runs/${this.runId}/events`, JSON.stringify({ events: batch }), 'application/json')
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
      } catch (err) {
        this.log(`live events not sent (${(err as Error).message})`)
      }
    })
    return this.sending
  }

  onTestBegin(test: PwTestCase, result: PwTestResult): void {
    this.event('test.started', `${test.id}:${result.retry}:started`, test, undefined, result.retry + 1)
  }

  onTestEnd(test: PwTestCase, result: PwTestResult): void {
    const status = statusOf(result.status)
    const error = result.errors[0] ?? {}
    this.attempts.push({
      name: test.titlePath().slice(1).join(' › ') || test.title,
      className: test.location.file,
      tcId: tcIdOf(test),
      attempt: result.retry + 1,
      status,
      seconds: result.duration / 1000,
      message: (error.message ?? '').split('\n')[0],
      details: error.stack ?? error.message ?? '',
    })
    this.event('test.finished', `${test.id}:${result.retry}:finished`, test, status, result.retry + 1)
  }

  async onEnd(result: PwFullResult): Promise<void> {
    if (!this.enabled) return
    this.event('run.finished', 'run:finished')
    await this.flush()
    const query = new URLSearchParams({
      provider: this.opts.provider,
      runId: this.opts.runId,
      runAttempt: String(this.opts.runAttempt),
    })
    for (const [k, v] of Object.entries({
      project: this.opts.project,
      suite: this.opts.suite,
      pipeline: this.opts.pipeline,
      branch: this.opts.branch,
      commit: this.opts.commit,
    })) {
      if (v) query.set(k, v)
    }
    if (result.status === 'interrupted' || result.status === 'timedout') query.set('status', 'interrupted')
    const xml = junitXml(this.attempts, this.startedAt)
    for (let attempt = 1; attempt <= 3; attempt++) {
      try {
        const res = await this.call('POST', `/api/v1/ingestion/junit?${query}`, xml, 'application/xml')
        if (res.ok) return
        if (res.status < 500) {
          this.log(`report rejected (HTTP ${res.status}): ${await res.text()}`)
          return
        }
        throw new Error(`HTTP ${res.status}`)
      } catch (err) {
        this.log(`report not sent, attempt ${attempt} of 3 (${(err as Error).message})`)
      }
    }
  }
}
