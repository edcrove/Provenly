import { useState, type FormEvent } from 'react'
import { Link } from 'react-router'

import { useTestCaseHistory } from '@/api/queries'
import { Pagination } from '@/components/Pagination'
import { QueryState } from '@/components/QueryState'
import { AttemptBadge, StatusBadge } from '@/components/StatusBadge'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { formatDateTime, formatDuration, shortCommit } from '@/lib/format'

/** "Last passed …" for the chosen branch (or any branch): did it pass lately, and where. */
function LastPassed({ testCaseId, branch }: { testCaseId: number; branch: string }) {
  const last = useTestCaseHistory(testCaseId, 1, {
    branch: branch || undefined,
    status: 'passed',
    pageSize: 1,
  })
  const where = branch ? ` on ${branch}` : ''
  if (!last.data) return null
  const entry = last.data.items[0]
  return (
    <p className="text-sm" data-testid="last-passed">
      {entry ? (
        <>
          Last passed{where}: {formatDateTime(entry.run.createdAt)} in{' '}
          <Link to={`/test-runs/${entry.run.id}`} className="font-mono underline">
            {entry.run.externalRunId}
          </Link>
          {branch ? '' : ` (${entry.run.branch || 'no branch'})`}
        </>
      ) : (
        `Never passed${where}.`
      )}
    </p>
  )
}

/** Results of a TC-ID across runs, with the metadata observed at execution time; filterable by branch. */
export function TestCaseHistory({ testCaseId }: { testCaseId: number }) {
  const [page, setPage] = useState(1)
  const [branch, setBranch] = useState('')
  const [draft, setDraft] = useState('')
  const query = useTestCaseHistory(testCaseId, page, { branch: branch || undefined })
  const apply = (e: FormEvent) => {
    e.preventDefault()
    setBranch(draft.trim())
    setPage(1)
  }
  return (
    <div className="grid gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <LastPassed testCaseId={testCaseId} branch={branch} />
        <form onSubmit={apply} role="search" aria-label="Filter the history by branch">
          <Input
            aria-label="History branch"
            placeholder="Branch, e.g. main (Enter)"
            className="h-8 w-56"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
          />
        </form>
      </div>
      <QueryState query={query}>
        {(data) =>
          data.totalItems === 0 ? (
            <p className="text-muted-foreground text-sm">
              {branch ? `No results on ${branch} yet for this TC-ID.` : 'No results yet for this TC-ID.'}
            </p>
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
                    <TableHead>Reported</TableHead>
                    <TableHead>Duration</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.items.map(({ result, run }) => (
                    <TableRow key={result.id}>
                      <TableCell>
                        <span className="flex items-center gap-2">
                          <StatusBadge status={result.status} manual={run.mode === 'manual'} />
                          <AttemptBadge attempt={result.attempt} retried={result.retried} />
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
                      <TableCell>{formatDateTime(run.startedAt)}</TableCell>
                      <TableCell>{formatDateTime(run.createdAt)}</TableCell>
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
    </div>
  )
}
