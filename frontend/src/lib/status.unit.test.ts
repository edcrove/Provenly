import { describe, expect, it } from 'vitest'

import {
  correlationExplanation,
  correlations,
  correlationVariant,
  invalidCorrelations,
  pickEnum,
  positiveInt,
  resultStatuses,
  statusVariant,
  summaryStatuses,
} from './status'

describe('status helpers', () => {
  it('lists the contract enums', () => {
    expect(resultStatuses).toEqual(['passed', 'failed', 'error', 'skipped'])
    expect(summaryStatuses).toEqual(['untested', 'passed', 'failed', 'error', 'skipped'])
    expect(correlations).toEqual(['valid', 'missing', 'malformed', 'unknown', 'deprecated'])
    expect(invalidCorrelations).not.toContain('valid')
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
