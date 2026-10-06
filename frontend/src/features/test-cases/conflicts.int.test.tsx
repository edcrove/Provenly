import { screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'

/** Someone else saves the test case while it is open: the next write from this page is stale. */
const savedElsewhere = (title = 'Saved by someone else') => {
  const tc = db.testCases.find((t) => t.id === 153)!
  Object.assign(tc, { title, version: tc.version + 1 })
}

describe('FE-INT-034 concurrent edits (optimistic locking)', () => {
  it('FE-INT-034 a stale edit is refused with a notice; reloading shows the other version and the next save works', async () => {
    const { user } = renderRoute('/test-cases/153')
    await user.click(await screen.findByRole('button', { name: 'Edit' }))
    savedElsewhere()
    const title = screen.getByLabelText('Title')
    await user.clear(title)
    await user.type(title, 'My edit')
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    const conflict = await screen.findByTestId('conflict')
    expect(conflict).toHaveTextContent('Someone else saved this test case')
    expect(db.testCases[0].title).toBe('Saved by someone else')

    await user.click(within(conflict).getByRole('button', { name: 'Reload' }))
    expect(await screen.findByRole('heading', { name: 'TC-153 · Saved by someone else' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    expect(await screen.findByRole('heading', { name: 'TC-153 · My edit' })).toBeInTheDocument()
  })

  it('FE-INT-034 step writes carry the version forward (no false conflicts) and a stale one is refused', async () => {
    const { user } = renderRoute('/test-cases/153')
    await screen.findByRole('list', { name: 'Steps' })
    const add = screen.getByRole('form', { name: 'Add step' })
    for (const action of ['third', 'fourth']) {
      await user.type(within(add).getByLabelText('Step action'), action)
      await user.click(within(add).getByRole('button', { name: 'Add step' }))
      await waitFor(() => expect(within(add).getByLabelText('Step action')).toHaveValue(''))
    }
    expect(db.steps.map((s) => s.action)).toContain('fourth')
    expect(screen.queryByTestId('conflict')).not.toBeInTheDocument()

    savedElsewhere()
    await user.click(screen.getByRole('button', { name: 'Delete step 1' }))
    await user.click(
      within(screen.getByRole('group', { name: 'Confirm deleting step 1' })).getByRole('button', {
        name: 'Delete',
      }),
    )
    expect(await screen.findByTestId('conflict')).toBeInTheDocument()
    expect(db.steps.filter((s) => s.testCaseId === 153)).toHaveLength(4)
  })

  it('FE-INT-034 deprecating a test case someone else changed is refused too', async () => {
    const { user } = renderRoute('/test-cases/153')
    await screen.findByRole('button', { name: 'Deprecate' })
    savedElsewhere()
    await user.click(screen.getByRole('button', { name: 'Deprecate' }))
    await user.click(screen.getByRole('button', { name: 'Confirm deprecation' }))
    expect(await screen.findByTestId('conflict')).toBeInTheDocument()
    expect(db.testCases[0].status).toBe('active')
  })

  it('FE-INT-034 a save sends only the fields changed, so after a reload it keeps what the other person changed', async () => {
    const { user } = renderRoute('/test-cases/153')
    await user.click(await screen.findByRole('button', { name: 'Edit' }))
    savedElsewhere('Their title')
    await user.clear(screen.getByLabelText('Description'))
    await user.type(screen.getByLabelText('Description'), 'My description')
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    await user.click(within(await screen.findByTestId('conflict')).getByRole('button', { name: 'Reload' }))
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    expect(await screen.findByRole('heading', { name: 'TC-153 · Their title' })).toBeInTheDocument()
    expect(db.testCases[0]).toMatchObject({ title: 'Their title', description: 'My description' })

    // Saving without changes just closes the form.
    const version = db.testCases[0].version
    await user.click(screen.getByRole('button', { name: 'Edit' }))
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    expect(await screen.findByRole('button', { name: 'Edit' })).toBeInTheDocument()
    expect(db.testCases[0].version).toBe(version)
  })
})
