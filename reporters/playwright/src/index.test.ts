import { describe, expect, it, vi } from 'vitest'

import ProvenlyReporter, { junitXml, statusOf, tcIdOf, type PwTestCase, type PwTestResult } from './index.ts'

const now = new Date('2026-10-05T12:00:00Z')

const test = (over: Partial<PwTestCase> = {}): PwTestCase => ({
  id: 'abc',
  title: 'pays by card TC-1 then CHK-12',
  titlePath: () => ['', 'checkout.spec.ts', 'pays by card TC-1 then CHK-12'],
  location: { file: 'checkout.spec.ts' },
  annotations: [],
  ...over,
})

const result = (over: Partial<PwTestResult> = {}): PwTestResult => ({
  retry: 0,
  status: 'passed',
  duration: 1500,
  startTime: now,
  errors: [],
  ...over,
})

interface Call {
  url: string
  method: string
  headers: Record<string, string>
  body: string
}

/** A fake Provenly: answers each request with the next queued response (default 200 {}). */
function fakeFetch(responses: (Response | Error)[] = []) {
  const calls: Call[] = []
  const fn = vi.fn(async (url: string, init: { method: string; headers: Record<string, string>; body: string }) => {
    calls.push({ url, method: init.method, headers: init.headers, body: init.body })
    const next = responses.shift()
    if (next instanceof Error) throw next
    return next ?? new Response('{}', { status: 200 })
  })
  return { fn: fn as unknown as typeof fetch, calls }
}

const json = (body: unknown, status = 201) => new Response(JSON.stringify(body), { status })

describe('helpers', () => {
  it('reads the TC-ID from an annotation, a tag or the title', () => {
    expect(tcIdOf(test({ annotations: [{ type: 'TC-ID', description: ' CHK-7 ' }] }))).toBe('CHK-7')
    expect(tcIdOf(test({ annotations: [{ type: 'tc-id' }], tags: ['@smoke', '@CHK-9'] }))).toBe('CHK-9')
    expect(tcIdOf(test())).toBe('CHK-12')
    expect(tcIdOf(test({ title: 'no id here', tags: ['@smoke'] }))).toBe('')
  })

  it('maps Playwright outcomes', () => {
    expect(statusOf('passed')).toBe('passed')
    expect(statusOf('skipped')).toBe('skipped')
    expect(statusOf('interrupted')).toBe('error')
    expect(statusOf('failed')).toBe('failed')
    expect(statusOf('timedOut')).toBe('failed')
  })

  it('writes one testcase per attempt with escaped text', () => {
    const xml = junitXml(
      [
        { name: 'a "<b>" & c', className: 'x.spec.ts', tcId: 'TC-1', attempt: 1, status: 'failed', seconds: 1.5, message: 'boom <1>', details: 'stack\u0001' },
        { name: 'a', className: 'x.spec.ts', tcId: 'TC-1', attempt: 2, status: 'passed', seconds: 0.25, message: '', details: '' },
        { name: 'e', className: 'x.spec.ts', tcId: '', attempt: 1, status: 'error', seconds: 0, message: 'm', details: 'd' },
        { name: 's', className: 'x.spec.ts', tcId: '', attempt: 1, status: 'skipped', seconds: 0, message: '', details: '' },
      ],
      now,
    )
    expect(xml).toContain('<testsuite name="playwright" tests="4" timestamp="2026-10-05T12:00:00.000Z">')
    expect(xml).toContain(
      '<testcase name="a &quot;&lt;b&gt;&quot; &amp; c" classname="x.spec.ts" time="1.500"><properties><property name="tc-id" value="TC-1"/><property name="attempt" value="1"/></properties><failure message="boom &lt;1&gt;">stack</failure></testcase>',
    )
    expect(xml).toContain('time="0.250"><properties><property name="tc-id" value="TC-1"/><property name="attempt" value="2"/></properties></testcase>')
    expect(xml).toContain('<properties><property name="attempt" value="1"/></properties><error message="m">d</error>')
    expect(xml).toContain('<skipped/>')
  })
})

describe('ProvenlyReporter', () => {
  it('does nothing without a URL', async () => {
    const f = fakeFetch()
    const r = new ProvenlyReporter({ fetch: f.fn, env: {} })
    expect(r.printsToStdio()).toBe(false)
    r.onBegin()
    r.onTestBegin(test(), result())
    r.onTestEnd(test(), result())
    await r.onEnd({ status: 'passed' })
    expect(f.calls).toHaveLength(0)
  })

  it('streams a live run and sends the final report with every attempt', async () => {
    const f = fakeFetch([json({ id: 42 })])
    const log = vi.fn()
    const r = new ProvenlyReporter({
      fetch: f.fn,
      log,
      now: () => now,
      flushEvery: 3,
      env: {
        PROVENLY_URL: 'https://provenly.test/',
        PROVENLY_API_KEY: 'pk_1',
        PROVENLY_PROJECT: 'CHK',
        PROVENLY_SUITE: 'smoke',
        GITHUB_ACTIONS: 'true',
        GITHUB_RUN_ID: '77',
        GITHUB_RUN_ATTEMPT: '2',
        GITHUB_WORKFLOW: 'e2e',
        GITHUB_REF_NAME: 'main',
        GITHUB_SHA: 'abc',
      },
    })
    r.onBegin()
    const flaky = test({ annotations: [{ type: 'tc-id', description: 'CHK-3' }] })
    r.onTestBegin(flaky, result())
    r.onTestEnd(flaky, result({ status: 'failed', errors: [{ message: 'expected 1\nreceived 2', stack: 'Error: expected 1' }] }))
    r.onTestBegin(flaky, result({ retry: 1 }))
    r.onTestEnd(flaky, result({ retry: 1 }))
    const plain = test({ id: 'p', title: 'plain', titlePath: () => [], annotations: [] })
    r.onTestEnd(plain, result({ status: 'skipped' }))
    await r.onEnd({ status: 'passed' })

    const [start, batch1, batch2, report] = f.calls
    expect(start.url).toBe('https://provenly.test/api/v1/test-runs/live')
    expect(start.headers.Authorization).toBe('Bearer pk_1')
    expect(JSON.parse(start.body)).toEqual({
      project: 'CHK', suite: 'smoke', provider: 'github', runId: '77', runAttempt: 2, pipeline: 'e2e', branch: 'main', commit: 'abc',
    })
    expect(batch1.url).toBe('https://provenly.test/api/v1/test-runs/42/events')
    const events1 = JSON.parse(batch1.body).events
    expect(events1.map((e: { eventId: string }) => e.eventId)).toEqual(['abc:0:started', 'abc:0:finished', 'abc:1:started'])
    expect(events1[2].attempt).toBe(2)
    expect(events1[1]).toMatchObject({ attempt: 1, sequence: 2, type: 'test.finished', testCase: 'CHK-3', status: 'failed', testName: ' › checkout.spec.ts › pays by card TC-1 then CHK-12' })
    const events2 = JSON.parse(batch2.body).events
    expect(events2.map((e: { type: string }) => e.type)).toEqual(['test.finished', 'test.finished', 'run.finished'])
    expect(events2[1].testCase).toBeUndefined()
    expect(report.url).toBe(
      'https://provenly.test/api/v1/ingestion/junit?provider=github&runId=77&runAttempt=2&project=CHK&suite=smoke&pipeline=e2e&branch=main&commit=abc',
    )
    expect(report.headers['Content-Type']).toBe('application/xml')
    expect(report.body).toContain('<property name="tc-id" value="CHK-3"/><property name="attempt" value="1"/></properties><failure message="expected 1">Error: expected 1</failure>')
    expect(report.body).toContain('<property name="attempt" value="2"/></properties></testcase>')
    expect(report.body).toContain('<testcase name="plain" classname="checkout.spec.ts" time="1.500"><properties><property name="attempt" value="1"/></properties><skipped/>')
    expect(log).not.toHaveBeenCalled()
  })

  it('keeps going when the live channel fails and retries the final report', async () => {
    const f = fakeFetch([new Response('no', { status: 503 }), new Error('offline'), new Response('down', { status: 502 }), new Response('ok', { status: 201 })])
    const log = vi.fn()
    const r = new ProvenlyReporter({ url: 'https://p.test', fetch: f.fn, log, env: {}, runId: '9', runAttempt: 1 })
    r.onBegin()
    r.onTestEnd(test(), result({ status: 'timedOut', errors: [{ message: 'timeout' }] }))
    await r.onEnd({ status: 'timedout' })
    expect(log.mock.calls.map((c) => c[0])).toEqual([
      'live run not started (HTTP 503); the final report is still sent',
      'report not sent, attempt 1 of 3 (offline)',
      'report not sent, attempt 2 of 3 (HTTP 502)',
    ])
    expect(f.calls.at(-1)!.url).toBe('https://p.test/api/v1/ingestion/junit?provider=local&runId=9&runAttempt=1&status=interrupted')
    expect(f.calls.at(-1)!.headers.Authorization).toBeUndefined()
    expect(f.calls.at(-1)!.body).toContain('<failure message="timeout">timeout</failure>')
  })

  it('logs rejected reports and failed event batches without throwing', async () => {
    const f = fakeFetch([json({ id: 5 }), new Response('bad', { status: 409 }), new Response('invalid', { status: 400 })])
    const log = vi.fn()
    const r = new ProvenlyReporter({ url: 'https://p.test', fetch: f.fn, log, env: {}, now: () => now })
    r.onBegin()
    r.onTestEnd(test({ errors: [] } as never), result({ status: 'interrupted', errors: [{}] }))
    await r.onEnd({ status: 'interrupted' })
    expect(log.mock.calls.map((c) => c[0])).toEqual(['live events not sent (HTTP 409)', 'report rejected (HTTP 400): invalid'])
    expect(f.calls[0].body).toContain(`"runId":"${now.getTime()}"`)

    const gaveUp = fakeFetch([new Error('a'), new Error('b'), new Error('c')])
    const quiet = new ProvenlyReporter({ url: 'https://p.test', fetch: gaveUp.fn, log: vi.fn(), env: {}, live: false })
    quiet.onBegin()
    quiet.onTestBegin(test(), result())
    await quiet.onEnd({ status: 'failed' })
    expect(gaveUp.calls).toHaveLength(3)
    expect(gaveUp.calls.every((c) => c.url.includes('/ingestion/junit'))).toBe(true)
  })

  it('defaults to the global fetch, a real clock and a console log', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const original = globalThis.fetch
    globalThis.fetch = vi.fn(async () => {
      throw new Error('nope')
    }) as unknown as typeof fetch
    try {
      process.env.PROVENLY_URL = 'https://env.test'
      const r = new ProvenlyReporter({ live: false })
      r.onBegin()
      await r.onEnd({ status: 'passed' })
      expect(warn).toHaveBeenCalledWith('[provenly] report not sent, attempt 1 of 3 (nope)')
    } finally {
      delete process.env.PROVENLY_URL
      globalThis.fetch = original
      warn.mockRestore()
    }
  })
})
