import { screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { http, HttpResponse } from 'msw'

import { project, testCase, testRun } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'
import { server } from '@/test/server'

const at = '2026-10-05T10:00:00Z'

const asViewer = () => {
  db.users.push({ ...db.users[0], id: 2, username: 'ana', isAdmin: false })
  db.members.push({ projectId: 1, userId: 2, role: 'viewer', since: at })
  db.session = 2
}

describe('FE-INT-038 suites and partial runs', () => {
  it('FE-INT-038 asks for a project, then lists suites and creates a query suite', async () => {
    db.testCases[0] = testCase({ tags: ['smoke'] })
    const { user: u } = renderRoute('/suites')
    expect(await screen.findByText('Choose a project (top right) to see its suites.')).toBeInTheDocument()
    await screen.findByRole('option', { name: /^TC/ })
    await u.selectOptions(screen.getByRole('combobox', { name: 'Current project' }), 'TC')
    expect(await screen.findByText('No suites in TC yet.')).toBeInTheDocument()

    await u.type(screen.getByLabelText('Name'), 'Smoke')
    await u.type(screen.getByLabelText('Key'), 'smoke')
    await u.type(screen.getByLabelText('Tag'), 'smoke')
    await u.selectOptions(screen.getByLabelText('Classification'), 'risk:critical')
    await u.click(screen.getByRole('button', { name: 'Create suite' }))
    const row = await screen.findByTestId('suite-smoke')
    expect(row).toHaveTextContent('#smoke and risk:critical')
    expect(db.suites[0]).toMatchObject({
      kind: 'query',
      query: { tag: 'smoke', classification: ['risk:critical'] },
    })
    expect(screen.getByLabelText('Name')).toHaveValue('')

    // A query without criteria and a duplicate key are reported.
    await u.type(screen.getByLabelText('Name'), 'Empty')
    await u.type(screen.getByLabelText('Key'), 'empty')
    await u.click(screen.getByRole('button', { name: 'Create suite' }))
    expect(await screen.findByText('Could not create the suite')).toBeInTheDocument()
  })

  it('FE-INT-038 a static suite lists, adds and removes test cases; archiving and the runs link', async () => {
    localStorage.setItem('provenly.project', 'TC')
    const { user: u, router } = renderRoute('/suites')
    await u.type(await screen.findByLabelText('Name'), 'Release')
    await u.type(screen.getByLabelText('Key'), 'release')
    await u.selectOptions(screen.getByLabelText('Kind'), 'static')
    expect(screen.queryByLabelText('Tag')).not.toBeInTheDocument()
    await u.click(screen.getByRole('button', { name: 'Create suite' }))
    expect(await screen.findByText('0 listed test cases')).toBeInTheDocument()
    await u.click(screen.getByRole('link', { name: 'Release' }))

    expect(await screen.findByText('This suite lists no test cases yet.')).toBeInTheDocument()
    expect(screen.getByTestId('suite-ci-hint')).toHaveTextContent('&project=TC&suite=release')
    await u.selectOptions(await screen.findByLabelText('Test case to add'), '153')
    await u.click(screen.getByRole('button', { name: 'Add to suite' }))
    const row = await screen.findByTestId('suite-case-TC-153')
    expect(db.suites[0].members).toEqual([153])
    expect(
      within(screen.getByLabelText('Test case to add')).queryByRole('option', { name: /TC-153/ }),
    ).toBeNull()

    await u.click(within(row).getByRole('button', { name: 'Remove' }))
    await waitFor(() => expect(screen.queryByTestId('suite-case-TC-153')).not.toBeInTheDocument())
    expect(db.suites[0].members).toEqual([])

    await u.click(screen.getByRole('button', { name: 'Archive' }))
    expect(await screen.findByText('archived')).toBeInTheDocument()
    await u.click(screen.getByRole('button', { name: 'Restore' }))
    await waitFor(() => expect(screen.queryByText('archived')).not.toBeInTheDocument())

    db.runs.push(testRun({ id: 8, suite: { key: 'release', name: 'Release' } }))
    await u.click(screen.getByRole('link', { name: 'Runs of this suite' }))
    expect(router.state.location.search).toBe('?project=TC&suite=release')
    expect(await screen.findByTestId('suite-filter')).toHaveTextContent('Runs of suite TC/release')
    expect(await screen.findByText('#8')).toBeInTheDocument()
    expect(screen.queryByText('#7')).not.toBeInTheDocument()
    expect(screen.getByTestId('suite-badge')).toHaveTextContent('suite: Release')
  })

  it("FE-INT-038 the runs of a suite follow the link's project, not the one chosen in the header", async () => {
    db.projects.push(project({ id: 2, key: 'CHK', name: 'Checkout', description: '' }))
    db.runs.push(testRun({ id: 8, suite: { key: 'release', name: 'Release' } }))
    db.runs.push(testRun({ id: 9, projectId: 2, suite: { key: 'release', name: 'Checkout release' } }))
    localStorage.setItem('provenly.project', 'CHK')
    renderRoute('/test-runs?project=TC&suite=release')
    expect(await screen.findByText('#8')).toBeInTheDocument()
    expect(screen.queryByText('#9')).not.toBeInTheDocument()
    expect(screen.getByTestId('suite-filter')).toHaveTextContent('Runs of suite TC/release')
  })

  it('FE-INT-038 a failed change is reported; viewers only read; unknown suites are not found', async () => {
    localStorage.setItem('provenly.project', 'TC')
    db.suites.push({
      projectId: 1,
      id: 900,
      members: [153],
      key: 'release',
      name: 'Release',
      description: '',
      kind: 'static',
      query: null,
      archivedAt: null,
      createdAt: at,
      updatedAt: at,
      caseCount: 0,
    })
    const { user: u, unmount } = renderRoute('/suites/TC/release')
    await screen.findByTestId('suite-case-TC-153')
    db.testCases.splice(1, 1) // someone removes the other candidate meanwhile: the add is refused
    await u.selectOptions(await screen.findByLabelText('Test case to add'), '154')
    await u.click(screen.getByRole('button', { name: 'Add to suite' }))
    expect(await screen.findByText('Could not change the suite')).toBeInTheDocument()
    server.use(
      http.patch('*/api/v1/projects/:projectKey/suites/:suiteKey', () =>
        HttpResponse.json(
          { code: 'internal_error', title: 'Internal Server Error', status: 500 },
          { status: 500 },
        ),
      ),
    )
    await u.click(screen.getByRole('button', { name: 'Archive' }))
    expect(await screen.findByText('Could not update the suite')).toBeInTheDocument()
    unmount()

    asViewer()
    const viewer = renderRoute('/suites/TC/release')
    await screen.findByTestId('suite-case-TC-153')
    expect(screen.queryByRole('button', { name: 'Archive' })).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Test case to add')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Remove' })).not.toBeInTheDocument()
    viewer.unmount()
    renderRoute('/suites')
    expect(await screen.findByTestId('suite-release')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Create suite' })).not.toBeInTheDocument()
  })

  it('FE-INT-038 a query suite shows its matches; a run reported for a suite says so', async () => {
    db.testCases[0] = testCase({ tags: ['smoke'] })
    db.suites.push({
      projectId: 1,
      id: 901,
      members: [],
      key: 'smoke',
      name: 'Smoke',
      description: '',
      kind: 'query',
      query: { tag: 'nightly', classification: [] },
      archivedAt: null,
      createdAt: at,
      updatedAt: at,
      caseCount: 0,
    })
    const { unmount } = renderRoute('/suites/TC/smoke')
    expect(await screen.findByText('No test case matches this query yet.')).toBeInTheDocument()
    expect(screen.queryByLabelText('Test case to add')).not.toBeInTheDocument()
    unmount()
    db.suites[0].query = { tag: 'smoke', classification: [] }
    const second = renderRoute('/suites/TC/smoke')
    expect(await screen.findByTestId('suite-case-TC-153')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Remove' })).not.toBeInTheDocument()
    second.unmount()
    renderRoute('/suites/TC/nope')
    expect(await screen.findByText('Not found')).toBeInTheDocument()

    db.runs[0] = testRun({ suite: { key: 'smoke', name: 'Smoke' } })
    renderRoute('/test-runs/7')
    expect(await screen.findByTestId('suite-badge')).toHaveTextContent('suite: Smoke')
  })
})
