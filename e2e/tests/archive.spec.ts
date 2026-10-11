import { apiURL } from '../playwright.config'
import { expect, test, uniqueProjectKey } from '../support/fixtures'

test.describe('Archived projects (Ed, 2026-10-11)', () => {
  test('[FE-E2E-039] an administrator archives a project: it leaves the lists, says it is archived, offers no changes, and unarchiving brings it back', async ({ page, request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Archive me')
    const tc = await (await request.post(`${apiURL}/api/v1/test-cases`, { data: { title: 'Kept forever', project: key } })).json()

    await page.goto(`/projects?q=${key}`)
    const row = page.getByTestId(`project-${key}`)
    await row.getByRole('button', { name: 'Archive…' }).click()
    await expect(row.getByRole('group', { name: `Archive ${key}` })).toContainText('nothing is deleted')
    await row.getByRole('button', { name: 'Archive', exact: true }).click()
    await expect(page.getByText(`No project matches “${key}”.`)).toBeVisible()

    // A controlled checkbox: it follows the address, which the click changes.
    await page.getByRole('checkbox', { name: 'Show archived projects instead' }).click()
    await expect(page.getByRole('checkbox', { name: 'Show archived projects instead' })).toBeChecked()
    await expect(page).toHaveURL(/archived=1/)
    await page.getByTestId(`project-${key}`).getByRole('link', { name: 'Archive me' }).click()
    await expect(page.getByTestId('archived-notice')).toContainText(`${key} · Archive me is archived`)

    await page.goto(`/test-cases/${tc.id}`)
    await expect(page.getByRole('heading', { name: new RegExp(`${key}-1`) })).toBeVisible()
    await expect(page.getByTestId('archived-notice')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Edit' })).toHaveCount(0)
    expect((await request.patch(`${apiURL}/api/v1/projects/${key}`, { data: { name: 'x' } })).status()).toBe(409)

    await page.getByTestId('archived-notice').getByRole('link', { name: 'archived projects' }).click()
    await page.getByLabel('Search projects').fill(key)
    await page.getByRole('button', { name: 'Find' }).click()
    await page.getByTestId(`project-${key}`).getByRole('button', { name: 'Unarchive' }).click()
    await expect(page.getByText(`No archived project matches “${key}”.`)).toBeVisible()
    await page.goto(`/test-cases/${tc.id}`)
    await expect(page.getByRole('button', { name: 'Edit' })).toBeVisible()
    await expect(page.getByTestId('archived-notice')).toHaveCount(0)
  })
})
