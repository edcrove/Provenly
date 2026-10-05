import { defineConfig } from 'vitest/config'

// The reporter's only gate: every statement and branch exercised by its unit tests.
export default defineConfig({
  test: {
    include: ['src/**/*.test.ts'],
    coverage: {
      provider: 'v8',
      include: ['src/**/*.ts'],
      exclude: ['src/**/*.test.ts'],
      reporter: ['text', 'json-summary'],
      thresholds: { statements: 100, branches: 100, functions: 100, lines: 100 },
    },
  },
})
