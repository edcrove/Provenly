import { screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { testCase } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { pickProject, renderRoute } from '@/test/render'

const asViewer = () => {
  db.users.push({ ...db.users[0], id: 2, username: 'ana', isAdmin: false })
  db.members.push({ projectId: 1, userId: 2, role: 'viewer', since: '2026-10-05T10:00:00Z' })
  db.session = 2
}

describe('FE-INT-037 taxonomy: tags and classification', () => {
  it('FE-INT-037 a maintainer adds dimensions and values and archives them; viewers only read', async () => {
    const { user: u, unmount } = renderRoute('/projects/TC')
    const riskRow = await screen.findByTestId('dimension-risk')
    expect(riskRow).toHaveTextContent('built-in')

    await u.type(screen.getByLabelText('Add dimension: key'), 'browser')
    await u.type(screen.getByLabelText('Add dimension: name'), 'Browser')
    await u.click(screen.getByRole('button', { name: 'Add dimension' }))
    const browser = await screen.findByTestId('dimension-browser')
    expect(within(browser).getByText('No values yet.')).toBeInTheDocument()
    expect(screen.getByLabelText('Add dimension: key')).toHaveValue('')

    await u.type(within(browser).getByLabelText('Add value to Browser: key'), 'chrome')
    await u.type(within(browser).getByLabelText('Add value to Browser: name'), 'Chrome')
    await u.click(within(browser).getByRole('button', { name: 'Add value to Browser' }))
    const chrome = await within(browser).findByRole('button', { name: 'Archive Browser: Chrome' })

    // A duplicate key is reported where it happens.
    await u.type(within(browser).getByLabelText('Add value to Browser: key'), 'chrome')
    await u.type(within(browser).getByLabelText('Add value to Browser: name'), 'Again')
    await u.click(within(browser).getByRole('button', { name: 'Add value to Browser' }))
    expect(await within(browser).findByText('Could not update Browser')).toBeInTheDocument()

    await u.click(chrome)
    await within(browser).findByRole('button', { name: 'Restore Browser: Chrome' })
    expect(db.dimensions.find((d) => d.key === 'browser')!.values[0].archivedAt).not.toBeNull()

    await u.click(within(browser).getByRole('button', { name: 'Archive' }))
    await waitFor(() => expect(within(browser).getByText('archived')).toBeInTheDocument())
    expect(within(browser).queryByLabelText('Add value to Browser: key')).not.toBeInTheDocument()
    await u.click(within(browser).getByRole('button', { name: 'Restore' }))
    await waitFor(() => expect(within(browser).queryByText('archived')).not.toBeInTheDocument())

    await u.type(screen.getByLabelText('Add dimension: key'), 'browser')
    await u.type(screen.getByLabelText('Add dimension: name'), 'Again')
    await u.click(screen.getByRole('button', { name: 'Add dimension' }))
    expect(await screen.findByText('Could not add the dimension')).toBeInTheDocument()
    unmount()

    asViewer()
    renderRoute('/projects/TC')
    const risk = await screen.findByTestId('dimension-risk')
    expect(within(risk).getByText('Critical')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Add dimension' })).not.toBeInTheDocument()
    expect(within(risk).queryByRole('button')).not.toBeInTheDocument()
  })

  it('FE-INT-037 creates a test case with tags and a classification and shows them', async () => {
    db.dimensions[1].values[2].archivedAt = '2026-10-05T10:00:00Z' // risk: low is archived
    const { user: u } = renderRoute('/test-cases/new')
    await u.type(await screen.findByLabelText('Title'), 'Pay by card')
    await u.type(screen.getByLabelText('Tags'), 'Smoke, checkout')
    const riskSelect = await screen.findByLabelText('Risk')
    expect(within(riskSelect).queryByRole('option', { name: 'Low' })).not.toBeInTheDocument()
    await u.selectOptions(riskSelect, 'critical')
    await u.click(screen.getByRole('button', { name: 'Create test case' }))

    const taxonomy = await screen.findByTestId('taxonomy')
    expect(taxonomy).toHaveTextContent('Risk: Critical')
    expect(taxonomy).toHaveTextContent('#checkout')
    expect(taxonomy).toHaveTextContent('#smoke')
    const created = db.testCases.at(-1)!
    expect(created.tags).toEqual(['checkout', 'smoke'])
    expect(created.classification).toEqual({ risk: 'critical' })
  })

  it('FE-INT-037 an edit sends only the tags and dimensions that changed; errors show on the field', async () => {
    db.testCases[0] = testCase({ tags: ['smoke'], classification: { risk: 'low' } })
    db.dimensions[1].values[2].archivedAt = '2026-10-05T10:00:00Z'
    const { user: u } = renderRoute('/test-cases/153')
    await u.click(await screen.findByRole('button', { name: 'Edit' }))
    const risk = await screen.findByLabelText('Risk')
    expect(risk).toHaveValue('low')
    expect(within(risk).getByRole('option', { name: 'Low' })).toBeInTheDocument()

    // Someone else adds a dimension value meanwhile: the save keeps it because only risk is sent.
    db.testCases[0].classification = { ...db.testCases[0].classification, feature: 'pay' }
    db.dimensions[0].values.push({ key: 'pay', name: 'Payments', archivedAt: null })
    await u.selectOptions(risk, 'high')
    await u.click(screen.getByRole('button', { name: 'Save changes' }))
    await waitFor(() =>
      expect(screen.queryByRole('button', { name: 'Save changes' })).not.toBeInTheDocument(),
    )
    expect(db.testCases[0].classification).toEqual({ risk: 'high', feature: 'pay' })
    expect(db.testCases[0].tags).toEqual(['smoke'])

    await u.click(screen.getByRole('button', { name: 'Edit' }))
    await u.clear(await screen.findByLabelText('Tags'))
    await u.type(screen.getByLabelText('Tags'), 'a b!')
    await u.click(screen.getByRole('button', { name: 'Save changes' }))
    expect(await screen.findByText('Tags: "b!" is not a valid tag')).toBeInTheDocument()
    expect(screen.getByLabelText('Tags')).toHaveAttribute('aria-invalid', 'true')

    await u.clear(screen.getByLabelText('Tags'))
    await u.selectOptions(screen.getByLabelText('Risk'), '')
    await u.click(screen.getByRole('button', { name: 'Save changes' }))
    await waitFor(() => expect(db.testCases[0].tags).toEqual([]))
    expect(db.testCases[0].classification).toEqual({ feature: 'pay' })
  })

  it('FE-INT-037 a classification error is shown on its dimension', async () => {
    const { user: u } = renderRoute('/test-cases/153')
    await u.click(await screen.findByRole('button', { name: 'Edit' }))
    const risk = await screen.findByLabelText('Risk')
    await u.selectOptions(risk, 'high')
    db.dimensions[1].values[1].archivedAt = '2026-10-05T10:00:00Z' // archived by someone else meanwhile
    await u.click(screen.getByRole('button', { name: 'Save changes' }))
    expect(await screen.findByText('"high" is archived')).toBeInTheDocument()
    expect(risk).toHaveAttribute('aria-invalid', 'true')
  })

  it('FE-INT-037 the list shows tags and filters by tag and classification', async () => {
    db.testCases[0] = testCase({ tags: ['smoke'], classification: { risk: 'critical' } })
    db.testCases[1] = testCase({
      id: 154,
      title: 'Logout works',
      tags: ['smoke'],
      classification: { risk: 'high' },
    })
    const { user: u, router } = renderRoute('/test-cases')
    expect(await screen.findAllByText('#smoke')).toHaveLength(2)
    // Without a project there is no classification filter: dimensions are per project.
    expect(screen.queryByLabelText('Filter by classification')).not.toBeInTheDocument()

    await u.type(screen.getByLabelText('Tag'), ' Nope {Enter}')
    expect(await screen.findByText('No test cases match the filters.')).toBeInTheDocument()
    expect(router.state.location.search).toBe('?tag=nope')
    await u.clear(screen.getByLabelText('Tag'))
    await u.type(screen.getByLabelText('Tag'), '{Enter}')
    expect(await screen.findAllByText('#smoke')).toHaveLength(2)

    await pickProject(u, 'TC')
    await u.selectOptions(await screen.findByLabelText('Filter by classification'), 'risk:critical')
    await waitFor(() => expect(screen.queryByText('Logout works')).not.toBeInTheDocument())
    expect(screen.getByText('Login works')).toBeInTheDocument()
    expect(router.state.location.search).toContain('classification=risk%3Acritical')
  })
})
