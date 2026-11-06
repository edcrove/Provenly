import { screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'

import { project, testRun } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'
import { server } from '@/test/server'

const ended = { completedAt: '2026-10-05T16:00:00Z' }

describe('FE-INT-077 a run Provenly ended says why (CHG-003, Ed 2026-11-06)', () => {
  it.each([
    [
      'finalized',
      'live' as const,
      'interrupted' as const,
      'CI ended this run before its report arrived',
      'CI finalized it: its final report or some of its shards never arrived.',
    ],
    [
      'timeout',
      'sharded' as const,
      'interrupted' as const,
      'Provenly ended this run: nothing arrived for hours',
      'No event or shard arrived for the abandoned-run timeout',
    ],
    [
      'archived',
      'manual' as const,
      'cancelled' as const,
      'This run ended when its project was archived',
      'It was still being executed by hand: it was cancelled.',
    ],
    [
      'archived',
      'live' as const,
      'interrupted' as const,
      'This run ended when its project was archived',
      'It was still running in CI: it was interrupted.',
    ],
  ])('FE-INT-077 closed as %s (%s run)', async (reason, mode, status, title, detail) => {
    db.runs = [
      testRun({
        mode,
        executionStatus: status,
        closedReason: reason as 'finalized' | 'timeout' | 'archived',
        ...ended,
      }),
    ]
    renderRoute('/test-runs/7')
    const alert = await screen.findByTestId('interrupted-run')
    expect(alert).toHaveTextContent(title)
    expect(alert).toHaveTextContent(detail)
    expect(alert).toHaveTextContent('Untested test cases may simply not have run.')
    expect(alert).not.toHaveTextContent('The CI execution')
  })

  it('FE-INT-077 a run CI reported as interrupted keeps the CI wording', async () => {
    db.runs = [testRun({ executionStatus: 'interrupted', closedReason: null, ...ended })]
    renderRoute('/test-runs/7')
    const alert = await screen.findByTestId('interrupted-run')
    expect(alert).toHaveTextContent('The CI execution was interrupted')
    expect(alert).toHaveTextContent('The report may be incomplete')
  })
})

describe('FE-INT-078 archiving a project ends its running runs (DA-1, CHG-003)', () => {
  const addShop = () => {
    db.projects.push(project({ id: 2, key: 'SHOP', name: 'Shop' }))
    db.runs = [
      testRun({
        id: 31,
        projectId: 2,
        projectKey: 'SHOP',
        mode: 'manual',
        executionStatus: 'running',
        completedAt: null,
      }),
      testRun({
        id: 32,
        projectId: 2,
        projectKey: 'SHOP',
        mode: 'live',
        executionStatus: 'running',
        completedAt: null,
      }),
      testRun({ id: 33, projectId: 2, projectKey: 'SHOP' }),
      testRun({
        id: 34,
        projectId: 1,
        projectKey: 'TC',
        mode: 'live',
        executionStatus: 'running',
        completedAt: null,
      }),
    ]
  }

  it('FE-INT-078 the confirmation names the running runs and archiving closes them', async () => {
    addShop()
    const { user: u } = renderRoute('/projects')
    const row = await screen.findByTestId('project-SHOP')
    await u.click(within(row).getByRole('button', { name: 'Archive…' }))
    const confirm = within(row).getByRole('group', { name: 'Archive SHOP' })
    const note = await within(confirm).findByText(/It also ends its 2 running runs/)
    expect(note).toHaveTextContent('#32 (CI: interrupted), #31 (manual: cancelled).')
    expect(within(note).getByRole('link', { name: '#31' })).toHaveAttribute('href', '/test-runs/31')

    await u.click(within(confirm).getByRole('button', { name: 'Archive' }))
    await waitFor(() => expect(screen.queryByTestId('project-SHOP')).not.toBeInTheDocument())
    const byId = (id: number) => db.runs.find((r) => r.id === id)
    expect(byId(31)).toMatchObject({ executionStatus: 'cancelled', closedReason: 'archived' })
    expect(byId(32)).toMatchObject({ executionStatus: 'interrupted', closedReason: 'archived' })
    expect(byId(33)).toMatchObject({ executionStatus: 'completed', closedReason: null })
    expect(byId(34)).toMatchObject({ executionStatus: 'running', closedReason: null })
  })

  it('FE-INT-078 with one running run, or none, the confirmation says so; past five it counts the rest', async () => {
    addShop()
    db.runs = db.runs.filter((r) => r.id !== 32)
    const { user: u } = renderRoute('/projects')
    const row = await screen.findByTestId('project-SHOP')
    await u.click(within(row).getByRole('button', { name: 'Archive…' }))
    expect(await within(row).findByText(/It also ends its running run:/)).toHaveTextContent(
      '#31 (manual: cancelled).',
    )
    await u.click(within(row).getByRole('button', { name: 'Cancel' }))

    db.runs = db.runs.filter((r) => r.id !== 31)
    await u.click(within(row).getByRole('button', { name: 'Archive…' }))
    const confirm = within(row).getByRole('group', { name: 'Archive SHOP' })
    await waitFor(() => expect(within(confirm).queryByTestId('archive-running-runs')).toBeNull())
    await u.click(within(row).getByRole('button', { name: 'Cancel' }))

    db.runs = Array.from({ length: 7 }, (_, i) =>
      testRun({
        id: 40 + i,
        projectId: 2,
        projectKey: 'SHOP',
        mode: 'live',
        executionStatus: 'running',
        completedAt: null,
      }),
    )
    await u.click(within(row).getByRole('button', { name: 'Archive…' }))
    expect(await within(row).findByText(/It also ends its 7 running runs/)).toHaveTextContent(
      '#46 (CI: interrupted), #45 (CI: interrupted), #44 (CI: interrupted), #43 (CI: interrupted), #42 (CI: interrupted) and 2 more.',
    )
  })

  it('FE-INT-078 when the runs cannot be read the confirmation still warns', async () => {
    addShop()
    server.use(http.get('*/api/v1/test-runs', () => HttpResponse.json({ title: 'boom' }, { status: 500 })))
    const { user: u } = renderRoute('/projects')
    const row = await screen.findByTestId('project-SHOP')
    await u.click(within(row).getByRole('button', { name: 'Archive…' }))
    expect(await within(row).findByTestId('archive-running-runs')).toHaveTextContent(
      'Its running runs, if any, end too.',
    )
  })
})
