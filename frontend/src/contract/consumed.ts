import { readFileSync } from 'node:fs'
import path from 'node:path'

/**
 * Operations consumed by the frontend, derived from the generated-client calls
 * in src/api (so adding a call automatically adds contract targets).
 */
export function consumedOperations(): string[] {
  const dir = path.resolve(import.meta.dirname, '../api')
  const source = readFileSync(path.join(dir, 'queries.ts'), 'utf8')
  const found = new Set<string>()
  for (const m of source.matchAll(/api\.(GET|POST|PUT|PATCH|DELETE)\(\s*'([^']+)'/g))
    found.add(`${m[1]} ${m[2]}`)
  return [...found].sort()
}
