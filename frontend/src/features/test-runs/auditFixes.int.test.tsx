import { screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { summary, testRun } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'

// Regressions found by the full audit of the prototype (2026-10-05).
describe('FE-INT-048 audit regressions', () => {
  it('FE-INT-048 a running run shows its verdict as provisional on its page, in the list and on the dashboard', async () => {
    db.runs[0] = testRun({ executionStatus: 'running', mode: 'manual', startedBy: 'admin' })
    const { unmount } = renderRoute('/test-runs/7')
    const badge = await screen.findByTestId('verdict-badge')
    expect(badge).toHaveTextContent('failed so far')
    expect(badge).toHaveAttribute('title', 'Provisional: the run is still running')
    unmount()
    const list = renderRoute('/test-runs')
    expect(await screen.findByTestId('verdict-badge')).toHaveTextContent('failed so far')
    list.unmount()
    db.runs[0] = testRun()
    renderRoute('/test-runs')
    const finished = await screen.findByTestId('verdict-badge')
    expect(finished).toHaveTextContent(/^failed$/)
    expect(finished).not.toHaveAttribute('title')
  })

  it('FE-INT-048 with nothing executed every % of executed is "—" (and with nothing expected every % of expected); an unfiltered empty result list says so', async () => {
    db.summaries[7] = summary({
      executedTotal: 0,
      counts: { untested: 2, passed: 0, failed: 0, error: 0, skipped: 0 },
      percentOfExpected: { untested: 100, passed: 0, failed: 0, error: 0, skipped: 0 },
      percentOfExecuted: { passed: 0, failed: 0, error: 0, skipped: 0 },
    })
    db.results = []
    renderRoute('/test-runs/7')
    const total = await screen.findByTestId('summary-total')
    expect(within(total).getAllByRole('cell').at(-1)).toHaveTextContent('—')
    const passed = within(screen.getByTestId('summary-passed')).getAllByRole('cell')
    expect(passed.map((c) => c.textContent)).toEqual(['passed', '0', '0%', '—'])
    expect(await screen.findByText('No results yet.')).toBeInTheDocument()
  })
})
