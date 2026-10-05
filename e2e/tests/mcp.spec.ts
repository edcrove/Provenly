import { adminPassword, adminUsername, apiURL } from '../playwright.config'
import { byProperty, expect, junit, test, uniqueProjectKey, uniqueRunId } from '../support/fixtures'

test.describe('Agents: MCP server (prototype feature 19)', () => {
  test('[BE-E2E-025] an MCP client signs in, initializes, lists the tools and reads test cases, runs and history as the user', async ({ playwright, provenly, request }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'MCP')
    const tc = await provenly.createTestCase({ title: 'pay by card', project: key, automated: true })
    const run = await request.post(`${apiURL}/api/v1/ingestion/junit?project=${key}&provider=github&runId=${uniqueRunId()}&runAttempt=1`, {
      headers: { 'Content-Type': 'application/xml' },
      data: junit(byProperty('pay by card', tc.key, '<failure message="declined"/>')),
    })
    const runId = (await run.json()).testRun.id

    // A fresh client with no cookies: the agent authenticates with a session token only.
    const agent = await playwright.request.newContext({ storageState: { cookies: [], origins: [] } })
    expect((await agent.post(`${apiURL}/api/v1/mcp`, { data: { jsonrpc: '2.0', id: 0, method: 'ping' } })).status()).toBe(401)
    const token = (await (await agent.post(`${apiURL}/api/v1/auth/login`, { data: { username: adminUsername, password: adminPassword } })).json()).token
    let id = 0
    const rpc = async (method: string, params?: object) => {
      const res = await agent.post(`${apiURL}/api/v1/mcp`, {
        headers: { Authorization: `Bearer ${token}` },
        data: { jsonrpc: '2.0', id: ++id, method, ...(params ? { params } : {}) },
      })
      expect(res.status()).toBe(200)
      return (await res.json()).result
    }
    const tool = async (name: string, args: object) => {
      const result = await rpc('tools/call', { name, arguments: args })
      expect(result.isError, JSON.stringify(result)).toBe(false)
      return result.structuredContent
    }

    expect(await rpc('initialize', { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 'e2e', version: '1' } })).toMatchObject({
      protocolVersion: '2025-06-18',
      serverInfo: { name: 'provenly' },
    })
    const names = (await rpc('tools/list')).tools.map((t: { name: string }) => t.name)
    expect(names).toEqual(expect.arrayContaining(['search_test_cases', 'get_test_case_history', 'get_test_run_summary', 'get_project_quality']))

    const found = await tool('search_test_cases', { project: key })
    expect(found.items.map((i: { key: string }) => i.key)).toEqual([tc.key])
    expect((await tool('get_test_case_history', { testCaseId: tc.id })).items[0]).toMatchObject({ result: { status: 'failed' }, run: { id: runId } })
    expect(await tool('get_test_run', { testRunId: runId })).toMatchObject({ outcome: { verdict: 'failed' } })
    expect((await tool('get_project_quality', { projectKey: key })).testCases).toMatchObject({ active: 1, automated: 1 })
    const missing = await rpc('tools/call', { name: 'get_test_case', arguments: { testCaseId: 999999999 } })
    expect(missing.isError).toBe(true)
    await agent.dispose()
  })

  test('[FE-E2E-025] the account page shows the command that connects an MCP client as the signed-in user', async ({ page }) => {
    await page.goto('/account')
    const snippet = page.getByTestId('mcp-snippet')
    await expect(snippet).toContainText('claude mcp add --transport http provenly')
    await expect(snippet).toContainText('/api/v1/mcp')
    await expect(snippet).toContainText(`"username":"${adminUsername}"`)
  })
})
