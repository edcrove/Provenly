import { screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'

import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'
import { server } from '@/test/server'

const at = '2026-10-05T10:00:00Z'
const jiraIssue = () => ({
  projectId: 1,
  id: 960,
  provider: 'jira' as const,
  externalId: 'PAY-7',
  title: 'Login rejects valid passwords',
  description: 'Since 2.3',
  url: 'https://jira.test/PAY-7',
  state: 'open' as const,
  providerStatus: 'In Progress',
  closedAt: null as string | null,
  lastSyncedAt: at as string | null,
  createdAt: at,
  updatedAt: at,
  testCaseIds: [153],
  verification: { status: 'unlinked' as const, testCases: [] },
})

const problem500 = () =>
  HttpResponse.json(
    { type: 'about:blank', title: 'Internal Server Error', status: 500, code: 'internal_error', detail: 'x' },
    { status: 500, headers: { 'Content-Type': 'application/problem+json' } },
  )

describe('FE-INT-041 issues and verification', () => {
  it('FE-INT-041 asks for a project, lists verification, filters by state and adds native and tracker issues', async () => {
    db.issues.push(jiraIssue())
    db.latest[153] = 'failed'
    const { user: u } = renderRoute('/issues')
    expect(await screen.findByText('Choose a project (top right) to see its issues.')).toBeInTheDocument()
    await screen.findByRole('option', { name: /^TC/ })
    await u.selectOptions(screen.getByRole('combobox', { name: 'Current project' }), 'TC')
    const row = await screen.findByTestId('issue-PAY-7')
    expect(row).toHaveTextContent('Jira PAY-7')
    expect(row).toHaveTextContent('open')
    expect(row).toHaveTextContent('Known issue')

    await u.selectOptions(screen.getByLabelText('State'), 'closed')
    expect(await screen.findByText('No issues here.')).toBeInTheDocument()
    await u.selectOptions(screen.getByLabelText('State'), 'open')
    expect(await screen.findByTestId('issue-PAY-7')).toBeInTheDocument()
    await u.selectOptions(screen.getByLabelText('State'), '')

    await u.type(screen.getByLabelText('Title'), 'Checkout crashes')
    await u.type(screen.getByLabelText('Link (optional)'), 'https://x.test/1')
    await u.click(screen.getByRole('button', { name: 'Add issue' }))
    expect(await screen.findByTestId('issue-I-1')).toHaveTextContent('No linked test')
    expect(screen.getByLabelText('Title')).toHaveValue('')
    await u.type(screen.getByLabelText('Title'), 'Slow search')
    await u.selectOptions(screen.getByLabelText('Tracker'), 'github')
    await u.type(screen.getByLabelText('Id in the tracker'), '42')
    await u.click(screen.getByRole('button', { name: 'Add issue' }))
    expect(await screen.findByTestId('issue-42')).toHaveTextContent('GitHub 42')

    await u.type(screen.getByLabelText('Title'), 'Again')
    await u.selectOptions(screen.getByLabelText('Tracker'), 'jira')
    await u.type(screen.getByLabelText('Id in the tracker'), 'PAY-7')
    await u.click(screen.getByRole('button', { name: 'Add issue' }))
    expect(await screen.findByText('Could not add the issue')).toBeInTheDocument()
  })

  it('FE-INT-041 an issue shows the evidence and verification of each test; closes, reopens, links and unlinks', async () => {
    db.issues.push(jiraIssue())
    db.latest[153] = 'failed'
    db.latest[154] = 'skipped'
    const { user: u } = renderRoute('/issues/TC/960')
    // FE-INT-048: the tracker link and the list link are separated.
    expect(
      (await screen.findByRole('link', { name: 'Open in the tracker' })).parentElement,
    ).toHaveTextContent('Open in the tracker · All issues')
    expect(await screen.findByTestId('verification-badge')).toHaveTextContent('Known issue')
    expect(screen.getByText('Status in the tracker: In Progress', { exact: false })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Open in the tracker' })).toHaveAttribute(
      'href',
      'https://jira.test/PAY-7',
    )
    const failing = await screen.findByTestId('reproducing-153')
    expect(within(failing).getByTestId('status-badge')).toHaveTextContent('failed')
    expect(within(failing).getByRole('link', { name: 'run #1' })).toHaveAttribute('href', '/test-runs/1')

    await u.click(screen.getByRole('button', { name: 'Close issue' }))
    await waitFor(() =>
      expect(screen.getByTestId('verification-badge')).toHaveTextContent('Reopen candidate'),
    )
    expect(screen.getByTestId('issue-state')).toHaveTextContent('closed')
    expect(screen.getByText(/^Status in the tracker: In Progress · Closed /)).toBeInTheDocument()
    db.latest[153] = 'passed'
    await u.click(screen.getByRole('button', { name: 'Reopen issue' }))
    await waitFor(() =>
      expect(screen.getByTestId('verification-badge')).toHaveTextContent('Not reproducible'),
    )

    await u.selectOptions(await screen.findByLabelText('Test case to link'), '154')
    await u.click(screen.getByRole('button', { name: 'Link test case' }))
    const added = await screen.findByTestId('reproducing-154')
    expect(added).toHaveTextContent('no conclusive result')
    expect(added).toHaveTextContent('latest run skipped it')
    expect(screen.getByTestId('verification-badge')).toHaveTextContent('Unverified')
    await u.click(within(added).getByRole('button', { name: 'Unlink' }))
    await waitFor(() => expect(screen.queryByTestId('reproducing-154')).not.toBeInTheDocument())

    server.use(
      http.put('*/api/v1/projects/:projectKey/issues/:id/test-cases', problem500),
      http.patch('*/api/v1/projects/:projectKey/issues/:id', problem500),
    )
    await u.click(within(screen.getByTestId('reproducing-153')).getByRole('button', { name: 'Unlink' }))
    expect(await screen.findByText('Could not change the linked tests')).toBeInTheDocument()
    await u.click(screen.getByRole('button', { name: 'Close issue' }))
    expect(await screen.findByText('Could not update the issue')).toBeInTheDocument()
  })

  it('FE-INT-041 the test case page lists its issues; viewers only read; unknown issues are not found', async () => {
    db.issues.push({ ...jiraIssue(), url: '', providerStatus: '', lastSyncedAt: null, description: '' })
    const { unmount } = renderRoute('/test-cases/153')
    const linked = await screen.findByTestId('linked-issues')
    expect(linked).toHaveTextContent('Jira PAY-7')
    expect(linked).toHaveTextContent('Unverified')
    unmount()
    const other = renderRoute('/test-cases/154')
    expect(await screen.findByText('No issue is linked to this test case.')).toBeInTheDocument()
    other.unmount()

    db.users.push({ ...db.users[0], id: 2, username: 'ana', isAdmin: false })
    db.members.push({ projectId: 1, userId: 2, role: 'viewer', since: at })
    db.session = 2
    localStorage.setItem('provenly.project', 'TC')
    const list = renderRoute('/issues')
    await screen.findByTestId('issue-PAY-7')
    expect(screen.queryByRole('button', { name: 'Add issue' })).not.toBeInTheDocument()
    list.unmount()
    const detail = renderRoute('/issues/TC/960')
    await screen.findByTestId('reproducing-153')
    expect(screen.queryByRole('button', { name: 'Unlink' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Close issue' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Open in the tracker' })).not.toBeInTheDocument()
    detail.unmount()
    const unknown = renderRoute('/issues/TC/987654')
    expect(await screen.findByText('Not found')).toBeInTheDocument()
    unknown.unmount()
    // A malformed id is not found at once (it used to load forever).
    for (const bad of ['abc', '0']) {
      const page = renderRoute(`/issues/TC/${bad}`)
      expect(await screen.findByRole('heading', { name: 'Page not found' })).toBeInTheDocument()
      page.unmount()
    }
  })

  it('FE-INT-041 a closed issue without tests says so; a test outside the first page shows its id', async () => {
    db.issues.push({ ...jiraIssue(), testCaseIds: [], state: 'closed', closedAt: at })
    const detail = renderRoute('/issues/TC/960')
    expect(await screen.findByText('No test case reproduces this issue yet.')).toBeInTheDocument()
    expect(screen.getByTestId('verification-badge')).toHaveTextContent('No linked test')
    expect(screen.getByRole('button', { name: 'Reopen issue' })).toBeInTheDocument()
    detail.unmount()
    db.issues[0].testCaseIds = [987654]
    renderRoute('/issues/TC/960')
    expect(await screen.findByText('#987654')).toBeInTheDocument()
  })

  it('FE-INT-041 a run splits its failures into known issues and new failures', async () => {
    const { unmount } = renderRoute('/test-runs/7')
    expect(await screen.findByTestId('run-new-failures')).toHaveTextContent(
      'New failures (no open issue): TC-153',
    )
    unmount()
    db.issues.push(jiraIssue())
    renderRoute('/test-runs/7')
    const known = await screen.findByTestId('run-known-issues')
    await waitFor(() =>
      expect(known).toHaveTextContent(
        'Known issue: TC-153 fails with Jira PAY-7 (Login rejects valid passwords)',
      ),
    )
    expect(screen.queryByTestId('run-new-failures')).not.toBeInTheDocument()
  })
})
