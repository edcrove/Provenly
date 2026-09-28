import type { components } from '@/api/schema'

export type Problem = components['schemas']['Problem']

/** Error thrown for any non-2xx API response; carries the Problem body when present. */
export class ApiError extends Error {
  readonly status: number
  readonly problem?: Problem

  constructor(status: number, problem?: Problem) {
    super(problem?.detail ?? problem?.title ?? `Request failed with status ${status}`)
    this.name = 'ApiError'
    this.status = status
    this.problem = problem
  }

  /** Field-level validation messages keyed by field name. */
  get fieldErrors(): Record<string, string> {
    const out: Record<string, string> = {}
    for (const e of this.problem?.errors ?? []) out[e.field] = e.message
    return out
  }
}

function isProblem(value: unknown): value is Problem {
  return typeof value === 'object' && value !== null && 'code' in value && 'status' in value
}

interface FetchResult<T> {
  data?: T
  error?: unknown
  response: Response
}

/** Returns data of a successful openapi-fetch call or throws an ApiError. */
export function unwrap<T>(result: FetchResult<T>): T {
  if (result.error !== undefined || !result.response.ok) {
    throw new ApiError(result.response.status, isProblem(result.error) ? result.error : undefined)
  }
  return result.data as T
}

/** Human message for any error surfaced in the UI. */
export function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    const fields = Object.entries(error.fieldErrors)
      .map(([field, msg]) => `${field}: ${msg}`)
      .join('; ')
    return fields ? `${error.message} (${fields})` : error.message
  }
  if (error instanceof Error) return error.message
  return 'Unexpected error'
}
