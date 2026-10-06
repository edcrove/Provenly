import { apiURL } from '../playwright.config'
import { expect, test, uniqueProjectKey } from '../support/fixtures'

test.describe('Personal access tokens (card #62)', () => {
  test('[BE-E2E-030] a personal access token reads its projects through REST and MCP, never changes anything, and stops when revoked', async ({ playwright, provenly, request }) => {
    const mine = uniqueProjectKey()
    const other = uniqueProjectKey()
    await provenly.createProject(mine, 'Tokens')
    await provenly.createProject(other, 'Tokens elsewhere')
    const tc = await provenly.createTestCase({ title: 'read me', project: mine })
    expect((await request.post(`${apiURL}/api/v1/auth/tokens`, { data: { name: 'x', projects: [mine], expiresInDays: 366 } })).status()).toBe(400)
    const created = await request.post(`${apiURL}/api/v1/auth/tokens`, { data: { name: 'scripts', projects: [mine], expiresInDays: 365 } })
    expect(created.status()).toBe(201)
    const { personalAccessToken, token } = await created.json()
    expect(token).toMatch(/^pvly_pat_[0-9a-f]{8}_/)
    expect(personalAccessToken).toMatchObject({ projects: [mine], status: 'active', lastUsedAt: null })

    // A client with no cookies, only the token.
    const script = await playwright.request.newContext({ storageState: { cookies: [], origins: [] }, extraHTTPHeaders: { Authorization: `Bearer ${token}` } })
    const read = await script.get(`${apiURL}/api/v1/test-cases?project=${mine}`)
    expect(read.status()).toBe(200)
    expect((await read.json()).items.map((t: { key: string }) => t.key)).toEqual([tc.key])
    expect((await script.get(`${apiURL}/api/v1/projects/${other}/suites`)).status()).toBe(403)
    expect((await script.get(`${apiURL}/api/v1/users`)).status()).toBe(403)
    const write = await script.post(`${apiURL}/api/v1/test-cases`, { data: { title: 'nope', project: mine } })
    expect(write.status()).toBe(403)
    expect((await write.json()).detail).toContain('read-only')
    const mcp = await script.post(`${apiURL}/api/v1/mcp`, {
      data: { jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name: 'search_test_cases', arguments: { project: mine } } },
    })
    expect(mcp.status()).toBe(200)
    expect((await mcp.json()).result.structuredContent.items.map((t: { key: string }) => t.key)).toEqual([tc.key])

    const listed = (await (await request.get(`${apiURL}/api/v1/auth/tokens`)).json()).items.find((t: { id: number }) => t.id === personalAccessToken.id)
    expect(listed.lastUsedAt).toBeTruthy()
    expect((await request.post(`${apiURL}/api/v1/auth/tokens/${personalAccessToken.id}/revoke`)).status()).toBe(200)
    expect((await script.get(`${apiURL}/api/v1/test-cases?project=${mine}`)).status()).toBe(401)
    await script.dispose()
  })

  test('[FE-E2E-028] a person makes a token on the account page for one project, sees it once with the MCP command, and revokes it', async ({ page, playwright, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Tokens UI')
    await page.goto('/account')
    const card = page.getByTestId('tokens')
    await card.getByLabel('Token name').fill('Claude Desktop')
    await card.getByRole('checkbox', { name: new RegExp(`^${key} `) }).check()
    await card.getByRole('button', { name: 'Create token' }).click()
    const secret = (await card.getByTestId('token-secret').textContent())!
    expect(secret).toMatch(/^pvly_pat_[0-9a-f]{8}_/)
    await expect(card.getByTestId('token-mcp-snippet')).toContainText(`Authorization: Bearer ${secret}`)

    const script = await playwright.request.newContext({ storageState: { cookies: [], origins: [] }, extraHTTPHeaders: { Authorization: `Bearer ${secret}` } })
    expect((await script.get(`${apiURL}/api/v1/projects/${key}/suites`)).status()).toBe(200)
    await page.reload()
    const row = card.getByRole('row', { name: /Claude Desktop/ }).first()
    await expect(row).toContainText(key)
    await expect(row).toContainText('active')
    await expect(row).not.toContainText('Never')
    await row.getByRole('button', { name: 'Revoke…' }).click()
    await row.getByRole('button', { name: 'Revoke', exact: true }).click()
    await expect(row).toContainText('revoked')
    expect((await script.get(`${apiURL}/api/v1/projects/${key}/suites`)).status()).toBe(401)
    await script.dispose()
  })
})
