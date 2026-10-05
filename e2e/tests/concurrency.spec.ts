import { apiURL } from '../playwright.config'
import { expect, test } from '../support/fixtures'

test.describe('Concurrent edits', () => {
  test('[BE-E2E-011] optimistic locking through the API: ETag on reads, 412 on a stale If-Match, nothing overwritten', async ({ request, provenly }) => {
    const tc = await provenly.createTestCase({ title: 'shared case' })
    const url = `${apiURL}/api/v1/test-cases/${tc.id}`
    const read = await request.get(url)
    const tag = read.headers()['etag']
    expect(tag).toBe('"1"')

    // Ana saves first; Bob, who read the same version, is refused.
    const ana = await request.patch(url, { headers: { 'If-Match': tag }, data: { title: 'Ana' } })
    expect(ana.status()).toBe(200)
    expect(ana.headers()['etag']).toBe('"2"')
    const bob = await request.patch(url, { headers: { 'If-Match': tag }, data: { title: 'Bob' } })
    expect(bob.status()).toBe(412)
    expect((await bob.json()).code).toBe('precondition_failed')
    expect((await (await request.get(url)).json()).title).toBe('Ana')

    // Step writes advance the same version; without If-Match the write applies (last write wins).
    const step = await request.post(`${url}/steps`, { headers: { 'If-Match': '"2"' }, data: { action: 'open' } })
    expect(step.headers()['etag']).toBe('"3"')
    expect((await request.post(`${url}/steps`, { headers: { 'If-Match': '"2"' }, data: { action: 'late' } })).status()).toBe(412)
    expect((await request.patch(url, { data: { title: 'no precondition' } })).status()).toBe(200)
    expect((await request.patch(url, { headers: { 'If-Match': 'nope' }, data: { title: 'x' } })).status()).toBe(400)
  })

  test('[FE-E2E-014] two people edit the same test case: the second is warned, reloads and saves without losing the first', async ({ page, browser, provenly }) => {
    const tc = await provenly.createTestCase({ title: 'Checkout total' })
    const other = await browser.newPage()

    await page.goto(`/test-cases/${tc.id}`)
    await other.goto(`/test-cases/${tc.id}`)
    await page.getByRole('button', { name: 'Edit', exact: true }).click()
    await other.getByRole('button', { name: 'Edit', exact: true }).click()

    await other.getByLabel('Title').fill('Checkout total with tax')
    await other.getByRole('button', { name: 'Save changes' }).click()
    await expect(other.getByRole('heading', { level: 1 })).toContainText('Checkout total with tax')

    await page.getByLabel('Expected result', { exact: true }).fill('Total includes shipping')
    await page.getByRole('button', { name: 'Save changes' }).click()
    const conflict = page.getByTestId('conflict')
    await expect(conflict).toContainText('Someone else saved this test case')
    await conflict.getByRole('button', { name: 'Reload' }).click()
    await expect(page.getByRole('heading', { level: 1 })).toContainText('Checkout total with tax')
    // The form keeps what was typed and sends only it: the other person's title survives.
    await page.getByRole('button', { name: 'Save changes' }).click()
    await expect(page.getByText('Total includes shipping')).toBeVisible()
    await expect(page.getByRole('heading', { level: 1 })).toContainText('Checkout total with tax')
    await other.close()
  })
})
