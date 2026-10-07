import path from 'node:path'

import { defineConfig } from '@playwright/test'

const root = path.resolve(import.meta.dirname, '..')
const apiPort = process.env.E2E_API_PORT ?? '8081'
const webPort = process.env.E2E_WEB_PORT ?? '4173'
/** The GitHub API double the integrations journey serves (the backend's PROVENLY_GITHUB_API_URL). */
export const githubPort = Number(process.env.E2E_GITHUB_PORT ?? '8098')
const databaseUrl =
  process.env.PROVENLY_DATABASE_URL ?? 'postgres://provenly:provenly@localhost:5439/provenly_e2e?sslmode=disable'

/** E2E_REMOTE_API / E2E_REMOTE_WEB: run against a deployed instance instead of the local stack. */
const remoteApi = process.env.E2E_REMOTE_API
const remoteWeb = process.env.E2E_REMOTE_WEB
export const apiURL = remoteApi ?? `http://localhost:${apiPort}`
export const adminUsername = 'admin'
export const adminPassword = process.env.E2E_ADMIN_PASSWORD ?? 'e2e admin password'
/** Browser state signed in as the administrator, written by the global setup. */
export const adminState = path.join(import.meta.dirname, '.auth/admin.json')

/**
 * E2E journeys run serially against the real stack: the coverage-instrumented
 * backend binary (go build -cover, GOCOVERDIR) and the istanbul-instrumented
 * frontend bundle served by vite preview. Test titles carry the journey ids of
 * coverage/inventories/*-e2e.yaml.
 */
export default defineConfig({
  testDir: './tests',
  globalSetup: './support/sign-in.ts',
  globalTeardown: remoteApi ? undefined : './support/remap-coverage.ts',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: !!process.env.CI,
  reporter: [
    ['list'],
    ['json', { outputFile: 'coverage/results.json' }],
    ['html', { open: 'never' }],
    // Dogfooding (prototype feature 16): with PROVENLY_URL and PROVENLY_API_KEY set, these journeys report themselves
    // to a Provenly instance; without them the reporter does nothing.
    ['../reporters/playwright/src/index.ts'],
  ],
  use: {
    baseURL: remoteWeb ?? `http://localhost:${webPort}`,
    // Every journey (browser and API request fixture) starts signed in as the administrator.
    storageState: adminState,
    trace: 'retain-on-failure',
    // Optional: run on an already installed Chromium instead of Playwright's own build.
    launchOptions: { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE || undefined },
  },
  timeout: remoteApi ? 120_000 : undefined,
  expect: remoteApi ? { timeout: 20_000 } : undefined,
  webServer: remoteApi ? undefined : [
    {
      command: `${path.join(root, 'backend/bin/provenly-cover')} serve`,
      url: `${apiURL}/healthz`,
      reuseExistingServer: false,
      gracefulShutdown: { signal: 'SIGTERM', timeout: 10_000 },
      env: {
        PROVENLY_DATABASE_URL: databaseUrl,
        PROVENLY_HTTP_ADDR: `:${apiPort}`,
        PROVENLY_AUTO_MIGRATE: 'true',
        PROVENLY_LOG_LEVEL: 'warn',
        // The administrator every journey signs in as (created on the empty E2E database).
        PROVENLY_ADMIN_USERNAME: adminUsername,
        PROVENLY_ADMIN_PASSWORD: adminPassword,
        PROVENLY_JWT_SECRET: 'e2e-only-session-signing-secret-0123456789',
        // Webhooks and the GitHub connector (prototype feature 18): a fixed key, local endpoints, GitHub double.
        PROVENLY_SECRETS_KEY: Buffer.alloc(32, 7).toString('base64'),
        PROVENLY_WEBHOOKS_ALLOW_PRIVATE: 'true',
        PROVENLY_GITHUB_API_URL: `http://127.0.0.1:${githubPort}`,
        GOCOVERDIR: path.join(import.meta.dirname, 'coverage/backend'),
      },
    },
    {
      command: `npm --prefix ${path.join(root, 'frontend')} run preview -- --port ${webPort} --strictPort`,
      url: `http://localhost:${webPort}`,
      reuseExistingServer: false,
      env: { PROVENLY_API_URL: apiURL },
    },
  ],
})
