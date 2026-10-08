import { byProperty, expect, junit, test, uniqueRunId } from '../support/fixtures'

test.describe('A failed run (deployed audit)', () => {
  test('[FE-E2E-035] a developer opening a failed run sees its failures first, before the metadata', async ({ page, provenly }) => {
    const ok = await provenly.createTestCase({ title: 'checkout passes', automated: true })
    const broken = await provenly.createTestCase({ title: 'refund breaks', automated: true })
    const res = await provenly.ingest(
      uniqueRunId(),
      1,
      junit(byProperty('checkout passes', ok.key), byProperty('refund breaks', broken.key, '<failure message="Refund total is 0.00"/>')),
    )
    const runId = ((await res.json()) as { testRun: { id: number } }).testRun.id
    await page.goto(`/test-runs/${runId}`)
    const headings = page.getByRole('heading', { level: 2 })
    await expect(headings.first()).toHaveText('Results')
    const rows = page.getByRole('table', { name: 'Results' }).getByTestId('result-row')
    await expect(rows.first()).toContainText('Refund total is 0.00')
    await expect(rows.nth(1)).toContainText(ok.key)
  })
})

test.describe('Did it pass on main lately? (deployed audit)', () => {
  test('[FE-E2E-036] a developer reads when a test case last passed on main and filters its history by branch', async ({ page, provenly }) => {
    const tc = await provenly.createTestCase({ title: 'pay with voucher', automated: true })
    expect((await provenly.ingest(uniqueRunId(), 1, junit(byProperty('pay with voucher', tc.key)), { branch: 'main' })).status()).toBe(201)
    expect((await provenly.ingest(uniqueRunId(), 1, junit(byProperty('pay with voucher', tc.key, '<failure/>')), { branch: 'feature/vouchers' })).status()).toBe(201)
    await page.goto(`/test-cases/${tc.id}`)
    await expect(page.getByTestId('last-passed')).toContainText('Last passed: ')
    await expect(page.getByTestId('last-passed')).toContainText('(main)')
    await page.getByLabel('History branch').fill('feature/vouchers')
    await page.keyboard.press('Enter')
    await expect(page.getByTestId('last-passed')).toHaveText('Never passed on feature/vouchers.')
    await expect(page.getByRole('table', { name: 'Execution history' }).getByRole('row')).toHaveCount(2)
  })
})

test.describe('Printing a report (deployed audit)', () => {
  test('[FE-E2E-037] a printed run shows its content and caption, without the header, menu or buttons', async ({ page, provenly }) => {
    const tc = await provenly.createTestCase({ title: 'printable', automated: true })
    const res = await provenly.ingest(uniqueRunId(), 1, junit(byProperty('printable', tc.key)))
    const runId = ((await res.json()) as { testRun: { id: number } }).testRun.id
    await page.goto(`/test-runs/${runId}`)
    await expect(page.getByRole('button', { name: 'Print' })).toBeVisible()
    await expect(page.getByTestId('print-caption')).toBeHidden()
    await page.emulateMedia({ media: 'print' })
    await expect(page.getByRole('banner')).toBeHidden()
    await expect(page.getByRole('button', { name: 'Print' })).toBeHidden()
    await expect(page.getByTestId('print-caption')).toBeVisible()
    await expect(page.getByTestId('print-caption')).toContainText(`test run #${runId}`)
    await expect(page.getByRole('table', { name: 'Results' })).toBeVisible()
  })
})
