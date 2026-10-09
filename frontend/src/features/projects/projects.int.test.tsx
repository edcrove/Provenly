import { screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'

import { project, testCase, testResult, testRun } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { pickProject, projectSwitcher, renderRoute } from '@/test/render'
import { server } from '@/test/server'

const checkout = () => project({ id: 2, key: 'CHK', name: 'Checkout', description: '' })

describe('FE-INT-022 projects page', () => {
  it('FE-INT-022 lists projects and creates one, which becomes the current project', async () => {
    const { user } = renderRoute('/projects')
    const row = await screen.findByTestId('project-TC')
    expect(row).toHaveTextContent('Default')
    expect(row).toHaveTextContent('Test cases created before projects existed.')

    await user.type(screen.getByLabelText('Key'), 'chk')
    await user.type(screen.getByLabelText('Name'), 'Checkout')
    await user.click(screen.getByRole('button', { name: 'Create project' }))
    const created = await screen.findByTestId('project-CHK')
    expect(created).toHaveTextContent('Checkout')
    expect(created).toHaveTextContent('—')
    expect(screen.getByLabelText('Key')).toHaveValue('')
    await waitFor(() => expect(projectSwitcher()).toHaveAccessibleName(/^Current project: CHK · /))
    expect(localStorage.getItem('provenly.project')).toBe('CHK')
  })

  it('FE-INT-022 reports a key already in use and invalid input', async () => {
    const { user } = renderRoute('/projects')
    await screen.findByTestId('project-TC')
    await user.type(screen.getByLabelText('Key'), 'TC')
    await user.type(screen.getByLabelText('Name'), 'Again')
    await user.click(screen.getByRole('button', { name: 'Create project' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('project TC already exists')
    expect(db.projects).toHaveLength(1)
  })

  it('FE-INT-022 renames a project, cancels and reports failures', async () => {
    db.projects.push(checkout())
    const { user } = renderRoute('/projects')
    const row = await screen.findByTestId('project-CHK')
    await user.click(within(row).getByRole('button', { name: 'Rename' }))
    await user.click(within(row).getByRole('button', { name: 'Cancel' }))
    expect(within(row).queryByLabelText('Name of CHK')).not.toBeInTheDocument()

    await user.click(within(row).getByRole('button', { name: 'Rename' }))
    const input = within(row).getByLabelText('Name of CHK')
    await user.clear(input)
    await user.type(input, 'Checkout v2')
    await user.click(within(row).getByRole('button', { name: 'Save' }))
    expect(await within(row).findByText('Checkout v2')).toBeInTheDocument()
    expect(db.projects[1].name).toBe('Checkout v2')

    await user.click(within(row).getByRole('button', { name: 'Rename' }))
    await user.clear(within(row).getByLabelText('Name of CHK'))
    await user.type(within(row).getByLabelText('Name of CHK'), '   ')
    await user.click(within(row).getByRole('button', { name: 'Save' }))
    expect(await within(row).findByText('Could not rename the project')).toBeInTheDocument()
  })

  it('FE-INT-022 paginates projects', async () => {
    for (let i = 0; i < 25; i++)
      db.projects.push(project({ id: 10 + i, key: `P${String(i).padStart(2, '0')}` }))
    const { user, router } = renderRoute('/projects')
    await screen.findByTestId('project-P00')
    await user.click(screen.getByRole('button', { name: /next/i }))
    expect(await screen.findByTestId('project-TC')).toBeInTheDocument()
    expect(router.state.location.search).toBe('?page=2')
  })
})

describe('FE-INT-023 current project', () => {
  it('FE-INT-023 narrows test cases and runs to the chosen project and remembers it', async () => {
    db.projects.push(checkout())
    db.testCases.push(testCase({ id: 200, projectId: 2, projectKey: 'CHK', number: 1, title: 'Pay by card' }))
    db.runs.push(testRun({ id: 9, projectId: 2, externalRunId: 'github:1:1' }))
    const { user } = renderRoute('/test-cases')
    expect(await screen.findByText('Pay by card')).toBeInTheDocument()
    expect(screen.getByText('Login works')).toBeInTheDocument()

    await pickProject(user, 'CHK')
    await waitFor(() => expect(screen.queryByText('Login works')).not.toBeInTheDocument())
    expect(screen.getByRole('link', { name: 'CHK-1' })).toHaveAttribute('href', '/test-cases/200')
    expect(localStorage.getItem('provenly.project')).toBe('CHK')

    await user.selectOptions(screen.getByRole('combobox', { name: 'Filter by status' }), 'deprecated')
    expect(await screen.findByText('No deprecated test cases in CHK.')).toBeInTheDocument()

    await user.click(screen.getByRole('link', { name: 'Test Runs' }))
    const projects = await screen.findAllByTestId('run-project')
    expect(projects.map((c) => c.textContent)).toEqual(['CHK'])

    await pickProject(user, '')
    await waitFor(() => expect(screen.getAllByTestId('run-project')).toHaveLength(2))
    expect(screen.getAllByTestId('run-project').map((c) => c.textContent)).toEqual(['CHK', 'TC'])
    expect(localStorage.getItem('provenly.project')).toBeNull()
  })

  it('FE-INT-023 a remembered project that does not exist here means every project', async () => {
    localStorage.setItem('provenly.project', 'GONE')
    renderRoute('/test-cases')
    expect(await screen.findByText('Login works')).toBeInTheDocument()
    expect(projectSwitcher()).toHaveAccessibleName('Current project: All projects')
  })

  it('FE-INT-023 an empty list says which project is empty', async () => {
    db.projects.push(checkout())
    localStorage.setItem('provenly.project', 'CHK')
    renderRoute('/test-cases')
    expect(await screen.findByText('No test cases yet in CHK.')).toBeInTheDocument()
  })

  it('FE-INT-023 a run of a project not listed shows no key', async () => {
    db.runs = [testRun({ projectId: 99 })]
    renderRoute('/test-runs')
    expect(await screen.findByTestId('run-project')).toHaveTextContent('—')
  })
})

describe('FE-INT-024 test cases in a project', () => {
  it('FE-INT-024 creates a test case in the chosen project, numbered with its key', async () => {
    db.projects.push(checkout())
    const { user } = renderRoute('/test-cases/new')
    const select = await screen.findByLabelText('Project')
    await waitFor(() => expect(within(select).getAllByRole('option')).toHaveLength(2))
    // No current project: the default TC (what the select shows is what is sent).
    await waitFor(() => expect(select).toHaveValue('TC'))
    await user.selectOptions(select, 'CHK')
    expect(screen.getByText(/e\.g\. CHK-12/)).toBeInTheDocument()
    await user.type(screen.getByLabelText('Title'), 'Refund')
    await user.click(screen.getByRole('button', { name: 'Create test case' }))
    expect(await screen.findByRole('heading', { name: /CHK-1 · Refund/ })).toBeInTheDocument()
    expect(db.testCases.at(-1)).toMatchObject({ projectKey: 'CHK', number: 1 })
  })

  it('FE-INT-024 defaults to the current project and reports an unknown one', async () => {
    db.projects.push(checkout())
    localStorage.setItem('provenly.project', 'CHK')
    server.use(
      http.post('*/api/v1/test-cases', () =>
        HttpResponse.json(
          {
            type: 'about:blank',
            title: 'Not Found',
            status: 404,
            code: 'not_found',
            detail: 'project CHK not found',
          },
          { status: 404, headers: { 'Content-Type': 'application/problem+json' } },
        ),
      ),
    )
    const { user } = renderRoute('/test-cases/new')
    await waitFor(() => expect(screen.getByLabelText('Project')).toHaveValue('CHK'))
    await user.type(screen.getByLabelText('Title'), 'x')
    await user.click(screen.getByRole('button', { name: 'Create test case' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('project CHK not found')
  })
})

describe('FE-INT-025 keys of other projects in runs', () => {
  it('FE-INT-025 shows the key the API resolves, and the id when a result has none', async () => {
    db.results = [
      testResult({ id: 1, testCaseId: 154, testCaseKey: 'CHK-4' }),
      testResult({ id: 2, testCaseId: 153, testCaseKey: null, testName: 'keyless' }),
      testResult({
        id: 3,
        testCaseId: null,
        correlation: 'wrong_project',
        requestedTestCaseId: 'WEB-5',
        testName: 'cross',
      }),
    ]
    renderRoute('/test-runs/7')
    expect(await screen.findByRole('link', { name: 'CHK-4' })).toHaveAttribute('href', '/test-cases/154')
    expect(screen.getByRole('link', { name: '#153' })).toHaveAttribute('href', '/test-cases/153')
    const rows = screen.getAllByTestId('result-row')
    expect(within(rows[2]).getByText('WEB-5')).toBeInTheDocument()
    expect(within(rows[2]).getByText('wrong_project')).toBeInTheDocument()
  })
})
