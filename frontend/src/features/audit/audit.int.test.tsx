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
  summary: 'created a test case',
  testCase: null,
  ip: null,
  userAgent: null,
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
    expect(await screen.findByTestId('audit-22')).toHaveTextContent('created a test case/api/v1/test-cases')
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
    expect(await screen.findByText('No changes match these filters.')).toBeInTheDocument()
  })

  it('FE-INT-046 other users have no Audit link and are refused', async () => {
    db.users.push(user({ id: 2, username: 'ana', isAdmin: false }))
    db.session = 2
    renderRoute('/audit')
    expect(await screen.findByText(/administrator/i)).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Audit' })).not.toBeInTheDocument()
  })
})

describe('FE-INT-054 readable audit entries', () => {
  it('FE-INT-054 each change reads in words with its path as the detail, and the log filters by test case (card #48)', async () => {
    db.audit.push(
      event(1, {
        summary: null,
        action: 'PATCH /api/v1/test-cases/{testCaseId}',
        path: '/api/v1/test-cases/9',
      }),
      event(2, {
        summary: 'edited CHK-4 step 3',
        action: 'PATCH /api/v1/test-cases/{testCaseId}/steps/{stepId}',
        path: '/api/v1/test-cases/4/steps/30',
        project: 'CHK',
        testCase: 'CHK-4',
      }),
    )
    const { user: u, router } = renderRoute('/audit')
    const edit = await screen.findByTestId('audit-2')
    expect(edit).toHaveTextContent('edited CHK-4 step 3/api/v1/test-cases/4/steps/30')
    expect(edit).toHaveTextContent('CHK')
    // An event recorded before summaries existed shows its route.
    expect(screen.getByTestId('audit-1')).toHaveTextContent(
      'PATCH /api/v1/test-cases/{testCaseId}/api/v1/test-cases/9',
    )

    await u.type(screen.getByLabelText('Test case'), ' chk-4 ')
    await u.click(screen.getByRole('button', { name: 'Filter' }))
    await waitFor(() => expect(screen.queryByTestId('audit-1')).not.toBeInTheDocument())
    expect(router.state.location.search).toBe('?testCase=CHK-4')
    expect(screen.getByTestId('audit-2')).toBeInTheDocument()
  })
})

describe('FE-INT-055 sign-in events in the audit log (card #49)', () => {
  it('FE-INT-055 a failed sign-in by an unknown username reads "unknown" with the client address and user agent', async () => {
    db.audit.push(
      event(1, {
        actor: 'unknown',
        action: 'POST /api/v1/auth/login',
        path: '/api/v1/auth/login',
        status: 401,
        summary: 'failed to sign in',
        ip: '198.51.100.7',
        userAgent: 'Mozilla/5.0',
      }),
    )
    renderRoute('/audit')
    const row = await screen.findByTestId('audit-1')
    expect(row).toHaveTextContent('unknown198.51.100.7')
    expect(row).toHaveTextContent('failed to sign in/api/v1/auth/login')
    expect(within(row).getByText('198.51.100.7')).toHaveAttribute('title', 'Mozilla/5.0')
  })
})
