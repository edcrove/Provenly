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
  const refund = await createTC(request, 'Refund an order', true, { project: 'CHK' })
  const discount = await createTC(request, 'Apply a discount code', false, { project: 'CHK' })
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
  // CI API keys (prototype feature 4): the key is shown once, with the CI step that uses it.
  await page.getByLabel('Key name').fill('GitHub Actions')
  await page.getByRole('button', { name: 'Create API key' }).click()
  await expect(page.getByTestId('api-key-token')).toBeVisible()
  await shot(page, 'project-api-keys')
  // Optimistic locking (prototype feature 5): someone saves while the form is open; this save is refused.
  await page.goto(`/test-cases/${pay}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  expect((await request.patch(`${api}/test-cases/${pay}`, { data: { title: 'Pay by card (3-D Secure)' } })).status()).toBe(200)
  await page.getByLabel('Expected result', { exact: true }).fill('Order confirmed and receipt emailed')
  await page.getByRole('button', { name: 'Save changes' }).click()
  await expect(page.getByTestId('conflict')).toBeVisible()
  await shot(page, 'test-case-conflict')
  await page.getByTestId('conflict').getByRole('button', { name: 'Reload' }).click()
  await page.getByRole('button', { name: 'Save changes' }).click()
  await expect(page.getByRole('heading', { level: 1 })).toContainText('3-D Secure')
  // Snapshot amendment (prototype feature 6): a manual test case reported by CI is included in its run.
  const amendParams = new URLSearchParams({ project: 'CHK', provider: 'github', runId: '5151', runAttempt: '1', pipeline: 'checkout', branch: 'main', commit: '4b1d2c3e9b' })
  const amendRun = await request.post(`${api}/ingestion/junit?${amendParams}`, {
    headers: { 'Content-Type': 'application/xml' },
    data: junit(tc('pay visa', 'CHK-1'), tc('discount code', 'CHK-3')),
  })
  expect(amendRun.status()).toBe(201)
  await page.goto(`/test-runs/${((await amendRun.json()) as { testRun: { id: number } }).testRun.id}`)
  const outsideItem = page.getByTestId(`outside-${discount}`)
  await outsideItem.getByLabel('Why it belongs in this run').fill('Automated in sprint 12; the manual flag was stale')
  await outsideItem.getByRole('button', { name: 'Include in this run' }).click()
  await expect(page.getByTestId('amendments')).toBeVisible()
  await shot(page, 'test-run-amended')
  // Retries (prototype feature 7): a test that failed and then passed on a retry is flaky; every attempt is kept.
  const retryParams = new URLSearchParams({ project: 'CHK', provider: 'github', runId: '5152', runAttempt: '1', pipeline: 'checkout', branch: 'main', commit: '4b1d2c3e9c' })
  const retryXml = junit(
    `<testcase name="pay visa" classname="web" time="2.104"><flakyFailure message="Timed out waiting for the 3-D Secure frame"><stackTrace>TimeoutError: frame not attached\n    at checkout.spec.ts:88</stackTrace></flakyFailure><properties><property name="tc-id" value="CHK-1"/></properties></testcase>`,
    tc('refund', 'CHK-2'),
  )
  const retryRun = await request.post(`${api}/ingestion/junit?${retryParams}`, { headers: { 'Content-Type': 'application/xml' }, data: retryXml })
  expect(retryRun.status()).toBe(201)
  await page.goto(`/test-runs/${((await retryRun.json()) as { testRun: { id: number } }).testRun.id}`)
  await expect(page.getByTestId('flaky-badge')).toBeVisible()
  await shot(page, 'test-run-flaky')
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
  // Taxonomy (prototype feature 9): the administrator adds feature values, classifies and tags test cases, and
  // filters the project's list by classification.
  await page.getByRole('button', { name: 'Sign out' }).click()
  await page.getByLabel('Username').fill('admin')
  await page.getByLabel('Password').fill('e2e admin password')
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page.getByTestId('current-user')).toBeVisible()
  for (const [key, name] of [['payments', 'Payments'], ['refunds', 'Refunds']]) {
    expect((await request.post(`${api}/projects/CHK/dimensions/feature/values`, { data: { key, name } })).status()).toBe(201)
  }
  expect((await request.patch(`${api}/test-cases/${refund}`, { data: { tags: ['regression'], classification: { feature: 'refunds', risk: 'high' } } })).status()).toBe(200)
  await page.goto('/projects/CHK')
  const featureRow = page.getByTestId('dimension-feature')
  await featureRow.getByLabel('Add value to Feature: key').fill('discounts')
  await featureRow.getByLabel('Add value to Feature: name').fill('Discounts')
  await featureRow.getByRole('button', { name: 'Add value to Feature' }).click()
  await expect(featureRow.getByRole('button', { name: 'Archive Feature: Discounts' })).toBeVisible()
  await shot(page, 'project-classification')
  await page.goto(`/test-cases/${pay}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page.getByLabel('Tags').fill('smoke, card')
  await page.getByLabel('Feature').selectOption('payments')
  await page.getByLabel('Risk').selectOption('critical')
  await page.getByLabel('Test level').selectOption('e2e')
  await shot(page, 'test-case-classification-form')
  await page.getByRole('button', { name: 'Save changes' }).click()
  await expect(page.getByTestId('taxonomy')).toContainText('Risk: Critical')
  await shot(page, 'test-case-classified')
  await page.goto('/test-cases')
  await page.getByLabel('Current project').selectOption('CHK')
  await page.getByLabel('Filter by classification').selectOption('risk:critical')
  await expect(page.getByText('Refund an order')).toBeHidden()
  await shot(page, 'test-cases-filtered-by-classification')
  // Suites (prototype feature 10, MVP D2): a query suite and a static one; CI reports a smoke run for the query suite.
  expect((await request.post(`${api}/projects/CHK/suites`, { data: { key: 'smoke', name: 'Smoke', kind: 'query', query: { tag: 'smoke' } } })).status()).toBe(201)
  await page.goto('/suites')
  await page.getByLabel('Name', { exact: true }).fill('Release candidate')
  await page.getByLabel('Key', { exact: true }).fill('release')
  await page.getByLabel('Kind').selectOption('static')
  await page.getByRole('button', { name: 'Create suite' }).click()
  await expect(page.getByTestId('suite-release')).toBeVisible()
  await shot(page, 'suites')
  await page.getByRole('link', { name: 'Release candidate' }).click()
  await page.getByLabel('Test case to add').selectOption({ label: 'CHK-2 · Refund an order' })
  await page.getByRole('button', { name: 'Add to suite' }).click()
  await expect(page.getByTestId('suite-case-CHK-2')).toBeVisible()
  await shot(page, 'suite-static')
  const suiteParams = new URLSearchParams({ project: 'CHK', suite: 'smoke', provider: 'github', runId: '5153', runAttempt: '1', pipeline: 'smoke', branch: 'main', commit: '4b1d2c3e9d' })
  const suiteRun = await request.post(`${api}/ingestion/junit?${suiteParams}`, { headers: { 'Content-Type': 'application/xml' }, data: junit(tc('pay visa', 'CHK-1')) })
  expect(suiteRun.status()).toBe(201)
  await page.goto(`/test-runs/${((await suiteRun.json()) as { testRun: { id: number } }).testRun.id}`)
  await expect(page.getByTestId('suite-badge')).toBeVisible()
  await shot(page, 'test-run-for-suite')
  // Manual execution (prototype feature 11, MVP D3): a tester starts a sign-off run and records results.
  await createTC(request, 'Refund to a gift card', false, { project: 'CHK' })
  await createTC(request, 'Receipt email looks right', false, { project: 'CHK' })
  await page.goto('/test-runs/manual')
  await page.getByLabel('What is being tested').fill('Release 2.4 sign-off')
  await page.getByLabel('Branch or build (optional)').fill('release/2.4')
  await shot(page, 'manual-run-new')
  await page.getByRole('button', { name: 'Start manual run' }).click()
  const giftCard = page.getByTestId('manual-CHK-4')
  await giftCard.getByLabel('Note for CHK-4').fill('Gift card balance not updated')
  await giftCard.getByLabel('Failed step of CHK-4').fill('3')
  await giftCard.getByRole('button', { name: 'Fail' }).click()
  await expect(giftCard.getByTestId('status-badge')).toHaveText('failed')
  await shot(page, 'manual-run-in-progress')
  const manualRunId = new URL(page.url()).pathname.split('/').pop()
  // Requirements (prototype feature 12): a Jira import and a native requirement, covered by CHK test cases.
  const chk = ((await (await request.get(`${api}/test-cases?project=CHK&pageSize=100`)).json()) as { items: { id: number; key: string }[] }).items
  const idOf = (k: string) => chk.find((t) => t.key === k)!.id
  const reqs = `${api}/projects/CHK/requirements`
  expect((await request.post(`${reqs}/import`, {
    data: { provider: 'jira', items: [
      { externalId: 'PAY-31', title: 'Refunds reach the original payment method', providerStatus: 'In progress', url: 'https://jira.example.com/browse/PAY-31' },
      { externalId: 'PAY-40', title: 'Receipts list every item', providerStatus: 'To do' },
    ] },
  })).status()).toBe(200)
  const listed = ((await (await request.get(reqs)).json()) as { items: { id: number; externalId: string }[] }).items
  const pay31 = listed.find((r) => r.externalId === 'PAY-31')!.id
  expect((await request.put(`${reqs}/${pay31}/test-cases`, { data: { testCaseIds: [idOf('CHK-2'), idOf('CHK-4')] } })).status()).toBe(200)
  await page.goto('/requirements')
  await page.getByLabel('Title').fill('Customers pay by card')
  await page.getByRole('button', { name: 'Add requirement' }).click()
  await page.getByRole('link', { name: 'R-1' }).click()
  await page.getByLabel('Test case to link').selectOption({ label: 'CHK-1 · Pay by card (3-D Secure)' })
  await page.getByRole('button', { name: 'Link test case' }).click()
  await expect(page.getByTestId('coverage-badge')).toHaveText('Passing')
  await page.goto('/requirements')
  await expect(page.getByTestId('requirement-PAY-31')).toBeVisible()
  await shot(page, 'requirements')
  await page.goto(`/requirements/CHK/${pay31}`)
  await expect(page.getByTestId('coverage-badge')).toHaveText('Failing')
  await shot(page, 'requirement-detail')
  await page.goto(`/test-cases/${idOf('CHK-4')}`)
  await expect(page.getByTestId('covered-requirements')).toBeVisible()
  await shot(page, 'test-case-covered-requirements')
  // Issues (prototype feature 13, DEC-8): a Jira bug reproduced by the failing gift-card test; a closed one that passes.
  const issuesApi = `${api}/projects/CHK/issues`
  expect((await request.post(`${issuesApi}/import`, {
    data: { provider: 'jira', items: [
      { externalId: 'PAY-52', title: 'Gift card balance not updated after refund', state: 'open', providerStatus: 'In progress', url: 'https://jira.example.com/browse/PAY-52' },
      { externalId: 'PAY-48', title: 'Refund email sent twice', state: 'closed', providerStatus: 'Done' },
    ] },
  })).status()).toBe(200)
  const imported = ((await (await request.get(issuesApi)).json()) as { items: { id: number; externalId: string }[] }).items
  const issueId = (ext: string) => imported.find((i) => i.externalId === ext)!.id
  expect((await request.put(`${issuesApi}/${issueId('PAY-52')}/test-cases`, { data: { testCaseIds: [idOf('CHK-4')] } })).status()).toBe(200)
  expect((await request.put(`${issuesApi}/${issueId('PAY-48')}/test-cases`, { data: { testCaseIds: [idOf('CHK-2')] } })).status()).toBe(200)
  await page.goto('/issues')
  await page.getByLabel('Title').fill('Discount code ignored on mobile')
  await page.getByRole('button', { name: 'Add issue' }).click()
  await expect(page.getByTestId('issue-I-1')).toBeVisible()
  await shot(page, 'issues')
  await page.goto(`/issues/CHK/${issueId('PAY-52')}`)
  await expect(page.getByTestId('verification-badge')).toHaveText('Known issue')
  await shot(page, 'issue-detail')
  await page.goto(`/test-runs/${manualRunId}`)
  await expect(page.getByTestId('run-known-issues')).toBeVisible()
  await shot(page, 'test-run-known-issues')
  // Quality dashboard (prototype feature 14): CHK with its runs, requirements, issues, manual and flaky test cases.
  await page.goto('/dashboard')
  await expect(page.getByTestId('automation-rate')).toBeVisible()
  await expect(page.getByTestId('coverage-breakdown')).toBeVisible()
  await shot(page, 'dashboard')
  // Live runs (prototype feature 15): CI starts a run, streams events, then sends its report.
  const liveRun = (await (await request.post(`${api}/test-runs/live`, { data: { project: 'CHK', provider: 'github', runId: '5160', runAttempt: 1, pipeline: 'e2e', branch: 'main' } })).json()) as { id: number }
  expect((await request.post(`${api}/test-runs/${liveRun.id}/events`, {
    data: { events: [
      { eventId: 'l1', sequence: 1, type: 'test.started', testName: 'pay visa', testCase: 'CHK-1' },
      { eventId: 'l2', sequence: 2, type: 'test.finished', testName: 'pay visa', testCase: 'CHK-1', status: 'passed' },
      { eventId: 'l3', sequence: 3, type: 'test.started', testName: 'refund', testCase: 'CHK-2' },
    ] },
  })).status()).toBe(200)
  await page.goto(`/test-runs/${liveRun.id}`)
  await expect(page.getByTestId('live-CHK-2')).toContainText('running')
  await shot(page, 'live-run-in-progress')
  const liveParams = new URLSearchParams({ project: 'CHK', provider: 'github', runId: '5160', runAttempt: '1' })
  expect((await request.post(`${api}/ingestion/junit?${liveParams}`, {
    headers: { 'Content-Type': 'application/xml' },
    data: junit(tc('pay visa', 'CHK-1'), tc('refund', 'CHK-2', '<failure message="refund not issued"/>')),
  })).status()).toBe(201)
  await expect(page.getByTestId('reconciliation')).toHaveText('mismatch')
  await shot(page, 'live-run-reconciled')
  // Integrations (prototype feature 18): a webhook with a queued ping and a GitHub Issues connection (never synced:
  // the screenshots run offline).
  const hook = (await (await request.post(`${api}/projects/CHK/webhooks`, {
    data: { url: 'https://hooks.example.com/provenly', events: ['run.completed'] },
  })).json()) as { webhook: { id: number } }
  expect((await request.post(`${api}/projects/CHK/webhooks/${hook.webhook.id}/ping`)).status()).toBe(202)
  expect((await request.put(`${api}/projects/CHK/github`, { data: { repository: 'acme/checkout', token: 'ghp_screenshot_7f3a', labels: 'bug' } })).status()).toBe(200)
  await page.goto('/projects/CHK')
  await expect(page.getByTestId('github-connection')).toContainText('acme/checkout')
  await expect(page.getByTestId(`webhook-${hook.webhook.id}`)).toContainText('ping:')
  await shot(page, 'project-integrations')
  // Audit log (prototype feature 20): every change above, by user or API key.
  await page.goto('/audit?project=CHK')
  await expect(page.locator('[data-testid^="audit-"]').first()).toBeVisible()
  await shot(page, 'audit-log')
  await page.getByLabel('Current project').selectOption('')
})
