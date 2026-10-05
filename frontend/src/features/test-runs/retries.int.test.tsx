import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { testResult } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'

/** Run 7: TC-153 failed on attempt 1 and passed on attempt 2 (flaky). */
const flakyRun = () => {
  db.runs[0] = { ...db.runs[0], outcome: { ...db.runs[0].outcome, flaky: 1 } }
  db.summaries[7] = {
    ...db.summaries[7],
    flaky: 1,
    testCases: [
      { testCaseId: 153, testCaseKey: 'TC-153', status: 'passed', resultCount: 2, flaky: true },
      { testCaseId: 154, testCaseKey: 'TC-154', status: 'untested', resultCount: 0, flaky: false },
    ],
  }
  db.results = [
    testResult({ id: 1, status: 'failed', attempt: 1, retried: true, errorMessage: 'timeout' }),
    testResult({ id: 2, status: 'passed', attempt: 2, retried: false }),
  ]
}

describe('FE-INT-036 retries and flaky tests (D1)', () => {
  it('FE-INT-036 the run shows its flaky test cases and which results were retried', async () => {
    flakyRun()
    const { router } = renderRoute('/test-runs/7')
    expect(await screen.findByTestId('flaky-badge')).toHaveTextContent('1 flaky')
    expect(await screen.findByTestId('flaky-cases')).toHaveTextContent('Flaky (passed on a retry): TC-153')
    expect(screen.getByText(/a retried test counts with its last attempt/)).toBeInTheDocument()
    const attempts = await screen.findAllByTestId('attempt-badge')
    expect(attempts.map((a) => a.textContent)).toEqual(['attempt 1 · retried', 'attempt 2'])

    await router.navigate('/test-runs')
    await screen.findByRole('heading', { name: 'Test Runs' })
    expect(await screen.findByTestId('flaky-badge')).toHaveTextContent('1 flaky')
  })

  it('FE-INT-036 single attempts show no attempt marker; the history marks retried results', async () => {
    const { router } = renderRoute('/test-runs/7')
    await screen.findByTestId('verdict-badge')
    expect(screen.queryByTestId('flaky-badge')).not.toBeInTheDocument()
    expect(screen.queryByTestId('attempt-badge')).not.toBeInTheDocument()

    flakyRun()
    await router.navigate('/test-cases/153')
    expect((await screen.findAllByTestId('attempt-badge')).map((a) => a.textContent)).toContain(
      'attempt 1 · retried',
    )
  })
})
