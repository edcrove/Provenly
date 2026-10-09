import { apiURL } from '../playwright.config'
import { expect, pickProject, test, uniqueProjectKey } from '../support/fixtures'

test.describe('Taxonomy', () => {
  test('[BE-E2E-015] dimensions, values, tags and classification through the API, and the list filters', async ({ request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Taxonomy API')
    const dims = `${apiURL}/api/v1/projects/${key}/dimensions`
    const seeded = await (await request.get(dims)).json()
    expect(seeded.items.map((d: { key: string }) => d.key)).toEqual(['feature', 'component', 'level', 'depth', 'type', 'risk', 'platform'])

    expect((await request.post(`${dims}/feature/values`, { data: { key: 'payments', name: 'Payments' } })).status()).toBe(201)
    expect((await request.post(`${dims}/feature/values`, { data: { key: 'payments', name: 'Again' } })).status()).toBe(409)
    expect((await request.post(dims, { data: { key: 'browser', name: 'Browser' } })).status()).toBe(201)

    const pay = await provenly.createTestCase({ title: 'pay', project: key, tags: ['Smoke'], classification: { feature: 'payments', risk: 'critical' } })
    expect(pay).toMatchObject({ tags: ['smoke'], classification: { feature: 'payments', risk: 'critical' } })
    await provenly.createTestCase({ title: 'login', project: key, tags: ['smoke'], classification: { risk: 'low' } })

    const list = async (query: string) => (await (await request.get(`${apiURL}/api/v1/test-cases?project=${key}&${query}`)).json()).items.map((t: { title: string }) => t.title)
    expect(await list('tag=smoke')).toEqual(['login', 'pay'])
    expect(await list('classification=risk:critical,feature:payments')).toEqual(['pay'])

    // Archived values stay on test cases but cannot be assigned again; the version advances with each change.
    expect((await request.patch(`${dims}/risk/values/low`, { data: { archived: true } })).status()).toBe(200)
    const url = `${apiURL}/api/v1/test-cases/${pay.id}`
    const refused = await request.patch(url, { data: { classification: { risk: 'low' } } })
    expect(refused.status()).toBe(400)
    expect((await refused.json()).errors[0].field).toBe('classification.risk')
    const before = (await request.get(url)).headers()['etag']
    const changed = await request.patch(url, { headers: { 'If-Match': before }, data: { tags: [], classification: { risk: null } } })
    expect(changed.status()).toBe(200)
    expect(await changed.json()).toMatchObject({ tags: [], classification: { feature: 'payments' } })
    expect(changed.headers()['etag']).not.toBe(before)
  })

  test('[FE-E2E-017] a maintainer sets up a dimension, classifies and tags a test case, and filters the list by them', async ({ page, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Taxonomy UI')
    await provenly.createTestCase({ title: 'Untagged case', project: key })

    await page.goto(`/projects/${key}`)
    const feature = page.getByTestId('dimension-feature')
    await feature.getByLabel('Add value to Feature: key').fill('payments')
    await feature.getByLabel('Add value to Feature: name').fill('Payments')
    await feature.getByRole('button', { name: 'Add value to Feature' }).click()
    await expect(feature.getByRole('button', { name: 'Archive Feature: Payments' })).toBeVisible()

    await page.goto('/test-cases/new')
    await page.getByLabel('Project', { exact: true }).selectOption(key)
    await page.getByLabel('Title').fill('Pay by card')
    await page.getByLabel('Tags').fill('smoke, checkout')
    await page.getByLabel('Feature').selectOption('payments')
    await page.getByLabel('Risk').selectOption('critical')
    await page.getByRole('button', { name: 'Create test case' }).click()
    const taxonomy = page.getByTestId('taxonomy')
    await expect(taxonomy).toContainText('Feature: Payments')
    await expect(taxonomy).toContainText('Risk: Critical')
    await expect(taxonomy).toContainText('#checkout')

    await page.getByRole('button', { name: 'Edit', exact: true }).click()
    await page.getByLabel('Risk').selectOption('high')
    await page.getByRole('button', { name: 'Save changes' }).click()
    await expect(taxonomy).toContainText('Risk: High')

    await page.goto('/test-cases')
    await pickProject(page, key)
    await expect(page.getByText('Untagged case')).toBeVisible()
    await page.getByLabel('Tag', { exact: true }).fill('smoke')
    await page.getByLabel('Tag', { exact: true }).press('Enter')
    await expect(page.getByText('Untagged case')).toBeHidden()
    await expect(page.getByText('Pay by card')).toBeVisible()
    await page.getByLabel('Tag', { exact: true }).fill('')
    await page.getByLabel('Tag', { exact: true }).press('Enter')
    await page.getByLabel('Filter by classification').selectOption('feature:payments')
    await expect(page.getByText('Pay by card')).toBeVisible()
    await expect(page.getByText('Untagged case')).toBeHidden()
    await pickProject(page, '')
  })
})
