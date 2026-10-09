import { screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { project } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { projectSwitcher, renderRoute } from '@/test/render'

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
})
