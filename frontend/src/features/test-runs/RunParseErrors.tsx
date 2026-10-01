import { useState } from 'react'

import { useTestRunParseErrors } from '@/api/queries'
import { Pagination } from '@/components/Pagination'
import { QueryState } from '@/components/QueryState'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'

/** Testcases of the ingested report that could not be fully normalized (shown only when there are any). */
export function RunParseErrors({ testRunId }: { testRunId: number }) {
  const [page, setPage] = useState(1)
  const query = useTestRunParseErrors(testRunId, page)
  if (query.data?.totalItems === 0) return null
  return (
    <Card>
      <CardHeader>
        <CardTitle as="h2">Report parse errors</CardTitle>
        <CardDescription>
          Testcases of the JUnit report that could not be fully read. Kept results are listed below with an
          unknown duration; discarded ones are not in the results.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <QueryState query={query}>
          {(data) => (
            <>
              <Table aria-label="Parse errors">
                <TableHeader>
                  <TableRow>
                    <TableHead>#</TableHead>
                    <TableHead>Severity</TableHead>
                    <TableHead>Test name</TableHead>
                    <TableHead>Problem</TableHead>
                    <TableHead>Result</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.items.map((e) => (
                    <TableRow key={e.index} data-testid="parse-error-row">
                      <TableCell>{e.index + 1}</TableCell>
                      <TableCell>
                        <Badge variant={e.severity === 'warning' ? 'warning' : 'destructive'}>
                          {e.severity}
                        </Badge>
                      </TableCell>
                      <TableCell>{e.testName || '(no name)'}</TableCell>
                      <TableCell>{e.message}</TableCell>
                      <TableCell>
                        <Badge variant={e.persisted ? 'secondary' : 'outline'}>
                          {e.persisted ? 'kept' : 'discarded'}
                        </Badge>
                      </TableCell>
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
          )}
        </QueryState>
      </CardContent>
    </Card>
  )
}
