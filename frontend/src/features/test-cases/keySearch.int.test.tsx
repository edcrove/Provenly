import { screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { testCase } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'

describe('FE-INT-050 find a test case by its TC-ID (card #53)', () => {
  it('FE-INT-050 a TC-ID (any case) narrows the list to that test case and clears back to all', async () => {
    db.testCases = [testCase({ id: 153, title: 'Login works' }), testCase({ id: 154, title: 'Logout works' })]
    const { user, router } = renderRoute('/test-cases')
    await screen.findByText('TC-154')
    await user.type(screen.getByRole('textbox', { name: 'TC-ID' }), ' tc-153 {Enter}')
    expect(router.state.location.search).toBe('?key=TC-153')
    await screen.findByText('Page 1 of 1 · 1 item')
    const table = screen.getByRole('table')
    expect(within(table).getByText('Login works')).toBeInTheDocument()
    expect(within(table).queryByText('Logout works')).not.toBeInTheDocument()

    await user.clear(screen.getByRole('textbox', { name: 'TC-ID' }))
    await user.type(screen.getByRole('textbox', { name: 'TC-ID' }), '{Enter}')
    expect(router.state.location.search).toBe('')
    expect(await screen.findByText('Logout works')).toBeInTheDocument()
  })

  it('FE-INT-050 an unknown TC-ID says so; something that is not a TC-ID is explained and not sent', async () => {
    const { user, router } = renderRoute('/test-cases?key=TC-999')
    expect(await screen.findByText('No test case TC-999.')).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'TC-ID' })).toHaveValue('TC-999')

    await user.clear(screen.getByRole('textbox', { name: 'TC-ID' }))
    await user.type(screen.getByRole('textbox', { name: 'TC-ID' }), 'login{Enter}')
    expect(await screen.findByRole('alert')).toHaveTextContent('Enter a TC-ID like CHK-12')
    expect(screen.getByRole('textbox', { name: 'TC-ID' })).toHaveAccessibleDescription(
      'Enter a TC-ID like CHK-12',
    )
    expect(router.state.location.search).toBe('?key=TC-999')
  })

  it('FE-INT-050 a malformed ?key= in the address is ignored, never sent', async () => {
    renderRoute('/test-cases?key=nope')
    expect(await screen.findByText('TC-153')).toBeInTheDocument()
  })
})
