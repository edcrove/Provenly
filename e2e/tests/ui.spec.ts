import { byName, byProperty, expect, junit, test, uniqueRunId } from '../support/fixtures'

test.describe('Frontend UI journeys', () => {
  test('[FE-E2E-001] manage a test case in the UI: create, edit, steps and deprecate', async ({ page }) => {
    await page.goto('/')
    await expect(page).toHaveURL(/\/test-cases$/)
    await page.getByRole('link', { name: 'New test case' }).click()
    await page.getByLabel('Title').fill('UI checkout')
    await page.getByLabel('Expected result').fill('Order confirmed')
    await page.getByRole('button', { name: 'Create test case' }).click()

    const heading = page.getByRole('heading', { level: 1 })
    await expect(heading).toContainText('UI checkout')
    const key = (await heading.textContent())!.match(/TC-\d+/)![0]
    await expect(page.getByText('manual')).toBeVisible()

    await page.getByRole('button', { name: 'Edit' }).click()
    await page.getByLabel('Title').fill('UI checkout v2')
    await page.getByLabel(/Automated/).check()
    await page.getByRole('button', { name: 'Save changes' }).click()
    await expect(heading).toHaveText(`${key} · UI checkout v2`)
    await expect(page.getByText('automated', { exact: true })).toBeVisible()

    const add = page.getByRole('form', { name: 'Add step' })
    for (const action of ['Add item to cart', 'Pay']) {
      await add.getByLabel('Step action').fill(action)
      await add.getByRole('button', { name: 'Add step' }).click()
      await expect(page.getByText(`. ${action}`)).toBeVisible()
    }
    await page.getByRole('button', { name: 'Move step 2 up' }).click()
    await expect(page.getByRole('list', { name: 'Steps' }).getByRole('listitem').first()).toContainText('1. Pay')
    await page.getByRole('button', { name: 'Delete step 2' }).click()
    await expect(page.getByText('Add item to cart')).toHaveCount(0)

    await page.getByRole('button', { name: 'Deprecate' }).click()
    await page.getByRole('button', { name: 'Confirm deprecation' }).click()
    await expect(page.getByText('deprecated', { exact: true })).toBeVisible()
    await expect(heading).toContainText(key)

    await page.getByRole('link', { name: 'Test Cases' }).click()
    await page.getByLabel('Filter by status').selectOption('deprecated')
    await page.getByRole('link', { name: key }).click()
    await page.getByRole('button', { name: 'Reactivate' }).click()
    await expect(page.getByRole('button', { name: 'Deprecate' })).toBeVisible()
    await expect(heading).toContainText(key)
  })

  test('[FE-E2E-002] POC demo: TC declared by an automated test, CI report, summary, detail and history', async ({ page, provenly }) => {
    await provenly.isolateUniverse()
    // 1. Create the test case in the UI (the TC-ID is assigned by Provenly, e.g. TC-153).
    await page.goto('/test-cases/new')
    await page.getByLabel('Title').fill('Demo: user can log in')
    await page.getByLabel('Expected result').fill('Dashboard is shown')
    await page.getByLabel(/Automated/).check()
    await page.getByRole('button', { name: 'Create test case' }).click()
    await expect(page.getByRole('heading', { level: 1 })).toContainText('Demo: user can log in')
    const id = Number(page.url().split('/').pop())
    const other = await provenly.createTestCase({ title: 'Demo: logout', automated: true })

    // 2. CI sends the JUnit report: two results for the TC (Chrome PASS, Firefox FAIL) + invalid TC-IDs.
    const runId = uniqueRunId()
    const res = await provenly.ingest(
      runId,
      1,
      junit(
        byProperty('login chrome', id),
        byName(`login firefox TC-${id}`, '<failure message="button not found">trace</failure>'),
        byName('unlabelled test'),
        byName('ghost TC-987654321'),
        '<testcase name=""/>',
      ),
    )
    expect(res.status()).toBe(201)

    // 3. The run is listed and its detail shows metadata, summary and diagnostics.
    await page.getByRole('link', { name: 'Test Runs' }).click()
    await page.getByRole('row', { name: new RegExp(`github:${runId}:1`) }).getByRole('link').click()
    await expect(page.getByText(`github:${runId}:1`).first()).toBeVisible()
    await expect(page.getByTestId('expected-total')).toHaveText('2')
    await expect(page.getByTestId('executed-total')).toHaveText('1')
    await expect(page.getByTestId('execution-percent')).toHaveText('50%')
    await expect(page.getByTestId('summary-failed').getByRole('cell')).toHaveText(['failed', '1', '50%', '100%'])
    await expect(page.getByTestId('summary-untested').getByRole('cell')).toHaveText(['untested', '1', '50%', '—'])
    await expect(page.getByRole('link', { name: `TC-${other.id}` }).first()).toBeVisible()
    await expect(page.getByTestId('diagnostic-missing')).toHaveText('1')
    await expect(page.getByTestId('diagnostic-unknown')).toHaveText('1')
    await expect(page.getByTestId('parse-error-row')).toContainText('discarded')

    // Both individual results are shown; the summary counts the TC once as failed.
    await expect(page.getByTestId('result-row')).toHaveCount(4)
    await page.getByLabel('Filter by status').selectOption('failed')
    await expect(page.getByTestId('result-row')).toHaveCount(1)
    await expect(page.getByTestId('result-row')).toContainText('login firefox')

    // 4. A later execution shows PASS/FAIL history on the test case, with observed metadata.
    await provenly.ingest(uniqueRunId(), 1, junit(byProperty('login chrome (renamed later)', id)))
    await page.getByTestId('result-row').getByRole('link', { name: `TC-${id}` }).click()
    const history = page.getByRole('table', { name: 'Execution history' })
    await expect(history.getByRole('row')).toHaveCount(4)
    await expect(history.getByTestId('status-badge')).toHaveText(['passed', 'failed', 'passed'])
    await expect(history.getByText('login chrome (renamed later)')).toBeVisible()
    await expect(page.getByText('Current definition')).toBeVisible()
    await expect(page.getByText(/Observed at execution time/)).toBeVisible()

    // 5. Editing the TC content later does not change how its earlier results read.
    await page.getByRole('button', { name: 'Edit' }).click()
    await page.getByLabel('Title').fill('Demo: user can log in (edited)')
    await page.getByRole('button', { name: 'Save changes' }).click()
    await expect(page.getByRole('heading', { level: 1 })).toContainText('(edited)')
    await expect(history.getByTestId('status-badge')).toHaveText(['passed', 'failed', 'passed'])
    await expect(history.getByText('login chrome (renamed later)')).toBeVisible()
  })

  test('[FE-E2E-003] resend is idempotent and a rerun appears as a new run in the UI', async ({ page, provenly }) => {
    const tc = await provenly.createTestCase({ title: 'UI idempotency', automated: true })
    const runId = uniqueRunId()
    const report = junit(byProperty('idempotency', tc.id))
    expect((await provenly.ingest(runId, 1, report)).status()).toBe(201)
    expect((await provenly.ingest(runId, 1, report)).status()).toBe(200)
    await page.goto('/test-runs')
    await expect(page.getByRole('row', { name: new RegExp(`github:${runId}:1`) })).toHaveCount(1)
    expect((await provenly.ingest(runId, 2, report)).status()).toBe(201)
    await page.reload()
    await expect(page.getByRole('row', { name: new RegExp(`github:${runId}:`) })).toHaveCount(2)
  })

  test('[FE-E2E-004] editing or deprecating test cases later does not change an existing run summary', async ({ page, provenly, request }) => {
    await provenly.isolateUniverse()
    const a = await provenly.createTestCase({ title: 'Snapshot A', automated: true })
    const b = await provenly.createTestCase({ title: 'Snapshot B', automated: true })
    const run = await (await provenly.ingest(uniqueRunId(), 1, junit(byProperty('a', a.id)))).json()
    await page.goto(`/test-runs/${run.testRun.id}`)
    await expect(page.getByTestId('expected-total')).toHaveText('2')
    await expect(page.getByTestId('execution-percent')).toHaveText('50%')

    await page.goto(`/test-cases/${a.id}`)
    await page.getByRole('button', { name: 'Edit' }).click()
    await page.getByLabel('Title').fill('Snapshot A edited')
    await page.getByRole('button', { name: 'Save changes' }).click()
    await expect(page.getByRole('heading', { level: 1 })).toContainText('Snapshot A edited')
    await request.post(`${new URL(page.url()).origin}/api/v1/test-cases/${b.id}/deprecate`)
    await provenly.createTestCase({ title: 'Snapshot C (new)', automated: true })

    await page.goto(`/test-runs/${run.testRun.id}`)
    await expect(page.getByTestId('expected-total')).toHaveText('2')
    await expect(page.getByTestId('execution-percent')).toHaveText('50%')
    await expect(page.getByRole('link', { name: `TC-${b.id}` }).first()).toBeVisible()
  })

  test('[FE-E2E-005] unknown pages and missing resources are reported', async ({ page }) => {
    await page.goto('/does-not-exist')
    await expect(page.getByText('Page not found')).toBeVisible()
    await page.getByRole('link', { name: 'Go to test cases' }).click()
    await expect(page).toHaveURL(/\/test-cases$/)
    await page.goto('/test-cases/987654321')
    await expect(page.getByRole('alert')).toContainText('not found')
    await page.goto('/test-runs/987654321')
    await expect(page.getByRole('alert')).toContainText('not found')
  })

  test('[FE-E2E-006] a manual TC receiving automated results is flagged and can be marked automated', async ({ page, provenly }) => {
    await provenly.isolateUniverse()
    const manual = await provenly.createTestCase({ title: 'Forgot to mark automated', automated: false })
    const run1 = await (await provenly.ingest(uniqueRunId(), 1, junit(byProperty('forgotten', manual.id)))).json()

    await page.goto(`/test-runs/${run1.testRun.id}`)
    await expect(page.getByTestId('expected-total')).toHaveText('0')
    const item = page.getByTestId(`outside-${manual.id}`)
    await expect(item).toContainText('Forgot to mark automated')
    await item.getByRole('button', { name: 'Mark as automated' }).click()
    await expect(item).toContainText('Now automated')
    await page.reload()
    await expect(page.getByTestId('expected-total')).toHaveText('0')

    const run2 = await (await provenly.ingest(uniqueRunId(), 1, junit(byProperty('forgotten', manual.id)))).json()
    await page.goto(`/test-runs/${run2.testRun.id}`)
    await expect(page.getByTestId('expected-total')).toHaveText('1')
    await expect(page.getByTestId('execution-percent')).toHaveText('100%')
  })

  test('[FE-E2E-006] the test case page warns when a manual TC has automated results', async ({ page, provenly }) => {
    const manual = await provenly.createTestCase({ title: 'Manual with results', automated: false })
    await provenly.ingest(uniqueRunId(), 1, junit(byProperty('manual', manual.id)))
    await page.goto(`/test-cases/${manual.id}`)
    const alert = page.getByTestId('manual-with-results')
    await expect(alert).toBeVisible()
    await alert.getByRole('button', { name: 'Mark as automated' }).click()
    await expect(alert).toHaveCount(0)
    await expect(page.getByText('automated', { exact: true })).toBeVisible()
  })

  test('[FE-E2E-007] a run whose CI pipeline broke is flagged as possibly incomplete', async ({ page, provenly }) => {
    await provenly.isolateUniverse()
    const a = await provenly.createTestCase({ title: 'Broken pipeline A', automated: true })
    await provenly.createTestCase({ title: 'Broken pipeline B', automated: true })
    const res = await provenly.ingest(uniqueRunId(), 1, junit(byProperty('a', a.id)), { status: 'failed' })
    expect(res.status()).toBe(201)
    const run = await res.json()
    expect(run.testRun.status).toBe('failed')

    await page.goto('/test-runs')
    await expect(page.getByRole('row', { name: new RegExp(run.testRun.externalRunId) })).toContainText('failed')
    await page.goto(`/test-runs/${run.testRun.id}`)
    await expect(page.getByTestId('interrupted-run')).toContainText('The CI execution failed')
    await expect(page.getByTestId('summary-untested').getByRole('cell').nth(1)).toHaveText('1')
  })
})
