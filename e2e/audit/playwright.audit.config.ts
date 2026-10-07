import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: '.',
  testMatch: 'capture.spec.ts',
  timeout: 30 * 60_000,
  workers: 1,
  reporter: [['list']],
  use: {
    baseURL: process.env.E2E_REMOTE_URL,
    launchOptions: { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE || undefined },
  },
})
