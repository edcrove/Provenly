import { request, type FullConfig } from '@playwright/test'

import { adminPassword, adminState, adminUsername, apiURL } from '../playwright.config'
import { sweepAsAdmin } from './cleanup'

/**
 * Signs in once as the administrator and saves the session cookie for every journey, then sweeps what an earlier run
 * may have left (a run that crashed before its teardown), so a run never depends on the previous one.
 */
export default async function signIn(_config: FullConfig) {
  const ctx = await request.newContext()
  const res = await ctx.post(`${apiURL}/api/v1/auth/login`, { data: { username: adminUsername, password: adminPassword } })
  if (res.status() !== 200) throw new Error(`E2E sign-in failed: ${res.status()} ${await res.text()}`)
  await ctx.storageState({ path: adminState })
  await ctx.dispose()
  await sweepAsAdmin('before the run')
}
