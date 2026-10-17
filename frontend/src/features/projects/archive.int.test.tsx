import { screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'

import { project, testCase, testRun, user } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { projectSwitcher, renderRoute } from '@/test/render'
import { server } from '@/test/server'

const at = '2026-10-05T10:00:00Z'
const addOld = (archivedAt: string | null = null) => {
  db.projects.push(project({ id: 2, key: 'OLD', name: 'Old product', archivedAt }))
  db.testCases.push(
    testCase({ id: 300, projectId: 2, projectKey: 'OLD', key: 'OLD-1', title: 'Old checkout' }),
  )
}

describe('FE-INT-069 archived projects (Ed, 2026-10-11)', () => {
  it('FE-INT-069 an administrator archives a project after confirming: it leaves the lists and the switcher, and unarchiving brings it back', async () => {
    addOld()
    const { user: u } = renderRoute('/projects')
    const row = await screen.findByTestId('project-OLD')
    expect(
      within(await screen.findByTestId('project-TC')).queryByRole('button', { name: 'Archive…' }),
    ).toBeNull()

    await u.click(within(row).getByRole('button', { name: 'Archive…' }))
    await u.click(within(row).getByRole('button', { name: 'Cancel' }))
    expect(db.projects[1].archivedAt).toBeNull()
    await u.click(within(row).getByRole('button', { name: 'Archive…' }))
    const confirm = within(row).getByRole('group', { name: 'Archive OLD' })
    expect(confirm).toHaveTextContent('It leaves every list and becomes read-only; nothing is deleted.')
    await u.click(within(confirm).getByRole('button', { name: 'Archive' }))
    await waitFor(() => expect(screen.queryByTestId('project-OLD')).not.toBeInTheDocument())
    expect(db.projects[1].archivedAt).not.toBeNull()

    await u.click(projectSwitcher())
    const list = await screen.findByRole('listbox', { name: 'Projects' })
    expect(within(list).queryByRole('option', { name: /^OLD · / })).not.toBeInTheDocument()
    await u.click(projectSwitcher())

    await u.click(screen.getByRole('checkbox', { name: 'Show archived projects instead' }))
    const archived = await screen.findByTestId('project-OLD')
    expect(screen.queryByTestId('project-TC')).not.toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: 'Archived' })).toBeInTheDocument()
    expect(within(archived).queryByRole('button', { name: 'Rename' })).toBeNull()
    await u.click(within(archived).getByRole('button', { name: 'Unarchive' }))
    await waitFor(() => expect(screen.queryByTestId('project-OLD')).not.toBeInTheDocument())
    expect(await screen.findByText('No archived projects.')).toBeInTheDocument()
    expect(db.projects[1].archivedAt).toBeNull()
  })

  it('FE-INT-069 only administrators archive: a maintainer sees no archive control', async () => {
    addOld()
    db.users.push(user({ id: 2, username: 'ana', displayName: 'Ana Pérez', isAdmin: false }))
    db.members.push({ projectId: 2, userId: 2, role: 'maintainer', since: at })
    db.session = 2
    renderRoute('/projects')
    const row = await screen.findByTestId('project-OLD')
    expect(within(row).getByRole('button', { name: 'Rename' })).toBeInTheDocument()
    expect(within(row).queryByRole('button', { name: 'Archive…' })).toBeNull()
  })

  it('FE-INT-069 an archived project is left out of lists that span projects, and its settings say it is archived', async () => {
    addOld('2026-10-10T10:00:00Z')
    const { router } = renderRoute('/test-cases')
    expect(await screen.findByText('Login works')).toBeInTheDocument()
    expect(screen.queryByText('Old checkout')).not.toBeInTheDocument()
    expect(screen.queryByTestId('archived-notice')).not.toBeInTheDocument()

    await router.navigate('/projects/OLD')
    const notice = await screen.findByTestId('archived-notice')
    expect(notice).toHaveTextContent(/OLD · Old product is archived/)
    expect(within(notice).getByRole('link', { name: 'archived projects' })).toHaveAttribute(
      'href',
      '/projects?archived=1',
    )
  })

  it('FE-INT-069 the current project, archived, is named on every page and offers no changes', async () => {
    addOld('2026-10-10T10:00:00Z')
    localStorage.setItem('provenly.project', 'OLD')
    const { router, user: u } = renderRoute('/test-cases/300')
    expect(await screen.findByRole('heading', { name: /OLD-1/ })).toBeInTheDocument()
    expect(await screen.findAllByTestId('archived-notice')).toHaveLength(1)
    expect(projectSwitcher()).toHaveAccessibleName('Current project: OLD · Old product')
    expect(screen.queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Deprecate' })).not.toBeInTheDocument()
    // Unarchived, the same administrator edits it again.
    await router.navigate('/projects?archived=1')
    await u.click(within(await screen.findByTestId('project-OLD')).getByRole('button', { name: 'Unarchive' }))
    await waitFor(() => expect(screen.queryByTestId('project-OLD')).not.toBeInTheDocument())
    await router.navigate('/test-cases/300')
    expect(await screen.findByRole('button', { name: 'Deprecate' })).toBeInTheDocument()
    expect(screen.queryByTestId('archived-notice')).not.toBeInTheDocument()
  })

  it('FE-INT-069 a refused archive says why and keeps the project listed', async () => {
    addOld()
    server.use(
      http.post('*/api/v1/projects/OLD/archive', () =>
        HttpResponse.json(
          {
            type: 'about:blank',
            title: 'Conflict',
            status: 409,
            code: 'conflict',
            detail: 'try again later',
          },
          { status: 409, headers: { 'Content-Type': 'application/problem+json' } },
        ),
      ),
    )
    const { user: u } = renderRoute('/projects')
    const row = await screen.findByTestId('project-OLD')
    await u.click(within(row).getByRole('button', { name: 'Archive…' }))
    await u.click(within(row).getByRole('button', { name: 'Archive' }))
    expect(await within(row).findByText('Could not archive the project')).toBeInTheDocument()
    expect(within(row).getByText(/try again later/)).toBeInTheDocument()
  })

  it('FE-INT-071 a run of an archived project (not in the projects list) still names it: breadcrumb and archived notice', async () => {
    addOld('2026-10-10T10:00:00Z')
    db.runs.push(testRun({ id: 70, projectId: 2, projectKey: 'OLD', externalRunId: 'github:70:1' }))
    // Another project's run (the current one is TC): named, and said to be archived, exactly once.
    renderRoute('/test-runs/70')
    expect(await screen.findByRole('heading', { name: 'Test run #70' })).toBeInTheDocument()
    expect(screen.getByRole('navigation', { name: 'Breadcrumb' })).toHaveTextContent(/OLD/)
    expect(await screen.findAllByTestId('archived-notice')).toHaveLength(1)
  })

  it('FE-INT-071 the runs list of an archived current project names it (it used to read "—", from the active projects list)', async () => {
    addOld('2026-10-10T10:00:00Z')
    db.runs.push(testRun({ id: 70, projectId: 2, projectKey: 'OLD', externalRunId: 'github:70:1' }))
    localStorage.setItem('provenly.project', 'OLD')
    const { router } = renderRoute('/test-runs')
    expect(await screen.findByTestId('run-project')).toHaveTextContent('OLD')
    await router.navigate('/test-runs/70')
    expect(await screen.findByRole('heading', { name: 'Test run #70' })).toBeInTheDocument()
    expect(await screen.findAllByTestId('archived-notice')).toHaveLength(1)
  })
})
