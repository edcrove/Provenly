import { screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { user } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'

const ana = () => user({ id: 2, username: 'ana', displayName: 'Ana Pérez', isAdmin: false })

describe('FE-INT-051 account offboarding (card #61)', () => {
  it('FE-INT-051 an administrator deactivates a user after confirming, and reactivates them', async () => {
    db.users.push(ana())
    db.passwords.ana = 'ana password'
    const { user: u } = renderRoute('/users')
    const row = await screen.findByTestId('user-ana')
    expect(within(screen.getByTestId('user-admin')).queryByRole('button')).not.toBeInTheDocument()

    await u.click(within(row).getByRole('button', { name: 'Deactivate…' }))
    const confirm = within(row).getByRole('group', { name: 'Confirm deactivating ana' })
    expect(confirm).toHaveTextContent('Deactivate Ana Pérez? Their sessions end now.')
    expect(within(confirm).getByRole('button', { name: 'Cancel' })).toHaveFocus()
    await u.click(within(confirm).getByRole('button', { name: 'Cancel' }))
    expect(db.users[1].deactivatedAt).toBeNull()

    await u.click(within(row).getByRole('button', { name: 'Deactivate…' }))
    await u.click(within(row).getByRole('button', { name: 'Deactivate' }))
    expect(await within(screen.getByTestId('user-ana')).findByText('deactivated')).toBeInTheDocument()
    expect(db.users[1].deactivatedAt).not.toBeNull()
    await u.click(within(screen.getByTestId('user-ana')).getByRole('button', { name: 'Reactivate' }))
    await waitFor(() =>
      expect(within(screen.getByTestId('user-ana')).queryByText('deactivated')).not.toBeInTheDocument(),
    )
  })

  it('FE-INT-051 a reset link is shown once and lets the person choose a new password and sign in', async () => {
    db.users.push(ana())
    db.passwords.ana = 'ana password'
    const { user: u, router } = renderRoute('/users')
    const row = await screen.findByTestId('user-ana')
    await u.click(within(row).getByRole('button', { name: 'Password reset link' }))
    const link = await within(row).findByTestId('reset-link')
    expect(link.textContent).toMatch(/\/reset-password#token=reset-\d+$/)
    expect(within(row).getByRole('status')).toHaveTextContent(
      'It works once, for 24 hours, and is not shown again.',
    )

    db.session = null
    await router.navigate(`/reset-password${link.textContent!.slice(link.textContent!.indexOf('#'))}`)
    await screen.findByRole('heading', { name: 'Choose a new password' })
    await u.type(screen.getByLabelText('New password'), 'short')
    await u.type(screen.getByLabelText('Repeat new password'), 'shorter')
    await u.click(screen.getByRole('button', { name: 'Set password and sign in' }))
    expect(await screen.findByText('The passwords do not match.')).toBeInTheDocument()
    await u.clear(screen.getByLabelText('Repeat new password'))
    await u.type(screen.getByLabelText('Repeat new password'), 'short')
    await u.click(screen.getByRole('button', { name: 'Set password and sign in' }))
    expect(await screen.findByText('Password must be at least 10 characters')).toBeInTheDocument()
    expect(screen.getByLabelText('New password')).toHaveAccessibleDescription(
      'Password must be at least 10 characters',
    )

    for (const field of ['New password', 'Repeat new password']) {
      await u.clear(screen.getByLabelText(field))
      await u.type(screen.getByLabelText(field), "ana's new password")
    }
    await u.click(screen.getByRole('button', { name: 'Set password and sign in' }))
    await waitFor(() => expect(router.state.location.pathname).not.toBe('/reset-password'))
    expect(db.passwords.ana).toBe("ana's new password")
  })

  it('FE-INT-051 a used or missing reset link says so', async () => {
    db.session = null
    const { user: u, router } = renderRoute('/reset-password')
    expect(await screen.findByText('Invalid reset link')).toBeInTheDocument()
    await router.navigate('/reset-password#token=used')
    for (const field of ['New password', 'Repeat new password'])
      await u.type(await screen.findByLabelText(field), 'a long password')
    await u.click(screen.getByRole('button', { name: 'Set password and sign in' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'password reset link not found, expired or already used',
    )
  })

  it('FE-INT-051 a deactivated user cannot sign in, with the usual answer', async () => {
    db.users.push(
      user({
        id: 2,
        username: 'ana',
        displayName: 'Ana Pérez',
        isAdmin: false,
        deactivatedAt: '2026-10-06T10:00:00Z',
      }),
    )
    db.passwords.ana = 'ana password'
    db.session = null
    const { user: u } = renderRoute('/login')
    await u.type(await screen.findByLabelText('Username'), 'ana')
    await u.type(screen.getByLabelText('Password'), 'ana password')
    await u.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('invalid username or password')
  })
})
