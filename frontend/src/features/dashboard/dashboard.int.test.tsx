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
    expect(await screen.findByText('Choose a project (top right) to see its quality.')).toBeInTheDocument()
    await screen.findByRole('option', { name: /^TC/ })
    await u.selectOptions(screen.getByRole('combobox', { name: 'Current project' }), 'TC')

    expect(await screen.findByTestId('latest-run')).toHaveTextContent('#7')
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

  it('FE-INT-042 a running run reads as provisional in the trend', async () => {
    db.runs.push(testRun({ id: 8, executionStatus: 'running' }))
    localStorage.setItem('provenly.project', 'TC')
    renderRoute('/dashboard')
    expect(await screen.findByRole('link', { name: /^Run #8: .* so far \(running\)/ })).toBe(
      screen.getByTestId('trend-8'),
    )
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
