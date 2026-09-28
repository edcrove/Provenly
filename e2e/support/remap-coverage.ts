import { mkdirSync, readdirSync, readFileSync, writeFileSync } from 'node:fs'
import path from 'node:path'

import libCoverage from 'istanbul-lib-coverage'
import libSourceMaps from 'istanbul-lib-source-maps'

/**
 * Playwright global teardown: merges the raw istanbul coverage collected from
 * the instrumented bundle and remaps it through the embedded source maps to the
 * original .ts/.tsx lines, so it can be merged with the Vitest (v8) coverage.
 */
export default async function remapCoverage() {
  const rawDir = path.join(import.meta.dirname, '../coverage/frontend')
  const outDir = path.join(import.meta.dirname, '../coverage/frontend-remapped')
  let files: string[] = []
  try {
    files = readdirSync(rawDir).filter((f) => f.endsWith('.json'))
  } catch {
    return
  }
  const map = libCoverage.createCoverageMap({})
  for (const f of files) map.merge(JSON.parse(readFileSync(path.join(rawDir, f), 'utf8')))
  const remapped = await libSourceMaps.createSourceMapStore().transformCoverage(map)
  mkdirSync(outDir, { recursive: true })
  writeFileSync(path.join(outDir, 'coverage-final.json'), JSON.stringify(remapped.toJSON()))
}
