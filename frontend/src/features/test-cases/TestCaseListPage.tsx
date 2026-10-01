import { Plus } from 'lucide-react'
import { Link, useSearchParams } from 'react-router'

import { useTestCases } from '@/api/queries'
import { Pagination } from '@/components/Pagination'
import { QueryState } from '@/components/QueryState'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { PageTitle } from '@/components/PageTitle'
import { NativeSelect } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { pickEnum, positiveInt } from '@/lib/status'

const statuses = ['active', 'deprecated'] as const

export function TestCaseListPage() {
  const [params, setParams] = useSearchParams()
  const page = positiveInt(params.get('page'), 1)
  const status = pickEnum(params.get('status'), statuses)
  const query = useTestCases(page, status)

  const update = (next: Record<string, string | undefined>, replace = false) => {
    const merged = new URLSearchParams(params)
    for (const [k, v] of Object.entries(next)) {
      if (v) merged.set(k, v)
      else merged.delete(k)
    }
    setParams(merged, { replace })
  }

  return (
    <Card>
      <CardHeader className="flex-row flex-wrap items-center justify-between gap-2">
        <PageTitle title="Test Cases" />
        <CardTitle as="h1" className="text-xl">
          Test Cases
        </CardTitle>
        <div className="flex flex-wrap items-center gap-2">
          <NativeSelect
            aria-label="Filter by status"
            value={status ?? ''}
            onChange={(e) => update({ status: e.target.value || undefined, page: undefined })}
          >
            <option value="">All statuses</option>
            <option value="active">Active</option>
            <option value="deprecated">Deprecated</option>
          </NativeSelect>
          <Button asChild>
            <Link to="/test-cases/new">
              <Plus /> New test case
            </Link>
          </Button>
        </div>
      </CardHeader>
      <CardContent>
        <QueryState query={query}>
          {(data) => (
            <>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>TC-ID</TableHead>
                    <TableHead>Title</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Automated</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.items.length === 0 && (
                    <TableRow>
                      <TableCell colSpan={4} className="text-muted-foreground">
                        {status ? `No ${status} test cases.` : 'No test cases yet.'}
                      </TableCell>
                    </TableRow>
                  )}
                  {data.items.map((tc) => (
                    <TableRow key={tc.id}>
                      <TableCell className="font-mono whitespace-nowrap">
                        <Link to={`/test-cases/${tc.id}`} className="underline">
                          {tc.key}
                        </Link>
                      </TableCell>
                      <TableCell>{tc.title}</TableCell>
                      <TableCell>
                        <Badge variant={tc.status === 'active' ? 'secondary' : 'outline'}>{tc.status}</Badge>
                      </TableCell>
                      <TableCell>{tc.automated ? 'Yes' : 'No'}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              <Pagination
                page={data.page}
                totalPages={data.totalPages}
                totalItems={data.totalItems}
                onPageChange={(p, replace) => update({ page: String(p) }, replace)}
              />
            </>
          )}
        </QueryState>
      </CardContent>
    </Card>
  )
}
