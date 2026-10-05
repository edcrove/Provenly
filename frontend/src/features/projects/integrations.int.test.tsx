import { screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'

import { user } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'
import { server } from '@/test/server'

const at = '2026-10-05T10:00:00Z'
const fail500 = () =>
  HttpResponse.json(
    { type: 'about:blank', title: 'Internal Server Error', status: 500, code: 'internal_error', detail: 'x' },
    { status: 500, headers: { 'Content-Type': 'application/problem+json' } },
  )

describe('FE-INT-044 project integrations', () => {
  it('FE-INT-044 a maintainer adds a webhook, sees its secret once, pings, pauses and reads its deliveries', async () => {
    const { user: u } = renderRoute('/projects/TC')
    expect(await screen.findByText('No webhooks yet.')).toBeInTheDocument()
    await u.type(screen.getByLabelText('Endpoint URL'), 'https://hooks.example.com/p')
    await u.click(screen.getByRole('button', { name: 'Add webhook' }))
    expect(await screen.findByTestId('webhook-secret')).toHaveTextContent(/^whsec_0{48}$/)
    expect(screen.getByLabelText('Endpoint URL')).toHaveValue('')
    const id = db.webhooks[0].id
    const row = await screen.findByTestId(`webhook-${id}`)
    expect(row).toHaveTextContent('active')
    expect(row).toHaveTextContent('None yet')

    await u.click(within(row).getByRole('button', { name: 'Send ping' }))
    expect(await within(row).findByText('ping: pending')).toBeInTheDocument()
    await u.click(within(row).getByRole('button', { name: 'Pause' }))
    expect(await within(row).findByText('paused')).toBeInTheDocument()
    await u.click(within(row).getByRole('button', { name: 'Resume' }))
    expect(await within(row).findByText('active')).toBeInTheDocument()

    Object.assign(db.deliveries[0], { status: 'failed', attempts: 5, lastError: 'the endpoint answered 500' })
    db.deliveries.push({
      ...db.deliveries[0],
      id: 2001,
      status: 'succeeded',
      attempts: 1,
      lastError: null,
      lastStatusCode: 204,
    })
    await u.click(within(row).getByRole('button', { name: 'Deliveries' }))
    const list = await screen.findByTestId(`deliveries-${id}`)
    expect(list).toHaveTextContent('the endpoint answered 500')
    expect(list).toHaveTextContent('204')
    await u.click(within(row).getByRole('button', { name: 'Deliveries' }))
    expect(screen.queryByTestId(`deliveries-${id}`)).not.toBeInTheDocument()
  })

  it('FE-INT-044 webhook errors are reported where they happen; deliveries page; empty deliveries', async () => {
    db.webhooks.push({
      id: 5,
      projectId: 1,
      url: 'https://x.test',
      events: ['run.completed'],
      active: true,
      createdBy: 'admin',
      createdAt: at,
      updatedAt: at,
    })
    for (let i = 0; i < 21; i++)
      db.deliveries.push({
        id: 3000 + i,
        webhookId: 5,
        event: 'run.completed',
        status: 'pending',
        attempts: 0,
        nextAttemptAt: at,
        lastStatusCode: null,
        lastError: null,
        createdAt: at,
        completedAt: null,
        payload: {},
      })
    db.webhooks.push({
      id: 6,
      projectId: 1,
      url: 'https://y.test',
      events: ['run.completed'],
      active: true,
      createdBy: 'admin',
      createdAt: at,
      updatedAt: at,
    })
    const { user: u } = renderRoute('/projects/TC')
    const row = await screen.findByTestId('webhook-5')
    expect(row).toHaveTextContent('run.completed: pending')
    await u.click(within(row).getByRole('button', { name: 'Deliveries' }))
    const list = await screen.findByTestId('deliveries-5')
    expect(within(list).getAllByText('—')).toHaveLength(20)
    await u.click(within(list).getByRole('button', { name: /next/i }))
    await waitFor(() => expect(within(screen.getByTestId('deliveries-5')).getAllByText('—')).toHaveLength(1))
    await u.click(within(screen.getByTestId('webhook-6')).getByRole('button', { name: 'Deliveries' }))
    expect(
      await within(await screen.findByTestId('deliveries-6')).findByText('No deliveries yet.'),
    ).toBeInTheDocument()

    server.use(http.post('*/api/v1/projects/:projectKey/webhooks/:id/ping', () => fail500()))
    await u.click(within(row).getByRole('button', { name: 'Send ping' }))
    expect(await within(row).findByText('Could not update the webhook')).toBeInTheDocument()

    await u.type(screen.getByLabelText('Endpoint URL'), 'https://z.test')
    db.failing = true
    await u.click(screen.getByRole('button', { name: 'Add webhook' }))
    expect(await screen.findByText('Could not add the webhook')).toBeInTheDocument()
  })

  it('FE-INT-044 a maintainer connects GitHub, syncs, sees a failed sync, rotates the token and disconnects', async () => {
    const { user: u } = renderRoute('/projects/TC')
    await u.type(await screen.findByLabelText('Repository'), ' acme/shop ')
    await u.type(screen.getByLabelText('Labels (optional)'), 'qa')
    await u.type(screen.getByLabelText('Token'), 'ghp_secret1234')
    await u.click(screen.getByRole('button', { name: 'Connect' }))
    const conn = await screen.findByTestId('github-connection')
    expect(conn).toHaveTextContent('acme/shop labelled qa with token …1234. Last sync: never.')
    expect(db.github[0].repository).toBe('acme/shop')

    await u.click(within(conn).getByRole('button', { name: 'Sync issues now' }))
    expect(await within(conn).findByText('Synced: 2 created, 1 updated.')).toBeInTheDocument()
    await waitFor(() => expect(conn).not.toHaveTextContent('Last sync: never'))

    // Change the repository only (the token is kept), to one whose sync fails.
    const repo = screen.getByLabelText('Repository')
    await u.clear(repo)
    await u.type(repo, 'acme/down')
    expect(screen.getByLabelText('New token (optional)')).not.toBeRequired()
    await u.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(screen.getByTestId('github-connection')).toHaveTextContent(
        'acme/down labelled qa with token …1234',
      ),
    )
    await u.click(screen.getByRole('button', { name: 'Sync issues now' }))
    expect(await screen.findByText('GitHub request failed')).toBeInTheDocument()
    expect(await screen.findByText(/The last sync failed: GitHub answered 503/)).toBeInTheDocument()

    await u.type(screen.getByLabelText('New token (optional)'), 'ghp_new9876')
    await u.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(screen.getByTestId('github-connection')).toHaveTextContent('…9876'))

    await u.click(screen.getByRole('button', { name: 'Disconnect' }))
    await waitFor(() => expect(screen.queryByTestId('github-connection')).not.toBeInTheDocument())
    expect(screen.getByRole('button', { name: 'Connect' })).toBeInTheDocument()
  })

  it('FE-INT-044 an unreadable token and save errors are shown; members and viewers see no integrations', async () => {
    db.github.push({
      projectId: 1,
      repository: 'acme/shop',
      labels: '',
      tokenHint: '',
      lastSyncedAt: null,
      lastError: null,
      updatedAt: at,
    })
    const { user: u, unmount } = renderRoute('/projects/TC')
    expect(await screen.findByTestId('github-connection')).toHaveTextContent('(unreadable: save a new one)')
    const repo = screen.getByLabelText('Repository')
    await u.clear(repo)
    await u.type(repo, 'not a repo')
    await u.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('Could not save the connection')).toBeInTheDocument()
    server.use(http.delete('*/api/v1/projects/:projectKey/github', () => fail500()))
    await u.click(screen.getByRole('button', { name: 'Disconnect' }))
    expect(await screen.findByText('GitHub request failed')).toBeInTheDocument()
    unmount()

    db.users.push(user({ id: 2, username: 'ana', isAdmin: false }))
    db.members.push({ projectId: 1, userId: 2, role: 'member', since: at })
    db.session = 2
    renderRoute('/projects/TC')
    expect(await screen.findByTestId('member-ana')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Webhooks' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'GitHub Issues' })).not.toBeInTheDocument()
  })

  it('FE-INT-044 a failing connection read is an error state (not "not connected")', async () => {
    server.use(
      http.get('*/api/v1/projects/:projectKey/github', () =>
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
    expect(await screen.findByText('Something went wrong')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Connect' })).not.toBeInTheDocument()
  })
})
