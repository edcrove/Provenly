import { screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { testCase } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'

describe('FE-INT-002 test case list', () => {
  it('FE-INT-002 lists test cases with TC-ID, title, status and automated', async () => {
    renderRoute('/test-cases')
    const row = (await screen.findByText('TC-153')).closest('tr')!
    expect(within(row).getByText('Login works')).toBeInTheDocument()
    expect(within(row).getByText('active')).toBeInTheDocument()
    expect(within(row).getByText('Yes')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'TC-153' })).toHaveAttribute('href', '/test-cases/153')
  })

  it('FE-INT-002 filters by status and paginates', async () => {
    db.testCases = Array.from({ length: 25 }, (_, i) =>
      testCase({ id: i + 1, title: `Case ${i + 1}`, status: i < 3 ? 'deprecated' : 'active' }),
    )
    const { user, router } = renderRoute('/test-cases')
    expect(await screen.findByText('Page 1 of 2 · 25 items')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Previous' })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: 'Next' }))
    expect(await screen.findByText('Page 2 of 2 · 25 items')).toBeInTheDocument()
    expect(router.state.location.search).toBe('?page=2')

    await user.selectOptions(screen.getByLabelText('Filter by status'), 'deprecated')
    expect(await screen.findByText('Page 1 of 1 · 3 items')).toBeInTheDocument()
    expect(router.state.location.search).toBe('?status=deprecated')
    await user.selectOptions(screen.getByLabelText('Filter by status'), '')
    expect(await screen.findByText('Page 1 of 2 · 25 items')).toBeInTheDocument()
  })

  it('FE-INT-002 shows empty and error states', async () => {
    db.testCases = []
    renderRoute('/test-cases')
    expect(await screen.findByText('No test cases yet.')).toBeInTheDocument()
    expect(screen.getByText('Page 1 of 1 · 0 items')).toBeInTheDocument()
  })

  it('FE-INT-012 surfaces API failures as an error alert', async () => {
    db.failing = true
    renderRoute('/test-cases')
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Something went wrongan unexpected error occurred',
    )
  })
})

describe('FE-INT-003 create test case', () => {
  it('FE-INT-003 creates a test case; the TC-ID comes from the backend', async () => {
    const { user, router } = renderRoute('/test-cases/new')
    await user.type(await screen.findByLabelText('Title'), 'Checkout')
    await user.type(screen.getByLabelText('Description'), 'Pay with card')
    await user.type(screen.getByLabelText('Expected result'), 'Order confirmed')
    await user.click(screen.getByLabelText(/Automated/))
    await user.click(screen.getByRole('button', { name: 'Create test case' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/test-cases/1001'))
    expect(await screen.findByRole('heading', { name: /TC-1001 · Checkout/ })).toBeInTheDocument()
    expect(db.testCases.at(-1)).toMatchObject({
      title: 'Checkout',
      automated: true,
      expectedResult: 'Order confirmed',
    })
  })

  it('FE-INT-003 shows validation errors from the API and supports cancel', async () => {
    const { user, router } = renderRoute('/test-cases/new')
    await user.type(await screen.findByLabelText('Title'), '   ')
    await user.click(screen.getByRole('button', { name: 'Create test case' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('title: is required')
    const title = screen.getByLabelText('Title')
    expect(title).toHaveAttribute('aria-invalid', 'true')
    expect(title).toHaveAccessibleDescription('Title is required')
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(router.state.location.pathname).toBe('/test-cases')
  })
})

describe('FE-INT-004 test case detail and edit', () => {
  it('FE-INT-004 distinguishes the current definition from observed execution metadata', async () => {
    renderRoute('/test-cases/153')
    expect(await screen.findByRole('heading', { name: 'TC-153 · Login works' })).toBeInTheDocument()
    expect(screen.getByText('Current definition')).toBeInTheDocument()
    expect(screen.getByText(/editing content never creates a version/)).toBeInTheDocument()
    expect(screen.getByText('Execution history')).toBeInTheDocument()
    expect(screen.getByText(/Observed at execution time/)).toBeInTheDocument()
    expect(screen.getByText('User logs in with valid credentials')).toBeInTheDocument()
    expect(screen.getByText('automated')).toBeInTheDocument()
  })

  it('FE-INT-004 edits content without changing the TC-ID', async () => {
    const { user } = renderRoute('/test-cases/153')
    await user.click(await screen.findByRole('button', { name: 'Edit' }))
    const title = screen.getByLabelText('Title')
    await user.clear(title)
    await user.type(title, 'Login v2')
    await user.click(screen.getByLabelText(/Automated/))
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    expect(await screen.findByRole('heading', { name: 'TC-153 · Login v2' })).toBeInTheDocument()
    expect(screen.getByText('manual')).toBeInTheDocument()
    expect(db.testCases.find((t) => t.id === 153)).toMatchObject({ title: 'Login v2', automated: false })
  })

  it('FE-INT-004 shows edit errors and cancels editing', async () => {
    const { user } = renderRoute('/test-cases/153')
    await user.click(await screen.findByRole('button', { name: 'Edit' }))
    await user.clear(screen.getByLabelText('Title'))
    await user.type(screen.getByLabelText('Title'), ' ')
    await user.click(screen.getByRole('button', { name: 'Save changes' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not save the test case')
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.getByRole('button', { name: 'Edit' })).toBeInTheDocument()
  })

  it('FE-INT-004 shows empty description/expected result and deprecation date', async () => {
    db.testCases[0] = testCase({
      description: '',
      expectedResult: '',
      status: 'deprecated',
      deprecatedAt: '2026-09-29T00:00:00Z',
    })
    renderRoute('/test-cases/153')
    expect(await screen.findByText(/Deprecated 2026-09-29 00:00:00 UTC/)).toBeInTheDocument()
    expect(screen.getAllByText('—')).toHaveLength(2)
    expect(screen.queryByRole('button', { name: 'Deprecate' })).not.toBeInTheDocument()
  })

  it('FE-INT-012 shows not found and invalid id errors', async () => {
    renderRoute('/test-cases/999')
    expect(await screen.findByRole('alert')).toHaveTextContent('Not foundtest case TC-999 not found')
  })

  it('FE-INT-012 rejects a non-numeric id', async () => {
    renderRoute('/test-cases/abc')
    expect(await screen.findByRole('alert')).toHaveTextContent('request validation failed')
  })
})

describe('FE-INT-014 manual test case receiving automated results', () => {
  it('FE-INT-014 warns and marks the test case automated', async () => {
    db.testCases[0] = testCase({ automated: false })
    const { user } = renderRoute('/test-cases/153')
    const alert = await screen.findByTestId('manual-with-results')
    expect(alert).toHaveTextContent('Receives automated results but is marked manual')
    await user.click(within(alert).getByRole('button', { name: 'Mark as automated' }))
    await waitFor(() => expect(screen.queryByTestId('manual-with-results')).not.toBeInTheDocument())
    expect(db.testCases[0].automated).toBe(true)
  })

  it('FE-INT-014 does not warn without results or when deprecated', async () => {
    db.testCases[1] = testCase({ id: 154, title: 'Logout works', automated: false })
    const first = renderRoute('/test-cases/154')
    await screen.findByText('No results yet for this TC-ID.')
    expect(screen.queryByTestId('manual-with-results')).not.toBeInTheDocument()
    first.unmount()
    db.testCases[0] = testCase({ automated: false, status: 'deprecated' })
    renderRoute('/test-cases/153')
    await screen.findByRole('table', { name: 'Execution history' })
    expect(screen.queryByTestId('manual-with-results')).not.toBeInTheDocument()
  })
})

describe('FE-INT-005 deprecate', () => {
  it('FE-INT-005 requires confirmation and keeps the TC-ID', async () => {
    const { user } = renderRoute('/test-cases/153')
    await user.click(await screen.findByRole('button', { name: 'Deprecate' }))
    await user.click(screen.getByRole('button', { name: 'Keep active' }))
    expect(db.testCases[0].status).toBe('active')
    await user.click(screen.getByRole('button', { name: 'Deprecate' }))
    await user.click(screen.getByRole('button', { name: 'Confirm deprecation' }))
    expect(await screen.findByText('deprecated')).toBeInTheDocument()
    expect(db.testCases[0]).toMatchObject({ id: 153, status: 'deprecated' })
  })

  it('FE-INT-005 reactivates a deprecated test case with the same TC-ID', async () => {
    db.testCases[0] = testCase({ status: 'deprecated', deprecatedAt: '2026-09-29T00:00:00Z' })
    const { user } = renderRoute('/test-cases/153')
    await user.click(await screen.findByRole('button', { name: 'Reactivate' }))
    expect(await screen.findByRole('button', { name: 'Deprecate' })).toBeInTheDocument()
    expect(db.testCases[0]).toMatchObject({ id: 153, status: 'active', deprecatedAt: null })
  })

  it('FE-INT-005 reports reactivation failures', async () => {
    db.testCases[0] = testCase({ status: 'deprecated' })
    const { user } = renderRoute('/test-cases/153')
    const button = await screen.findByRole('button', { name: 'Reactivate' })
    db.failing = true
    await user.click(button)
    expect(await screen.findByText('Could not reactivate')).toBeInTheDocument()
  })

  it('FE-INT-005 reports deprecation failures', async () => {
    const { user } = renderRoute('/test-cases/153')
    await user.click(await screen.findByRole('button', { name: 'Deprecate' }))
    db.failing = true
    await user.click(screen.getByRole('button', { name: 'Confirm deprecation' }))
    expect(await screen.findByText('Could not deprecate')).toBeInTheDocument()
  })
})

describe('FE-INT-007 execution history', () => {
  it('FE-INT-007 lists results across runs with observed metadata', async () => {
    renderRoute('/test-cases/153')
    const table = await screen.findByRole('table', { name: 'Execution history' })
    expect(within(table).getByText('login firefox')).toBeInTheDocument()
    expect(within(table).getAllByRole('link', { name: 'github:9876:1' })[0]).toHaveAttribute(
      'href',
      '/test-runs/7',
    )
    expect(within(table).getAllByText('0123456789')).toHaveLength(2)
    expect(within(table).getByText('failed')).toBeInTheDocument()
  })

  it('FE-INT-007 marks results received after deprecation', async () => {
    db.results = [{ ...db.results[0], correlation: 'deprecated' }]
    renderRoute('/test-cases/153')
    const table = await screen.findByRole('table', { name: 'Execution history' })
    expect(within(table).getByText('after deprecation')).toBeInTheDocument()
  })

  it('FE-INT-007 shows an empty history and paginates', async () => {
    renderRoute('/test-cases/154')
    expect(await screen.findByText('No results yet for this TC-ID.')).toBeInTheDocument()
  })

  it('FE-INT-007 paginates long histories', async () => {
    db.results = Array.from({ length: 21 }, (_, i) => ({ ...db.results[0], id: i + 1 }))
    db.runs[0] = { ...db.runs[0], branch: '', completedAt: null }
    const { user } = renderRoute('/test-cases/153')
    expect(await screen.findByText('Page 1 of 2 · 21 items')).toBeInTheDocument()
    const history = screen.getByRole('table', { name: 'Execution history' }).parentElement!.parentElement!
    await user.click(within(history).getByRole('button', { name: 'Next' }))
    expect(await screen.findByText('Page 2 of 2 · 21 items')).toBeInTheDocument()
  })
})
