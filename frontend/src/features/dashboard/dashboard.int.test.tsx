import { screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'

import { testRun } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'
import { server } from '@/test/server'

const at = '2026-10-05T10:00:00Z'

describe('FE-INT-042 quality dashboard', () => {
  it('FE-INT-042 asks for a project, then shows the latest run, trend, automation, stale and flaky tests', async () => {
    db.flaky = { 153: 3 }
    const { user: u } = renderRoute('/dashboard')
    expect(await screen.findByText('Pick a project to see its quality.')).toBeInTheDocument()
    // The chooser offers the projects themselves: one click opens the project.
    await u.click(await screen.findByRole('button', { name: /^TC · / }))

    expect(await screen.findByTestId('latest-run')).toHaveTextContent('#7')
    expect(screen.getByTestId('latest-run-rates')).toHaveTextContent('Executed 1 of 2 (50%) · Passed 0%')
    expect(screen.getByTestId('trend-7')).toHaveAttribute('href', '/test-runs/7')
    // The trend is a list of links (a link with role=listitem was neither), each saying its verdict.
    const trend = screen.getByRole('list', { name: 'Pass rate of the latest runs' })
    expect(within(trend).getAllByRole('listitem')).toHaveLength(1)
    expect(within(trend).getByRole('link', { name: /^Run #7: (passed|failed|incomplete|no tests)/ })).toBe(
      screen.getByTestId('trend-7'),
    )
    expect(await screen.findByTestId('automation-rate')).toHaveTextContent('100%')
    expect(screen.getByTestId('stale-count')).toHaveTextContent('1')
    expect(
      within(screen.getByTestId('stale-cases')).getByRole('link', { name: 'TC-154' }),
    ).toBeInTheDocument()
    expect(screen.getByTestId('stale-cases')).toHaveTextContent('never executed')
    expect(screen.getByText('By execution start date.')).toBeInTheDocument()
    expect(screen.getByTestId('flaky-cases')).toHaveTextContent('TC-153 flaky in 3 runs')
    expect(screen.getByText('No requirements yet.')).toBeInTheDocument()
    expect(screen.getByText('No issues yet.')).toBeInTheDocument()

    await u.selectOptions(screen.getByLabelText('Older than'), '30')
    await u.selectOptions(screen.getByLabelText('Over the latest'), '50')
    await waitFor(() =>
      expect(screen.getByTestId('flaky-count').nextSibling).toHaveTextContent('latest 50 runs'),
    )
    expect(screen.getByTestId('stale-count').nextSibling).toHaveTextContent('not in 30 days')

    // Changing a filter keeps the figures and the select on screen (it used to unmount, losing focus).
    const older = screen.getByLabelText('Older than')
    await u.selectOptions(older, '7')
    await waitFor(() =>
      expect(screen.getByTestId('stale-count').nextSibling).toHaveTextContent('not in 7 days'),
    )
    expect(screen.getByLabelText('Older than')).toBe(older)
    expect(older).toHaveFocus()
  })

  it('FE-INT-042 breaks requirement coverage and issue verification down; empty projects say so', async () => {
    localStorage.setItem('provenly.project', 'TC')
    db.latest[153] = 'failed'
    db.requirements.push({
      projectId: 1,
      id: 950,
      provider: 'provenly',
      externalId: 'R-1',
      title: 'Sign in',
      description: '',
      url: '',
      providerStatus: '',
      archivedAt: null,
      lastSyncedAt: null,
      createdAt: at,
      updatedAt: at,
      testCaseIds: [153],
      coverage: { status: 'uncovered', linked: 0, passed: 0, failed: 0, notRun: 0, testCases: [] },
    })
    db.requirements.push({
      ...db.requirements[0],
      id: 951,
      externalId: 'R-2',
      testCaseIds: [],
      archivedAt: at,
    })
    db.issues.push({
      projectId: 1,
      id: 960,
      provider: 'jira',
      externalId: 'PAY-7',
      title: 'Login fails',
      description: '',
      url: '',
      state: 'open',
      providerStatus: '',
      closedAt: null,
      lastSyncedAt: at,
      createdAt: at,
      updatedAt: at,
      testCaseIds: [153],
      verification: { status: 'unlinked', testCases: [] },
    })
    const { unmount } = renderRoute('/dashboard')
    expect(await screen.findByTestId('coverage-breakdown')).toHaveTextContent('Failing: 1')
    expect(screen.getByTestId('coverage-breakdown')).not.toHaveTextContent('Not covered')
    expect(await screen.findByTestId('verification-breakdown')).toHaveTextContent('Known issue: 1')
    unmount()

    db.runs = []
    db.results = []
    db.testCases = []
    db.flaky = {}
    renderRoute('/dashboard')
    expect(await screen.findByText('No runs yet.')).toBeInTheDocument()
    expect(screen.getByTestId('latest-run')).toHaveTextContent('—')
    expect(screen.getByTestId('latest-run-hint')).toHaveTextContent('No runs yet')
    expect(await screen.findByText('Every active test case ran recently.')).toBeInTheDocument()
    expect(screen.getByText('No flaky test cases.')).toBeInTheDocument()
    expect(screen.getByTestId('automation-rate')).toHaveTextContent('0%')
  })

  it('FE-INT-042 a test case last executed before the window is listed as stale with its date', async () => {
    const old = new Date(Date.now() - 30 * 24 * 3600 * 1000).toISOString()
    db.runs = db.runs.map((r) => ({ ...r, createdAt: old }))
    localStorage.setItem('provenly.project', 'TC')
    renderRoute('/dashboard')
    const stale = await screen.findByTestId('stale-cases')
    expect(within(stale).getByRole('link', { name: 'TC-153' })).toBeInTheDocument()
    expect(stale).toHaveTextContent(/TC-153\s*last /)
    expect(screen.getByTestId('stale-count').nextSibling).toHaveTextContent(/, [1-9]\d* not in 14 days/)
  })

  it('FE-INT-042 a running run reads as provisional in the trend', async () => {
    db.runs.push(testRun({ id: 8, executionStatus: 'running' }))
    localStorage.setItem('provenly.project', 'TC')
    renderRoute('/dashboard')
    expect(await screen.findByRole('link', { name: /^Run #8: .* so far \(running\)/ })).toBe(
      screen.getByTestId('trend-8'),
    )
  })

  it('FE-INT-042 the trend has an axis, labels each run and never draws an incomplete run like a failed one', async () => {
    const outcome = (verdict: 'passed' | 'failed' | 'incomplete', executed: number, passRate: number) => ({
      verdict,
      executed,
      passed: Math.round((executed * passRate) / 100),
      failed: executed - Math.round((executed * passRate) / 100),
      error: 0,
      skipped: 0,
      untested: 10 - executed,
      passRate,
      flaky: 0,
    })
    db.runs = [
      testRun({
        id: 21,
        createdAt: '2026-10-06T12:00:00Z',
        expectedCount: 10,
        outcome: outcome('incomplete', 1, 100),
      }),
      testRun({
        id: 22,
        createdAt: '2026-10-07T12:00:00Z',
        expectedCount: 10,
        outcome: outcome('failed', 10, 50),
      }),
      testRun({
        id: 23,
        createdAt: '2026-10-08T12:00:00Z',
        expectedCount: 10,
        outcome: outcome('passed', 10, 100),
      }),
    ]
    localStorage.setItem('provenly.project', 'TC')
    renderRoute('/dashboard')
    const incomplete = await screen.findByTestId('trend-21')
    expect(incomplete).toHaveClass('bg-amber-500')
    expect(incomplete).not.toHaveClass('bg-red-600')
    expect(incomplete.style.height).toBe('100%')
    expect(incomplete).toHaveAccessibleName(
      'Run #21: incomplete, 100% of executed passed, executed 1 of 10 (10%)',
    )
    expect(screen.getByTestId('trend-22')).toHaveClass('bg-red-600')
    expect(screen.getByTestId('trend-23')).toHaveClass('bg-emerald-600')
    // Each bar names its run and date (oldest first); the legend says what each colour means.
    expect(screen.getByTestId('trend-label-21')).toHaveTextContent('#2110-06')
    expect(screen.getByTestId('trend-label-23')).toHaveTextContent('#2310-08')
    const legend = screen.getByRole('list', { name: 'Legend' })
    expect(
      within(legend)
        .getAllByRole('listitem')
        .map((l) => l.textContent),
    ).toEqual(['passed', 'failed', 'incomplete', 'no tests'])
    expect(screen.getByText('50%')).toBeInTheDocument()
  })

  it('FE-INT-042 the latest run card tells loading and a failed read from no runs', async () => {
    localStorage.setItem('provenly.project', 'TC')
    server.use(
      http.get('*/api/v1/test-runs', () =>
        HttpResponse.json(
          {
            type: 'about:blank',
            title: 'Internal Server Error',
            status: 500,
            code: 'internal_error',
            detail: 'x',
          },
          { status: 500, headers: { 'Content-Type': 'application/problem+json' } },
        ),
      ),
    )
    renderRoute('/dashboard')
    expect(await screen.findByTestId('latest-run-hint')).toHaveTextContent('Loading…')
    await waitFor(() =>
      expect(screen.getByTestId('latest-run-hint')).toHaveTextContent('Could not load the runs'),
    )
  })

  it('FE-INT-042 a failing quality read is reported', async () => {
    localStorage.setItem('provenly.project', 'TC')
    server.use(
      http.get('*/api/v1/projects/:projectKey/quality', () =>
        HttpResponse.json(
          {
            type: 'about:blank',
            title: 'Internal Server Error',
            status: 500,
            code: 'internal_error',
            detail: 'x',
          },
          { status: 500, headers: { 'Content-Type': 'application/problem+json' } },
        ),
      ),
    )
    renderRoute('/dashboard')
    expect((await screen.findAllByText('Something went wrong')).length).toBeGreaterThan(0)
  })
})
