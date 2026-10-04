import { useState } from 'react'
import { Link } from 'react-router'

import { useTestCaseHistory } from '@/api/queries'
import { Pagination } from '@/components/Pagination'
import { QueryState } from '@/components/QueryState'
import { StatusBadge } from '@/components/StatusBadge'
import { Badge } from '@/components/ui/badge'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { formatDateTime, formatDuration, shortCommit } from '@/lib/format'

/** Results of a TC-ID across runs, with the metadata observed at execution time. */
export function TestCaseHistory({ testCaseId }: { testCaseId: number }) {
  const [page, setPage] = useState(1)
  const query = useTestCaseHistory(testCaseId, page)
  return (
    <QueryState query={query}>
      {(data) =>
        data.totalItems === 0 ? (
          <p className="text-muted-foreground text-sm">No results yet for this TC-ID.</p>
        ) : (
          <>
            <Table aria-label="Execution history">
              <TableHeader>
                <TableRow>
                  <TableHead>Result</TableHead>
                  <TableHead>Observed test name</TableHead>
                  <TableHead>Run</TableHead>
                  <TableHead>Branch</TableHead>
                  <TableHead>Commit</TableHead>
                  <TableHead>Executed</TableHead>
                  <TableHead>Duration</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.items.map(({ result, run }) => (
                  <TableRow key={result.id}>
                    <TableCell>
                      <span className="flex items-center gap-2">
                        <StatusBadge status={result.status} />
                        {result.correlation === 'deprecated' ? (
                          <Badge
                            variant="outline"
                            title="Received after this test case was deprecated; not counted in run summaries"
                          >
                            after deprecation
                          </Badge>
                        ) : null}
                      </span>
                    </TableCell>
                    <TableCell>{result.testName}</TableCell>
                    <TableCell className="font-mono">
                      <Link to={`/test-runs/${run.id}`} className="underline">
                        {run.externalRunId}
                      </Link>
                    </TableCell>
                    <TableCell>{run.branch || '—'}</TableCell>
                    <TableCell className="font-mono">{shortCommit(run.commit)}</TableCell>
                    <TableCell>{formatDateTime(run.startedAt ?? run.completedAt ?? run.createdAt)}</TableCell>
                    <TableCell>{formatDuration(result.durationMs)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            <Pagination
              page={data.page}
              totalPages={data.totalPages}
              totalItems={data.totalItems}
              onPageChange={setPage}
            />
          </>
        )
      }
    </QueryState>
  )
}
