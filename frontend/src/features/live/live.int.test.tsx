import { screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import type { LiveRun } from '@/api/client'
import { summary } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'

const live = (over: Partial<LiveRun> = {}): LiveRun => ({
  reconciliation: 'pending',
  events: 3,
  lastSequence: 3,
  runFinished: false,
  waiting: 0,
  running: 1,
  finished: 1,
  testCases: [
    { testCaseId: 153, testCaseKey: 'TC-153', state: 'passed' },
    { testCaseId: 154, testCaseKey: 'TC-154', state: 'running' },
  ],
  mismatches: [],
  ...over,
})

describe('FE-INT-043 live runs', () => {
  it('FE-INT-043 a running live run shows provisional progress and reloads itself when the final report arrives', async () => {
    db.runs[0] = { ...db.runs[0], mode: 'live', executionStatus: 'running' }
    db.live[7] = live()
    renderRoute('/test-runs/7')
    expect(await screen.findByTestId('live-progress')).toHaveTextContent(
      '1 of 2 finished · 1 running · 0 waiting',
    )
    expect(screen.getByTestId('reconciliation')).toHaveTextContent('awaiting final report')
    expect(screen.getByTestId('live-TC-154')).toHaveTextContent('TC-154 · running')
    expect(screen.getByText('live')).toBeInTheDocument()

    // The report arrives: the next poll is reconciled and the run reloads as completed.
    db.runs[0] = { ...db.runs[0], executionStatus: 'completed' }
    db.live[7] = live({
      reconciliation: 'mismatch',
      runFinished: true,
      running: 0,
      finished: 2,
      mismatches: [
        {
          kind: 'status_mismatch',
          testCaseId: 153,
          testCaseKey: 'TC-153',
          requestedTestCaseId: null,
          liveStatus: 'passed',
          finalStatus: 'failed',
        },
        {
          kind: 'invalid_correlation',
          testCaseId: null,
          testCaseKey: null,
          requestedTestCaseId: 'TC-999',
          liveStatus: null,
          finalStatus: null,
        },
        {
          kind: 'final_only',
          testCaseId: 155,
          testCaseKey: null,
          requestedTestCaseId: null,
          liveStatus: null,
          finalStatus: 'passed',
        },
      ],
    })
    await waitFor(() => expect(screen.getByTestId('reconciliation')).toHaveTextContent('mismatch'), {
      timeout: 4000,
    })
    await waitFor(() =>
      expect(
        screen.getByText('The final report completed this run; its live events were reconciled with it.'),
      ).toBeInTheDocument(),
    )
    const rows = screen.getAllByTestId('mismatch')
    expect(rows[0]).toHaveTextContent('TC-153Live and final status differpassedfailed')
    expect(rows[1]).toHaveTextContent('TC-999Live event with an unknown TC-ID——')
    expect(within(rows[2]).getByRole('link', { name: '#155' })).toHaveAttribute('href', '/test-cases/155')
    expect(screen.getByText(/runner finished/)).toBeInTheDocument()
    expect(screen.queryByTestId('live-TC-154')).not.toBeInTheDocument()
  }, 10000)

  it("FE-INT-043 a running run's pass rate reads as provisional everywhere and keeps itself current", async () => {
    db.runs[0] = { ...db.runs[0], mode: 'live', executionStatus: 'running' }
    db.live[7] = live()
    const detail = renderRoute('/test-runs/7')
    expect(await screen.findByTestId('run-pass-rate')).toHaveTextContent(/of executed passed so far$/)
    expect(await screen.findByText('Pass rate so far')).toBeInTheDocument()
    // The summary polls while the run runs: new counts show without a reload.
    db.summaries[7] = summary({ testRunId: 7, executedTotal: 9 })
    await waitFor(() => expect(screen.getByTestId('executed-total')).toHaveTextContent('9'), {
      timeout: 4000,
    })
    detail.unmount()
    renderRoute('/test-runs')
    expect(await screen.findByTestId('pass-rate')).toHaveTextContent(/so far$/)
  })

  it('FE-INT-043 a reconciled run without mismatches is consistent; batch runs have no live panel', async () => {
    db.runs[0] = { ...db.runs[0], mode: 'live' }
    db.live[7] = live({ reconciliation: 'consistent', running: 0, finished: 2, waiting: 0, testCases: [] })
    const { unmount } = renderRoute('/test-runs/7')
    expect(await screen.findByTestId('reconciliation')).toHaveTextContent('consistent')
    expect(screen.queryAllByTestId('mismatch')).toHaveLength(0)
    // FE-INT-048: once finished, the panel reads as history, never as tests still running.
    expect(screen.getByTestId('live-progress')).toHaveTextContent(
      'As streamed live: 2 of 2 finished · 0 started but never finished · 0 never started',
    )
    unmount()
    db.runs[0] = { ...db.runs[0], mode: 'batch' }
    renderRoute('/test-runs/7')
    await screen.findAllByTestId('verdict-badge')
    expect(screen.queryByTestId('live-panel')).not.toBeInTheDocument()
  })
})
