import { describe, expect, it } from 'vitest'

import type { Suite } from '@/api/client'

import { selectionLabel } from './suites'

const base: Suite = {
  key: 's',
  name: 'S',
  description: '',
  kind: 'static',
  query: null,
  archivedAt: null,
  createdAt: '2026-10-05T10:00:00Z',
  updatedAt: '2026-10-05T10:00:00Z',
  caseCount: 1,
}

describe('selectionLabel', () => {
  it('describes static and query suites', () => {
    expect(selectionLabel(base)).toBe('1 listed test case')
    expect(selectionLabel({ ...base, caseCount: 3 })).toBe('3 listed test cases')
    expect(
      selectionLabel({ ...base, kind: 'query', query: { tag: 'smoke', classification: ['risk:critical'] } }),
    ).toBe('#smoke and risk:critical')
    expect(
      selectionLabel({ ...base, kind: 'query', query: { tag: null, classification: ['risk:high'] } }),
    ).toBe('risk:high')
    expect(selectionLabel({ ...base, kind: 'query', query: null })).toBe('')
  })
})
