import { screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { invitation, user } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'
import { server } from '@/test/server'

const member = () => user({ id: 2, username: 'ana', displayName: 'Ana Pérez', isAdmin: false })

describe('FE-INT-026 sign in', () => {
  it('FE-INT-026 sends to sign-in, rejects wrong passwords and comes back to the page asked for', async () => {
    db.session = null
    const { user: u, router } = renderRoute('/test-runs?page=1')
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
    expect(router.state.location.search).toBe(`?next=${encodeURIComponent('/test-runs?page=1')}`)
    await waitFor(() => expect(document.title).toBe('Sign in · Provenly'))

    await u.type(screen.getByLabelText('Username'), 'admin')
    await u.type(screen.getByLabelText('Password'), 'wrong password')
    await u.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('invalid username or password')

    await u.clear(screen.getByLabelText('Password'))
    await u.type(screen.getByLabelText('Password'), 'correct horse')
    await u.click(screen.getByRole('button', { name: 'Sign in' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/test-runs'))
    expect(await screen.findByTestId('current-user')).toHaveTextContent('Ada Admin')
  })

  it('FE-INT-026 a signed-in visit to sign-in goes on; a next outside the app is ignored', async () => {
    const { router } = renderRoute('/login?next=//evil.example/x')
    await waitFor(() => expect(router.state.location.pathname).toBe('/test-cases'))
  })

  it('FE-INT-026 signing out returns to sign-in and protects every page', async () => {
    const { user: u, router } = renderRoute('/test-cases')
    await u.click(await screen.findByRole('button', { name: 'Sign out' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
    expect(db.session).toBeNull()
    await router.navigate('/projects')
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
  })

  it('FE-INT-026 a session that expires while browsing sends to sign-in', async () => {
    const { user: u, router } = renderRoute('/test-cases')
    await screen.findByText('Login works')
    db.session = null
    await u.click(screen.getByRole('link', { name: 'Test Runs' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
  })

  it('FE-INT-026 shows an error when it cannot tell who is signed in', async () => {
    server.use(
      http.get('*/api/v1/auth/me', () =>
        HttpResponse.json(
          {
            type: 'about:blank',
            title: 'Internal Server Error',
            status: 500,
            code: 'internal_error',
            detail: 'db down',
          },
          { status: 500, headers: { 'Content-Type': 'application/problem+json' } },
        ),
      ),
    )
    renderRoute('/test-cases')
    expect(await screen.findByRole('alert')).toHaveTextContent('db down')
  })
})

describe('FE-INT-027 users and invitations', () => {
  afterEach(() => vi.restoreAllMocks())

  it('FE-INT-027 an administrator lists users and creates, copies and revokes an invitation link', async () => {
    db.users.push(member())
    db.invitations.push(invitation({ id: 50, status: 'accepted', email: 'old@example.com' }))
    const { user: u } = renderRoute('/test-cases')
    const writeText = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue(undefined)
    await u.click(await screen.findByRole('link', { name: 'Users' }))
    expect(await screen.findByTestId('user-admin')).toHaveTextContent('admin')
    expect(screen.getByTestId('user-ana')).toHaveTextContent('member')
    expect(screen.getByTestId('invitation-50')).toHaveTextContent('accepted')
    expect(within(screen.getByTestId('invitation-50')).queryByRole('button')).not.toBeInTheDocument()

    await u.type(screen.getByLabelText('Email (optional)'), 'bob@example')
    await u.click(screen.getByRole('button', { name: 'Create invitation link' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('email: must be an email address')

    await u.clear(screen.getByLabelText('Email (optional)'))
    await u.type(screen.getByLabelText('Email (optional)'), 'bob@example.com')
    await u.type(screen.getByLabelText('Note (optional)'), 'QA')
    await u.click(screen.getByRole('button', { name: 'Create invitation link' }))
    const link = await screen.findByTestId('invitation-link')
    expect(link.textContent).toMatch(/\/accept-invite#token=invite-\d+$/)
    expect(screen.getByLabelText('Email (optional)')).toHaveValue('')
    await u.click(screen.getByRole('button', { name: 'Copy link' }))
    expect(writeText).toHaveBeenCalledWith(link.textContent)
    expect(await screen.findByText('Copied')).toBeInTheDocument()

    const id = db.invitations.at(-1)!.id
    const row = await screen.findByTestId(`invitation-${id}`)
    expect(row).toHaveTextContent('bob@example.com')
    await u.click(within(row).getByRole('button', { name: 'Revoke' }))
    await waitFor(() => expect(screen.getByTestId(`invitation-${id}`)).toHaveTextContent('revoked'))
  })

  it('FE-INT-027 a link that cannot be copied stays on screen; failed revokes are reported', async () => {
    db.invitations.push(invitation({ id: 60 }))
    server.use(
      http.post('*/api/v1/invitations/:id/revoke', () =>
        HttpResponse.json(
          {
            type: 'about:blank',
            title: 'Conflict',
            status: 409,
            code: 'conflict',
            detail: 'already accepted',
          },
          { status: 409, headers: { 'Content-Type': 'application/problem+json' } },
        ),
      ),
    )
    const { user: u } = renderRoute('/users')
    vi.spyOn(navigator.clipboard, 'writeText').mockRejectedValue(new Error('denied'))
    await u.click(await screen.findByRole('button', { name: 'Create invitation link' }))
    await screen.findByTestId('invitation-link')
    await u.click(screen.getByRole('button', { name: 'Copy link' }))
    expect(screen.queryByText('Copied')).not.toBeInTheDocument()
    expect(screen.getByTestId('invitation-link')).toBeVisible()
    await u.click(within(await screen.findByTestId('invitation-60')).getByRole('button', { name: 'Revoke' }))
    expect(await screen.findByText('Could not revoke')).toBeInTheDocument()
  })

  it('FE-INT-027 lists many users and invitations by page', async () => {
    for (let i = 0; i < 21; i++) {
      db.users.push(user({ id: 100 + i, username: `user${String(i).padStart(2, '0')}`, isAdmin: false }))
      db.invitations.push(invitation({ id: 200 + i }))
    }
    const { user: u, router } = renderRoute('/users')
    await screen.findByTestId('user-admin')
    const [usersNext, invitationsNext] = screen.getAllByRole('button', { name: /next/i })
    await u.click(usersNext)
    await waitFor(() => expect(router.state.location.search).toContain('page=2'))
    await u.click(invitationsNext)
    await waitFor(() => expect(router.state.location.search).toContain('invitations=2'))
    expect(await screen.findByTestId('invitation-200')).toBeInTheDocument()
  })

  it('FE-INT-027 members have no Users page', async () => {
    db.users.push(member())
    db.session = 2
    const { router } = renderRoute('/test-cases')
    expect(await screen.findByTestId('current-user')).toHaveTextContent('Ana Pérez')
    expect(screen.queryByRole('link', { name: 'Users' })).not.toBeInTheDocument()
    await router.navigate('/users')
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Only administrators can manage users and invitations.',
    )
  })

  it('FE-INT-027 an empty invitation list says so', async () => {
    renderRoute('/users')
    expect(await screen.findByText('No invitations yet.')).toBeInTheDocument()
  })
})

describe('FE-INT-028 accepting an invitation', () => {
  const open = (token = 'invite-70') => {
    db.session = null
    db.invitations.push(invitation({ id: 70, email: 'carla@example.com' }))
    db.invitationTokens['invite-70'] = 70
    return renderRoute(`/accept-invite#token=${token}`)
  }

  it('FE-INT-028 creates the account and signs the new user in', async () => {
    const { user: u, router } = open()
    await u.type(screen.getByLabelText('Username'), 'Carla')
    await u.type(screen.getByLabelText('Display name'), 'Carla Gómez')
    await u.type(screen.getByLabelText('Password'), 'carla password')
    await u.type(screen.getByLabelText('Repeat password'), 'carla password!')
    await u.click(screen.getByRole('button', { name: 'Create account' }))
    expect(screen.getByText('The passwords do not match.')).toBeInTheDocument()
    expect(screen.getByLabelText('Repeat password')).toHaveAccessibleDescription(
      'The passwords do not match.',
    )

    await u.clear(screen.getByLabelText('Repeat password'))
    await u.type(screen.getByLabelText('Repeat password'), 'carla password')
    await u.click(screen.getByRole('button', { name: 'Create account' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/test-cases'))
    expect(await screen.findByTestId('current-user')).toHaveTextContent('Carla Gómez')
    expect(db.users.at(-1)).toMatchObject({ username: 'carla', email: 'carla@example.com', isAdmin: false })
  })

  it('FE-INT-028 reports a taken username, an invalid one and a used link', async () => {
    const { user: u } = open()
    const fill = async (username: string) => {
      await u.clear(screen.getByLabelText('Username'))
      await u.type(screen.getByLabelText('Username'), username)
      await u.click(screen.getByRole('button', { name: 'Create account' }))
    }
    await u.type(screen.getByLabelText('Display name'), 'Twin')
    await u.type(screen.getByLabelText('Email (optional)'), 'twin@example.com')
    await u.type(screen.getByLabelText('Password'), 'a long password')
    await u.type(screen.getByLabelText('Repeat password'), 'a long password')
    await fill('admin')
    expect(await screen.findByRole('alert')).toHaveTextContent('username admin is taken')
    await fill('x')
    await waitFor(() => expect(screen.getByLabelText('Username')).toHaveAttribute('aria-invalid', 'true'))
    db.invitations[0].status = 'accepted'
    await fill('twin')
    expect(await screen.findByRole('alert')).toHaveTextContent('already used')
  })

  it('FE-INT-028 every field the server refuses is marked and explained next to it', async () => {
    server.use(
      http.post('*/api/v1/invitations/accept', () =>
        HttpResponse.json(
          {
            type: 'about:blank',
            title: 'Bad Request',
            status: 400,
            code: 'validation_error',
            detail: 'request validation failed',
            errors: [
              { field: 'username', message: 'is taken' },
              { field: 'displayName', message: 'must be at most 100 characters' },
              { field: 'email', message: 'must be an email address' },
              { field: 'password', message: 'must be at most 72 bytes' },
            ],
          },
          { status: 400, headers: { 'Content-Type': 'application/problem+json' } },
        ),
      ),
    )
    const { user: u } = open()
    await u.type(screen.getByLabelText('Username'), 'carla')
    await u.type(screen.getByLabelText('Display name'), 'Carla')
    await u.type(screen.getByLabelText('Password'), 'carla password')
    await u.type(screen.getByLabelText('Repeat password'), 'carla password')
    await u.click(screen.getByRole('button', { name: 'Create account' }))
    await waitFor(() =>
      expect(screen.getByLabelText('Username')).toHaveAccessibleDescription('Username is taken'),
    )
    for (const [label, description] of [
      ['Display name', 'Display name must be at most 100 characters'],
      ['Email (optional)', 'Email must be an email address'],
      ['Password', 'Password must be at most 72 bytes'],
    ]) {
      expect(screen.getByLabelText(label)).toHaveAttribute('aria-invalid', 'true')
      expect(screen.getByLabelText(label)).toHaveAccessibleDescription(description)
    }
  })

  it('FE-INT-028 a link without a token says it is invalid', async () => {
    db.session = null
    renderRoute('/accept-invite')
    expect(await screen.findByRole('alert')).toHaveTextContent('This link has no invitation token.')
  })
})

describe('FE-INT-029 account', () => {
  it('FE-INT-029 shows who is signed in and changes the password', async () => {
    db.users[0] = user({ email: 'ada@example.com' })
    const { user: u } = renderRoute('/test-cases')
    await u.click(await screen.findByTestId('current-user'))
    expect(await screen.findByTestId('account-identity')).toHaveTextContent(
      'Ada Admin · @admin · ada@example.com · administrator',
    )
    const fill = async (current: string, next: string, confirm: string) => {
      for (const [label, value] of [
        ['Current password', current],
        ['New password', next],
        ['Repeat new password', confirm],
      ]) {
        await u.clear(screen.getByLabelText(label))
        await u.type(screen.getByLabelText(label), value)
      }
      await u.click(screen.getByRole('button', { name: 'Change password' }))
    }
    expect(screen.getByLabelText('New password')).toHaveAccessibleDescription(
      'At least 10 characters, at most 72 bytes (letters outside ASCII count as 2 to 4).',
    )
    // 37 two-byte letters are 74 bytes: over bcrypt's limit although only 37 characters.
    await fill('correct horse', 'ñ'.repeat(37), 'ñ'.repeat(37))
    expect(await screen.findByRole('alert')).toHaveTextContent('newPassword: must be at most 72 bytes')
    await fill('correct horse', 'a brand new password', 'something else!')
    expect(screen.getByText('The new passwords do not match.')).toBeInTheDocument()
    await fill('wrong password', 'a brand new password', 'a brand new password')
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'currentPassword: is not your current password',
    )
    await fill('correct horse', 'a brand new password', 'a brand new password')
    expect(await screen.findByRole('status')).toHaveTextContent('Other sessions were signed out.')
    expect(db.passwords.admin).toBe('a brand new password')
    expect(screen.getByLabelText('Current password')).toHaveValue('')
  })

  it('FE-INT-029 a member account shows no admin role', async () => {
    db.users.push(member())
    db.session = 2
    renderRoute('/account')
    expect(await screen.findByTestId('account-identity')).toHaveTextContent('Ana Pérez · @ana')
    expect(screen.getByTestId('account-identity')).not.toHaveTextContent('administrator')
  })
})
