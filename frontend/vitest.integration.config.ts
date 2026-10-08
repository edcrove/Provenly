import { defineConfig } from 'vitest/config'
import { nonSourceFiles, sourceFiles, withBase } from './vitest.shared.ts'

// Integration gate: components rendered against the API mocked with MSW.
// Code coverage here is evidence; the gate is the inventory in
// coverage/inventories/frontend-integration.yaml.
export default withBase(
  defineConfig({
    test: {
      // Dates show in the viewer's time zone: tests read them in UTC wherever they run.
      env: { TZ: 'UTC' },
      name: 'integration',
      environment: 'jsdom',
      setupFiles: ['src/test/setup.ts'],
      include: ['src/**/*.int.test.tsx'],
      coverage: {
        provider: 'v8',
        include: sourceFiles,
        exclude: [...nonSourceFiles, 'src/main.tsx'],
        reporter: ['text-summary', 'json', 'json-summary'],
        reportsDirectory: 'coverage/integration',
      },
      reporters: ['default', ['json', { outputFile: 'coverage/integration/results.json' }]],
    },
  }),
)
