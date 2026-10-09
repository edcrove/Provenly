import { screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'

import { db } from '@/test/mockApi'
import { pickProject, renderRoute } from '@/test/render'
import { server } from '@/test/server'

const at = '2026-10-05T10:00:00Z'
const jiraReq = () => ({
  projectId: 1,
  id: 950,
  provider: 'jira' as const,
  externalId: 'PAY-12',
  title: 'Refunds',
  description: 'Refund a paid order',
  url: 'https://jira.test/PAY-12',
  providerStatus: 'In Progress',
  archivedAt: null,
  lastSyncedAt: at,
  createdAt: at,
  updatedAt: at,
  testCaseIds: [153],
  coverage: { status: 'uncovered' as const, linked: 0, passed: 0, failed: 0, notRun: 0, testCases: [] },
})

describe('FE-INT-040 requirements and traceability', () => {
  it('FE-INT-040 asks for a project, lists coverage and adds native and external requirements', async () => {
    db.requirements.push(jiraReq())
    db.latest[153] = 'failed'
    const { user: u } = renderRoute('/requirements')
    expect(await screen.findByText('Pick a project to see its requirements.')).toBeInTheDocument()
    await screen.findByRole('button', { name: /^TC · / })
    await pickProject(u, 'TC')
    const row = await screen.findByTestId('requirement-PAY-12')
    expect(row).toHaveTextContent('Jira PAY-12')
    expect(row).toHaveTextContent('Failing')
    expect(row).toHaveTextContent('0/1 passing')

    await u.type(screen.getByLabelText('Title'), 'Pay by card')
    await u.click(screen.getByRole('button', { name: 'Add requirement' }))
    const native = await screen.findByTestId('requirement-R-1')
    expect(native).toHaveTextContent('Not covered')
    expect(screen.getByLabelText('Title')).toHaveValue('')

    await u.type(screen.getByLabelText('Title'), 'Chargebacks')
    await u.selectOptions(screen.getByLabelText('Source'), 'github')
    await u.type(screen.getByLabelText('Id in the source'), '42')
    await u.type(screen.getByLabelText('Link (optional)'), 'https://github.test/42')
    await u.click(screen.getByRole('button', { name: 'Add requirement' }))
    expect(await screen.findByTestId('requirement-42')).toHaveTextContent('GitHub 42')
    expect(db.requirements.at(-1)).toMatchObject({
      provider: 'github',
      externalId: '42',
      url: 'https://github.test/42',
    })

    await u.type(screen.getByLabelText('Title'), 'Again')
    await u.selectOptions(screen.getByLabelText('Source'), 'jira')
    await u.type(screen.getByLabelText('Id in the source'), 'PAY-12')
    await u.click(screen.getByRole('button', { name: 'Add requirement' }))
    expect(await screen.findByText('Could not add the requirement')).toBeInTheDocument()
  })

  it('FE-INT-040 a requirement shows the latest result of each covering test case; links, unlinks and archives', async () => {
    db.requirements.push(jiraReq())
    db.latest[153] = 'passed'
    const { user: u } = renderRoute('/requirements/TC/950')
    // FE-INT-048: the source link and the list link are separated.
    expect((await screen.findByRole('link', { name: 'Open in the source' })).parentElement).toHaveTextContent(
      'Open in the source · All requirements',
    )
    expect(await screen.findByTestId('coverage-badge')).toHaveTextContent('Passing')
    expect(screen.getByText('Status in the source: In Progress', { exact: false })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Open in the source' })).toHaveAttribute(
      'href',
      'https://jira.test/PAY-12',
    )
    expect(within(await screen.findByTestId('covering-153')).getByTestId('status-badge')).toHaveTextContent(
      'passed',
    )

    await u.selectOptions(await screen.findByLabelText('Test case to link'), '154')
    await u.click(screen.getByRole('button', { name: 'Link test case' }))
    const added = await screen.findByTestId('covering-154')
    expect(added).toHaveTextContent('not run')
    expect(screen.getByTestId('coverage-badge')).toHaveTextContent('Partially passing')
    expect(db.requirements[0].testCaseIds).toEqual([153, 154])

    await u.click(within(added).getByRole('button', { name: 'Unlink' }))
    await waitFor(() => expect(screen.queryByTestId('covering-154')).not.toBeInTheDocument())

    await u.click(screen.getByRole('button', { name: 'Archive' }))
    expect(await screen.findByText('archived')).toBeInTheDocument()
    await u.click(screen.getByRole('button', { name: 'Restore' }))
    await waitFor(() => expect(screen.queryByText('archived')).not.toBeInTheDocument())

    server.use(
      http.put('*/api/v1/projects/:projectKey/requirements/:id/test-cases', () =>
        HttpResponse.json(
          {
            type: 'about:blank',
            title: 'Bad Request',
            status: 400,
            code: 'validation_error',
            detail: 'nope',
          },
          { status: 400, headers: { 'Content-Type': 'application/problem+json' } },
        ),
      ),
      http.patch('*/api/v1/projects/:projectKey/requirements/:id', () =>
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
    await u.click(within(screen.getByTestId('covering-153')).getByRole('button', { name: 'Unlink' }))
    expect(await screen.findByText('Could not change the coverage')).toBeInTheDocument()
    await u.click(screen.getByRole('button', { name: 'Archive' }))
    expect(await screen.findByText('Could not update the requirement')).toBeInTheDocument()
  })

  it('FE-INT-040 the test case page lists what it covers; viewers only read; unknown requirements are not found', async () => {
    db.requirements.push({ ...jiraReq(), url: '', providerStatus: '', lastSyncedAt: null, description: '' })
    const { unmount } = renderRoute('/test-cases/153')
    const covered = await screen.findByTestId('covered-requirements')
    expect(covered).toHaveTextContent('Jira PAY-12')
    expect(covered).toHaveTextContent('Not run')
    unmount()
    const other = renderRoute('/test-cases/154')
    expect(await screen.findByText('This test case covers no requirement.')).toBeInTheDocument()
    other.unmount()

    db.users.push({ ...db.users[0], id: 2, username: 'ana', isAdmin: false })
    db.members.push({ projectId: 1, userId: 2, role: 'viewer', since: at })
    db.session = 2
    localStorage.setItem('provenly.project', 'TC')
    const list = renderRoute('/requirements')
    await screen.findByTestId('requirement-PAY-12')
    expect(screen.queryByRole('button', { name: 'Add requirement' })).not.toBeInTheDocument()
    list.unmount()
    const detail = renderRoute('/requirements/TC/950')
    await screen.findByTestId('covering-153')
    expect(screen.queryByRole('button', { name: 'Unlink' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Archive' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Open in the source' })).not.toBeInTheDocument()
    detail.unmount()
    const unknown = renderRoute('/requirements/TC/987654')
    expect(await screen.findByText('Not found')).toBeInTheDocument()
    unknown.unmount()
    // A malformed id is not found at once (it used to load forever).
    for (const bad of ['abc', '-1']) {
      const page = renderRoute(`/requirements/TC/${bad}`)
      expect(await screen.findByRole('heading', { name: 'Page not found' })).toBeInTheDocument()
      page.unmount()
    }
  })

  it('FE-INT-040 an empty project says so; a covering test case outside the first page shows its id', async () => {
    localStorage.setItem('provenly.project', 'TC')
    const { unmount } = renderRoute('/requirements')
    expect(await screen.findByText('No requirements in TC yet.')).toBeInTheDocument()
    unmount()
    db.requirements.push({ ...jiraReq(), testCaseIds: [], archivedAt: at })
    const detail = renderRoute('/requirements/TC/950')
    expect(await screen.findByText('No test case covers this requirement yet.')).toBeInTheDocument()
    expect(screen.getByText('archived')).toBeInTheDocument()
    detail.unmount()
    db.requirements[0].testCaseIds = [987654]
    renderRoute('/requirements/TC/950')
    expect(await screen.findByText('#987654')).toBeInTheDocument()
    expect(screen.getByTestId('coverage-badge')).toHaveTextContent('Not run')
  })
})
