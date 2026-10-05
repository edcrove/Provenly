import { screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { user } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'

const event = (id: number, extra: Partial<(typeof db.audit)[number]> = {}) => ({
  id,
  occurredAt: '2026-10-05T10:00:00Z',
  actor: 'admin',
  action: 'POST /api/v1/test-cases',
  path: '/api/v1/test-cases',
  project: null,
  status: 201,
  ...extra,
})

describe('FE-INT-046 audit log', () => {
  it('FE-INT-046 an administrator reads who changed what, filters by project and actor and pages', async () => {
    db.audit.push(
      event(1, {
        actor: 'ana',
        action: 'PATCH /api/v1/projects/{projectKey}',
        path: '/api/v1/projects/CHK',
        project: 'CHK',
      }),
      event(2, { actor: 'api key pvk_0000002a… (GitHub Actions)', action: 'POST /api/v1/ingestion/junit' }),
    )
    for (let i = 3; i <= 22; i++) db.audit.push(event(i))
    const { user: u } = renderRoute('/audit')
    expect(await screen.findByTestId('audit-22')).toHaveTextContent('POST /api/v1/test-cases')
    expect(screen.getByRole('link', { name: 'Audit' })).toBeInTheDocument()
    await u.click(screen.getByRole('button', { name: /next/i }))
    const ana = await screen.findByTestId('audit-1')
    expect(ana).toHaveTextContent('/api/v1/projects/CHK')
    expect(screen.getByTestId('audit-2')).toHaveTextContent('—')

    await u.type(screen.getByLabelText('Project'), ' chk ')
    await u.type(screen.getByLabelText('Actor'), 'ana')
    await u.click(screen.getByRole('button', { name: 'Filter' }))
    await waitFor(() => expect(screen.queryByTestId('audit-2')).not.toBeInTheDocument())
    expect(within(screen.getByTestId('audit-1')).getByText('CHK')).toBeInTheDocument()

    await u.clear(screen.getByLabelText('Project'))
    await u.clear(screen.getByLabelText('Actor'))
    await u.type(screen.getByLabelText('Actor'), 'nobody')
    await u.click(screen.getByRole('button', { name: 'Filter' }))
    expect(await screen.findByText('No changes recorded.')).toBeInTheDocument()
  })

  it('FE-INT-046 other users have no Audit link and are refused', async () => {
    db.users.push(user({ id: 2, username: 'ana', isAdmin: false }))
    db.session = 2
    renderRoute('/audit')
    expect(await screen.findByText(/administrator/i)).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Audit' })).not.toBeInTheDocument()
  })
})
