import { screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { testCase } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'

const at = '2026-10-05T10:00:00Z'
const requirement = (id: number, extra: Partial<(typeof db.requirements)[number]> = {}) => ({
  projectId: 1,
  id,
  provider: 'jira' as const,
  externalId: `REQ-${id}`,
  title: `Requirement ${id}`,
  description: '',
  url: '',
  providerStatus: '',
  archivedAt: null,
  lastSyncedAt: null,
  createdAt: at,
  updatedAt: at,
  testCaseIds: [] as number[],
  coverage: { status: 'uncovered' as const, linked: 0, passed: 0, failed: 0, notRun: 0, testCases: [] },
  ...extra,
})
const issue = (id: number) => ({
  projectId: 1,
  id,
  provider: 'jira' as const,
  externalId: `BUG-${id}`,
  title: `Issue ${id}`,
  description: '',
  url: '',
  state: 'open' as const,
  providerStatus: '',
  closedAt: null,
  lastSyncedAt: null,
  createdAt: at,
  updatedAt: at,
  testCaseIds: [] as number[],
  verification: { status: 'unlinked' as const, testCases: [] },
})
/** Test cases TC-1000.. TC-(1000+n-1), titled "Checkout step N". */
const manyCases = (n: number) => {
  for (let i = 0; i < n; i++)
    db.testCases.push(testCase({ id: 1000 + i, number: 1000 + i, title: `Checkout step ${i}`, tags: [] }))
}

describe('FE-INT-056 long lists (DEC-78)', () => {
  it('FE-INT-056 the dashboard counts coverage and verification over every page, not the first one', async () => {
    for (let i = 0; i < 45; i++) db.requirements.push(requirement(2000 + i))
    db.requirements.push(requirement(2100, { archivedAt: at }))
    for (let i = 0; i < 30; i++) db.issues.push(issue(3000 + i))
    localStorage.setItem('provenly.project', 'TC')
    renderRoute('/dashboard')
    // 45 active requirements (the archived one is not counted) and 30 issues, more than a page of each.
    expect(await screen.findByTestId('coverage-breakdown')).toHaveTextContent('Not covered: 45')
    expect(await screen.findByTestId('verification-breakdown')).toHaveTextContent('No linked test: 30')
  })

  it('FE-INT-056 requirements, issues, suites and webhooks are listed by page', async () => {
    for (let i = 0; i < 25; i++) db.requirements.push(requirement(2000 + i))
    for (let i = 0; i < 25; i++) db.issues.push(issue(3000 + i))
    for (let i = 0; i < 25; i++)
      db.suites.push({
        projectId: 1,
        id: 4000 + i,
        members: [],
        key: `s${String(i).padStart(2, '0')}`,
        name: `Suite ${i}`,
        description: '',
        kind: 'static',
        query: null,
        archivedAt: null,
        createdAt: at,
        updatedAt: at,
        caseCount: 0,
      })
    for (let i = 0; i < 25; i++)
      db.webhooks.push({
        id: 5000 + i,
        projectId: 1,
        url: `https://hook${i}.test`,
        events: ['run.completed'],
        active: true,
        createdBy: 'admin',
        createdAt: at,
        updatedAt: at,
      })
    localStorage.setItem('provenly.project', 'TC')

    // 20 rows on the first page, the other 5 on the second, whatever the list's order.
    const rows = (prefix: string) =>
      screen.queryAllByTestId(new RegExp(`^${prefix}`)).map((r) => r.getAttribute('data-testid'))
    for (const [path, prefix] of [
      ['/requirements', 'requirement-REQ-'],
      ['/issues', 'issue-BUG-'],
      ['/suites', 'suite-s'],
    ] as const) {
      const { user: u, unmount } = renderRoute(path)
      await waitFor(() => expect(rows(prefix)).toHaveLength(20))
      const first = rows(prefix)
      expect(screen.getByText(/Page 1 of 2 · 25 items/)).toBeInTheDocument()
      await u.click(screen.getByRole('button', { name: 'Next' }))
      await waitFor(() => expect(rows(prefix)).toHaveLength(5))
      expect(rows(prefix).filter((r) => first.includes(r))).toEqual([])
      unmount()
    }

    const { user: u } = renderRoute('/projects/TC')
    const card = (await screen.findByRole('heading', { name: 'Webhooks' })).closest<HTMLElement>(
      '[data-slot="card"]',
    )!
    expect(await within(card).findByTestId('webhook-5000')).toBeInTheDocument()
    await u.click(within(card).getByRole('button', { name: 'Next' }))
    expect(await within(card).findByTestId('webhook-5024')).toBeInTheDocument()
  })

  it('FE-INT-056 pickers search the server by key or title, say how many match and never show an id', async () => {
    manyCases(120)
    db.requirements.push(requirement(2000, { testCaseIds: [1005] }))
    const { user: u } = renderRoute('/requirements/TC/2000')
    // The linked test case reads by its key, not "#1005".
    expect(await screen.findByTestId('covering-1005')).toHaveTextContent('TC-1005')
    expect(screen.queryByText('#1005')).not.toBeInTheDocument()

    const picker = await screen.findByLabelText('Test case to link')
    await waitFor(() => expect(screen.getByTestId('picker-more')).toHaveTextContent('Showing 50 of 122'))
    for (const option of within(picker).getAllByRole('option').slice(1))
      expect(option.textContent).toMatch(/^TC-\d+ · /)

    // By key, in any case: one match and no "Showing" line.
    await u.type(screen.getByLabelText('Search: Test case to link'), 'tc-1117')
    await waitFor(() => expect(within(picker).getAllByRole('option')).toHaveLength(2))
    expect(within(picker).getByRole('option', { name: 'TC-1117 · Checkout step 117' })).toBeInTheDocument()
    expect(screen.queryByTestId('picker-more')).not.toBeInTheDocument()
    await u.selectOptions(picker, '1117')
    await u.click(screen.getByRole('button', { name: 'Link test case' }))
    expect(await screen.findByTestId('covering-1117')).toHaveTextContent('TC-1117')
    expect(db.requirements.at(-1)!.testCaseIds).toEqual([1005, 1117])

    // By title: past the first 50, still found.
    await u.clear(screen.getByLabelText('Search: Test case to link'))
    await u.type(screen.getByLabelText('Search: Test case to link'), 'step 99')
    await waitFor(() =>
      expect(within(picker).getByRole('option', { name: 'TC-1099 · Checkout step 99' })).toBeInTheDocument(),
    )

    // Nothing matches: the select says so.
    await u.clear(screen.getByLabelText('Search: Test case to link'))
    await u.type(screen.getByLabelText('Search: Test case to link'), 'zzz')
    expect(await within(picker).findByRole('option', { name: 'No test case matches' })).toBeInTheDocument()
  })
})
