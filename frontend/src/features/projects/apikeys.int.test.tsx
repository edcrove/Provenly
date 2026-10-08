import { screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'

import { user } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'
import { server } from '@/test/server'

const at = '2026-10-05T10:00:00Z'
const key = (id: number, extra: Partial<(typeof db.apiKeys)[number]> = {}) => ({
  id,
  projectId: 1,
  name: `key ${id}`,
  prefix: `pvk_${id.toString(16).padStart(8, '0')}`,
  status: 'active' as const,
  createdBy: 1,
  createdAt: at,
  lastUsedAt: null,
  revokedAt: null,
  ...extra,
})

describe('FE-INT-033 project API keys', () => {
  it('FE-INT-033 a maintainer creates a key, sees it once with the CI step, and revokes it', async () => {
    db.apiKeys.push(key(3, { lastUsedAt: '2026-10-05T11:00:00Z' }))
    const { user: u } = renderRoute('/projects/TC')
    const old = await screen.findByTestId('api-key-3')
    expect(old).toHaveTextContent('pvk_00000003…')
    expect(old).toHaveTextContent('2026-10-05 11:00:00 UTC')

    await u.type(screen.getByLabelText('Key name'), '  GitHub Actions  ')
    await u.click(screen.getByRole('button', { name: 'Create API key' }))
    const token = await screen.findByTestId('api-key-token')
    expect(token.textContent).toMatch(/^pvk_[0-9a-f]{8}_k{43}$/)
    expect(screen.getByTestId('api-key-snippet')).toHaveTextContent('Authorization: Bearer $PROVENLY_API_KEY')
    expect(screen.getByLabelText('Key name')).toHaveValue('')
    const created = db.apiKeys.at(-1)!
    expect(created.name).toBe('GitHub Actions')
    const row = await screen.findByTestId(`api-key-${created.id}`)
    expect(row).toHaveTextContent('Never')

    // Revoking asks first: CI using the key will be refused.
    await u.click(within(row).getByRole('button', { name: 'Revoke…' }))
    const confirm = within(row).getByRole('group', { name: 'Confirm revoking GitHub Actions' })
    expect(confirm).toHaveTextContent('Revoke key GitHub Actions? CI using it will get 401.')
    expect(within(confirm).getByRole('button', { name: 'Cancel' })).toHaveFocus()
    await u.click(within(confirm).getByRole('button', { name: 'Cancel' }))
    expect(created.status).toBe('active')
    await u.click(within(row).getByRole('button', { name: 'Revoke…' }))
    await u.click(within(row).getByRole('button', { name: 'Revoke' }))
    await waitFor(() => expect(within(row).getByText('revoked')).toBeInTheDocument())
    expect(within(row).queryByRole('button', { name: /^Revoke/ })).not.toBeInTheDocument()
  })

  it('FE-INT-033 errors are reported where they happen; viewers and members do not see keys', async () => {
    db.apiKeys.push(key(4, { status: 'revoked', revokedAt: at }), key(5))
    const { user: u, unmount } = renderRoute('/projects/TC')
    const row = await screen.findByTestId('api-key-5')
    db.apiKeys[1] = { ...db.apiKeys[1], status: 'revoked', revokedAt: at }
    await u.click(within(row).getByRole('button', { name: 'Revoke…' }))
    await u.click(within(row).getByRole('button', { name: 'Revoke' }))
    expect(await within(row).findByText('Could not revoke the key')).toBeInTheDocument()

    await u.type(screen.getByLabelText('Key name'), 'x'.repeat(100))
    db.failing = true
    await u.click(screen.getByRole('button', { name: 'Create API key' }))
    expect(await screen.findByText('Could not create the key')).toBeInTheDocument()
    db.failing = false
    unmount()

    db.users.push(user({ id: 2, username: 'ana', isAdmin: false }))
    db.members.push({ projectId: 1, userId: 2, role: 'member', since: at })
    db.session = 2
    renderRoute('/projects/TC')
    expect(await screen.findByTestId('member-ana')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'API keys' })).not.toBeInTheDocument()
  })

  it('FE-INT-033 a failed key list stays in its section: the tab keeps the project title', async () => {
    server.use(
      http.get('*/api/v1/projects/:projectKey/api-keys', () =>
        HttpResponse.json(
          {
            type: 'about:blank',
            title: 'Internal Server Error',
            status: 500,
            code: 'internal_error',
            detail: 'x',
          },
          { status: 500, headers: { 'Content-Type': 'application/problem+json' } },
        ),
      ),
    )
    renderRoute('/projects/TC')
    await screen.findByRole('heading', { name: 'API keys' })
    expect(await screen.findByText('Something went wrong')).toBeInTheDocument()
    expect(document.title).toBe('TC · settings · Provenly')
    expect(document.querySelectorAll('title')).toHaveLength(1)
  })

  it('FE-INT-033 lists many keys by page', async () => {
    for (let i = 1; i <= 21; i++) db.apiKeys.push(key(100 + i))
    const { user: u } = renderRoute('/projects/TC')
    expect(await screen.findByTestId('api-key-121')).toBeInTheDocument()
    // The keys' pager is the one in the keys' card (members and webhooks have their own).
    const card = screen.getByRole('heading', { name: 'API keys' }).closest<HTMLElement>('[data-slot="card"]')!
    await u.click(within(card).getByRole('button', { name: /next/i }))
    expect(await screen.findByTestId('api-key-101')).toBeInTheDocument()
  })
})
