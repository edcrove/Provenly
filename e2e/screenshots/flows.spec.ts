import path from 'node:path'

import { expect, test, type APIRequestContext, type Page } from '@playwright/test'

/**
 * Walks every relevant UI flow on a freshly reset database and saves one
 * full-page screenshot per state in docs/screenshots (see its README.md).
 */
const out = path.resolve(import.meta.dirname, '../../docs/screenshots')
const api = `http://localhost:${process.env.E2E_API_PORT ?? '8082'}/api/v1`

let n = 0
async function shot(page: Page, name: string) {
  await page.waitForLoadState('networkidle')
  n += 1
  await page.screenshot({ path: path.join(out, `${String(n).padStart(2, '0')}-${name}.png`), fullPage: true })
}

async function createTC(request: APIRequestContext, title: string, automated: boolean, extra: Record<string, string> = {}) {
  const res = await request.post(`${api}/test-cases`, { data: { title, automated, ...extra } })
  expect(res.status()).toBe(201)
  return ((await res.json()) as { id: number }).id
}

async function ingest(request: APIRequestContext, runId: string, xml: string, status = 'completed', branch = 'main') {
  const params = new URLSearchParams({ provider: 'github', runId, runAttempt: '1', pipeline: 'e2e-nightly', branch, commit: '9f3c2a1e7b', status })
  const res = await request.post(`${api}/ingestion/junit?${params}`, { headers: { 'Content-Type': 'application/xml' }, data: xml })
  expect(res.status()).toBe(201)
  return ((await res.json()) as { testRun: { id: number } }).testRun.id
}

const junit = (...cases: string[]) =>
  `<?xml version="1.0" encoding="UTF-8"?><testsuites><testsuite name="web" timestamp="2026-09-28T10:00:00">${cases.join('')}</testsuite></testsuites>`
const tc = (name: string, id: number | string, body = '', time = '1.284') =>
  `<testcase name="${name}" classname="web" time="${time}"><properties><property name="tc-id" value="${id}"/></properties>${body}</testcase>`
const fail = (msg: string) => `<failure message="${msg}">Error: ${msg}\n    at checkout.spec.ts:42</failure>`

test('UI flows', async ({ page }) => {
  test.setTimeout(120_000)
  // Sign in as the bootstrapped administrator; page.request shares the browser's session cookie.
  await page.goto('/test-cases')
  await page.getByLabel('Username').fill('admin')
  await page.getByLabel('Password').fill('e2e admin password')
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page).toHaveURL(/\/test-cases$/)
  const request = page.request

  // Empty states.
  await page.goto('/test-cases')
  await expect(page.getByText('No test cases yet.')).toBeVisible()
  await shot(page, 'test-cases-empty')
  await page.goto('/test-runs')
  await shot(page, 'test-runs-empty')

  // A run before any automated test case exists: 0 of 0 executed.
  const emptyRun = await ingest(request, '1000', junit(tc('orphan test', 987654321)))

  // Create a test case in the UI, with steps.
  await page.goto('/test-cases/new')
  await page.getByLabel('Title').fill('User can log in with email and password')
  await page.getByLabel('Description').fill('Registered user signs in from the login page.')
  await page.getByLabel('Expected result').fill('The dashboard is shown with the user name')
  await page.getByLabel(/Automated/).check()
  await shot(page, 'test-case-new-form')
  await page.getByRole('button', { name: 'Create test case' }).click()
  await expect(page.getByRole('heading', { level: 1 })).toContainText('User can log in')
  const login = Number(page.url().split('/').pop())
  const add = page.getByRole('form', { name: 'Add step' })
  for (const [action, expected] of [
    ['Open /login', 'Login form is shown'],
    ['Enter valid email and password', ''],
    ['Click "Sign in"', 'Dashboard is shown'],
  ]) {
    await add.getByLabel('Step action').fill(action)
    if (expected) await add.getByLabel('Step expected result').fill(expected)
    await add.getByRole('button', { name: 'Add step' }).click()
    await expect(page.getByText(`. ${action}`)).toBeVisible()
  }
  await shot(page, 'test-case-detail-with-steps')
  await page.getByRole('button', { name: 'Edit step 2' }).click()
  await shot(page, 'test-case-step-edit')
  await page.keyboard.press('Escape')
  await page.goto(`/test-cases/${login}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await shot(page, 'test-case-edit')
  await page.getByRole('button', { name: 'Cancel' }).click()

  // Catalog.
  const logout = await createTC(request, 'User can log out', true)
  const checkout = await createTC(request, 'Checkout with credit card', true, { expectedResult: 'Order is confirmed' })
  const search = await createTC(request, 'Search products by name', true)
  const legacy = await createTC(request, 'Export orders to CSV (legacy)', true)
  const reset = await createTC(request, 'Password reset email is sent', false)

  // Full run: pass/fail per browser, skipped, error, 0 ms warning, manual TC, invalid ids, parse errors.
  const mainRun = await ingest(
    request,
    '1001',
    junit(
      tc('login [chromium]', login),
      tc('login [firefox]', `TC-${login}`, fail('Sign in button not found')),
      tc('logout', logout, '', '0'),
      tc('checkout with visa', checkout, '<skipped message="payment sandbox down"/>'),
      tc('export csv', legacy, '<error message="timeout after 30s">TimeoutError</error>'),
      tc('password reset', reset),
      `<testcase name="header renders" classname="web" time="0.2"/>`,
      tc('wishlist', 987654321),
      tc('cart badge', 'TC-12x'),
      tc('invalid time', search, '', 'abc'),
      `<testcase name="" classname="web" time="0.1"/>`,
    ),
  )

  await page.goto('/test-cases')
  await shot(page, 'test-cases-list')

  // Runs whose pipeline broke or was cancelled.
  const failedRun = await ingest(request, '1002', junit(tc('login [chromium]', login)), 'interrupted', 'feature/checkout')
  await ingest(request, '1003', junit(tc('logout', logout)), 'cancelled', 'feature/search')

  // Deprecate in the UI, then a later run still reports the deprecated TC.
  await page.goto(`/test-cases/${legacy}`)
  await page.getByRole('button', { name: 'Deprecate' }).click()
  await shot(page, 'test-case-deprecate-confirm')
  await page.getByRole('button', { name: 'Confirm deprecation' }).click()
  await expect(page.getByRole('button', { name: 'Reactivate' })).toBeVisible()
  await ingest(request, '1004', junit(tc('export csv', legacy)))
  await page.reload()
  await shot(page, 'test-case-deprecated-with-history')
  await page.goto('/test-cases')
  await page.getByLabel('Filter by status').selectOption('deprecated')
  await shot(page, 'test-cases-filter-deprecated')

  // Rounding: 3 executed of 4 expected, one each passed/failed/skipped (33.33% x3, Total 100%).
  const roundingRun = await ingest(
    request,
    '1005',
    junit(tc('login', login), tc('logout', logout, fail('session cookie still present')), tc('checkout', checkout, '<skipped/>')),
  )

  await page.goto('/test-runs')
  await shot(page, 'test-runs-list')

  await page.goto(`/test-runs/${mainRun}`)
  await expect(page.getByTestId('expected-total')).toBeVisible()
  await shot(page, 'test-run-detail')
  await page.getByLabel('Filter by status').selectOption('failed')
  await expect(page.getByTestId('result-row')).toHaveCount(1)
  await shot(page, 'test-run-results-filter-failed')
  await page.getByLabel('Filter by status').selectOption('')
  await page.getByLabel('Filter by TC-ID correlation').selectOption('unknown')
  await expect(page.getByTestId('result-row')).toHaveCount(1)
  await shot(page, 'test-run-results-filter-unknown')

  await page.goto(`/test-runs/${failedRun}`)
  await expect(page.getByTestId('interrupted-run')).toBeVisible()
  await shot(page, 'test-run-failed-pipeline')
  await page.goto(`/test-runs/${roundingRun}`)
  await shot(page, 'test-run-rounding-total')
  await page.goto(`/test-runs/${emptyRun}`)
  await shot(page, 'test-run-zero-of-zero')

  // Manual TC with automated results: warning on the test case, action on the run.
  await page.goto(`/test-cases/${reset}`)
  await expect(page.getByTestId('manual-with-results')).toBeVisible()
  await shot(page, 'test-case-manual-with-results')
  await page.goto(`/test-runs/${mainRun}`)
  const outside = page.getByTestId(`outside-${reset}`)
  await outside.getByRole('button', { name: 'Mark as automated' }).click()
  await expect(outside).toContainText('Now automated')
  await shot(page, 'test-run-outside-universe-marked')

  // Test case history across runs.
  await page.goto(`/test-cases/${login}`)
  await shot(page, 'test-case-history')

  // Reactivate.
  await page.goto(`/test-cases/${legacy}`)
  await page.getByRole('button', { name: 'Reactivate' }).click()
  await expect(page.getByRole('button', { name: 'Deprecate' })).toBeVisible()
  await shot(page, 'test-case-reactivated')

  // Validation and errors.
  await page.goto('/test-cases/new')
  await page.getByLabel('Title').fill('   ')
  await page.getByRole('button', { name: 'Create test case' }).click()
  await expect(page.getByLabel('Title')).toHaveAttribute('aria-invalid', 'true')
  await shot(page, 'test-case-new-validation-error')
  await page.goto('/test-cases/987654321')
  await expect(page.getByRole('alert')).toBeVisible()
  await shot(page, 'test-case-not-found')
  await page.goto('/test-runs/987654321')
  await expect(page.getByRole('alert')).toBeVisible()
  await shot(page, 'test-run-not-found')
  await page.goto('/does-not-exist')
  await shot(page, 'page-not-found')

  // Pagination (20 per page).
  for (let i = 1; i <= 20; i++) await createTC(request, `Catalog entry ${String(i).padStart(2, '0')}`, i % 2 === 0)
  await page.goto('/test-cases')
  await page.getByRole('button', { name: 'Next' }).click()
  await expect(page.getByText('Page 2 of 2')).toBeVisible()
  await shot(page, 'test-cases-pagination')

  // Validation fixes (Test Case UI card review, 2026-10-01).
  await page.goto('/test-cases?page=99')
  await expect(page).toHaveURL(/page=2$/)
  await expect(page.getByText('Page 2 of 2')).toBeVisible()
  await shot(page, 'test-cases-page-past-end')
  await page.goto('/test-cases?status=deprecated')
  await expect(page.getByText('No deprecated test cases.')).toBeVisible()
  await shot(page, 'test-cases-filter-no-matches')
  await page.goto('/test-cases/abc')
  await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible()
  await shot(page, 'test-case-invalid-id')
  await page.goto(`/test-cases/${login}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page.getByLabel('Title').fill('   ')
  await page.getByRole('button', { name: 'Save changes' }).click()
  await expect(page.getByLabel('Title')).toHaveAttribute('aria-invalid', 'true')
  await shot(page, 'test-case-edit-validation-error')

  // Phone width (375 px).
  await page.setViewportSize({ width: 375, height: 812 })
  await page.goto('/test-cases')
  await expect(page.getByText('Page 1 of 2')).toBeVisible()
  await shot(page, 'mobile-test-cases-list')
  await page.goto(`/test-cases/${login}`)
  await expect(page.getByRole('heading', { level: 1 })).toContainText('User can log in')
  await shot(page, 'mobile-test-case-detail')
  await page.goto('/test-cases/new')
  await shot(page, 'mobile-test-case-new-form')
  await page.goto(`/test-runs/${mainRun}`)
  await expect(page.getByTestId('expected-total')).toBeVisible()
  await shot(page, 'mobile-test-run-detail')
  // No page-level horizontal scroll at phone width: wide tables scroll inside their card.
  for (const url of ['/test-cases', `/test-cases/${login}`, '/test-runs', `/test-runs/${mainRun}`]) {
    await page.goto(url)
    await page.waitForLoadState('networkidle')
    expect(await page.evaluate(() => document.documentElement.scrollWidth), url).toBeLessThanOrEqual(375)
  }

  // Error details of a result: the full stack trace opens on demand.
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto(`/test-runs/${mainRun}?status=failed`)
  await page.getByRole('button', { name: /Show error details/ }).first().click()
  await expect(page.getByTestId('error-details')).toBeVisible()
  await shot(page, 'test-run-error-details')

  // Projects (prototype feature 1): a second project with its own numbering, a run ingested into it
  // (one reference with the default project's key is wrong_project) and the lists narrowed to it.
  await page.goto('/projects')
  await page.getByLabel('Key').fill('CHK')
  await page.getByLabel('Name').fill('Checkout')
  await page.getByLabel('Description').fill('Cart, payment and order confirmation')
  await page.getByRole('button', { name: 'Create project' }).click()
  await expect(page.getByTestId('project-CHK')).toBeVisible()
  await shot(page, 'projects')
  const pay = await createTC(request, 'Pay by card', true, { project: 'CHK' })
  await createTC(request, 'Refund an order', true, { project: 'CHK' })
  await createTC(request, 'Apply a discount code', false, { project: 'CHK' })
  const params = new URLSearchParams({ project: 'CHK', provider: 'github', runId: '5150', runAttempt: '1', pipeline: 'checkout', branch: 'main', commit: '4b1d2c3e9a' })
  const chkRun = await request.post(`${api}/ingestion/junit?${params}`, {
    headers: { 'Content-Type': 'application/xml' },
    data: junit(tc('pay visa', 'CHK-1'), tc('pay amex', 'CHK-1', fail('Card declined')), tc('login from checkout', `TC-${login}`)),
  })
  expect(chkRun.status()).toBe(201)
  await page.goto('/test-cases')
  await shot(page, 'test-cases-in-project')
  await page.goto(`/test-runs/${((await chkRun.json()) as { testRun: { id: number } }).testRun.id}`)
  await expect(page.getByRole('link', { name: 'CHK-1' }).first()).toBeVisible()
  await shot(page, 'test-run-in-project')
  expect(pay).toBeGreaterThan(0)

  // Accounts (prototype feature 2): users and invitations, account, joining by link and signing in.
  await page.goto('/users')
  await page.getByLabel('Email (optional)').fill('carla@example.com')
  await page.getByLabel('Note (optional)').fill('QA team')
  await page.getByRole('button', { name: 'Create invitation link' }).click()
  const link = (await page.getByTestId('invitation-link').textContent())!
  await shot(page, 'users-and-invitations')
  await page.goto('/account')
  await shot(page, 'account')
  await page.getByRole('button', { name: 'Sign out' }).click()
  await expect(page).toHaveURL(/\/login$/)
  await page.goto(link.replace(/^https?:\/\/[^/]+/, ''))
  await page.getByLabel('Username').fill('carla')
  await page.getByLabel('Display name').fill('Carla Gómez')
  await page.getByLabel('Password', { exact: true }).fill('carla password')
  await page.getByLabel('Repeat password').fill('carla password')
  await shot(page, 'accept-invitation')
  await page.getByRole('button', { name: 'Create account' }).click()
  await expect(page.getByTestId('current-user')).toHaveText('Carla Gómez')
  await page.getByRole('button', { name: 'Sign out' }).click()
  await page.getByLabel('Username').fill('carla')
  await page.getByLabel('Password').fill('wrong password')
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page.getByRole('alert')).toBeVisible()
  await shot(page, 'sign-in-error')

  // Roles (prototype feature 3): the administrator makes Carla a viewer of CHK; she reads but cannot edit.
  await page.getByLabel('Username').fill('admin')
  await page.getByLabel('Password').fill('e2e admin password')
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page.getByTestId('current-user')).toBeVisible()
  await page.goto('/projects/CHK')
  await page.getByLabel('Username').fill('carla')
  await page.getByRole('combobox', { name: 'Role', exact: true }).selectOption('viewer')
  await page.getByRole('button', { name: 'Add member' }).click()
  await expect(page.getByTestId('member-carla')).toBeVisible()
  await shot(page, 'project-members')
  await page.getByRole('button', { name: 'Sign out' }).click()
  await expect(page).toHaveURL(/\/login$/)
  await page.getByLabel('Username').fill('carla')
  await page.getByLabel('Password').fill('carla password')
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page.getByTestId('current-user')).toHaveText('Carla Gómez')
  await page.goto(`/test-cases/${pay}`)
  await expect(page.getByRole('heading', { level: 1 })).toContainText('Pay by card')
  await expect(page.getByRole('button', { name: 'Edit' })).toHaveCount(0)
  await shot(page, 'viewer-test-case')
})
