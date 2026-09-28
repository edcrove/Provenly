import path from 'node:path'

import { defineConfig } from '@playwright/test'

const root = path.resolve(import.meta.dirname, '..')
const apiPort = process.env.E2E_API_PORT ?? '8082'
const webPort = process.env.E2E_WEB_PORT ?? '4174'
const databaseUrl =
  process.env.PROVENLY_DATABASE_URL ?? 'postgres://provenly:provenly@localhost:5432/provenly_e2e?sslmode=disable'

/**
 * Captures screenshots of every relevant UI flow into docs/screenshots for
 * visual inspection (`make screenshots`). Not a test gate: it runs against the
 * plain (non-instrumented) backend binary and frontend bundle.
 */
export default defineConfig({
  testDir: './screenshots',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: [['list']],
  use: { baseURL: `http://localhost:${webPort}`, viewport: { width: 1440, height: 900 }, colorScheme: 'light' },
  webServer: [
    {
      command: `${path.join(root, 'backend/bin/provenly')} serve`,
      url: `http://localhost:${apiPort}/healthz`,
      reuseExistingServer: false,
      env: {
        PROVENLY_DATABASE_URL: databaseUrl,
        PROVENLY_HTTP_ADDR: `:${apiPort}`,
        PROVENLY_AUTO_MIGRATE: 'true',
        PROVENLY_LOG_LEVEL: 'warn',
      },
    },
    {
      command: `npm --prefix ${path.join(root, 'frontend')} run preview -- --port ${webPort} --strictPort`,
      url: `http://localhost:${webPort}`,
      reuseExistingServer: false,
      env: { PROVENLY_API_URL: `http://localhost:${apiPort}` },
    },
  ],
})
