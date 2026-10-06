import { request as apiRequest } from '@playwright/test'

import { adminPassword, adminUsername, apiURL } from '../playwright.config'
import { expect, test } from '../support/fixtures'

const unique = () => `u${Date.now().toString(36)}${Math.floor(Math.random() * 1000)}`

test.describe('Accounts', () => {
  test('[BE-E2E-008] accounts through the API: 401 without a session, bearer sessions, invitations and admin-only routes', async ({ request }) => {
    const anon = await apiRequest.newContext({ storageState: { cookies: [], origins: [] } })
    expect((await anon.get(`${apiURL}/api/v1/test-cases`)).status()).toBe(401)
    expect((await anon.get(`${apiURL}/healthz`)).status()).toBe(200)
    const bad = await anon.post(`${apiURL}/api/v1/auth/login`, { data: { username: adminUsername, password: 'wrong password' } })
    expect(bad.status()).toBe(401)
    expect((await bad.json()).code).toBe('unauthorized')

    // The admin (signed in through the saved cookie) invites someone; the token is shown once.
    const created = await (await request.post(`${apiURL}/api/v1/invitations`, { data: { note: 'e2e' } })).json()
    expect(created.invitation.status).toBe('pending')
    const username = unique()
    const accept = await anon.post(`${apiURL}/api/v1/invitations/accept`, {
      data: { token: created.token, username, displayName: 'E2E member', password: 'member password' },
    })
    expect(accept.status()).toBe(201)
    const again = await anon.post(`${apiURL}/api/v1/invitations/accept`, {
      data: { token: created.token, username: unique(), displayName: 'x', password: 'member password' },
    })
    expect(again.status()).toBe(404)

    // A bearer token from sign-in opens the API; members cannot manage users.
    const login = await (await anon.post(`${apiURL}/api/v1/auth/login`, { data: { username, password: 'member password' } })).json()
    const member = await apiRequest.newContext({ storageState: { cookies: [], origins: [] }, extraHTTPHeaders: { Authorization: `Bearer ${login.token}` } })
    expect((await (await member.get(`${apiURL}/api/v1/auth/me`)).json()).isAdmin).toBe(false)
    expect((await member.get(`${apiURL}/api/v1/test-cases`)).status()).toBe(200)
    expect((await member.get(`${apiURL}/api/v1/users`)).status()).toBe(403)
    const users = await (await request.get(`${apiURL}/api/v1/users?pageSize=100`)).json()
    expect(users.items.map((u: { username: string }) => u.username)).toEqual(expect.arrayContaining([adminUsername, username]))

    // Changing the password signs the old session out.
    const changed = await member.post(`${apiURL}/api/v1/auth/password`, { data: { currentPassword: 'member password', newPassword: 'another member password' } })
    expect(changed.status()).toBe(200)
    expect((await member.get(`${apiURL}/api/v1/auth/me`)).status()).toBe(401)
    await member.dispose()
    await anon.dispose()
  })

  test('[FE-E2E-011] sign in, invite someone, they join from the link, and sign out', async ({ browser }) => {
    // A fresh browser without a session is sent to sign-in and returns to the page it asked for.
    const adminCtx = await browser.newContext({ storageState: { cookies: [], origins: [] } })
    const admin = await adminCtx.newPage()
    await admin.goto('/test-runs')
    await expect(admin).toHaveURL(/\/login\?next=%2Ftest-runs$/)
    await admin.getByLabel('Username').fill(adminUsername)
    await admin.getByLabel('Password').fill(adminPassword)
    await admin.getByRole('button', { name: 'Sign in' }).click()
    await expect(admin).toHaveURL(/\/test-runs$/)

    await admin.getByRole('link', { name: 'Users' }).click()
    await admin.getByLabel('Note (optional)').fill('FE-E2E-011')
    await admin.getByRole('button', { name: 'Create invitation link' }).click()
    const link = (await admin.getByTestId('invitation-link').textContent())!
    expect(link).toContain('/accept-invite#token=')

    const guestCtx = await browser.newContext({ storageState: { cookies: [], origins: [] } })
    const guest = await guestCtx.newPage()
    await guest.goto(link)
    const username = unique()
    await guest.getByLabel('Username').fill(username)
    await guest.getByLabel('Display name').fill('Guest Tester')
    await guest.getByLabel('Password', { exact: true }).fill('guest password')
    await guest.getByLabel('Repeat password').fill('guest password')
    await guest.getByRole('button', { name: 'Create account' }).click()
    await expect(guest.getByTestId('current-user')).toHaveText('Guest Tester')
    await expect(guest.getByRole('link', { name: 'Users' })).toHaveCount(0)

    await admin.reload()
    await expect(admin.getByTestId(`user-${username}`)).toContainText('Guest Tester')
    await expect(admin.getByRole('table', { name: 'Invitations' })).toContainText('accepted')

    await guest.getByRole('button', { name: 'Sign out' }).click()
    await expect(guest).toHaveURL(/\/login$/)
    await guest.goto('/test-cases')
    await expect(guest).toHaveURL(/\/login\?next=%2Ftest-cases$/)
    await adminCtx.close()
    await guestCtx.close()
  })
})
