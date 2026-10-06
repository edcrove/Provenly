import { describe, expect, it } from 'vitest'

import {
  correlationExplanation,
  correlations,
  correlationVariant,
  diagnosticCount,
  invalidCorrelations,
  executionVariant,
  isInterruptedRun,
  pickEnum,
  positiveInt,
  resultStatuses,
  statusLabel,
  statusVariant,
  summaryStatuses,
  verdictLabel,
  verdictVariant,
} from './status'

describe('status helpers', () => {
  it('lists the contract enums', () => {
    expect(resultStatuses).toEqual(['passed', 'failed', 'error', 'skipped'])
    expect(summaryStatuses).toEqual(['untested', 'passed', 'failed', 'error', 'skipped'])
    expect(correlations).toEqual(['valid', 'missing', 'malformed', 'unknown', 'deprecated', 'wrong_project'])
    expect(invalidCorrelations).not.toContain('valid')
  })

  it('reads the diagnostic count of each invalid correlation', () => {
    const d = { missing: 1, malformed: 2, unknown: 3, deprecated: 4, wrongProject: 5, total: 15 }
    expect(invalidCorrelations.map((c) => diagnosticCount(d, c))).toEqual([1, 2, 3, 4, 5])
    expect(correlationExplanation('wrong_project')).toMatch(/another project/)
  })

  it('maps statuses and correlations to badge variants', () => {
    expect(summaryStatuses.map(statusVariant)).toEqual([
      'outline',
      'success',
      'destructive',
      'warning',
      'secondary',
    ])
    expect(correlationVariant('valid')).toBe('outline')
    expect(correlationVariant('unknown')).toBe('warning')
  })

  it('maps execution statuses and detects interrupted runs', () => {
    expect(executionVariant('completed')).toBe('secondary')
    expect(executionVariant('interrupted')).toBe('destructive')
    expect(executionVariant('cancelled')).toBe('warning')
    expect(isInterruptedRun('interrupted')).toBe(true)
    expect(isInterruptedRun('cancelled')).toBe(true)
    expect(isInterruptedRun('completed')).toBe(false)
    expect(isInterruptedRun('running')).toBe(false)
    expect(executionVariant('running')).toBe('outline')
  })

  it('maps verdicts to badges and labels', () => {
    expect(verdictVariant('passed')).toBe('success')
    expect(verdictVariant('failed')).toBe('destructive')
    expect(verdictVariant('incomplete')).toBe('warning')
    expect(verdictVariant('no_tests')).toBe('outline')
    expect(verdictLabel('no_tests')).toBe('no tests')
    expect(verdictLabel('passed')).toBe('passed')
  })

  it("reads a manual run's stored error as Blocked, and nothing else changes (card #65)", () => {
    expect(statusLabel('error', true)).toBe('Blocked')
    expect(statusLabel('error')).toBe('error')
    expect(statusLabel('failed', true)).toBe('failed')
    expect(statusLabel('untested', true)).toBe('untested')
  })

  it('explains every correlation', () => {
    for (const c of correlations) expect(correlationExplanation(c)).not.toBe('')
    expect(correlationExplanation('unknown')).toContain('never created automatically')
  })

  it('picks enum values and parses positive integers', () => {
    expect(pickEnum('failed', resultStatuses)).toBe('failed')
    expect(pickEnum('untested', resultStatuses)).toBeUndefined()
    expect(pickEnum(null, resultStatuses)).toBeUndefined()
    expect(positiveInt('3', 1)).toBe(3)
    expect(positiveInt('0', 1)).toBe(1)
    expect(positiveInt('1.5', 1)).toBe(1)
    expect(positiveInt('abc', 7)).toBe(7)
    expect(positiveInt(null, 2)).toBe(2)
    expect(positiveInt(undefined, 2)).toBe(2)
  })
})
