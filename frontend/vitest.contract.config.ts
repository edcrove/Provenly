import { defineConfig } from 'vitest/config'
import { withBase } from './vitest.shared.ts'

// Contract gate: every OpenAPI operation the frontend consumes, through the
// generated client, against MSW handlers validated against the spec.
export default withBase(
  defineConfig({
    test: {
      name: 'contract',
      environment: 'node',
      include: ['src/contract/**/*.test.ts'],
      reporters: ['default', ['json', { outputFile: 'coverage/contract/results.json' }]],
    },
  }),
)
