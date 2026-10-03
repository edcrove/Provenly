import type { ReactNode } from 'react'

import { PageTitle } from '@/components/PageTitle'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { ApiError, errorMessage } from '@/lib/problem'

const isNotFound = (error: unknown) => error instanceof ApiError && error.status === 404

export function ErrorAlert({ error, title }: { error: unknown; title?: string }) {
  const fallback = isNotFound(error) ? 'Not found' : 'Something went wrong'
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
  /** The query is the whole page: its loading and error states also set the browser tab title. */
  page?: boolean
}

/** Renders loading / error states of a query, and children once data is available. */
export function QueryState<T>({ query, children, page }: QueryStateProps<T>) {
  if (query.isPending)
    return (
      <>
        {page ? <PageTitle title="Loading…" /> : null}
        <p className="text-muted-foreground text-sm">Loading…</p>
      </>
    )
  if (query.error || query.data === undefined)
    return (
      <>
        {page ? <PageTitle title={isNotFound(query.error) ? 'Not found' : 'Error'} /> : null}
        <ErrorAlert error={query.error} />
      </>
    )
  return <>{children(query.data)}</>
}
