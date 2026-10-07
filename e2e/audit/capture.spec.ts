import { randomBytes } from 'node:crypto'
import { mkdirSync, writeFileSync } from 'node:fs'
import path from 'node:path'

import { test, type Browser, type BrowserContext, type Page } from '@playwright/test'

const base = process.env.E2E_REMOTE_URL!.replace(/\/+$/, '')
const out = path.join(import.meta.dirname, '../../audit-out')
const shots = path.join(out, 'shots')
mkdirSync(shots, { recursive: true })

interface Visit { role: string; name: string; url: string; status?: number; ms: number; consoleErrors: string[]; failed: string[]; text: string; error?: string }
const visits: Visit[] = []
const apiTimings: { what: string; ms: number; status: number }[] = []

async function signedIn(browser: Browser, username: string, password: string, viewport = { width: 1366, height: 900 }) {
  const ctx = await browser.newContext({ viewport })
  const t = Date.now()
  const res = await ctx.request.post(`${base}/api/v1/auth/login`, { data: { username, password } })
  apiTimings.push({ what: `login ${username}`, ms: Date.now() - t, status: res.status() })
  if (res.status() !== 200) throw new Error(`login ${username}: ${res.status()}`)
  return ctx
}

async function visit(ctx: BrowserContext, role: string, name: string, url: string, after?: (p: Page) => Promise<void>) {
  const page = await ctx.newPage()
  const v: Visit = { role, name, url, ms: 0, consoleErrors: [], failed: [], text: '' }
  page.on('console', (m) => m.type() === 'error' && v.consoleErrors.push(m.text().slice(0, 300)))
  page.on('response', (r) => r.status() >= 400 && v.failed.push(`${r.status()} ${r.request().method()} ${r.url().replace(base, '')}`))
  const t = Date.now()
  try {
    const r = await page.goto(url, { waitUntil: 'networkidle', timeout: 90_000 })
    v.status = r?.status()
    if (after) await after(page)
    await page.waitForLoadState('networkidle').catch(() => {})
  } catch (e) {
    v.error = String(e).slice(0, 400)
  }
  v.ms = Date.now() - t
  v.text = (await page.locator('body').innerText().catch(() => '')).slice(0, 4000)
  await page.screenshot({ path: path.join(shots, `${role}--${name}.png`), fullPage: true }).catch(() => {})
  visits.push(v)
  await page.close()
}

const pick = (p: Page) => p.getByLabel('Current project').selectOption('TC').catch(() => {})

test('capture the deployed instance as each kind of user', async ({ browser }) => {
  const admin = await signedIn(browser, process.env.E2E_ADMIN_USERNAME ?? 'admin', process.env.E2E_ADMIN_PASSWORD!)
  const api = admin.request
  const get = async (p: string) => {
    const t = Date.now()
    const r = await api.get(`${base}${p}`)
    apiTimings.push({ what: `GET ${p}`, ms: Date.now() - t, status: r.status() })
    return r.json()
  }
  const cases = (await get('/api/v1/test-cases?project=TC&pageSize=100')).items ?? []
  const runs = (await get('/api/v1/test-runs?project=TC&pageSize=20')).items ?? []
  await get('/api/v1/projects?pageSize=100')
  const requirements = (await get('/api/v1/projects/TC/requirements')).items ?? []
  const issues = (await get('/api/v1/projects/TC/issues')).items ?? []

  // One account per project role in the demo project (TC), through invitations like real people.
  const accounts: Record<string, { username: string; password: string }> = {}
  for (const role of ['maintainer', 'member', 'viewer']) {
    const inv = await (await api.post(`${base}/api/v1/invitations`, { data: { project: 'TC', role } })).json()
    const username = `audit-${role}-${randomBytes(3).toString('hex')}`
    const password = randomBytes(18).toString('base64url')
    const acc = await browser.newContext()
    const r = await acc.request.post(`${base}/api/v1/invitations/accept`, { data: { token: inv.token, username, displayName: `Audit ${role}`, password } })
    if (r.status() >= 300) throw new Error(`accept ${role}: ${r.status()} ${await r.text()}`)
    await acc.close()
    accounts[role] = { username, password }
  }

  const anon = await browser.newContext({ viewport: { width: 1366, height: 900 } })
  await visit(anon, 'anonymous', 'login', '/login')
  await visit(anon, 'anonymous', 'deep-link', '/test-runs')

  const tc = cases.find((c: { automated: boolean }) => c.automated) ?? cases[0]
  const pages: [string, string, ((p: Page) => Promise<void>)?][] = [
    ['test-cases', '/test-cases', pick],
    ...(tc ? [['test-case-detail', `/test-cases/${tc.id}`] as [string, string]] : []),
    ['new-test-case', '/test-cases/new'],
    ['test-runs', '/test-runs', pick],
    ...runs.slice(0, 3).map((r: { id: number }, i: number) => [`run-detail-${i + 1}`, `/test-runs/${r.id}`] as [string, string]),
    ['manual-run', '/test-runs/manual'],
    ['dashboard', '/dashboard', pick],
    ['suites', '/suites', pick],
    ['requirements', '/requirements', pick],
    ...(requirements[0] ? [['requirement-detail', `/requirements/TC/${requirements[0].id}`] as [string, string]] : []),
    ['issues', '/issues', pick],
    ...(issues[0] ? [['issue-detail', `/issues/TC/${issues[0].id}`] as [string, string]] : []),
    ['projects', '/projects'],
    ['project-TC', '/projects/TC'],
    ['users', '/users'],
    ['audit', '/audit'],
    ['account', '/account'],
    ['not-found', '/nope'],
  ]
  for (const [name, url, after] of pages) await visit(admin, 'admin', name, url, after)

  const phone = await signedIn(browser, process.env.E2E_ADMIN_USERNAME ?? 'admin', process.env.E2E_ADMIN_PASSWORD!, { width: 390, height: 844 })
  for (const [name, url, after] of pages.filter(([n]) => ['test-cases', 'run-detail-1', 'dashboard', 'manual-run', 'test-case-detail'].includes(n)))
    await visit(phone, 'admin-phone', name, url, after)

  for (const role of ['maintainer', 'member', 'viewer']) {
    const ctx = await signedIn(browser, accounts[role].username, accounts[role].password)
    for (const [name, url, after] of pages.filter(([n]) => !['not-found'].includes(n))) await visit(ctx, role, name, url, after)
    await ctx.close()
  }

  writeFileSync(path.join(out, 'report.json'), JSON.stringify({ base, at: new Date().toISOString(), counts: { cases: cases.length, runs: runs.length, requirements: requirements.length, issues: issues.length }, accounts: Object.fromEntries(Object.entries(accounts).map(([r, a]) => [r, a.username])), apiTimings, visits }, null, 2))
})
