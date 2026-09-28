/** Display form of a TC-ID. */
export function tcKey(id: number): string {
  return `TC-${id}`
}

/**
 * Formats a duration in milliseconds. null means unknown (not reported or
 * invalid); 0 means reported as 0 or rounded down from under 0.5 ms.
 */
export function formatDuration(ms: number | null): string {
  if (ms === null) return '—'
  if (ms === 0) return '<1 ms'
  if (ms < 1000) return `${ms} ms`
  const seconds = ms / 1000
  if (seconds < 60) return `${seconds.toFixed(seconds < 10 ? 2 : 1)} s`
  const minutes = Math.floor(seconds / 60)
  return `${minutes}m ${Math.round(seconds % 60)}s`
}

/** Formats an ISO timestamp (UTC) for tables; empty for null. */
export function formatDateTime(iso: string | null | undefined): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'
  return d.toISOString().replace('T', ' ').slice(0, 19) + ' UTC'
}

/** Formats a 0..100 percentage as returned by the API. */
export function formatPercent(value: number): string {
  return `${Number.isInteger(value) ? value : value.toFixed(2)}%`
}

/** Shortens a commit SHA for display. */
export function shortCommit(commit: string): string {
  return commit.length > 10 ? commit.slice(0, 10) : commit || '—'
}
