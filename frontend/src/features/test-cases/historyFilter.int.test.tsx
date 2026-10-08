import { screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { testResult, testRun } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'

describe('FE-INT-064 did it pass on main lately?', () => {
  it('FE-INT-064 the history says when it last passed, on any branch or the one chosen, and filters by branch', async () => {
    db.runs = [
      testRun({ id: 1, branch: 'main', externalRunId: 'github:1:1', createdAt: '2026-10-01T12:00:00Z' }),
      testRun({ id: 2, branch: 'feature/x', externalRunId: 'github:2:1', createdAt: '2026-10-02T12:00:00Z' }),
      testRun({ id: 3, branch: 'main', externalRunId: 'github:3:1', createdAt: '2026-10-03T12:00:00Z' }),
    ]
    db.results = [
      testResult({ id: 11, testRunId: 1, testCaseId: 153, status: 'passed' }),
      testResult({ id: 12, testRunId: 2, testCaseId: 153, status: 'passed' }),
      testResult({ id: 13, testRunId: 3, testCaseId: 153, status: 'failed' }),
    ]
    const { user: u } = renderRoute('/test-cases/153')
    const last = await screen.findByTestId('last-passed')
    expect(last).toHaveTextContent('Last passed: 2026-10-02 12:00:00 UTC in github:2:1 (feature/x)')

    await u.type(screen.getByLabelText('History branch'), ' main {Enter}')
    await waitFor(() =>
      expect(screen.getByTestId('last-passed')).toHaveTextContent(
        'Last passed on main: 2026-10-01 12:00:00 UTC in github:1:1',
      ),
    )
    const table = screen.getByRole('table', { name: 'Execution history' })
    await waitFor(() => expect(within(table).getAllByRole('row')).toHaveLength(3))
    expect(within(table).queryByText('github:2:1')).not.toBeInTheDocument()

    await u.clear(screen.getByLabelText('History branch'))
    await u.type(screen.getByLabelText('History branch'), 'release{Enter}')
    expect(await screen.findByText('Never passed on release.')).toBeInTheDocument()
    expect(screen.getByText('No results on release yet for this TC-ID.')).toBeInTheDocument()
  })
})
