import { request as apiRequest } from '@playwright/test'

import { apiURL } from '../playwright.config'
import { expect, secret, test, uniqueProjectKey } from '../support/fixtures'

const unique = () => `r${Date.now().toString(36)}${Math.floor(Math.random() * 1000)}`
const empty = { cookies: [], origins: [] }

test.describe('Project roles', () => {
  test('[BE-E2E-009] roles through the API: a project member sees only their project and each role unlocks its writes', async ({ request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Roles project')
    const mine = await provenly.createTestCase({ title: 'in my project', project: key })
    const other = await provenly.createTestCase({ title: 'in the default project' })

    // An invitation makes the new account a viewer of the project.
    const inv = await (await request.post(`${apiURL}/api/v1/invitations`, { data: { project: key, role: 'viewer' } })).json()
    const username = unique()
    const anon = await apiRequest.newContext({ storageState: empty })
    const session = await (await anon.post(`${apiURL}/api/v1/invitations/accept`, {
      data: { token: inv.token, username, displayName: 'Viewer', password: secret('viewer password') },
    })).json()
    const viewer = await apiRequest.newContext({ storageState: empty, extraHTTPHeaders: { Authorization: `Bearer ${session.token}` } })

    const projects = await (await viewer.get(`${apiURL}/api/v1/projects`)).json()
    expect(projects.items.map((p: { key: string; myRole: string }) => [p.key, p.myRole])).toEqual([[key, 'viewer']])
    expect((await viewer.get(`${apiURL}/api/v1/test-cases/${mine.id}`)).status()).toBe(200)
    expect((await viewer.get(`${apiURL}/api/v1/test-cases/${other.id}`)).status()).toBe(404)
    expect((await viewer.patch(`${apiURL}/api/v1/test-cases/${mine.id}`, { data: { title: 'nope' } })).status()).toBe(403)

    // Promoted to member: edits pass, deprecation still needs a maintainer.
    const promoted = await request.put(`${apiURL}/api/v1/projects/${key}/members/${username}`, { data: { role: 'member' } })
    expect(promoted.status()).toBe(200)
    expect((await viewer.patch(`${apiURL}/api/v1/test-cases/${mine.id}`, { data: { title: 'edited by a member' } })).status()).toBe(200)
    expect((await viewer.post(`${apiURL}/api/v1/test-cases/${mine.id}/deprecate`)).status()).toBe(403)
    expect((await viewer.post(`${apiURL}/api/v1/projects`, { data: { key: uniqueProjectKey(), name: 'x' } })).status()).toBe(403)
    const members = await (await viewer.get(`${apiURL}/api/v1/projects/${key}/members`)).json()
    expect(members.items).toMatchObject([{ user: { username }, role: 'member' }])

    // Removed: the project disappears.
    expect((await request.delete(`${apiURL}/api/v1/projects/${key}/members/${username}`)).status()).toBe(204)
    expect((await viewer.get(`${apiURL}/api/v1/test-cases/${mine.id}`)).status()).toBe(404)
    await viewer.dispose()
    await anon.dispose()
  })

  test('[FE-E2E-012] an administrator adds a member in the UI; the member works only within their role', async ({ page, browser, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'UI roles')
    const tc = await provenly.createTestCase({ title: 'Role-gated case', project: key })
    const inv = await (await page.request.post(`${apiURL}/api/v1/invitations`, { data: {} })).json()
    const username = unique()
    const anon = await apiRequest.newContext({ storageState: empty })
    expect((await anon.post(`${apiURL}/api/v1/invitations/accept`, {
      data: { token: inv.token, username, displayName: 'UI Member', password: secret('member password') },
    })).status()).toBe(201)
    await anon.dispose()

    // Straight to the project's page: the paginated project list may show it on another page.
    await page.goto(`/projects/${key}`)
    await page.getByLabel('Username').fill(username)
    await page.getByRole('combobox', { name: 'Role', exact: true }).selectOption('member')
    await page.getByRole('button', { name: 'Add member' }).click()
    await expect(page.getByTestId(`member-${username}`)).toContainText('UI Member')

    const ctx = await browser.newContext({ storageState: empty })
    const member = await ctx.newPage()
    await member.goto(`/test-cases/${tc.id}`)
    await member.getByLabel('Username').fill(username)
    await member.getByLabel('Password').fill(secret('member password'))
    await member.getByRole('button', { name: 'Sign in' }).click()
    await expect(member.getByRole('heading', { level: 1 })).toContainText('Role-gated case')
    await expect(member.getByRole('button', { name: 'Edit' })).toBeVisible()
    await expect(member.getByRole('button', { name: 'Deprecate' })).toHaveCount(0)
    await expect(member.getByRole('link', { name: 'Users' })).toHaveCount(0)

    // Down to viewer: editing disappears after a reload.
    await page.getByRole('combobox', { name: `Role of ${username}` }).selectOption('viewer')
    await expect(page.getByRole('combobox', { name: `Role of ${username}` })).toHaveValue('viewer')
    await member.reload()
    await expect(member.getByRole('heading', { level: 1 })).toContainText('Role-gated case')
    await expect(member.getByRole('button', { name: 'Edit' })).toHaveCount(0)
    await ctx.close()
  })
})
