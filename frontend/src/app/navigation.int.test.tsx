import { screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { project, testRun, user } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { pickProject, projectSwitcher, renderRoute } from '@/test/render'

const projectPages = (root: HTMLElement) =>
  within(within(root).getByRole('navigation', { name: 'Project pages' }))
    .getAllByRole('link')
    .map((l) => l.textContent)

describe('FE-INT-059 navigation: the project first, then what it holds', () => {
  it('FE-INT-059 the header lists the project pages in working order; Settings only for one project', async () => {
    const { user: u, router } = renderRoute('/test-runs')
    const header = await screen.findByRole('banner')
    await waitFor(() => expect(projectSwitcher()).toHaveAccessibleName('Current project: All projects'))
    expect(projectPages(header)).toEqual([
      'Dashboard',
      'Test Runs',
      'Test Cases',
      'Suites',
      'Requirements',
      'Issues',
    ])
    expect(screen.getByTestId('scope-label')).toHaveTextContent('All projects')
    const workspace = within(header).getByRole('navigation', { name: 'Workspace' })
    expect(
      within(workspace)
        .getAllByRole('link')
        .map((l) => l.textContent),
    ).toEqual(['Projects', 'Users', 'Audit'])

    await pickProject(u, 'TC')
    expect(projectSwitcher()).toHaveAccessibleName('Current project: TC · Default')
    expect(projectPages(header).at(-1)).toBe('Settings')
    expect(screen.getByTestId('scope-label')).toHaveTextContent('TC · Default')
    await u.click(within(header).getByRole('link', { name: 'Settings' }))
    expect(router.state.location.pathname).toBe('/projects/TC')
    await u.click(screen.getByRole('link', { name: 'Provenly home' }))
    expect(router.state.location.pathname).toBe('/dashboard')
  })

  it('FE-INT-059 people who are not administrators see Projects but not Users or Audit', async () => {
    db.users.push(user({ id: 2, username: 'ana', isAdmin: false }))
    db.members.push({ projectId: 1, userId: 2, role: 'member', since: '2026-10-05T10:00:00Z' })
    db.session = 2
    renderRoute('/test-cases')
    const workspace = await screen.findByRole('navigation', { name: 'Workspace' })
    expect(
      within(workspace)
        .getAllByRole('link')
        .map((l) => l.textContent),
    ).toEqual(['Projects'])
  })

  it('FE-INT-059 the switcher is searchable and works with the keyboard', async () => {
    for (const [id, key, name] of [
      [2, 'CHK', 'Checkout'],
      [3, 'PAY', 'Payments'],
    ] as const)
      db.projects.push(project({ id, key, name, description: '' }))
    const { user: u, router } = renderRoute('/test-cases')
    await waitFor(() => expect(projectSwitcher()).toHaveAccessibleName('Current project: All projects'))

    await u.click(projectSwitcher())
    const find = screen.getByRole('combobox', { name: 'Find a project' })
    expect(find).toHaveFocus()
    const options = () =>
      within(screen.getByRole('listbox', { name: 'Projects' }))
        .getAllByRole('option')
        .map((o) => o.textContent)
    expect(options()).toEqual(['All projects', 'CHK · Checkout', 'PAY · Payments', 'TC · Default'])
    expect(screen.getByRole('option', { name: 'All projects' })).toHaveAttribute('aria-selected', 'true')

    // Typing filters by key or name; arrows move, Enter picks.
    await u.type(find, 'pay')
    expect(options()).toEqual(['PAY · Payments'])
    await u.clear(find)
    await u.type(find, 'check')
    expect(options()).toEqual(['CHK · Checkout'])
    await u.clear(find)
    await u.type(find, 'zzz')
    expect(screen.getByText('No project matches.')).toBeInTheDocument()
    await u.clear(find)
    await u.keyboard(
      '{ArrowUp}{ArrowDown}{ArrowDown}{ArrowDown}{ArrowDown}{ArrowDown}{ArrowUp}{ArrowUp}{Enter}',
    )
    expect(projectSwitcher()).toHaveAccessibleName('Current project: CHK · Checkout')
    expect(projectSwitcher()).toHaveFocus()
    expect(screen.queryByRole('listbox', { name: 'Projects' })).not.toBeInTheDocument()

    // Escape and a click elsewhere close without changing anything; hovering marks the option.
    await u.click(projectSwitcher())
    await u.hover(screen.getByRole('option', { name: 'PAY · Payments' }))
    await u.keyboard('{Escape}')
    expect(screen.queryByRole('listbox', { name: 'Projects' })).not.toBeInTheDocument()
    expect(projectSwitcher()).toHaveFocus()
    await u.click(projectSwitcher())
    await u.click(screen.getByRole('heading', { name: 'Test Cases' }))
    expect(screen.queryByRole('listbox', { name: 'Projects' })).not.toBeInTheDocument()
    await u.click(projectSwitcher())
    await u.click(projectSwitcher())
    expect(screen.queryByRole('listbox', { name: 'Projects' })).not.toBeInTheDocument()
    expect(projectSwitcher()).toHaveAccessibleName('Current project: CHK · Checkout')

    // Every project is one click away.
    await u.click(projectSwitcher())
    await u.click(screen.getByRole('link', { name: 'View all projects' }))
    expect(router.state.location.pathname).toBe('/projects')
    expect(screen.queryByRole('listbox', { name: 'Projects' })).not.toBeInTheDocument()
  })

  it('FE-INT-059 on a phone the items move into a Menu that closes when one is chosen', async () => {
    const { user: u, router } = renderRoute('/test-cases')
    const menu = await screen.findByRole('button', { name: 'Menu' })
    expect(menu).toHaveAttribute('aria-expanded', 'false')
    await u.click(menu)
    const panel = document.getElementById('main-menu')!
    expect(projectPages(panel)).toEqual([
      'Dashboard',
      'Test Runs',
      'Test Cases',
      'Suites',
      'Requirements',
      'Issues',
    ])
    expect(within(panel).getByRole('link', { name: 'Ada Admin' })).toBeInTheDocument()
    await u.click(within(panel).getByRole('link', { name: 'Issues' }))
    expect(router.state.location.pathname).toBe('/issues')
    expect(document.getElementById('main-menu')).toBeNull()

    // Choosing a project from the switcher also closes the menu (the page now shows that project).
    await u.click(screen.getByRole('button', { name: 'Menu' }))
    await pickProject(u, 'TC')
    expect(document.getElementById('main-menu')).toBeNull()
    await u.click(screen.getByRole('button', { name: 'Menu' }))
    expect(
      within(document.getElementById('main-menu')!).getByRole('link', { name: 'Settings' }),
    ).toHaveAttribute('href', '/projects/TC')
    await u.click(within(document.getElementById('main-menu')!).getByRole('link', { name: 'Projects' }))
    expect(router.state.location.pathname).toBe('/projects')
    await u.click(screen.getByRole('button', { name: 'Menu' }))
    await u.click(within(document.getElementById('main-menu')!).getByRole('link', { name: 'Ada Admin' }))
    expect(router.state.location.pathname).toBe('/account')
    await u.click(screen.getByRole('button', { name: 'Menu' }))
    await u.click(within(document.getElementById('main-menu')!).getByRole('button', { name: 'Sign out' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
  })

  it('FE-INT-059 a page of one project, with every project chosen, offers the projects to pick', async () => {
    for (let id = 2; id <= 10; id++)
      db.projects.push(project({ id, key: `P${id}`, name: `Project ${id}`, description: '' }))
    const { user: u } = renderRoute('/suites')
    const chooser = await screen.findByTestId('project-chooser')
    await within(chooser).findByRole('button', { name: 'P8 · Project 8' })
    expect(within(chooser).getAllByRole('button')).toHaveLength(8)
    expect(chooser).toHaveTextContent('2 more: view all projects or use the project switcher.')
    await u.click(within(chooser).getByRole('button', { name: 'P2 · Project 2' }))
    expect(projectSwitcher()).toHaveAccessibleName('Current project: P2 · Project 2')
    expect(screen.queryByTestId('project-chooser')).not.toBeInTheDocument()
  })

  it('FE-INT-059 detail pages show where they sit and offer to switch to their project', async () => {
    db.projects.push(project({ id: 2, key: 'CHK', name: 'Checkout', description: '' }))
    db.runs.push(testRun({ id: 8, projectId: 2 }))
    const { user: u, router } = renderRoute('/test-runs/8')
    const trail = await screen.findByRole('navigation', { name: 'Breadcrumb' })
    await waitFor(() => expect(trail).toHaveTextContent('CHK · Checkout/Test Runs/Run #8'))
    await u.click(within(trail).getByRole('button', { name: 'Switch to CHK' }))
    expect(projectSwitcher()).toHaveAccessibleName('Current project: CHK · Checkout')
    expect(within(trail).queryByRole('button', { name: /Switch/ })).not.toBeInTheDocument()
    await u.click(within(trail).getByRole('link', { name: 'Test Runs' }))
    expect(router.state.location.pathname).toBe('/test-runs')
  })
})
