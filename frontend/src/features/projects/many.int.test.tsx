import { screen, waitFor, within } from '@testing-library/react'
import { delay, http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'

import { project } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { projectSwitcher, renderRoute } from '@/test/render'
import { server } from '@/test/server'

/** 120 projects whose keys sort before TC, so TC is not on the first page of the projects list. */
const manyProjects = () => {
  for (let i = 0; i < 120; i++)
    db.projects.push(project({ id: 100 + i, key: `A${String(i).padStart(3, '0')}`, name: `Bulk ${i}` }))
}

describe('FE-INT-068 an instance with more projects than a page (deployed E2E on Render)', () => {
  it('FE-INT-068 the switcher searches the server, says how many it shows and finds a project past the first page', async () => {
    manyProjects()
    const { user: u } = renderRoute('/test-runs')
    await u.click(await screen.findByRole('button', { name: 'Current project: All projects' }))
    const list = await screen.findByRole('listbox', { name: 'Projects' })
    expect(await screen.findByTestId('switcher-more')).toHaveTextContent(
      'Showing 50 of 121: type to find the others.',
    )
    expect(within(list).queryByRole('option', { name: /^TC · / })).not.toBeInTheDocument()
    await u.type(screen.getByRole('combobox', { name: 'Find a project' }), 'default')
    await u.click(await within(list).findByRole('option', { name: 'TC · Default' }))
    expect(projectSwitcher()).toHaveAccessibleName('Current project: TC · Default')
    expect(await screen.findByTestId('scope-label')).toHaveTextContent('TC · Default')
  })

  it('FE-INT-068 a project past the first page keeps its role: an administrator edits its test case', async () => {
    manyProjects()
    localStorage.setItem('provenly.project', 'TC')
    renderRoute('/test-cases/153')
    expect(await screen.findByRole('button', { name: 'Edit' })).toBeInTheDocument()
    expect(projectSwitcher()).toHaveAccessibleName('Current project: TC · Default')
  })

  it('FE-INT-068 the projects page finds a project by key or name, and forms say how many projects they do not list', async () => {
    manyProjects()
    const { user: u } = renderRoute('/projects')
    await screen.findByTestId('project-A000')
    expect(screen.queryByTestId('project-TC')).not.toBeInTheDocument()
    await u.type(screen.getByLabelText('Search projects'), 'default')
    await u.click(screen.getByRole('button', { name: 'Find' }))
    expect(await screen.findByTestId('project-TC')).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByTestId('project-A000')).not.toBeInTheDocument())
    await u.clear(screen.getByLabelText('Search projects'))
    await u.type(screen.getByLabelText('Search projects'), 'nothing like it')
    await u.click(screen.getByRole('button', { name: 'Find' }))
    expect(await screen.findByText('No project matches “nothing like it”.')).toBeInTheDocument()

    const form = renderRoute('/users')
    expect(await screen.findAllByText(/21 more projects not listed/)).not.toHaveLength(0)
    form.unmount()
  })

  it('FE-INT-075 the switcher says when it is searching or could not load; a server error keeps the stored project', async () => {
    const problem = (status: number) =>
      HttpResponse.json(
        { type: 'about:blank', title: 'boom', status, code: 'internal_error', detail: 'boom' },
        { status },
      )
    server.use(http.get('*/api/v1/projects', async () => (await delay(200), problem(500))))
    localStorage.setItem('provenly.project', 'TC')
    const { user: u } = renderRoute('/test-runs')
    await u.click(await screen.findByRole('button', { name: 'Current project: TC · Default' }))
    expect(await screen.findByTestId('switcher-searching')).toHaveTextContent('Searching…')
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not load the projects')
    expect(screen.queryByText('No project matches.')).not.toBeInTheDocument()
  })

  it('FE-INT-075 a server error reading the current project does not widen the scope to every project; a 404 does', async () => {
    const problem = (status: number) =>
      HttpResponse.json({ type: 'about:blank', title: 'x', status, code: 'x', detail: 'x' }, { status })
    server.use(http.get('*/api/v1/projects/TC', () => problem(500)))
    localStorage.setItem('provenly.project', 'TC')
    const first = renderRoute('/test-runs')
    expect(await screen.findByRole('button', { name: 'Current project: TC' })).toBeInTheDocument()
    first.unmount()
    server.use(http.get('*/api/v1/projects/TC', () => problem(404)))
    renderRoute('/test-runs')
    expect(await screen.findByRole('button', { name: 'Current project: All projects' })).toBeInTheDocument()
  })

  it('FE-INT-075 the project chooser says when it is loading or could not load', async () => {
    server.use(
      http.get('*/api/v1/projects', async () => {
        await delay(200)
        return HttpResponse.json(
          { type: 'about:blank', title: 'boom', status: 500, code: 'internal_error', detail: 'boom' },
          { status: 500 },
        )
      }),
    )
    renderRoute('/suites')
    const chooser = await screen.findByTestId('project-chooser')
    expect(chooser).toHaveTextContent('Loading the projects…')
    expect(await within(chooser).findByText('Could not load the projects')).toBeInTheDocument()
  })
})
