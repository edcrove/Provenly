import { screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'

import { summary, testResult, testRun } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'
import { server } from '@/test/server'

describe('FE-INT-008 test run list', () => {
  it('FE-INT-008 lists runs newest first with their metadata', async () => {
    db.runs = [
      testRun(),
      testRun({
        id: 8,
        externalRunId: 'github:9876:2',
        runAttempt: 2,
        pipeline: '',
        branch: '',
        commit: '',
        resultCount: 1,
        executionStatus: 'interrupted',
        outcome: {
          verdict: 'passed',
          executed: 2,
          passed: 2,
          failed: 0,
          error: 0,
          skipped: 0,
          untested: 0,
          passRate: 100,
          flaky: 0,
        },
      }),
      testRun({
        id: 9,
        expectedCount: 0,
        executionStatus: 'cancelled',
        outcome: {
          verdict: 'no_tests',
          executed: 0,
          passed: 0,
          failed: 0,
          error: 0,
          skipped: 0,
          untested: 0,
          passRate: 0,
          flaky: 0,
        },
      }),
    ]
    renderRoute('/test-runs')
    const rows = (await screen.findAllByRole('row')).slice(1)
    expect(within(rows[0]).getByTestId('verdict-badge')).toHaveTextContent('no tests')
    expect(within(rows[0]).getByTestId('pass-rate')).toHaveTextContent('—')
    expect(within(rows[0]).getByTestId('executed')).toHaveTextContent('0 of 0')
    expect(within(rows[0]).getByTestId('outcome-breakdown')).toHaveTextContent('no test cases · 0 expected')
    expect(within(rows[0]).getByTestId('execution-badge')).toHaveTextContent('cancelled')

    expect(within(rows[1]).getByText('github:9876:2')).toBeInTheDocument()
    expect(within(rows[1]).getAllByText('—')).toHaveLength(3)
    expect(within(rows[1]).getByTestId('verdict-badge')).toHaveTextContent('passed')
    expect(within(rows[1]).getByTestId('pass-rate')).toHaveTextContent('100%')
    expect(within(rows[1]).getByTestId('executed')).toHaveTextContent('2 of 2 (100%)')
    expect(within(rows[1]).getByTestId('outcome-breakdown')).toHaveTextContent('2 passed · 2 expected')
    expect(within(rows[1]).getByTestId('execution-badge')).toHaveTextContent('interrupted')

    expect(within(rows[2]).getByTestId('verdict-badge')).toHaveTextContent('failed')
    expect(within(rows[2]).getByTestId('pass-rate')).toHaveTextContent('0%')
    // Next to the pass rate, how much of the expected universe executed.
    expect(within(rows[2]).getByTestId('executed')).toHaveTextContent('1 of 2 (50%)')
    expect(within(rows[2]).getByTestId('outcome-breakdown')).toHaveTextContent(
      '1 failed · 1 untested · 2 expected',
    )
    expect(within(rows[2]).queryByTestId('execution-badge')).not.toBeInTheDocument()
    expect(within(rows[2]).getByText('completed')).toBeInTheDocument()
    expect(within(rows[2]).getByRole('link', { name: '#7' })).toHaveAttribute('href', '/test-runs/7')
  })

  it('FE-INT-008 shows the empty state and paginates', async () => {
    db.runs = []
    const first = renderRoute('/test-runs')
    expect(await screen.findByText(/^No test runs yet/)).toBeInTheDocument()
    first.unmount()
    // The empty state names the filter that emptied the list.
    localStorage.setItem('provenly.project', 'TC')
    const inProject = renderRoute('/test-runs')
    expect(await screen.findByText(/^No test runs in TC yet/)).toBeInTheDocument()
    inProject.unmount()
    const ofSuite = renderRoute('/test-runs?project=TC&suite=nightly')
    expect(await screen.findByText('No runs of suite nightly yet.')).toBeInTheDocument()
    ofSuite.unmount()
    localStorage.removeItem('provenly.project')

    db.runs = Array.from({ length: 21 }, (_, i) => testRun({ id: i + 1, externalRunId: `github:${i + 1}:1` }))
    const { user, router } = renderRoute('/test-runs')
    await user.click(await screen.findByRole('button', { name: 'Next' }))
    expect(await screen.findByText('Page 2 of 2 · 21 items')).toBeInTheDocument()
    expect(router.state.location.search).toBe('?page=2')
  })
})

describe('FE-INT-009 test run detail and summary', () => {
  it('FE-INT-009 shows run metadata and the summary with the three percentages', async () => {
    renderRoute('/test-runs/7')
    expect(await screen.findByRole('heading', { name: 'Test run #7' })).toBeInTheDocument()
    expect(screen.getAllByText('github:9876:1').length).toBeGreaterThan(0)
    expect(screen.getByText('9876')).toBeInTheDocument()
    expect(screen.getByText('0123456789abcdef')).toBeInTheDocument()
    expect(await screen.findByTestId('expected-total')).toHaveTextContent('2')
    expect(screen.getByTestId('executed-total')).toHaveTextContent('1')
    expect(screen.getByTestId('execution-percent')).toHaveTextContent('50%')
    const failed = screen.getByTestId('summary-failed')
    expect(
      within(failed)
        .getAllByRole('cell')
        .map((c) => c.textContent),
    ).toEqual(['failed', '1', '50%', '100%'])
    const untested = screen.getByTestId('summary-untested')
    expect(
      within(untested)
        .getAllByRole('cell')
        .map((c) => c.textContent),
    ).toEqual(['untested', '1', '50%', '—'])
    expect(screen.getByRole('link', { name: 'TC-154' })).toHaveAttribute('href', '/test-cases/154')
    const total = screen.getByTestId('summary-total')
    expect(
      within(total)
        .getAllByRole('cell')
        .map((c) => c.textContent),
    ).toEqual(['Total', '2', '100%', '100%'])
    expect(screen.getByTestId('execution-counts')).toHaveTextContent('1 of 2 test cases executed')
    expect(screen.getByTestId('pass-rate')).toHaveTextContent('0%')
    expect(screen.getByTestId('pass-counts')).toHaveTextContent('0 of 1 executed passed')
    expect(screen.getByTestId('verdict-badge')).toHaveTextContent('failed')
    expect(screen.getByTestId('run-pass-rate')).toHaveTextContent('0% of executed passed')
  })

  it('FE-INT-009 shows every metadata field of the run', async () => {
    db.runs[0] = {
      ...db.runs[0],
      provider: 'gitlab',
      providerRunId: '4242',
      runAttempt: 3,
      externalRunId: 'gitlab:4242:3',
      pipeline: 'nightly-e2e',
      branch: 'release/2.0',
      commit: 'cafe1234',
      createdAt: '2026-10-03T12:00:00Z',
      startedAt: '2026-10-03T11:00:00Z',
      completedAt: '2026-10-03T11:30:00Z',
    }
    renderRoute('/test-runs/7')
    await screen.findByRole('heading', { name: 'Test run #7' })
    const value = (label: string) =>
      screen.getByText(label, { selector: 'dt' }).nextElementSibling?.textContent
    expect(value('External run id')).toBe('gitlab:4242:3')
    expect(value('Provider')).toBe('gitlab')
    expect(value('Run id')).toBe('4242')
    expect(value('Attempt')).toBe('3')
    expect(value('Pipeline')).toBe('nightly-e2e')
    expect(value('Branch')).toBe('release/2.0')
    expect(value('Commit')).toBe('cafe1234')
    expect(value('Execution status')).toBe('completed')
    expect(value('Created')).toBe('2026-10-03 12:00:00 UTC')
    expect(value('Started')).toBe('2026-10-03 11:00:00 UTC')
    expect(value('Completed')).toBe('2026-10-03 11:30:00 UTC')
  })

  it('FE-INT-009 an empty universe reads — with its counts (0 of 0)', async () => {
    db.summaries[7] = summary({
      expectedTotal: 0,
      executedTotal: 0,
      counts: { untested: 0, passed: 0, failed: 0, error: 0, skipped: 0 },
      percentOfExpected: { untested: 0, passed: 0, failed: 0, error: 0, skipped: 0 },
      percentOfExecuted: { passed: 0, failed: 0, error: 0, skipped: 0 },
      executionPercent: 0,
      testCases: [],
    })
    db.runs = [
      testRun({
        expectedCount: 0,
        outcome: {
          verdict: 'no_tests',
          executed: 0,
          passed: 0,
          failed: 0,
          error: 0,
          skipped: 0,
          untested: 0,
          passRate: 0,
          flaky: 0,
        },
      }),
    ]
    renderRoute('/test-runs/7')
    expect(await screen.findByTestId('execution-percent')).toHaveTextContent('—')
    expect(screen.getByTestId('execution-counts')).toHaveTextContent('0 of 0 test cases executed')
    expect(screen.getByTestId('pass-rate')).toHaveTextContent('—')
    expect(screen.getByTestId('run-pass-rate')).toHaveTextContent('— (0 of 0 executed)')
    expect(screen.getByTestId('verdict-badge')).toHaveTextContent('no tests')
    // FE-INT-048: a share of nothing is "—", never 0%.
    expect(
      within(screen.getByTestId('summary-total'))
        .getAllByRole('cell')
        .map((c) => c.textContent),
    ).toEqual(['Total', '0', '—', '—'])
  })

  it('FE-INT-009 rounds only for display so thirds add up to a 100% total', async () => {
    db.summaries[7] = summary({
      expectedTotal: 3,
      executedTotal: 3,
      counts: { untested: 0, passed: 1, failed: 1, error: 1, skipped: 0 },
      percentOfExpected: { untested: 0, passed: 33.333333, failed: 33.333333, error: 33.333333, skipped: 0 },
      percentOfExecuted: { passed: 33.333333, failed: 33.333333, error: 33.333333, skipped: 0 },
      executionPercent: 100,
    })
    renderRoute('/test-runs/7')
    const passed = await screen.findByTestId('summary-passed')
    expect(
      within(passed)
        .getAllByRole('cell')
        .map((c) => c.textContent),
    ).toEqual(['passed', '1', '33.33%', '33.33%'])
    const total = screen.getByTestId('summary-total')
    expect(
      within(total)
        .getAllByRole('cell')
        .map((c) => c.textContent),
    ).toEqual(['Total', '3', '100%', '100%'])
  })

  it('FE-INT-009 renders runs without optional metadata and without untested cases', async () => {
    db.runs = [testRun({ pipeline: '', branch: '', commit: '', startedAt: null, completedAt: null })]
    db.summaries[7] = summary({
      counts: { untested: 0, passed: 2, failed: 0, error: 0, skipped: 0 },
      testCases: [
        { testCaseId: 153, testCaseKey: 'TC-153', status: 'passed', resultCount: 1, flaky: false },
        { testCaseId: 154, testCaseKey: 'TC-154', status: 'passed', resultCount: 1, flaky: false },
      ],
    })
    renderRoute('/test-runs/7')
    await screen.findByTestId('expected-total')
    expect(screen.queryByText('Untested:')).not.toBeInTheDocument()
  })

  it('FE-INT-016 flags runs whose CI execution was interrupted or cancelled', async () => {
    db.runs = [testRun({ executionStatus: 'interrupted' })]
    const first = renderRoute('/test-runs/7')
    expect(await screen.findByTestId('interrupted-run')).toHaveTextContent('The CI execution was interrupted')
    expect(screen.getByTestId('execution-badge')).toHaveTextContent('interrupted')
    first.unmount()
    db.runs = [testRun({ executionStatus: 'cancelled' })]
    renderRoute('/test-runs/7')
    expect(await screen.findByTestId('interrupted-run')).toHaveTextContent('was cancelled')
  })

  it('FE-INT-016 does not flag completed runs', async () => {
    renderRoute('/test-runs/7')
    await screen.findByTestId('expected-total')
    expect(screen.queryByTestId('interrupted-run')).not.toBeInTheDocument()
    expect(screen.queryByTestId('execution-badge')).not.toBeInTheDocument()
  })

  it('FE-INT-012 shows errors for unknown runs', async () => {
    renderRoute('/test-runs/999')
    expect(await screen.findByRole('alert')).toHaveTextContent('Not foundtest run 999 not found')
  })
})

describe('FE-INT-010 TC-ID diagnostics', () => {
  it('FE-INT-010 lists invalid TC-ID counts with explanations', async () => {
    db.summaries[7] = summary({
      diagnostics: { missing: 1, malformed: 2, unknown: 0, deprecated: 3, wrongProject: 4, total: 10 },
      outsideUniverse: 2,
    })
    renderRoute('/test-runs/7')
    const list = await screen.findByRole('list', { name: 'Diagnostics' })
    expect(within(list).getAllByRole('listitem')).toHaveLength(4)
    expect(screen.getByTestId('diagnostic-malformed')).toHaveTextContent('2')
    expect(screen.getByTestId('diagnostic-wrong_project')).toHaveTextContent('4')
    expect(within(list).getByText(/another project's key/)).toBeInTheDocument()
    expect(screen.getByTestId('diagnostic-deprecated')).toHaveTextContent('3')
    expect(screen.queryByTestId('diagnostic-unknown')).not.toBeInTheDocument()
    expect(screen.getByTestId('outside-universe')).toHaveTextContent(
      '2 results point to test cases that were not automated',
    )
    expect(
      screen.getByRole('list', { name: 'Test cases outside the expected universe' }),
    ).toBeEmptyDOMElement()
  })

  it('FE-INT-010 a suite run explains results outside its universe by the suite', async () => {
    db.runs[0] = { ...db.runs[0], suite: { key: 'release', name: 'Release' } }
    db.summaries[7] = { ...db.summaries[7], outsideUniverse: 1, outsideUniverseTestCaseIds: [] }
    renderRoute('/test-runs/7')
    expect(await screen.findByTestId('outside-universe')).toHaveTextContent(
      '1 result points to a test case outside suite Release (or not automated) when this run was created, so it is not counted in this summary. If it belongs to the suite, add it to it',
    )
  })

  it('FE-INT-010 warns about manual test cases outside the universe and marks them automated', async () => {
    db.testCases[1] = { ...db.testCases[1], automated: false }
    db.summaries[7] = summary({ outsideUniverse: 2, outsideUniverseTestCaseIds: [154] })
    const { user } = renderRoute('/test-runs/7')
    const item = await screen.findByTestId('outside-154')
    expect(await within(item).findByRole('link', { name: 'TC-154' })).toHaveAttribute(
      'href',
      '/test-cases/154',
    )
    expect(await within(item).findByText('Logout works')).toBeInTheDocument()
    await user.click(within(item).getByRole('button', { name: 'Mark as automated' }))
    expect(await within(item).findByText(/Now automated: future runs include it/)).toBeInTheDocument()
    expect(db.testCases[1].automated).toBe(true)
  })

  it('FE-INT-010 reports failures when marking a test case automated', async () => {
    db.testCases[1] = { ...db.testCases[1], automated: false }
    db.summaries[7] = summary({ outsideUniverse: 1, outsideUniverseTestCaseIds: [154] })
    const { user } = renderRoute('/test-runs/7')
    const item = await screen.findByTestId('outside-154')
    await within(item).findByText('Logout works')
    server.use(
      http.patch('*/api/v1/test-cases/:id', () =>
        HttpResponse.json(
          {
            type: 'about:blank',
            title: 'Internal Server Error',
            status: 500,
            code: 'internal_error',
            detail: 'boom',
          },
          { status: 500, headers: { 'Content-Type': 'application/problem+json' } },
        ),
      ),
    )
    expect(screen.getByTestId('outside-universe')).toHaveTextContent(
      '1 result points to a test case that was not automated',
    )
    await user.click(within(item).getByRole('button', { name: 'Mark as automated' }))
    expect(await within(item).findByText('Could not update the test case')).toBeInTheDocument()
  })

  it('FE-INT-010 states when every TC-ID is valid', async () => {
    db.summaries[7] = summary({
      diagnostics: { missing: 0, malformed: 0, unknown: 0, deprecated: 0, wrongProject: 0, total: 0 },
    })
    renderRoute('/test-runs/7')
    expect(await screen.findByText('Every result declared a valid TC-ID.')).toBeInTheDocument()
  })
})

describe('FE-INT-063 a failed run opens on its failures', () => {
  it('FE-INT-063 a run with failures shows its results first; a passing one keeps its metadata first', async () => {
    const order = () =>
      screen
        .getAllByRole('heading', { level: 2 })
        .map((h) => h.textContent)
        .filter((t) => t === 'Results' || t === 'Run metadata')
    const failing = renderRoute('/test-runs/7')
    await screen.findByRole('table', { name: 'Results' })
    expect(order()).toEqual(['Results', 'Run metadata'])
    failing.unmount()

    db.runs[0] = {
      ...db.runs[0],
      outcome: { ...db.runs[0].outcome, verdict: 'passed', failed: 0, error: 0, passed: 1 },
    }
    renderRoute('/test-runs/7')
    await screen.findByRole('table', { name: 'Results' })
    expect(order()).toEqual(['Run metadata', 'Results'])
  })
})

describe('FE-INT-011 run results', () => {
  it('FE-INT-011 shows every individual result, linking valid TC-IDs', async () => {
    renderRoute('/test-runs/7')
    const table = await screen.findByRole('table', { name: 'Results' })
    const rows = await within(table).findAllByTestId('result-row')
    expect(rows).toHaveLength(3)
    // The failure comes first (deployed audit), then the rest in ingestion order.
    expect(within(rows[0]).getByText('boom')).toBeInTheDocument()
    expect(within(rows[0]).getByRole('button', { name: 'Show error details of boom' })).toBeInTheDocument()
    expect(within(rows[1]).getByRole('link', { name: 'TC-153' })).toHaveAttribute('href', '/test-cases/153')
    expect(within(rows[1]).getByText('1.20 s')).toBeInTheDocument()
    expect(within(rows[2]).getByText('missing')).toBeInTheDocument()
  })

  it('FE-INT-011 shows deprecated results linked to their test case with a badge', async () => {
    db.results = [
      testResult({ id: 9, correlation: 'deprecated', requestedTestCaseId: 'TC-153', testName: 'late' }),
    ]
    renderRoute('/test-runs/7')
    const row = await screen.findByTestId('result-row')
    expect(within(row).getByRole('link', { name: 'TC-153' })).toBeInTheDocument()
    expect(within(row).getByText('deprecated')).toBeInTheDocument()
    expect(within(row).queryByText('TC-153', { selector: 'span' })).not.toBeInTheDocument()
  })

  it('FE-INT-011 filters by status and correlation', async () => {
    db.results.push(
      testResult({
        id: 4,
        testCaseId: null,
        correlation: 'unknown',
        requestedTestCaseId: 'TC-999',
        testName: 'ghost',
      }),
    )
    const { user, router } = renderRoute('/test-runs/7?page=1')
    await within(await screen.findByRole('table', { name: 'Results' })).findAllByTestId('result-row')
    await user.selectOptions(screen.getByLabelText('Filter by status'), 'failed')
    await waitFor(() => expect(rows()).toHaveLength(1))
    expect(within(rows()[0]).getByText('login firefox')).toBeInTheDocument()
    expect(router.state.location.search).toBe('?status=failed')

    await user.selectOptions(screen.getByLabelText('Filter by status'), '')
    await user.selectOptions(screen.getByLabelText('Filter by TC-ID correlation'), 'unknown')
    await waitFor(() => expect(rows()).toHaveLength(1))
    expect(within(rows()[0]).getByText('TC-999')).toBeInTheDocument()

    await user.selectOptions(screen.getByLabelText('Filter by TC-ID correlation'), 'deprecated')
    expect(await screen.findByText('No results match the filters.')).toBeInTheDocument()
  })

  it('FE-INT-011 paginates results', async () => {
    db.results = Array.from({ length: 21 }, (_, i) => testResult({ id: i + 1, testName: `t${i + 1}` }))
    const { user, router } = renderRoute('/test-runs/7')
    const table = await screen.findByRole('table', { name: 'Results' })
    await within(table).findAllByTestId('result-row')
    const section = table.closest('[data-slot="card"]') as HTMLElement
    await user.click(within(section).getByRole('button', { name: 'Next' }))
    await waitFor(() => expect(rows()).toHaveLength(1))
    expect(within(rows()[0]).getByText('t21')).toBeInTheDocument()
    expect(router.state.location.search).toBe('?page=2')
  })
})

function rows() {
  return within(screen.getByRole('table', { name: 'Results' })).queryAllByTestId('result-row')
}

describe('FE-INT-015 report parse errors', () => {
  it('FE-INT-015 is hidden when the report had no parse errors', async () => {
    renderRoute('/test-runs/7')
    await screen.findByRole('table', { name: 'Results' })
    await screen.findAllByTestId('result-row')
    expect(screen.queryByText('Report parse errors')).not.toBeInTheDocument()
  })

  it('FE-INT-015 lists kept and discarded testcases and paginates', async () => {
    db.parseErrors[7] = Array.from({ length: 21 }, (_, i) => ({
      index: i,
      testName: i === 0 ? '' : `t${i}`,
      message:
        i === 0
          ? 'testcase has no name; result discarded'
          : 'invalid time attribute; result kept without duration',
      persisted: i !== 0,
      severity: i === 1 ? ('warning' as const) : ('error' as const),
      shard: null,
    }))
    const { user } = renderRoute('/test-runs/7')
    const table = await screen.findByRole('table', { name: 'Parse errors' })
    const rows = within(table).getAllByTestId('parse-error-row')
    expect(within(rows[0]).getByText('(no name)')).toBeInTheDocument()
    expect(within(rows[0]).getByText('discarded')).toBeInTheDocument()
    expect(within(rows[1]).getByText('kept')).toBeInTheDocument()
    expect(within(rows[0]).getByText('error')).toBeInTheDocument()
    expect(within(rows[1]).getByText('warning')).toBeInTheDocument()
    const card = table.closest('[data-slot="card"]') as HTMLElement
    await user.click(within(card).getByRole('button', { name: 'Next' }))
    expect(await within(card).findByText('t20')).toBeInTheDocument()
  })

  it('FE-INT-015 shows unknown and sub-millisecond durations distinctly', async () => {
    db.results = [
      testResult({ id: 1, durationMs: null, testName: 'unknown' }),
      testResult({ id: 2, durationMs: 0, testName: 'tiny' }),
    ]
    renderRoute('/test-runs/7')
    const rows = await screen.findAllByTestId('result-row')
    expect(within(rows[0]).getByText('—')).toBeInTheDocument()
    expect(within(rows[1]).getByText('<1 ms')).toBeInTheDocument()
  })
})

describe('FE-INT-018 test run pages robustness', () => {
  it('FE-INT-018 an invalid run id is not found', async () => {
    renderRoute('/test-runs/abc')
    expect(await screen.findByRole('heading', { name: 'Page not found' })).toBeInTheDocument()
  })

  it('FE-INT-018 run list and results move a page past the end to the last page', async () => {
    db.runs = Array.from({ length: 21 }, (_, i) => testRun({ id: i + 1, externalRunId: `github:${i + 1}:1` }))
    const list = renderRoute('/test-runs?page=5')
    const pager = await screen.findByText('Page 2 of 2 · 21 items')
    await waitFor(() => expect(list.router.state.location.search).toBe('?page=2'))
    // Page 2 (one run) replaces the rows in place: no loading state flashes in between.
    await waitFor(() => expect(screen.getAllByRole('row')).toHaveLength(2))
    expect(pager).toBeInTheDocument()
    list.unmount()

    db.runs = [testRun()]
    db.results = Array.from({ length: 21 }, (_, i) => testResult({ id: i + 1, testName: `t${i + 1}` }))
    const { router } = renderRoute('/test-runs/7?page=5')
    await waitFor(() => expect(router.state.location.search).toBe('?page=2'))
    expect(router.state.historyAction).toBe('REPLACE')
  })

  it('FE-INT-018 a run page sets the tab title while loading', async () => {
    renderRoute('/test-runs/7')
    await waitFor(() => expect(document.title).toBe('Loading… · Provenly'))
    await waitFor(() => expect(document.title).toBe('Test run #7 · Provenly'))
  })
})

describe('FE-INT-021 result error details', () => {
  it('FE-INT-021 expands the full error details of a result (stack traces, every failure) on demand', async () => {
    db.results = [
      testResult({
        id: 1,
        status: 'failed',
        errorMessage: 'total mismatch',
        errorDetails: 'failure: total mismatch\nexpected 10 got 9\n\nfailure: tax mismatch\nexpected 2 got 1',
      }),
      testResult({ id: 2, testName: 'no details', status: 'failed', errorMessage: 'boom', errorDetails: '' }),
      testResult({
        id: 3,
        testName: 'long',
        status: 'failed',
        errorMessage: `expected ${'x'.repeat(200)} end`,
        errorDetails: '',
      }),
      testResult({
        id: 4,
        testName: 'lines',
        status: 'failed',
        errorMessage: 'first\nsecond\nthird',
        errorDetails: '',
      }),
    ]
    const { user } = renderRoute('/test-runs/7')
    const table = await screen.findByRole('table', { name: 'Results' })
    await within(table).findByText('total mismatch')
    expect(within(table).queryByText(/tax mismatch/)).not.toBeInTheDocument()
    const toggle = within(table).getByRole('button', { name: 'Show error details of total mismatch' })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    // A short message without details has nothing more to show; a long one (clamped to two lines) opens in full.
    expect(within(table).getAllByRole('button', { name: /Show error details/ })).toHaveLength(3)
    expect(
      within(table).queryByRole('button', { name: 'Show error details of boom' }),
    ).not.toBeInTheDocument()

    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    const details = within(table).getByTestId('error-details')
    expect(details.textContent).toBe(
      'failure: total mismatch\nexpected 10 got 9\n\nfailure: tax mismatch\nexpected 2 got 1',
    )
    await user.click(within(table).getByRole('button', { name: 'Hide error details of total mismatch' }))
    expect(within(table).queryByTestId('error-details')).not.toBeInTheDocument()
    await user.click(within(table).getByRole('button', { name: /^Show error details of expected x+ end$/ }))
    expect(within(table).getByTestId('error-details').textContent).toBe(`expected ${'x'.repeat(200)} end`)
  })
})
