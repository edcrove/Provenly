import { defineConfig } from 'vitest/config'
import { nonSourceFiles, sourceFiles, withBase } from './vitest.shared.ts'

// Unit gate: logic, hooks and utils. The denominator is every source file;
// files that belong to other layers are listed in coverage/exceptions.yaml.
export default withBase(
  defineConfig({
    test: {
      // Dates show in the viewer's time zone: tests read them in UTC wherever they run.
      env: { TZ: 'UTC' },
      name: 'unit',
      environment: 'node',
      include: ['src/**/*.unit.test.ts'],
      coverage: {
        provider: 'v8',
        include: sourceFiles,
        exclude: nonSourceFiles,
        reporter: ['text-summary', 'json', 'json-summary'],
        reportsDirectory: 'coverage/unit',
      },
      reporters: ['default', ['json', { outputFile: 'coverage/unit/results.json' }]],
    },
  }),
)
