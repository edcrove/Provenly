import { screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { testRun } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'

const ids = () =>
  screen
    .getAllByRole('row')
    .slice(1)
    .map((r) => within(r).queryByRole('link')?.textContent ?? r.textContent)

describe('FE-INT-061 filtering the runs list', () => {
  it('FE-INT-061 narrows runs by branch, execution, mode and days, keeps the filters in the URL and clears them', async () => {
    db.runs = [
      testRun({ id: 1, branch: 'main', createdAt: '2026-10-01T15:00:00Z' }),
      testRun({ id: 2, branch: 'main', executionStatus: 'interrupted', createdAt: '2026-10-05T15:00:00Z' }),
      testRun({ id: 3, branch: 'feature/x', mode: 'manual', createdAt: '2026-10-07T15:00:00Z' }),
    ]
    const { user: u, router } = renderRoute('/test-runs?page=1')
    await screen.findByRole('link', { name: '#3' })
    expect(ids()).toEqual(['#3', '#2', '#1'])

    await u.type(screen.getByRole('textbox', { name: 'Branch' }), ' main {Enter}')
    await waitFor(() => expect(ids()).toEqual(['#2', '#1']))
    expect(router.state.location.search).toBe('?branch=main')

    await u.selectOptions(screen.getByRole('combobox', { name: 'Execution' }), 'interrupted')
    await waitFor(() => expect(ids()).toEqual(['#2']))
    await u.selectOptions(screen.getByRole('combobox', { name: 'Execution' }), '')
    await u.selectOptions(screen.getByRole('combobox', { name: 'Mode' }), 'manual')
    expect(await screen.findByText('No runs match these filters.')).toBeInTheDocument()

    await u.click(screen.getByRole('button', { name: 'Clear filters' }))
    await waitFor(() => expect(ids()).toEqual(['#3', '#2', '#1']))
    expect(router.state.location.search).toBe('')
    expect(screen.getByRole('textbox', { name: 'Branch' })).toHaveValue('')
    expect(screen.queryByRole('button', { name: 'Clear filters' })).not.toBeInTheDocument()

    // Days are whole local days (the tests run in UTC).
    await router.navigate('/test-runs?from=2026-10-05&to=2026-10-05')
    await waitFor(() => expect(ids()).toEqual(['#2']))
    expect(screen.getByLabelText('From')).toHaveValue('2026-10-05')
    expect(screen.getByLabelText('To')).toHaveAttribute('min', '2026-10-05')
    expect(screen.getByLabelText('From')).toHaveAttribute('max', '2026-10-05')
    await u.clear(screen.getByLabelText('To'))
    await waitFor(() => expect(ids()).toEqual(['#3', '#2']))
    await u.clear(screen.getByLabelText('From'))
    await u.type(screen.getByLabelText('To'), '2026-10-01')
    await waitFor(() => expect(ids()).toEqual(['#1']))

    // A day that is not a date, or an unknown value, is ignored rather than sent.
    await router.navigate('/test-runs?from=yesterday&executionStatus=failed&mode=ci')
    await waitFor(() => expect(ids()).toEqual(['#3', '#2', '#1']))
  })
})
