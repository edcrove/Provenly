import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'

import { describe, expect, it } from 'vitest'

/** Every .tsx file under dir, recursively. */
function tsxFiles(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) =>
    e.isDirectory() ? tsxFiles(join(dir, e.name)) : e.name.endsWith('.tsx') ? [join(dir, e.name)] : [],
  )
}

describe('table cells', () => {
  it('no table cell is a flex or grid container (it stops being a cell and breaks the row)', () => {
    const offenders = tsxFiles(join(__dirname, '..')).flatMap((file) =>
      [...readFileSync(file, 'utf8').matchAll(/<TableCell\b[^>]*className="([^"]*)"/g)]
        .filter((m) => /(^|\s)(flex|grid|inline-flex)(\s|$)/.test(m[1]))
        .map((m) => `${file}: ${m[0]}`),
    )
    expect(offenders).toEqual([])
  })
})
