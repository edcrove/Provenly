import { screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { testCase } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'

describe('FE-INT-062 automated or manual test cases', () => {
  it('FE-INT-062 the list narrows to automated or manual test cases and says when none match', async () => {
    db.testCases = [
      testCase({ id: 1, key: 'TC-1', title: 'Login by script', automated: true }),
      testCase({ id: 2, key: 'TC-2', title: 'Checkout by hand', automated: false }),
    ]
    const { user: u, router } = renderRoute('/test-cases')
    await screen.findByText('Login by script')
    const titles = () =>
      screen
        .getAllByRole('row')
        .slice(1)
        .map((r) => within(r).queryAllByRole('cell')[1]?.textContent)

    await u.selectOptions(screen.getByLabelText('Filter by execution'), 'false')
    await waitFor(() => expect(titles()).toEqual(['Checkout by hand']))
    expect(router.state.location.search).toBe('?automated=false')
    await u.selectOptions(screen.getByLabelText('Filter by execution'), 'true')
    await waitFor(() => expect(titles()).toEqual(['Login by script']))

    await u.selectOptions(screen.getByLabelText('Filter by execution'), '')
    expect(router.state.location.search).toBe('')
  })

  it('FE-INT-062 no test case of that kind says the filters match nothing', async () => {
    db.testCases = [testCase({ id: 2, key: 'TC-2', title: 'Checkout by hand', automated: false })]
    renderRoute('/test-cases?automated=true')
    expect(await screen.findByText('No test cases match the filters.')).toBeInTheDocument()
    expect(screen.getByLabelText('Filter by execution')).toHaveValue('true')
  })
})
