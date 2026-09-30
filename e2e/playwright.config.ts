import path from 'node:path'

import { defineConfig } from '@playwright/test'

const root = path.resolve(import.meta.dirname, '..')
const apiPort = process.env.E2E_API_PORT ?? '8081'
const webPort = process.env.E2E_WEB_PORT ?? '4173'
const databaseUrl =
  process.env.PROVENLY_DATABASE_URL ?? 'postgres://provenly:provenly@localhost:5439/provenly_e2e?sslmode=disable'

export const apiURL = `http://localhost:${apiPort}`

/**
 * E2E journeys run serially against the real stack: the coverage-instrumented
 * backend binary (go build -cover, GOCOVERDIR) and the istanbul-instrumented
 * frontend bundle served by vite preview. Test titles carry the journey ids of
 * coverage/inventories/*-e2e.yaml.
 */
export default defineConfig({
  testDir: './tests',
  globalTeardown: './support/remap-coverage.ts',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: !!process.env.CI,
  reporter: [['list'], ['json', { outputFile: 'coverage/results.json' }], ['html', { open: 'never' }]],
  use: { baseURL: `http://localhost:${webPort}`, trace: 'retain-on-failure' },
  webServer: [
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
