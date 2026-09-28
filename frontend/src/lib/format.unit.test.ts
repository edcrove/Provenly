import { describe, expect, it } from 'vitest'

import { formatDateTime, formatDuration, formatPercent, shortCommit, tcKey } from './format'

describe('format', () => {
  it('formats TC keys', () => {
    expect(tcKey(153)).toBe('TC-153')
  })

  it('formats durations', () => {
    expect(formatDuration(null)).toBe('—')
    expect(formatDuration(0)).toBe('<1 ms')
    expect(formatDuration(1)).toBe('1 ms')
    expect(formatDuration(999)).toBe('999 ms')
    expect(formatDuration(1500)).toBe('1.50 s')
    expect(formatDuration(12_340)).toBe('12.3 s')
    expect(formatDuration(125_000)).toBe('2m 5s')
  })

  it('formats timestamps in UTC', () => {
    expect(formatDateTime('2026-09-28T10:11:12.345Z')).toBe('2026-09-28 10:11:12 UTC')
    expect(formatDateTime(null)).toBe('—')
    expect(formatDateTime(undefined)).toBe('—')
    expect(formatDateTime('not a date')).toBe('—')
  })

  it('formats percentages', () => {
    expect(formatPercent(100)).toBe('100%')
    expect(formatPercent(33.33)).toBe('33.33%')
    expect(formatPercent(12.5)).toBe('12.50%')
  })

  it('shortens commits', () => {
    expect(shortCommit('0123456789abcdef')).toBe('0123456789')
    expect(shortCommit('abc')).toBe('abc')
    expect(shortCommit('')).toBe('—')
  })
})
