import { request as apiRequest } from '@playwright/test'

import { adminUsername, apiURL } from '../playwright.config'
import { emptyReport, isE2EProject, isE2EUser, sweep } from '../support/cleanup'
import { expect, secret, test, uniqueProjectKey, uniqueUsername } from '../support/fixtures'

test.describe('Clean environment (Ed, 2026-10-09)', () => {
  test('[BE-E2E-032] the sweep removes what the journeys create and nothing else, and running it again finds nothing', async ({ request, provenly }) => {
    // What it recognizes: the conventions and the names earlier runs used, never ordinary accounts or projects.
    expect([isE2EProject('E2E1A2B3C4'), isE2EProject('P1A2B3C4D'), isE2EProject('TC'), isE2EProject('PAYMENTS')]).toEqual([true, true, false, false])
    expect(isE2EUser({ username: 'e2e-1234abcd', displayName: 'Anyone' })).toBe(true)
    expect(isE2EUser({ username: 'um0abc1234', displayName: 'E2E member' })).toBe(true)
    expect(isE2EUser({ username: 'um0abc1234', displayName: 'Ana Pérez' })).toBe(false)
    expect(isE2EUser({ username: 'ana', displayName: 'Viewer' })).toBe(false)

    const key = uniqueProjectKey()
    const base = `${apiURL}/api/v1/projects/${key}`
    await provenly.createProject(key, 'Cleanup')
    const { apiKey } = await (await request.post(`${base}/api-keys`, { data: { name: 'CI' } })).json()
    const webhook = await request.post(`${base}/webhooks`, { data: { url: 'https://example.com/provenly-e2e', events: ['run.completed'] } })
    expect(webhook.status()).toBe(201)
    expect((await request.put(`${base}/github`, { data: { repository: 'acme/shop', token: 'ghp_e2e_cleanup_token' } })).status()).toBe(200)
    const { personalAccessToken: token } = await (await request.post(`${apiURL}/api/v1/auth/tokens`, { data: { name: 'e2e', projects: [key], expiresInDays: 30 } })).json()
    // A token of the administrator over an ordinary project is not the sweep's to revoke.
    const { personalAccessToken: kept } = await (await request.post(`${apiURL}/api/v1/auth/tokens`, { data: { name: 'kept', projects: ['TC'], expiresInDays: 30 } })).json()
    const { invitation: pending } = await (await request.post(`${apiURL}/api/v1/invitations`, { data: { project: key, role: 'viewer' } })).json()
    const accepted = await (await request.post(`${apiURL}/api/v1/invitations`, { data: { note: 'e2e' } })).json()
    const username = uniqueUsername()
    const anon = await apiRequest.newContext({ storageState: { cookies: [], origins: [] } })
    expect((await anon.post(`${apiURL}/api/v1/invitations/accept`, {
      data: { token: accepted.token, username, displayName: 'Cleanup', password: secret('cleanup password') },
    })).status()).toBe(201)

    const first = await sweep(request)
    expect(first.users).toBeGreaterThanOrEqual(1)
    expect(first.apiKeys).toBeGreaterThanOrEqual(1)
    expect(first.webhooks).toBeGreaterThanOrEqual(1)
    expect(first.github).toBeGreaterThanOrEqual(1)
    expect(first.tokens).toBeGreaterThanOrEqual(1)
    expect(first.invitations).toBeGreaterThanOrEqual(1)

    const users = (await (await request.get(`${apiURL}/api/v1/users?pageSize=100`)).json()).items as { username: string; deactivatedAt: string | null }[]
    expect(users.find((u) => u.username === username)!.deactivatedAt).not.toBeNull()
    expect(users.find((u) => u.username === adminUsername)!.deactivatedAt).toBeNull()
    expect((await anon.post(`${apiURL}/api/v1/auth/login`, { data: { username, password: secret('cleanup password') } })).status()).toBe(401)
    await anon.dispose()
    const keys = (await (await request.get(`${base}/api-keys`)).json()).items as { id: number; status: string }[]
    expect(keys.find((k) => k.id === apiKey.id)!.status).toBe('revoked')
    expect((await (await request.get(`${base}/webhooks`)).json()).items.map((w: { active: boolean }) => w.active)).toEqual([false])
    expect((await request.get(`${base}/github`)).status()).toBe(204)
    const tokens = (await (await request.get(`${apiURL}/api/v1/auth/tokens?pageSize=100`)).json()).items as { id: number; status: string }[]
    expect(tokens.find((t) => t.id === token.id)!.status).toBe('revoked')
    expect(tokens.find((t) => t.id === kept.id)!.status).toBe('active')
    const invitations = (await (await request.get(`${apiURL}/api/v1/invitations?pageSize=100`)).json()).items as { id: number; status: string }[]
    expect(invitations.find((i) => i.id === pending.id)!.status).toBe('revoked')

    // Idempotent: a second sweep has nothing left to do.
    expect(await sweep(request)).toEqual(emptyReport())
    expect((await request.post(`${apiURL}/api/v1/auth/tokens/${kept.id}/revoke`)).status()).toBe(200)
  })
})
