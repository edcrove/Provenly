/**
 * Formats a duration in milliseconds. null means unknown (not reported or
 * invalid); 0 means reported as 0 or rounded down from under 0.5 ms.
 */
export function formatDuration(ms: number | null): string {
  if (ms === null) return '—'
  if (ms === 0) return '<1 ms'
  if (ms < 1000) return `${ms} ms`
  const seconds = ms / 1000
  if (seconds < 10) return `${seconds.toFixed(2)} s`
  if (Math.round(seconds * 10) < 600) return `${seconds.toFixed(1)} s`
  const total = Math.round(seconds)
  return `${Math.floor(total / 60)}m ${total % 60}s`
}

/** "UTC", "UTC−3", "UTC+5:30": a UTC offset in minutes, as people read it. */
export function formatOffset(minutes: number): string {
  if (minutes === 0) return 'UTC'
  const abs = Math.abs(minutes)
  const h = Math.floor(abs / 60)
  const m = abs % 60
  return `UTC${minutes < 0 ? '−' : '+'}${h}${m ? `:${String(m).padStart(2, '0')}` : ''}`
}

/**
 * Formats an ISO timestamp for tables in the viewer's local time with its UTC offset, e.g. "2026-10-08 11:03:05
 * UTC−3" ("—" when missing or invalid). offsetMinutes defaults to the browser's offset at that instant (so summer time
 * is right for each date).
 */
export function formatDateTime(iso: string | null | undefined, offsetMinutes?: number): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'
  const offset = offsetMinutes ?? -d.getTimezoneOffset()
  const local = new Date(d.getTime() + offset * 60_000)
  return `${local.toISOString().replace('T', ' ').slice(0, 19)} ${formatOffset(offset)}`
}

/** Formats a 0..100 percentage for display, rounded to 2 decimals (the API sends 6). */
export function formatPercent(value: number): string {
  const rounded = Math.round(value * 100) / 100
  return `${Number.isInteger(rounded) ? rounded : rounded.toFixed(2)}%`
}

/**
 * How much of a run's expected universe executed, e.g. "1 of 10 (10%)": next to the pass rate, so "100%" of one
 * executed test case does not read as a run where everything passed. "0 of 0" for a run that expected nothing.
 */
export function executedLabel(executed: number, expected: number): string {
  if (expected === 0) return `${executed} of 0`
  return `${executed} of ${expected} (${formatPercent((executed / expected) * 100)})`
}

/** Sums precise percentages (the API sends 6 decimals); round only when displaying the result. */
export function sumPercents(values: number[]): number {
  return values.reduce((acc, v) => acc + v, 0)
}

/** Shortens a commit SHA for display. */
export function shortCommit(commit: string): string {
  return commit.length > 10 ? commit.slice(0, 10) : commit || '—'
}

/** Counts of a run outcome, e.g. "2 passed · 1 failed · 1 untested": only the non-zero ones. */
export function outcomeBreakdown(o: {
  passed: number
  failed: number
  error: number
  skipped: number
  untested: number
}): string {
  const parts = (['passed', 'failed', 'error', 'skipped', 'untested'] as const)
    .filter((k) => o[k] > 0)
    .map((k) => `${o[k]} ${k}`)
  return parts.length ? parts.join(' · ') : 'no test cases'
}

/** "1 result", "2 results": count plus the singular or plural noun. */
export function plural(count: number, singular: string, pluralForm = `${singular}s`): string {
  return `${count} ${count === 1 ? singular : pluralForm}`
}
