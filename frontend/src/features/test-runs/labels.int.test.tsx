import { screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { summary, testResult, testRun } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'

const blocked = summary({
  testRunId: 900,
  counts: { untested: 1, passed: 0, failed: 0, error: 1, skipped: 0 },
  percentOfExpected: { untested: 50, passed: 0, failed: 0, error: 50, skipped: 0 },
  percentOfExecuted: { passed: 0, failed: 0, error: 100, skipped: 0 },
  testCases: [
    { testCaseId: 153, testCaseKey: 'TC-153', status: 'error', resultCount: 1, flaky: false },
    { testCaseId: 154, testCaseKey: 'TC-154', status: 'untested', resultCount: 0, flaky: false },
  ],
})

describe('FE-INT-052 status labels (card #65)', () => {
  it('FE-INT-052 a manual run reads its stored error as Blocked in the summary, results, filter and history', async () => {
    db.runs.push(
      testRun({ id: 900, mode: 'manual', executionStatus: 'completed', externalRunId: 'manual:900' }),
    )
    db.summaries[900] = blocked
    db.results.push(testResult({ id: 90, testRunId: 900, status: 'error', recordedBy: 'admin' }))
    const run = renderRoute('/test-runs/900')
    const row = await screen.findByTestId('summary-error')
    expect(within(row).getByTestId('status-badge')).toHaveTextContent('Blocked')
    expect(screen.getByText(/Blocked is stored as error\./)).toBeInTheDocument()
    const results = await screen.findByRole('table', { name: 'Results' })
    expect(await within(results).findByTestId('status-badge')).toHaveTextContent('Blocked')
    const filter = screen.getByLabelText('Filter by status')
    expect(within(filter).getByRole('option', { name: 'Blocked' })).toHaveValue('error')
    run.unmount()

    // The same stored error in a CI run still reads error, with no footnote.
    db.summaries[7] = { ...blocked, testRunId: 7 }
    db.results.push(testResult({ id: 91, testRunId: 7, status: 'error' }))
    const ci = renderRoute('/test-runs/7')
    expect(within(await screen.findByTestId('summary-error')).getByTestId('status-badge')).toHaveTextContent(
      /^error$/,
    )
    expect(screen.queryByText(/Blocked is stored as error/)).not.toBeInTheDocument()
    expect(
      within(screen.getByLabelText('Filter by status')).getByRole('option', { name: 'error' }),
    ).toBeInTheDocument()
    ci.unmount()

    // The history of the test case tells them apart by the run they came from.
    renderRoute('/test-cases/153')
    const history = await screen.findByRole('table', { name: 'Execution history' })
    const rowOf = (run: string) => within(history).getByRole('link', { name: run }).closest('tr')!
    expect(within(rowOf('manual:900')).getByTestId('status-badge')).toHaveTextContent('Blocked')
    const ciRows = within(history)
      .getAllByRole('link', { name: 'github:9876:1' })
      .map((l) => l.closest('tr')!)
    expect(ciRows.map((r) => within(r).getByTestId('status-badge').textContent)).toContain('error')
    expect(within(history).getAllByText('Blocked')).toHaveLength(1)
  })

  it('FE-INT-052 a manual run without results says nothing is recorded yet', async () => {
    db.runs.push(testRun({ id: 901, mode: 'manual', executionStatus: 'completed' }))
    renderRoute('/test-runs/901')
    expect(await screen.findByText('Nothing recorded yet.')).toBeInTheDocument()
  })

  it('FE-INT-052 a run that executed nothing has a grey trend bar and a — pass rate with its counts', async () => {
    db.runs.push(
      testRun({
        id: 8,
        outcome: {
          verdict: 'no_tests',
          executed: 0,
          passed: 0,
          failed: 0,
          error: 0,
          skipped: 0,
          untested: 2,
          passRate: 0,
          flaky: 0,
        },
      }),
    )
    localStorage.setItem('provenly.project', 'TC')
    const dashboard = renderRoute('/dashboard')
    const empty = await screen.findByTestId('trend-8')
    expect(empty).toHaveClass('bg-muted-foreground/40')
    expect(empty).toHaveAccessibleName(/^Run #8: no tests, 0 of 0 executed$/)
    expect(screen.getByTestId('trend-7')).toHaveClass('bg-red-600')
    dashboard.unmount()
    renderRoute('/test-runs/8')
    expect(await screen.findByTestId('run-pass-rate')).toHaveTextContent('— (0 of 2 executed)')
  })

  it('FE-INT-052 failures without an open issue are counted, in normal weight, linking each test case', async () => {
    renderRoute('/test-runs/7')
    const line = await screen.findByTestId('run-new-failures')
    expect(line).toHaveTextContent(/^Failures without an open issue \(1\): TC-153$/)
    expect(line.querySelector('.font-medium')).toBeNull()
    expect(within(line).getByRole('link', { name: 'TC-153' })).toHaveAttribute('href', '/test-cases/153')
  })
})
