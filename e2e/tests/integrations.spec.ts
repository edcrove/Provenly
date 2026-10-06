import { createHmac } from 'node:crypto'
import { createServer, type IncomingMessage, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'

import { apiURL, githubPort } from '../playwright.config'
import { byProperty, expect, junit, test, uniqueProjectKey, uniqueRunId } from '../support/fixtures'

interface Received {
  headers: IncomingMessage['headers']
  body: string
}

/** A local HTTP server answering every request with handle; it records what it received. */
async function serve(port: number, handle: (path: string) => { status: number; body?: string }) {
  const received: Received[] = []
  const server: Server = createServer((req, res) => {
    let body = ''
    req.on('data', (chunk) => (body += chunk))
    req.on('end', () => {
      received.push({ headers: req.headers, body })
      const out = handle(req.url ?? '')
      res.writeHead(out.status, { 'Content-Type': 'application/json' })
      res.end(out.body ?? '')
    })
  })
  await new Promise<void>((resolve) => server.listen(port, '127.0.0.1', resolve))
  return { received, url: `http://127.0.0.1:${(server.address() as AddressInfo).port}`, close: () => new Promise((r) => server.close(r)) }
}

const sign = (secret: string, ts: string, body: string) => `sha256=${createHmac('sha256', secret).update(`${ts}.${body}`).digest('hex')}`

const githubIssues = JSON.stringify([
  { number: 21, title: 'Checkout loses the cart', body: 'steps', html_url: 'https://github.com/acme/shop/issues/21', state: 'open' },
  { number: 22, title: 'Bump deps', html_url: 'https://github.com/acme/shop/pull/22', state: 'open', pull_request: {} },
  { number: 7, title: 'Old crash', html_url: 'https://github.com/acme/shop/issues/7', state: 'closed', state_reason: 'completed' },
])

test.describe('Integrations: webhooks and GitHub Issues (prototype feature 18)', () => {
  test('[BE-E2E-024] a completed run is delivered to a webhook, signed; GitHub issues are mirrored through the API', async ({ request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Integrations API')
    const tc = await provenly.createTestCase({ title: 'checkout', project: key, automated: true })
    const receiver = await serve(0, () => ({ status: 204 }))
    const github = await serve(githubPort, (path) =>
      path.startsWith('/repos/acme/shop/issues') && path.includes('page=1&') ? { status: 200, body: githubIssues } : { status: 200, body: '[]' },
    )
    try {
      const base = `${apiURL}/api/v1/projects/${key}`
      const created = await request.post(`${base}/webhooks`, { data: { url: `${receiver.url}/hook`, events: ['run.completed'] } })
      expect(created.status()).toBe(201)
      const { webhook, secret } = await created.json()
      expect(secret).toMatch(/^whsec_/)
      expect(JSON.stringify(await (await request.get(`${base}/webhooks`)).json())).not.toContain(secret)

      const run = await request.post(`${apiURL}/api/v1/ingestion/junit?project=${key}&provider=github&runId=${uniqueRunId()}&runAttempt=1`, {
        headers: { 'Content-Type': 'application/xml' },
        data: junit(byProperty('checkout', tc.key, '<failure message="boom"/>')),
      })
      const runId = (await run.json()).testRun.id
      // The delivery worker sends it within a few seconds.
      await expect.poll(() => receiver.received.length, { timeout: 15_000 }).toBe(1)
      const got = receiver.received[0]
      expect(got.headers['x-provenly-event']).toBe('run.completed')
      expect(got.headers['x-provenly-signature']).toBe(sign(secret, String(got.headers['x-provenly-timestamp']), got.body))
      expect(JSON.parse(got.body)).toMatchObject({ event: 'run.completed', project: { key }, run: { id: runId, outcome: { verdict: 'failed' } } })
      await expect
        .poll(async () => (await (await request.get(`${base}/webhooks/${webhook.id}/deliveries`)).json()).items[0].status)
        .toBe('succeeded')

      expect((await request.post(`${base}/webhooks`, { data: { url: 'ftp://x', events: ['run.completed'] } })).status()).toBe(400)
      expect((await request.get(`${base}/github`)).status()).toBe(404)
      const connected = await request.put(`${base}/github`, { data: { repository: 'acme/shop', token: 'ghp_e2e_token_4321' } })
      expect(await connected.json()).toMatchObject({ repository: 'acme/shop', tokenHint: '…4321', lastSyncedAt: null })
      expect(await (await request.post(`${base}/github/sync`)).json()).toEqual({ created: 2, updated: 0 })
      expect(github.received[0].headers.authorization).toBe('Bearer ghp_e2e_token_4321')
      const issues = (await (await request.get(`${base}/issues`)).json()).items
      expect(issues.map((i: { externalId: string; state: string }) => `${i.externalId}:${i.state}`).sort()).toEqual(['21:open', '7:closed'])
      expect((await request.put(`${base}/github`, { data: { repository: 'acme/nope' } })).status()).toBe(400)
      expect((await request.put(`${base}/github`, { data: { repository: 'acme/nope', token: 'ghp_e2e_token_4321' } })).status()).toBe(200)
      const failed = await request.post(`${base}/github/sync`)
      expect(failed.status()).toBe(200) // the double answers [] for unknown repositories
      expect((await request.delete(`${base}/github`)).status()).toBe(204)
    } finally {
      await receiver.close()
      await github.close()
    }
  })

  test('[FE-E2E-024] a maintainer adds a webhook from the project page, pings it and sees it delivered; connects GitHub and syncs issues', async ({ page, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Integrations UI')
    const receiver = await serve(0, () => ({ status: 200 }))
    const github = await serve(githubPort, (path) => (path.includes('page=1&') ? { status: 200, body: githubIssues } : { status: 200, body: '[]' }))
    try {
      await page.goto(`/projects/${key}`)
      await page.getByLabel('Endpoint URL').fill(`${receiver.url}/hook`)
      await page.getByRole('button', { name: 'Add webhook' }).click()
      await expect(page.getByTestId('webhook-secret')).toHaveText(/^whsec_/)
      const row = page.locator('[data-testid^="webhook-"]').filter({ hasText: receiver.url })
      await row.getByRole('button', { name: 'Send ping' }).click()
      await expect(row).toContainText(/ping: (pending|succeeded)/)
      await expect.poll(() => receiver.received.length, { timeout: 15_000 }).toBe(1)
      await page.reload()
      await expect(row).toContainText('ping: succeeded')
      await row.getByRole('button', { name: 'Deliveries' }).click()
      await expect(page.locator('[data-testid^="deliveries-"]')).toContainText('200')

      await page.getByLabel('Repository').fill('acme/shop')
      await page.getByLabel('Token').fill('ghp_ui_token_8765')
      await page.getByRole('button', { name: 'Connect' }).click()
      await expect(page.getByTestId('github-connection')).toContainText('acme/shop with token …8765')
      await page.getByRole('button', { name: 'Sync issues now' }).click()
      await expect(page.getByText('Synced: 2 created, 0 updated.')).toBeVisible()
      await page.goto('/issues')
      await page.getByLabel('Current project').selectOption(key)
      await expect(page.getByText('Checkout loses the cart')).toBeVisible()
    } finally {
      await receiver.close()
      await github.close()
    }
  })
})
