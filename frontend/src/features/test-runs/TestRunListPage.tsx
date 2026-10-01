import { Link, useSearchParams } from 'react-router'

import { useTestRuns } from '@/api/queries'
import { Pagination } from '@/components/Pagination'
import { QueryState } from '@/components/QueryState'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { PageTitle } from '@/components/PageTitle'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { formatDateTime, plural, shortCommit } from '@/lib/format'
import { positiveInt, runStatusVariant } from '@/lib/status'

export function TestRunListPage() {
  const [params, setParams] = useSearchParams()
  const page = positiveInt(params.get('page'), 1)
  const query = useTestRuns(page)
  return (
    <Card>
      <CardHeader>
        <PageTitle title="Test Runs" />
        <CardTitle as="h1" className="text-xl">
          Test Runs
        </CardTitle>
      </CardHeader>
      <CardContent>
        <QueryState query={query}>
          {(data) => (
            <>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Run</TableHead>
                    <TableHead>External run id</TableHead>
                    <TableHead>Pipeline</TableHead>
                    <TableHead>Branch</TableHead>
                    <TableHead>Commit</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Results</TableHead>
                    <TableHead>Created</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.items.length === 0 && (
                    <TableRow>
                      <TableCell colSpan={8} className="text-muted-foreground">
                        No test runs yet. CI sends JUnit reports to POST /api/v1/ingestion/junit.
                      </TableCell>
                    </TableRow>
                  )}
                  {data.items.map((run) => (
                    <TableRow key={run.id}>
                      <TableCell>
                        <Link to={`/test-runs/${run.id}`} className="underline">
                          #{run.id}
                        </Link>
                      </TableCell>
                      <TableCell className="font-mono">{run.externalRunId}</TableCell>
                      <TableCell>{run.pipeline || '—'}</TableCell>
                      <TableCell>{run.branch || '—'}</TableCell>
                      <TableCell className="font-mono">{shortCommit(run.commit)}</TableCell>
                      <TableCell>
                        <Badge variant={runStatusVariant(run.status)}>{run.status}</Badge>
                      </TableCell>
                      <TableCell>
                        {plural(run.resultCount, 'result')} · {run.expectedCount} expected
                      </TableCell>
                      <TableCell>{formatDateTime(run.createdAt)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              <Pagination
                page={data.page}
                totalPages={data.totalPages}
                totalItems={data.totalItems}
                onPageChange={(p) => setParams({ page: String(p) })}
              />
            </>
          )}
        </QueryState>
      </CardContent>
    </Card>
  )
}
