import type { components } from '@/api/schema'

export type ResultStatus = components['schemas']['ResultStatus']
export type SummaryStatus = components['schemas']['SummaryStatus']
export type Correlation = components['schemas']['Correlation']
export type BadgeVariant = 'default' | 'secondary' | 'destructive' | 'success' | 'warning' | 'outline'

export const resultStatuses: ResultStatus[] = ['passed', 'failed', 'error', 'skipped']
export const summaryStatuses: SummaryStatus[] = ['untested', 'passed', 'failed', 'error', 'skipped']
export const invalidCorrelations: Exclude<Correlation, 'valid'>[] = [
  'missing',
  'malformed',
  'unknown',
  'deprecated',
]
export const correlations: Correlation[] = ['valid', ...invalidCorrelations]

const statusVariants: Record<SummaryStatus, BadgeVariant> = {
  passed: 'success',
  failed: 'destructive',
  error: 'warning',
  skipped: 'secondary',
  untested: 'outline',
}

export function statusVariant(status: SummaryStatus): BadgeVariant {
  return statusVariants[status]
}

export function correlationVariant(c: Correlation): BadgeVariant {
  return c === 'valid' ? 'outline' : 'warning'
}

const explanations: Record<Correlation, string> = {
  valid: 'Linked to an active test case.',
  missing: 'The test declares no TC-ID (tc-id property or TC-<id> in its name).',
  malformed: 'The declared TC-ID reference is not a single positive integer id.',
  unknown: 'The declared TC-ID does not exist. Test cases are never created automatically.',
  deprecated: 'The declared TC-ID belongs to a deprecated test case.',
}

export function correlationExplanation(c: Correlation): string {
  return explanations[c]
}

/** Narrows an arbitrary string (e.g. a URL search param) to one of the allowed values. */
export function pickEnum<T extends string>(value: string | null, allowed: readonly T[]): T | undefined {
  return allowed.find((a) => a === value)
}

/** Parses a positive integer (e.g. page number or id) or returns the fallback. */
export function positiveInt(value: string | null | undefined, fallback: number): number {
  const n = Number(value)
  return Number.isInteger(n) && n >= 1 ? n : fallback
}
