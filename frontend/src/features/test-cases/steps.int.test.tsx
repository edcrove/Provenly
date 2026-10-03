import { screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'

import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'
import { server } from '@/test/server'

const stepTexts = () =>
  within(screen.getByRole('list', { name: 'Steps' }))
    .getAllByRole('listitem')
    .map((li) => li.querySelector('span')?.textContent)

describe('FE-INT-006 steps management', () => {
  it('FE-INT-006 lists steps in order and adds a step', async () => {
    const { user } = renderRoute('/test-cases/153')
    await screen.findByRole('list', { name: 'Steps' })
    expect(stepTexts()).toEqual(['1. Open the login page', '2. Submit credentials'])
    expect(screen.getByText('Expected: Form is visible')).toBeInTheDocument()

    const add = screen.getByRole('form', { name: 'Add step' })
    await user.type(within(add).getByLabelText('Step action'), 'See dashboard')
    await user.type(within(add).getByLabelText('Step expected result'), 'Welcome shown')
    await user.click(within(add).getByRole('button', { name: 'Add step' }))
    await waitFor(() =>
      expect(stepTexts()).toEqual(['1. Open the login page', '2. Submit credentials', '3. See dashboard']),
    )
    expect(within(add).getByLabelText('Step action')).toHaveValue('')
  })

  it('FE-INT-006 edits, cancels and deletes steps', async () => {
    const { user } = renderRoute('/test-cases/153')
    await screen.findByRole('list', { name: 'Steps' })
    await user.click(screen.getByRole('button', { name: 'Edit step 2' }))
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    await user.click(screen.getByRole('button', { name: 'Edit step 2' }))
    const form = screen.getByRole('form', { name: 'Save step' })
    const action = within(form).getByLabelText('Step action')
    await user.clear(action)
    await user.type(action, 'Submit valid credentials')
    await user.click(within(form).getByRole('button', { name: 'Save step' }))
    await waitFor(() => expect(stepTexts()).toContain('2. Submit valid credentials'))

    await user.click(screen.getByRole('button', { name: 'Delete step 1' }))
    await waitFor(() => expect(stepTexts()).toEqual(['1. Submit valid credentials']))
  })

  it('FE-INT-006 reorders steps up and down', async () => {
    const { user } = renderRoute('/test-cases/153')
    await screen.findByRole('list', { name: 'Steps' })
    expect(screen.getByRole('button', { name: 'Move step 1 up' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Move step 2 down' })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: 'Move step 1 down' }))
    await waitFor(() => expect(stepTexts()).toEqual(['1. Submit credentials', '2. Open the login page']))
    await user.click(screen.getByRole('button', { name: 'Move step 2 up' }))
    await waitFor(() => expect(stepTexts()).toEqual(['1. Open the login page', '2. Submit credentials']))
  })

  it('FE-INT-006 shows the empty state and step errors', async () => {
    db.steps = []
    const { user } = renderRoute('/test-cases/153')
    expect(await screen.findByText('No steps. A test case is valid without steps.')).toBeInTheDocument()
    server.use(
      http.post('*/api/v1/test-cases/:id/steps', () =>
        HttpResponse.json(
          {
            type: 'about:blank',
            title: 'Bad Request',
            status: 400,
            code: 'validation_error',
            detail: 'too many steps',
          },
          { status: 400, headers: { 'Content-Type': 'application/problem+json' } },
        ),
      ),
    )
    const add = screen.getByRole('form', { name: 'Add step' })
    await user.type(within(add).getByLabelText('Step action'), 'x')
    await user.click(within(add).getByRole('button', { name: 'Add step' }))
    expect(await screen.findByText('Could not update the steps')).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent('too many steps')
  })
})

const problem = (detail: string) =>
  HttpResponse.json(
    { type: 'about:blank', title: 'Bad Request', status: 400, code: 'validation_error', detail },
    { status: 400, headers: { 'Content-Type': 'application/problem+json' } },
  )

describe('FE-INT-019 step editor feedback', () => {
  it('FE-INT-019 a rejected step keeps what was typed; only the latest error shows; failures refetch the list', async () => {
    const { user } = renderRoute('/test-cases/153')
    await screen.findByRole('list', { name: 'Steps' })
    server.use(http.post('*/api/v1/test-cases/:id/steps', () => problem('too many steps'), { once: true }))
    const add = screen.getByRole('form', { name: 'Add step' })
    await user.type(within(add).getByLabelText('Step action'), 'keep me')
    await user.type(within(add).getByLabelText('Step expected result'), 'and me')
    await user.click(within(add).getByRole('button', { name: 'Add step' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('too many steps')
    expect(within(add).getByLabelText('Step action')).toHaveValue('keep me')
    expect(within(add).getByLabelText('Step expected result')).toHaveValue('and me')

    // The list on screen goes stale (step 1 deleted elsewhere): the reorder fails, its error replaces the
    // older one, and the list is refetched.
    db.steps = db.steps.filter((st) => st.id !== 1)
    await user.click(screen.getByRole('button', { name: 'Move step 1 down' }))
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('must list every step'))
    expect(screen.getByRole('alert')).not.toHaveTextContent('too many steps')
    await waitFor(() => expect(stepTexts()).toEqual(['2. Submit credentials']))

    // A later success clears the error and the form.
    await user.click(within(add).getByRole('button', { name: 'Add step' }))
    await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument())
    expect(within(add).getByLabelText('Step action')).toHaveValue('')
  })
})
