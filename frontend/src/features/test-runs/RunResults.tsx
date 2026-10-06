import { ChevronDown, ChevronRight } from 'lucide-react'
import { Fragment, useState } from 'react'
import { Link, useSearchParams } from 'react-router'

import { useTestRunResults } from '@/api/queries'
import { Pagination } from '@/components/Pagination'
import { QueryState } from '@/components/QueryState'
import { AttemptBadge, CorrelationBadge, StatusBadge } from '@/components/StatusBadge'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { NativeSelect } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { formatDuration } from '@/lib/format'
import { correlationExplanation, correlations, pickEnum, positiveInt, resultStatuses } from '@/lib/status'

/** Individual results of a run as ingested, filterable by status and TC-ID correlation. */
/** The message is clamped to two lines: a long one without details still opens in full. */
const CLAMPED_MESSAGE = 120
const expandable = (r: { errorMessage: string; errorDetails: string }) =>
  r.errorDetails !== '' || r.errorMessage.length > CLAMPED_MESSAGE || r.errorMessage.includes('\n')

export function RunResults({ testRunId, shards }: { testRunId: number; shards?: number }) {
  const [params, setParams] = useSearchParams()
  const page = positiveInt(params.get('page'), 1)
  const status = pickEnum(params.get('status'), resultStatuses)
  const correlation = pickEnum(params.get('correlation'), correlations)
  const requestedShard = positiveInt(params.get('shard'), 0)
  const shard = shards && requestedShard <= shards ? requestedShard || undefined : undefined
  const query = useTestRunResults(testRunId, page, status, correlation, shard)
  // Error details (stack traces, every failure of a testcase) open on demand, one row at a time or several.
  const [expanded, setExpanded] = useState<Set<number>>(new Set())
  const toggle = (id: number) =>
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

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
        {shards ? (
          <NativeSelect
            aria-label="Filter by shard"
            value={shard ?? ''}
            onChange={(e) => update('shard', e.target.value)}
          >
            <option value="">All shards</option>
            {Array.from({ length: shards }, (_, i) => i + 1).map((n) => (
              <option key={n} value={n}>
                Shard {n}
              </option>
            ))}
          </NativeSelect>
        ) : null}
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
                      {status || correlation || shard ? 'No results match the filters.' : 'No results yet.'}
                    </TableCell>
                  </TableRow>
                )}
                {data.items.map((r) => (
                  <Fragment key={r.id}>
                    <TableRow data-testid="result-row">
                      <TableCell>
                        <span
                          className="flex items-center gap-2"
                          title={correlationExplanation(r.correlation)}
                        >
                          {r.testCaseId ? (
                            <Link to={`/test-cases/${r.testCaseId}`} className="font-mono underline">
                              {r.testCaseKey ?? `#${r.testCaseId}`}
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
                        <span className="flex flex-wrap items-center gap-1">
                          <StatusBadge status={r.status} />
                          <AttemptBadge attempt={r.attempt} retried={r.retried} />
                          {r.shard ? (
                            <Badge variant="outline" title={`Reported by shard ${r.shard}`}>
                              shard {r.shard}
                            </Badge>
                          ) : null}
                        </span>
                      </TableCell>
                      <TableCell>{formatDuration(r.durationMs)}</TableCell>
                      <TableCell className="max-w-xs">
                        <span className="flex items-start gap-1">
                          {expandable(r) ? (
                            <Button
                              size="icon"
                              variant="ghost"
                              className="size-6 shrink-0"
                              aria-expanded={expanded.has(r.id)}
                              aria-label={`${expanded.has(r.id) ? 'Hide' : 'Show'} error details of ${r.errorMessage || r.testName}`}
                              onClick={() => toggle(r.id)}
                            >
                              {expanded.has(r.id) ? <ChevronDown /> : <ChevronRight />}
                            </Button>
                          ) : null}
                          <span className="line-clamp-2">{r.errorMessage}</span>
                        </span>
                      </TableCell>
                    </TableRow>
                    {expanded.has(r.id) ? (
                      <TableRow>
                        <TableCell colSpan={5} className="bg-muted/40">
                          <pre
                            data-testid="error-details"
                            className="max-h-96 overflow-auto font-mono text-xs whitespace-pre-wrap [overflow-wrap:anywhere]"
                          >
                            {r.errorDetails || r.errorMessage}
                          </pre>
                        </TableCell>
                      </TableRow>
                    ) : null}
                  </Fragment>
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
