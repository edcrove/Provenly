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
