import { screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { project, user } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { projectSwitcher, renderRoute } from '@/test/render'

describe('FE-INT-060 a project and its parts', () => {
  it('FE-INT-060 the projects page opens a project by its name and its settings by a link', async () => {
    db.projects.push(project({ id: 2, key: 'CHK', name: 'Checkout', description: '' }))
    const { user: u, router } = renderRoute('/projects')
    const row = await screen.findByTestId('project-CHK')
    await u.click(within(row).getByRole('link', { name: 'Checkout' }))
    expect(router.state.location.pathname).toBe('/dashboard')
    await waitFor(() => expect(projectSwitcher()).toHaveAccessibleName('Current project: CHK · Checkout'))

    await router.navigate('/projects')
    await u.click(within(await screen.findByTestId('project-TC')).getByRole('link', { name: 'Settings' }))
    expect(router.state.location.pathname).toBe('/projects/TC')
  })

  it('FE-INT-060 settings say what a project holds, link to its pages and list their sections', async () => {
    db.projects.push(project({ id: 2, key: 'CHK', name: 'Checkout', description: '' }))
    const { user: u, router } = renderRoute('/projects/CHK')
    expect(
      await screen.findByRole('heading', { level: 1, name: 'CHK · Checkout — Settings' }),
    ).toBeInTheDocument()
    expect(screen.getByText(/numbered CHK-1, CHK-2…/)).toBeInTheDocument()
    const sections = screen.getByRole('navigation', { name: 'On this page' })
    expect(
      within(sections)
        .getAllByRole('link')
        .map((l) => l.getAttribute('href')),
    ).toEqual(['#members', '#classification', '#api-keys', '#webhooks', '#github'])
    for (const id of ['members', 'classification', 'api-keys', 'webhooks', 'github'])
      expect(document.getElementById(id)).not.toBeNull()

    // How CI reports into this project, before any key exists.
    const ci = screen.getByTestId('ci-snippet')
    expect(ci).toHaveTextContent(`${window.location.origin}/api/v1/ingestion/junit?project=CHK`)
    expect(ci).toHaveTextContent('Authorization: Bearer $PROVENLY_API_KEY')

    // Its pages open in this project.
    const pages = screen.getByRole('navigation', { name: 'In this project' })
    expect(
      within(pages)
        .getAllByRole('link')
        .map((l) => l.textContent),
    ).toEqual(['Dashboard', 'Test Runs', 'Test Cases', 'Suites', 'Requirements', 'Issues'])
    await u.click(within(pages).getByRole('link', { name: 'Test Runs' }))
    expect(router.state.location.pathname).toBe('/test-runs')
    expect(projectSwitcher()).toHaveAccessibleName('Current project: CHK · Checkout')
  })

  it('FE-INT-060 a viewer reads members and classification only', async () => {
    db.users.push(user({ id: 2, username: 'vera', isAdmin: false }))
    db.members.push({ projectId: 1, userId: 2, role: 'viewer', since: '2026-10-05T10:00:00Z' })
    db.session = 2
    renderRoute('/projects/TC')
    const sections = await screen.findByRole('navigation', { name: 'On this page' })
    await waitFor(() =>
      expect(
        within(sections)
          .getAllByRole('link')
          .map((l) => l.textContent),
      ).toEqual(['Members', 'Classification']),
    )
    expect(document.getElementById('api-keys')).toBeNull()
    expect(screen.queryByTestId('ci-snippet')).not.toBeInTheDocument()
  })
})
