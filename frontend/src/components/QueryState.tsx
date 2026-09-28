import type { ReactNode } from 'react'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { ApiError, errorMessage } from '@/lib/problem'

export function ErrorAlert({ error, title }: { error: unknown; title?: string }) {
  const fallback = error instanceof ApiError && error.status === 404 ? 'Not found' : 'Something went wrong'
  return (
    <Alert variant="destructive">
      <AlertTitle>{title ?? fallback}</AlertTitle>
      <AlertDescription>{errorMessage(error)}</AlertDescription>
    </Alert>
  )
}

interface QueryStateProps<T> {
  query: { isPending: boolean; error: unknown; data?: T }
  children: (data: T) => ReactNode
}

/** Renders loading / error states of a query, and children once data is available. */
export function QueryState<T>({ query, children }: QueryStateProps<T>) {
  if (query.isPending) return <p className="text-muted-foreground text-sm">Loading…</p>
  if (query.error || query.data === undefined) return <ErrorAlert error={query.error} />
  return <>{children(query.data)}</>
}
