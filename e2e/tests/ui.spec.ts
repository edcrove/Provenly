import { byName, byProperty, expect, junit, test, uniqueProjectKey, uniqueRunId } from '../support/fixtures'

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
    await page.getByRole('group', { name: 'Confirm deleting step 2' }).getByRole('button', { name: 'Delete' }).click()
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
    const key = (await page.getByRole('heading', { level: 1 }).textContent())!.match(/TC-\d+/)![0]
    const other = await provenly.createTestCase({ title: 'Demo: logout', automated: true })

    // 2. CI sends the JUnit report: two results for the TC (Chrome PASS, Firefox FAIL) + invalid TC-IDs.
    const runId = uniqueRunId()
    const res = await provenly.ingest(
      runId,
      1,
      junit(
        byProperty('login chrome', key),
        byName(`login firefox ${key}`, '<failure message="button not found">trace</failure>'),
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
    await expect(page.getByRole('link', { name: `${other.key}` }).first()).toBeVisible()
    await expect(page.getByTestId('diagnostic-missing')).toHaveText('1')
    await expect(page.getByTestId('diagnostic-unknown')).toHaveText('1')
    await expect(page.getByTestId('parse-error-row')).toContainText('discarded')

    // Both individual results are shown; the summary counts the TC once as failed.
    await expect(page.getByTestId('result-row')).toHaveCount(4)
    await page.getByLabel('Filter by status').selectOption('failed')
    await expect(page.getByTestId('result-row')).toHaveCount(1)
    await expect(page.getByTestId('result-row')).toContainText('login firefox')

    // 4. A later execution shows PASS/FAIL history on the test case, with observed metadata.
    await provenly.ingest(uniqueRunId(), 1, junit(byProperty('login chrome (renamed later)', key)))
    await page.getByTestId('result-row').getByRole('link', { name: key }).click()
    const history = page.getByRole('table', { name: 'Execution history' })
    await expect(history.getByRole('row')).toHaveCount(4)
    await expect(history.getByTestId('status-badge')).toHaveText(['passed', 'failed', 'passed'])
    // Executed is when the report says the tests ran (suite timestamp); Reported is when Provenly received it.
    await expect(history.getByText('2026-09-28 10:00:00 UTC')).toHaveCount(3)
    await expect(history.getByRole('columnheader', { name: 'Reported' })).toBeVisible()
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
    const report = junit(byProperty('idempotency', tc.key))
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
    const run = await (await provenly.ingest(uniqueRunId(), 1, junit(byProperty('a', a.key)))).json()
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
    await expect(page.getByRole('link', { name: `${b.key}` }).first()).toBeVisible()
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
    const run1 = await (await provenly.ingest(uniqueRunId(), 1, junit(byProperty('forgotten', manual.key)))).json()

    await page.goto(`/test-runs/${run1.testRun.id}`)
    await expect(page.getByTestId('expected-total')).toHaveText('0')
    const item = page.getByTestId(`outside-${manual.id}`)
    await expect(item).toContainText('Forgot to mark automated')
    await item.getByRole('button', { name: 'Mark as automated' }).click()
    await expect(item).toContainText('Now automated')
    await page.reload()
    await expect(page.getByTestId('expected-total')).toHaveText('0')

    const run2 = await (await provenly.ingest(uniqueRunId(), 1, junit(byProperty('forgotten', manual.key)))).json()
    await page.goto(`/test-runs/${run2.testRun.id}`)
    await expect(page.getByTestId('expected-total')).toHaveText('1')
    await expect(page.getByTestId('execution-percent')).toHaveText('100%')
  })

  test('[FE-E2E-006] the test case page warns when a manual TC has automated results', async ({ page, provenly }) => {
    const manual = await provenly.createTestCase({ title: 'Manual with results', automated: false })
    await provenly.ingest(uniqueRunId(), 1, junit(byProperty('manual', manual.key)))
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
    const res = await provenly.ingest(uniqueRunId(), 1, junit(byProperty('a', a.key)), { status: 'interrupted' })
    expect(res.status()).toBe(201)
    const run = await res.json()
    expect(run.testRun.executionStatus).toBe('interrupted')
    expect(run.testRun.outcome).toMatchObject({ verdict: 'incomplete', executed: 1, passed: 1, untested: 1, passRate: 100 })

    // The verdict (test outcome) and the execution status (pipeline) are separate.
    await page.goto('/test-runs')
    const row = page.getByRole('row', { name: new RegExp(run.testRun.externalRunId) })
    await expect(row.getByTestId('verdict-badge')).toHaveText('incomplete')
    await expect(row.getByTestId('pass-rate')).toHaveText('100%')
    await expect(row.getByTestId('outcome-breakdown')).toContainText('1 passed · 1 untested')
    await expect(row.getByTestId('execution-badge')).toHaveText('interrupted')
    await page.goto(`/test-runs/${run.testRun.id}`)
    await expect(page.getByTestId('interrupted-run')).toContainText('The CI execution was interrupted')
    await expect(page.getByTestId('run-pass-rate')).toHaveText('100% of executed passed')
    await expect(page.getByTestId('summary-untested').getByRole('cell').nth(1)).toHaveText('1')
  })

  test('[FE-E2E-008] long unbroken text wraps at phone width and a rejected step keeps what was typed', async ({ page, provenly }) => {
    const tc = await provenly.createTestCase({ title: 'T'.repeat(200), description: 'D'.repeat(500) })
    await page.setViewportSize({ width: 375, height: 812 })
    await page.goto(`/test-cases/${tc.id}`)
    const add = page.getByRole('form', { name: 'Add step' })
    await add.getByLabel('Step action').fill('x'.repeat(300))
    await add.getByLabel('Step expected result').fill('y'.repeat(300))
    await add.getByRole('button', { name: 'Add step' }).click()
    await expect(page.getByRole('list', { name: 'Steps' }).getByRole('listitem')).toHaveCount(1)
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(375)

    // HTML and emoji in user text render literally (escaped), never as markup.
    await add.getByLabel('Step action').fill('<script>window.pwned = 1</script> <b>bold</b> 🚀 ñandú')
    await add.getByRole('button', { name: 'Add step' }).click()
    await expect(page.getByText('<script>window.pwned = 1</script> <b>bold</b> 🚀 ñandú')).toBeVisible()
    expect(await page.evaluate(() => (window as unknown as { pwned?: number }).pwned)).toBeUndefined()
    await expect(page.locator('ol[aria-label="Steps"] b')).toHaveCount(0)

    await add.getByLabel('Step action').fill('   ')
    await add.getByLabel('Step expected result').fill('kept')
    await add.getByRole('button', { name: 'Add step' }).click()
    await expect(page.getByRole('alert')).toContainText('must not be empty')
    await expect(add.getByLabel('Step expected result')).toHaveValue('kept')

    // The list pages fit a phone too (the screenshot spec checks this as well, but it does not run in CI).
    for (const url of ['/test-cases', '/test-runs']) {
      await page.goto(url)
      await expect(page.getByRole('table').first()).toBeVisible()
      expect(await page.evaluate(() => document.documentElement.scrollWidth), url).toBeLessThanOrEqual(375)
    }
  })

  test('[FE-E2E-009] run pages load cleanly and a result expands its full error details', async ({ page, provenly }) => {
    const problems: string[] = []
    page.on('console', (m) => { if (m.type() === 'error') problems.push(m.text()) })
    page.on('response', (r) => { if (r.status() >= 400) problems.push(`${r.status()} ${r.url()}`) })
    const tc = await provenly.createTestCase({ title: 'Checkout totals', automated: true })
    const report = `<testsuite name="s"><testcase name="checkout ${tc.key}"><failure message="total mismatch">expected 10 got 9
  at Cart.total (cart.ts:42)</failure><failure message="tax mismatch">expected 2 got 1</failure></testcase>
<testcase name="&lt;script&gt;window.pwned = 1&lt;/script&gt; 🚀 ${tc.key}"/></testsuite>`
    const branch = 'feature/' + 'b'.repeat(200)
    const run = await (await provenly.ingest(uniqueRunId(), 1, report, { branch })).json()

    await page.goto('/test-runs')
    await expect(page.getByRole('table').first()).toBeVisible()
    await page.goto(`/test-runs/${run.testRun.id}`)
    const toggle = page.getByRole('button', { name: 'Show error details of total mismatch' })
    await expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await toggle.click()
    await expect(page.getByTestId('error-details')).toContainText('at Cart.total (cart.ts:42)')
    await expect(page.getByTestId('error-details')).toContainText('failure: tax mismatch')
    // Test names render literally (escaped) and a very long branch wraps, even at phone width.
    await expect(page.getByText('<script>window.pwned = 1</script> 🚀', { exact: false })).toBeVisible()
    expect(await page.evaluate(() => (window as unknown as { pwned?: number }).pwned)).toBeUndefined()
    await page.setViewportSize({ width: 375, height: 812 })
    await expect(page.getByText(branch)).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(375)
    // The test case page with its execution history (long branch included) fits a phone too.
    await page.getByRole('link', { name: `${tc.key}` }).first().click()
    await expect(page.getByRole('table', { name: 'Execution history' }).getByText(branch.slice(0, 20), { exact: false }).first()).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(375)
    await page.setViewportSize({ width: 1280, height: 800 })
    await expect(page.getByRole('heading', { name: 'Execution history' })).toBeVisible()
    expect(problems, 'no console errors or failed requests (e.g. a missing favicon)').toEqual([])
  })

  test('[FE-E2E-010] projects: create one, work inside it and read its run with its own keys', async ({ page, provenly }) => {
    const key = uniqueProjectKey()
    await page.goto('/projects')
    await page.getByLabel('Key').fill(key)
    await page.getByLabel('Name').fill('E2E UI project')
    await page.getByRole('button', { name: 'Create project' }).click()
    // The project list is paginated by key and other journeys create projects too: check the new one where it is
    // always visible, as the current project.
    await expect(page.getByLabel('Current project')).toHaveValue(key)
    await expect(page.getByLabel('Current project').locator('option:checked')).toContainText('E2E UI project')

    await page.getByRole('link', { name: 'Test Cases' }).click()
    await expect(page.getByText(`No test cases yet in ${key}.`)).toBeVisible()
    await page.getByRole('link', { name: 'New test case' }).click()
    await expect(page.getByLabel('Project', { exact: true })).toHaveValue(key)
    await page.getByLabel('Title').fill('Pay by card')
    await page.getByLabel(/Automated/).check()
    await page.getByRole('button', { name: 'Create test case' }).click()
    await expect(page.getByRole('heading', { level: 1 })).toHaveText(`${key}-1 · Pay by card`)

    const run = await (await provenly.ingest(uniqueRunId(), 1, junit(byName(`pay ${key}-1`, '<failure/>')), { project: key })).json()
    await page.getByRole('link', { name: 'Test Runs' }).click()
    await expect(page.getByTestId('run-project')).toHaveText([key])
    await page.getByRole('link', { name: `#${run.testRun.id}` }).click()
    await expect(page.getByTestId('result-row').getByRole('link', { name: `${key}-1` })).toBeVisible()

    await page.getByLabel('Current project').selectOption('')
    await page.getByRole('link', { name: 'Test Cases' }).click()
    await expect(page).toHaveURL(/\/test-cases$/)
    // The run page links the test case too: wait until it is gone before looking it up in the list.
    await expect(page.getByTestId('result-row')).toHaveCount(0)
    await expect(page.getByRole('link', { name: `${key}-1` })).toBeVisible()
  })
})
