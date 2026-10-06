import { screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'

const manualRun = () => {
  db.runs.push({ ...db.runs[0], id: 900, mode: 'manual', executionStatus: 'running', startedBy: 'admin' })
  db.summaries[900] = { ...db.summaries[7], testRunId: 900 }
}

describe('FE-INT-053 manual run confirmations and recorded state (card #56)', () => {
  it('FE-INT-053 completing with untested test cases and cancelling ask first, with the safe option focused', async () => {
    manualRun()
    const { user: u } = renderRoute('/test-runs/900')
    const panel = await screen.findByTestId('manual-execution')

    await u.click(within(panel).getByRole('button', { name: 'Complete run' }))
    const complete = within(panel).getByRole('group', { name: 'Confirm completing the run' })
    expect(complete).toHaveTextContent('1 test case is still untested. Complete anyway?')
    expect(within(complete).getByRole('button', { name: 'Cancel' })).toHaveFocus()
    await u.click(within(complete).getByRole('button', { name: 'Cancel' }))
    expect(db.runs.at(-1)!.executionStatus).toBe('running')

    await u.click(within(panel).getByRole('button', { name: 'Cancel run…' }))
    const cancel = within(panel).getByRole('group', { name: 'Confirm cancelling the run' })
    expect(cancel).toHaveTextContent('Cancel this run? Recorded results are kept; 1 untested stay untested.')
    expect(within(cancel).getByRole('button', { name: 'Keep the run' })).toHaveFocus()
    await u.click(within(cancel).getByRole('button', { name: 'Keep the run' }))
    expect(db.runs.at(-1)!.executionStatus).toBe('running')

    await u.click(within(panel).getByRole('button', { name: 'Complete run' }))
    await u.click(within(panel).getByRole('button', { name: 'Complete anyway' }))
    await waitFor(() => expect(screen.queryByTestId('manual-execution')).not.toBeInTheDocument())
    expect(db.runs.at(-1)!.executionStatus).toBe('completed')
  })

  it('FE-INT-053 only the recorded result is highlighted, and saving says what was saved', async () => {
    manualRun()
    const { user: u } = renderRoute('/test-runs/900')
    const row = within(await screen.findByTestId('manual-execution')).getByTestId('manual-TC-154')
    const pressed = () =>
      within(row)
        .getAllByRole('button', { pressed: true })
        .map((b) => b.textContent)
    // Untested: no button looks selected.
    expect(within(row).queryAllByRole('button', { pressed: true })).toHaveLength(0)

    await u.type(within(row).getByLabelText('Failed step of TC-154'), '3')
    await u.click(within(row).getByRole('button', { name: 'Fail' }))
    await waitFor(() => expect(pressed()).toEqual(['Fail']))
    expect(within(row).getByRole('status')).toHaveTextContent('Saved · failed at step 3')

    await u.click(within(row).getByRole('button', { name: 'Blocked' }))
    await waitFor(() => expect(pressed()).toEqual(['Blocked']))
    expect(within(row).getByRole('status')).toHaveTextContent(/^Saved · blocked$/)
  })
})
