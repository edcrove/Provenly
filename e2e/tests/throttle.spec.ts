import { randomUUID } from 'node:crypto'

import { apiURL } from '../playwright.config'
import { expect, test } from '../support/fixtures'

test.describe('Sign-in throttle (prototype feature 21)', () => {
  test('[BE-E2E-027] five failed sign-ins lock a username for a while', async ({ playwright }) => {
    // A made-up username: the administrator the other journeys use must never be locked.
    const username = `locked-${randomUUID().slice(0, 8)}`
    const anon = await playwright.request.newContext({ storageState: { cookies: [], origins: [] } })
    const login = (name: string) => anon.post(`${apiURL}/api/v1/auth/login`, { data: { username: name, password: 'wrong password' } })
    for (let i = 0; i < 5; i++) expect((await login(username)).status()).toBe(401)
    const locked = await login(username.toUpperCase())
    expect(locked.status()).toBe(429)
    expect(await locked.json()).toMatchObject({ code: 'too_many_requests', detail: expect.stringContaining('try again in') })
    expect((await login(`other-${randomUUID().slice(0, 8)}`)).status()).toBe(401)
    await anon.dispose()
  })
})
