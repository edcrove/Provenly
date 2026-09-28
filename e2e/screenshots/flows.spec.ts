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

test('UI flows', async ({ page, request }) => {
  test.setTimeout(120_000)

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
  await page.getByRole('button', { name: 'Edit' }).click()
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
  const failedRun = await ingest(request, '1002', junit(tc('login [chromium]', login)), 'failed', 'feature/checkout')
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
  await shot(page, 'test-run-results-filter-failed')
  await page.getByLabel('Filter by status').selectOption('')
  await page.getByLabel('Filter by TC-ID correlation').selectOption('unknown')
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
  await shot(page, 'test-case-new-validation-error')
  await page.goto('/test-cases/987654321')
  await expect(page.getByRole('alert')).toBeVisible()
  await shot(page, 'test-case-not-found')
  await page.goto('/test-runs/987654321')
  await expect(page.getByRole('alert')).toBeVisible()
  await shot(page, 'test-run-not-found')
  await page.goto('/does-not-exist')
  await shot(page, 'page-not-found')
})
