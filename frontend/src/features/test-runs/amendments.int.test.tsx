import { screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { user } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'

/** Run 7 has a valid result for TC-154, which was manual (outside the snapshot) when the run was created. */
const outsideRun = (extra: { amended?: number[] } = {}) => {
  db.testCases[1] = { ...db.testCases[1], automated: false }
  db.summaries[7] = {
    ...db.summaries[7],
    outsideUniverse: 1,
    outsideUniverseTestCaseIds: [154],
    amendedTestCaseIds: extra.amended ?? [],
    testCases: db.summaries[7].testCases.filter((c) => c.testCaseId !== 154),
  }
}

describe('FE-INT-035 snapshot amendment (DEC-42)', () => {
  it('FE-INT-035 a maintainer includes a reported test case in the run; the run is marked as edited with its history', async () => {
    outsideRun()
    const { user: u, router } = renderRoute('/test-runs/7')
    const item = await screen.findByTestId('outside-154')
    expect(screen.queryByTestId('edited-badge')).not.toBeInTheDocument()
    await u.type(await within(item).findByLabelText('Why it belongs in this run'), 'Marked manual by mistake')
    await u.click(within(item).getByRole('button', { name: 'Include in this run' }))

    expect(await screen.findByTestId('edited-badge')).toHaveTextContent('edited')
    expect(await screen.findByTestId('expected-amended')).toHaveTextContent(
      '2 in the snapshot + 1 included later',
    )
    expect(screen.getByText(/plus the test cases a maintainer included later/)).toBeInTheDocument()
    const history = await screen.findByTestId('amendments')
    expect(history).toHaveTextContent('The snapshot frozen when CI reported this run had 2 test cases')
    expect(within(history).getByRole('list', { name: 'Amendments' })).toHaveTextContent(
      /TC-154\s*included by admin on .*:\s*Marked manual by mistake/,
    )
    await waitFor(() => expect(screen.queryByTestId('outside-154')).not.toBeInTheDocument())
    expect(db.amendments).toMatchObject([
      { testRunId: 7, testCaseId: 154, reason: 'Marked manual by mistake' },
    ])

    await router.navigate('/test-runs')
    await screen.findByRole('heading', { name: 'Test Runs' })
    expect(await screen.findByTestId('edited-badge')).toBeInTheDocument()
  })

  it('FE-INT-035 a refused amendment is reported; members cannot include test cases', async () => {
    outsideRun({ amended: [154] })
    const { user: u, unmount } = renderRoute('/test-runs/7')
    const item = await screen.findByTestId('outside-154')
    await u.type(await within(item).findByLabelText('Why it belongs in this run'), 'x')
    await u.click(within(item).getByRole('button', { name: 'Include in this run' }))
    expect(await within(item).findByText('Could not include the test case')).toBeInTheDocument()
    unmount()

    outsideRun()
    db.users.push(user({ id: 2, username: 'ana', isAdmin: false }))
    db.members.push({ projectId: 1, userId: 2, role: 'member', since: '2026-10-05T10:00:00Z' })
    db.session = 2
    renderRoute('/test-runs/7')
    const asMember = await screen.findByTestId('outside-154')
    expect(await within(asMember).findByRole('button', { name: 'Mark as automated' })).toBeInTheDocument()
    expect(within(asMember).queryByRole('button', { name: 'Include in this run' })).not.toBeInTheDocument()
  })
})
