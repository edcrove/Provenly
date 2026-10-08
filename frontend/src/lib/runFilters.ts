import type { RunListFilter } from '@/api/client'
import { pickEnum } from '@/lib/status'

export const executionStatuses = ['running', 'completed', 'interrupted', 'cancelled'] as const
export const modes = ['batch', 'live', 'manual', 'sharded'] as const
export const modeLabels: Record<(typeof modes)[number], string> = {
  batch: 'CI report',
  live: 'Live',
  manual: 'Manual',
  sharded: 'Sharded',
}
const DAY = /^\d{4}-\d{2}-\d{2}$/

/** The filters of the runs list as they sit in the URL (?branch=&executionStatus=&mode=&from=YYYY-MM-DD&to=…). */
export interface RunFilterParams {
  branch: string
  executionStatus?: (typeof executionStatuses)[number]
  mode?: (typeof modes)[number]
  from: string
  to: string
}

export function readRunFilters(params: URLSearchParams): RunFilterParams {
  const day = (name: string) => {
    const v = params.get(name) ?? ''
    return DAY.test(v) ? v : ''
  }
  return {
    branch: params.get('branch') ?? '',
    executionStatus: pickEnum(params.get('executionStatus'), executionStatuses),
    mode: pickEnum(params.get('mode'), modes),
    from: day('from'),
    to: day('to'),
  }
}

/** The API filter: days are the viewer's local days (from its first to its last millisecond). */
export function toApiFilter(f: RunFilterParams): RunListFilter {
  return {
    ...(f.branch ? { branch: f.branch } : {}),
    ...(f.executionStatus ? { executionStatus: f.executionStatus } : {}),
    ...(f.mode ? { mode: f.mode } : {}),
    ...(f.from ? { from: new Date(`${f.from}T00:00:00`).toISOString() } : {}),
    ...(f.to ? { to: new Date(`${f.to}T23:59:59.999`).toISOString() } : {}),
  }
}

export const hasRunFilters = (f: RunFilterParams) =>
  Boolean(f.branch || f.executionStatus || f.mode || f.from || f.to)
