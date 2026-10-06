import { screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'

import { testCase } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'
import { server } from '@/test/server'

const at = '2026-10-05T10:00:00Z'

describe('FE-INT-039 manual execution', () => {
  it('FE-INT-039 starts a manual run, records results (with a re-test) and completes it', async () => {
    db.testCases.push(testCase({ id: 160, title: 'Checkout by hand', automated: false }))
    const { user: u, router } = renderRoute('/test-runs')
    await u.click(await screen.findByRole('link', { name: /Start manual run/ }))
    await u.type(await screen.findByLabelText('What is being tested'), 'Release 2.4 sign-off')
    await u.type(screen.getByLabelText('Branch or build (optional)'), 'release/2.4')
    await u.click(screen.getByRole('button', { name: 'Start manual run' }))

    const panel = await screen.findByTestId('manual-execution')
    expect(router.state.location.pathname).toMatch(/^\/test-runs\/\d+$/)
    expect(panel).toHaveTextContent('Started by admin')
    expect(panel).toHaveTextContent('1 still untested')
    expect(screen.getByTestId('execution-badge')).toHaveTextContent('running')
    const row = within(panel).getByTestId('manual-TC-160')

    await u.type(within(row).getByLabelText('Note for TC-160'), 'Pay button missing')
    await u.type(within(row).getByLabelText('Failed step of TC-160'), '2')
    await u.click(within(row).getByRole('button', { name: 'Fail' }))
    await waitFor(() => expect(within(row).getByTestId('status-badge')).toHaveTextContent('failed'))
    const failed = db.results.at(-1)!
    expect(failed).toMatchObject({
      status: 'failed',
      errorMessage: 'Pay button missing',
      failedStep: 2,
      recordedBy: 'admin',
    })
    expect(within(row).getByLabelText('Note for TC-160')).toHaveValue('')

    // A re-test after the fix: the last result counts; the failed step is not sent for a pass.
    await u.type(within(row).getByLabelText('Failed step of TC-160'), '3')
    await u.click(within(row).getByRole('button', { name: 'Pass' }))
    await waitFor(() => expect(within(row).getByTestId('status-badge')).toHaveTextContent('passed'))
    expect(db.results.at(-1)).toMatchObject({ status: 'passed', failedStep: null, attempt: 2 })
    expect(await within(panel).findByText(/Every test case has a result/)).toBeInTheDocument()

    await u.click(within(panel).getByRole('button', { name: 'Complete run' }))
    await waitFor(() => expect(screen.queryByTestId('manual-execution')).not.toBeInTheDocument())
    expect(db.runs.at(-1)!.executionStatus).toBe('completed')
    // The verdict follows the recorded results (the last re-test passed), as the server derives it.
    expect(screen.getByTestId('verdict-badge')).toHaveTextContent('passed')
    expect(db.runs.at(-1)!.outcome).toMatchObject({
      verdict: 'passed',
      executed: 1,
      passed: 1,
      passRate: 100,
    })
  })

  it('FE-INT-039 records blocked and skipped, cancels, and reports refusals', async () => {
    db.runs.push({ ...db.runs[0], id: 900, mode: 'manual', executionStatus: 'running', startedBy: 'admin' })
    db.summaries[900] = { ...db.summaries[7], testRunId: 900 }
    const { user: u } = renderRoute('/test-runs/900')
    const panel = await screen.findByTestId('manual-execution')
    const row = within(panel).getByTestId('manual-TC-154')
    await u.click(within(row).getByRole('button', { name: 'Blocked' }))
    await waitFor(() => expect(within(row).getByTestId('status-badge')).toHaveTextContent('error'))
    await u.click(within(row).getByRole('button', { name: 'Skip' }))
    await waitFor(() => expect(within(row).getByTestId('status-badge')).toHaveTextContent('skipped'))

    // Someone finished the run meanwhile: the next record and the finish are refused and reported.
    server.use(
      http.post('*/api/v1/test-runs/:testRunId/*', () =>
        HttpResponse.json(
          {
            type: 'about:blank',
            title: 'Conflict',
            status: 409,
            code: 'conflict',
            detail: 'run 900 is completed',
          },
          { status: 409, headers: { 'Content-Type': 'application/problem+json' } },
        ),
      ),
    )
    await u.click(within(row).getByRole('button', { name: 'Pass' }))
    expect(await within(row).findByText('Could not record TC-154')).toBeInTheDocument()
    await u.click(within(panel).getByRole('button', { name: 'Cancel run' }))
    expect(await within(panel).findByText('Could not finish the run')).toBeInTheDocument()
    server.resetHandlers()
    await u.click(within(panel).getByRole('button', { name: 'Cancel run' }))
    await waitFor(() => expect(screen.queryByTestId('manual-execution')).not.toBeInTheDocument())
    expect(screen.getByTestId('execution-badge')).toHaveTextContent('cancelled')
  })

  it('FE-INT-039 a suite and every test case can be chosen; failures to start are shown; viewers only read', async () => {
    db.suites.push({
      projectId: 1,
      id: 901,
      members: [153],
      key: 'release',
      name: 'Release',
      description: '',
      kind: 'static',
      query: null,
      archivedAt: null,
      createdAt: at,
      updatedAt: at,
      caseCount: 0,
    })
    const { user: u, unmount } = renderRoute('/test-runs/manual')
    await u.type(await screen.findByLabelText('What is being tested'), 'Release check')
    await screen.findByRole('option', { name: 'Release' })
    await u.selectOptions(screen.getByLabelText('Suite'), 'release')
    await u.selectOptions(screen.getByLabelText('Expected test cases'), 'all')
    await u.selectOptions(screen.getByLabelText('Project'), 'TC')
    expect(screen.getByLabelText('Suite')).toHaveValue('')
    await u.selectOptions(screen.getByLabelText('Suite'), 'release')
    await u.click(screen.getByRole('button', { name: 'Start manual run' }))
    const panel = await screen.findByTestId('manual-execution')
    expect(within(panel).getByTestId('manual-TC-153')).toBeInTheDocument()
    expect(within(panel).queryByTestId('manual-TC-154')).not.toBeInTheDocument()
    expect(screen.getByText('suite: Release')).toBeInTheDocument()
    unmount()

    db.suites[0].archivedAt = at
    const second = renderRoute('/test-runs/manual')
    await u.type(await screen.findByLabelText('What is being tested'), 'x')
    server.use(
      http.post('*/api/v1/test-runs/manual', () =>
        HttpResponse.json(
          {
            type: 'about:blank',
            title: 'Conflict',
            status: 409,
            code: 'conflict',
            detail: 'suite release is archived',
          },
          { status: 409, headers: { 'Content-Type': 'application/problem+json' } },
        ),
      ),
    )
    await u.click(screen.getByRole('button', { name: 'Start manual run' }))
    expect(await screen.findByText('Could not start the run')).toBeInTheDocument()
    await u.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(await screen.findByRole('heading', { name: 'Test Runs' })).toBeInTheDocument()
    second.unmount()

    db.users.push({ ...db.users[0], id: 2, username: 'ana', isAdmin: false })
    db.members.push({ projectId: 1, userId: 2, role: 'viewer', since: at })
    db.session = 2
    db.runs.push({ ...db.runs[0], id: 902, mode: 'manual', executionStatus: 'running', startedBy: 'admin' })
    db.summaries[902] = { ...db.summaries[7], testRunId: 902, testCases: [] }
    renderRoute('/test-runs/902')
    expect(await screen.findByText('manual')).toBeInTheDocument()
    expect(screen.queryByTestId('manual-execution')).not.toBeInTheDocument()
  })

  it('FE-INT-039 a manual run without expected test cases says so', async () => {
    db.runs.push({ ...db.runs[0], id: 903, mode: 'manual', executionStatus: 'running', startedBy: null })
    db.summaries[903] = { ...db.summaries[7], testRunId: 903, testCases: [] }
    renderRoute('/test-runs/903')
    const panel = await screen.findByTestId('manual-execution')
    expect(panel).toHaveTextContent('This run expects no test case.')
    expect(panel).toHaveTextContent('Started by —')
  })
})
