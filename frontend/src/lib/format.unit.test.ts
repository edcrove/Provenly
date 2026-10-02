import { describe, expect, it } from 'vitest'

import {
  formatDateTime,
  formatDuration,
  formatPercent,
  outcomeBreakdown,
  plural,
  shortCommit,
  sumPercents,
  tcKey,
} from './format'

describe('format', () => {
  it('lists the non-zero counts of a run outcome', () => {
    expect(outcomeBreakdown({ passed: 2, failed: 1, error: 0, skipped: 0, untested: 1 })).toBe(
      '2 passed · 1 failed · 1 untested',
    )
    expect(outcomeBreakdown({ passed: 0, failed: 0, error: 1, skipped: 2, untested: 0 })).toBe(
      '1 error · 2 skipped',
    )
    expect(outcomeBreakdown({ passed: 0, failed: 0, error: 0, skipped: 0, untested: 0 })).toBe(
      'no test cases',
    )
  })

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
    // Rounding never produces "60.0 s" or "1m 60s".
    expect(formatDuration(9_999)).toBe('10.00 s')
    expect(formatDuration(59_940)).toBe('59.9 s')
    expect(formatDuration(59_960)).toBe('1m 0s')
    expect(formatDuration(119_600)).toBe('2m 0s')
  })

  it('pluralizes counts', () => {
    expect(plural(0, 'result')).toBe('0 results')
    expect(plural(1, 'result')).toBe('1 result')
    expect(plural(2, 'test case')).toBe('2 test cases')
    expect(plural(1, 'entry', 'entries')).toBe('1 entry')
    expect(plural(3, 'entry', 'entries')).toBe('3 entries')
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
    expect(formatPercent(33.333333)).toBe('33.33%')
    expect(formatPercent(66.666667)).toBe('66.67%')
    expect(formatPercent(99.999999)).toBe('100%')
    expect(formatPercent(12.5)).toBe('12.50%')
  })

  it('sums precise percentages so rounded totals read 100%', () => {
    const total = sumPercents([33.333333, 33.333333, 33.333333])
    expect(formatPercent(total)).toBe('100%')
    expect(sumPercents([])).toBe(0)
  })

  it('shortens commits', () => {
    expect(shortCommit('0123456789abcdef')).toBe('0123456789')
    expect(shortCommit('abc')).toBe('abc')
    expect(shortCommit('')).toBe('—')
  })
})
