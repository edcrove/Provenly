import { useQueryClient } from '@tanstack/react-query'
import type { ReactNode } from 'react'

import { PageTitle } from '@/components/PageTitle'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { ApiError, errorMessage } from '@/lib/problem'

const isNotFound = (error: unknown) => error instanceof ApiError && error.status === 404
/** 412: someone saved the test case after it was read (optimistic locking). */
const isStale = (error: unknown) => error instanceof ApiError && error.status === 412

/** A write refused because someone else saved first: nothing changed; reloading shows their version. */
function ConflictAlert() {
  const qc = useQueryClient()
  return (
    <Alert variant="destructive" data-testid="conflict">
      <AlertTitle>Someone else saved this test case</AlertTitle>
      <AlertDescription>
        <p>
          Your change was not applied, so nothing was overwritten. Reload to see the latest version, then
          apply your change again.
        </p>
        <Button
          type="button"
          size="sm"
          variant="outline"
          className="mt-2"
          onClick={() => void qc.invalidateQueries()}
        >
          Reload
        </Button>
      </AlertDescription>
    </Alert>
  )
}

export function ErrorAlert({ error, title }: { error: unknown; title?: string }) {
  if (isStale(error)) return <ConflictAlert />
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
