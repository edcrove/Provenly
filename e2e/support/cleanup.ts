import { request as playwrightRequest, type APIRequestContext } from '@playwright/test'

import { adminState, adminUsername, apiURL } from '../playwright.config'

/**
 * What the journeys create follows conventions, so a sweep can find it on any instance (local or deployed) without a
 * record of the run that made it: usernames start with `e2e-`, project keys with `E2E`. The sweep also recognizes what
 * runs made before these conventions (project keys `P` + 8 hex digits; usernames like `um0abc123` with one of the
 * display names those journeys used), so a deployment keeps no account of theirs either.
 */
export const E2E_USER_PREFIX = 'e2e-'
export const E2E_PROJECT_PREFIX = 'E2E'
const legacyProject = /^P[0-9A-F]{8}$/
const legacyUser = /^[ur]m[0-9a-z]{7}\d{0,3}$/
const legacyDisplayNames = new Set(['E2E member', 'UI Member', 'Viewer', 'x', 'Guest Tester'])

export const isE2EProject = (key: string) => key.startsWith(E2E_PROJECT_PREFIX) || legacyProject.test(key)
export const isE2EUser = (u: { username: string; displayName: string }) =>
  u.username.startsWith(E2E_USER_PREFIX) || (legacyUser.test(u.username) && legacyDisplayNames.has(u.displayName))

/** What a sweep did, by kind; a second sweep right after finds nothing. */
export interface CleanupReport {
  users: number
  invitations: number
  tokens: number
  apiKeys: number
  webhooks: number
  github: number
}

export const emptyReport = (): CleanupReport => ({ users: 0, invitations: 0, tokens: 0, apiKeys: 0, webhooks: 0, github: 0 })

async function ok(res: Promise<import('@playwright/test').APIResponse>, what: string, accepted = [200, 204]) {
  const r = await res
  if (!accepted.includes(r.status())) throw new Error(`E2E cleanup: ${what} answered ${r.status()} ${await r.text()}`)
  return r
}

/** Every item of a list, whether the API answers a bare array or the page envelope. */
async function all<T>(api: APIRequestContext, path: string): Promise<T[]> {
  const items: T[] = []
  for (let page = 1; ; page++) {
    const sep = path.includes('?') ? '&' : '?'
    const res = await ok(api.get(`${apiURL}${path}${sep}page=${page}&pageSize=100`), `GET ${path}`, [200])
    const body = (await res.json()) as T[] | { items: T[]; totalPages: number }
    if (Array.isArray(body)) return body
    items.push(...body.items)
    if (page >= body.totalPages) return items
  }
}

/** Runs fn over items, at most `width` at a time. */
async function eachInParallel<T>(items: T[], width: number, fn: (item: T) => Promise<void>) {
  let next = 0
  const worker = async () => {
    while (next < items.length) await fn(items[next++])
  }
  await Promise.all(Array.from({ length: Math.min(width, items.length) }, worker))
}

/**
 * Deactivates the journeys' accounts (which also revokes their personal access tokens), revokes their pending
 * invitations, the administrator's tokens that only reach E2E projects and the API keys of E2E projects, pauses those
 * projects' webhooks and removes their GitHub connections. Projects, runs, results and audit events stay: Provenly
 * never deletes them (P1-1, append-only results). Safe to run any number of times.
 */
export async function sweep(api: APIRequestContext): Promise<CleanupReport> {
  const report = emptyReport()
  const projects = await all<{ id: number; key: string }>(api, '/api/v1/projects')
  const e2eProjects = projects.filter((p) => isE2EProject(p.key))
  const e2eProjectIds = new Set(e2eProjects.map((p) => p.id))
  const e2eProjectKeys = new Set(e2eProjects.map((p) => p.key))

  const users = await all<{ username: string; displayName: string; deactivatedAt: string | null }>(api, '/api/v1/users')
  for (const u of users) {
    if (u.deactivatedAt === null && u.username !== adminUsername && isE2EUser(u)) {
      await ok(api.post(`${apiURL}/api/v1/users/${u.username}/deactivate`), `deactivate ${u.username}`)
      report.users++
    }
  }

  const invitations = await all<{ id: number; note: string; status: string; projectId: number | null }>(api, '/api/v1/invitations')
  for (const inv of invitations) {
    const e2e = /^e2e/i.test(inv.note) || (inv.projectId !== null && e2eProjectIds.has(inv.projectId))
    if (inv.status === 'pending' && e2e) {
      await ok(api.post(`${apiURL}/api/v1/invitations/${inv.id}/revoke`), `revoke invitation ${inv.id}`)
      report.invitations++
    }
  }

  const tokens = await all<{ id: number; status: string; projects: string[] }>(api, '/api/v1/auth/tokens')
  for (const t of tokens) {
    if (t.status === 'active' && t.projects.length > 0 && t.projects.every((k) => e2eProjectKeys.has(k))) {
      await ok(api.post(`${apiURL}/api/v1/auth/tokens/${t.id}/revoke`), `revoke token ${t.id}`)
      report.tokens++
    }
  }

  // A deployed instance keeps every project an earlier run made: check them a few at a time.
  await eachInParallel(e2eProjects, 8, async ({ key }) => {
    const base = `/api/v1/projects/${key}`
    for (const k of await all<{ id: number; status: string }>(api, `${base}/api-keys`)) {
      if (k.status === 'active') {
        await ok(api.post(`${apiURL}${base}/api-keys/${k.id}/revoke`), `revoke API key ${k.id} of ${key}`)
        report.apiKeys++
      }
    }
    // Webhooks cannot be deleted: pausing one stops every delivery.
    for (const w of await all<{ id: number; active: boolean }>(api, `${base}/webhooks`)) {
      if (w.active) {
        await ok(api.patch(`${apiURL}${base}/webhooks/${w.id}`, { data: { active: false } }), `pause webhook ${w.id} of ${key}`)
        report.webhooks++
      }
    }
    const github = await api.get(`${apiURL}${base}/github`)
    if (github.status() === 200) {
      await ok(api.delete(`${apiURL}${base}/github`), `disconnect GitHub from ${key}`)
      report.github++
    }
  })
  return report
}

/** Playwright global setup/teardown step: sweeps as the signed-in administrator and says what it did. */
export async function sweepAsAdmin(when: string) {
  const api = await playwrightRequest.newContext({ storageState: adminState })
  try {
    const r = await sweep(api)
    console.log(
      `E2E cleanup (${when}): ${r.users} accounts deactivated, ${r.invitations} invitations, ${r.tokens} tokens and ` +
        `${r.apiKeys} API keys revoked, ${r.webhooks} webhooks paused, ${r.github} GitHub connections removed`,
    )
  } finally {
    await api.dispose()
  }
}
