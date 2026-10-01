import { Link, useSearchParams } from 'react-router'

import { useTestRunResults } from '@/api/queries'
import { Pagination } from '@/components/Pagination'
import { QueryState } from '@/components/QueryState'
import { CorrelationBadge, StatusBadge } from '@/components/StatusBadge'
import { NativeSelect } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { formatDuration, tcKey } from '@/lib/format'
import { correlationExplanation, correlations, pickEnum, positiveInt, resultStatuses } from '@/lib/status'

/** Individual results of a run as ingested, filterable by status and TC-ID correlation. */
export function RunResults({ testRunId }: { testRunId: number }) {
  const [params, setParams] = useSearchParams()
  const page = positiveInt(params.get('page'), 1)
  const status = pickEnum(params.get('status'), resultStatuses)
  const correlation = pickEnum(params.get('correlation'), correlations)
  const query = useTestRunResults(testRunId, page, status, correlation)

  const update = (key: string, value: string, replace = false) => {
    const next = new URLSearchParams(params)
    if (value) next.set(key, value)
    else next.delete(key)
    if (key !== 'page') next.delete('page')
    setParams(next, { replace })
  }

  return (
    <div className="grid gap-3">
      <div className="flex gap-2">
        <NativeSelect
          aria-label="Filter by status"
          value={status ?? ''}
          onChange={(e) => update('status', e.target.value)}
        >
          <option value="">All statuses</option>
          {resultStatuses.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </NativeSelect>
        <NativeSelect
          aria-label="Filter by TC-ID correlation"
          value={correlation ?? ''}
          onChange={(e) => update('correlation', e.target.value)}
        >
          <option value="">All correlations</option>
          {correlations.map((c) => (
            <option key={c} value={c}>
              {c}
            </option>
          ))}
        </NativeSelect>
      </div>
      <QueryState query={query}>
        {(data) => (
          <>
            <Table aria-label="Results">
              <TableHeader>
                <TableRow>
                  <TableHead>TC-ID</TableHead>
                  <TableHead>Test name</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Duration</TableHead>
                  <TableHead>Error</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.items.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={5} className="text-muted-foreground">
                      No results match the filters.
                    </TableCell>
                  </TableRow>
                )}
                {data.items.map((r) => (
                  <TableRow key={r.id} data-testid="result-row">
                    <TableCell>
                      <span className="flex items-center gap-2" title={correlationExplanation(r.correlation)}>
                        {r.testCaseId ? (
                          <Link to={`/test-cases/${r.testCaseId}`} className="font-mono underline">
                            {tcKey(r.testCaseId)}
                          </Link>
                        ) : null}
                        {r.correlation !== 'valid' ? (
                          <>
                            <CorrelationBadge correlation={r.correlation} />
                            {r.testCaseId ? null : (
                              <span className="font-mono text-xs">{r.requestedTestCaseId ?? ''}</span>
                            )}
                          </>
                        ) : null}
                      </span>
                    </TableCell>
                    <TableCell>{r.testName}</TableCell>
                    <TableCell>
                      <StatusBadge status={r.status} />
                    </TableCell>
                    <TableCell>{formatDuration(r.durationMs)}</TableCell>
                    <TableCell className="max-w-xs truncate" title={r.errorDetails}>
                      {r.errorMessage}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            <Pagination
              page={data.page}
              totalPages={data.totalPages}
              totalItems={data.totalItems}
              onPageChange={(p, replace) => update('page', String(p), replace)}
            />
          </>
        )}
      </QueryState>
    </div>
  )
}
