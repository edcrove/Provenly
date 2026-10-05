import { useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router'

import type { Suite } from '@/api/client'
import { useSuite, useSuiteMutations, useTestCases } from '@/api/queries'
import { PageTitle } from '@/components/PageTitle'
import { Pagination } from '@/components/Pagination'
import { ErrorAlert, QueryState } from '@/components/QueryState'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { NativeSelect } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useProjectRole } from '@/features/projects/useProjectRole'
import { can } from '@/lib/roles'
import { positiveInt } from '@/lib/status'
import { selectionLabel } from '@/lib/suites'

/** Adds one of the project's active test cases to a static suite. */
function AddCase({ suite, projectKey }: { suite: Suite; projectKey: string }) {
  const m = useSuiteMutations(projectKey)
  const candidates = useTestCases(1, { project: projectKey, status: 'active', pageSize: 100 })
  const [picked, setPicked] = useState('')
  const members = suite.testCaseIds ?? []
  const options = (candidates.data?.items ?? []).filter((tc) => !members.includes(tc.id))
  return (
    <div className="flex flex-wrap items-center gap-2">
      <NativeSelect aria-label="Test case to add" value={picked} onChange={(e) => setPicked(e.target.value)}>
        <option value="">Choose a test case…</option>
        {options.map((tc) => (
          <option key={tc.id} value={tc.id}>
            {tc.key} · {tc.title}
          </option>
        ))}
      </NativeSelect>
      <Button
        size="sm"
        disabled={!picked || m.setCases.isPending}
        onClick={() =>
          m.setCases.mutate(
            { suiteKey: suite.key, testCaseIds: [...members, Number(picked)] },
            { onSuccess: () => setPicked('') },
          )
        }
      >
        Add to suite
      </Button>
      {m.setCases.error ? <ErrorAlert error={m.setCases.error} title="Could not change the suite" /> : null}
    </div>
  )
}

function SuiteCases({ suite, projectKey, manage }: { suite: Suite; projectKey: string; manage: boolean }) {
  const [params, setParams] = useSearchParams()
  const page = positiveInt(params.get('page'), 1)
  const cases = useTestCases(page, { project: projectKey, suite: suite.key })
  const m = useSuiteMutations(projectKey)
  const editable = manage && suite.kind === 'static'
  return (
    <QueryState query={cases}>
      {(data) => (
        <>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>TC-ID</TableHead>
                <TableHead>Title</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Automated</TableHead>
                {editable ? <TableHead /> : null}
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.items.length === 0 && (
                <TableRow>
                  <TableCell colSpan={5} className="text-muted-foreground">
                    {suite.kind === 'static'
                      ? 'This suite lists no test cases yet.'
                      : 'No test case matches this query yet.'}
                  </TableCell>
                </TableRow>
              )}
              {data.items.map((tc) => (
                <TableRow key={tc.id} data-testid={`suite-case-${tc.key}`}>
                  <TableCell className="font-mono">
                    <Link to={`/test-cases/${tc.id}`} className="underline">
                      {tc.key}
                    </Link>
                  </TableCell>
                  <TableCell>{tc.title}</TableCell>
                  <TableCell>
                    <Badge variant={tc.status === 'active' ? 'secondary' : 'outline'}>{tc.status}</Badge>
                  </TableCell>
                  <TableCell>{tc.automated ? 'Yes' : 'No'}</TableCell>
                  {editable ? (
                    <TableCell>
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={m.setCases.isPending}
                        onClick={() =>
                          m.setCases.mutate({
                            suiteKey: suite.key,
                            testCaseIds: (suite.testCaseIds ?? []).filter((id) => id !== tc.id),
                          })
                        }
                      >
                        Remove
                      </Button>
                    </TableCell>
                  ) : null}
                </TableRow>
              ))}
            </TableBody>
          </Table>
          <Pagination
            page={data.page}
            totalPages={data.totalPages}
            totalItems={data.totalItems}
            onPageChange={(p, replace) => setParams({ page: String(p) }, { replace })}
          />
        </>
      )}
    </QueryState>
  )
}

/** One suite: what it selects, its test cases, its runs and how CI reports for it. */
export function SuiteDetailPage() {
  const { projectKey = '', suiteKey = '' } = useParams()
  const suite = useSuite(projectKey, suiteKey)
  const m = useSuiteMutations(projectKey)
  const manage = can(useProjectRole(projectKey), 'maintainer')
  return (
    <QueryState page query={suite}>
      {(s) => (
        <Card>
          <CardHeader>
            <PageTitle title={`${s.name} · suite`} />
            <CardTitle as="h1" className="flex flex-wrap items-center gap-2 text-xl">
              {s.name}
              <Badge variant="outline">{s.kind}</Badge>
              {s.archivedAt ? <Badge variant="outline">archived</Badge> : null}
            </CardTitle>
            <CardDescription className="grid gap-1">
              <span>
                <span className="font-mono">
                  {projectKey}/{s.key}
                </span>{' '}
                · selects {selectionLabel(s)} · <Link to="/suites">All suites</Link> ·{' '}
                <Link to={`/test-runs?suite=${s.key}`} className="underline">
                  Runs of this suite
                </Link>
              </span>
              <span data-testid="suite-ci-hint">
                CI reports a run for this suite by adding{' '}
                <code>
                  &amp;project={projectKey}&amp;suite={s.key}
                </code>{' '}
                to the ingestion URL; only its active automated test cases are expected.
              </span>
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-4">
            {manage ? (
              <div className="flex flex-wrap gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  disabled={m.update.isPending}
                  onClick={() => m.update.mutate({ suiteKey: s.key, archived: s.archivedAt === null })}
                >
                  {s.archivedAt ? 'Restore' : 'Archive'}
                </Button>
                {m.update.error ? (
                  <ErrorAlert error={m.update.error} title="Could not update the suite" />
                ) : null}
              </div>
            ) : null}
            {manage && s.kind === 'static' ? <AddCase suite={s} projectKey={projectKey} /> : null}
            <SuiteCases suite={s} projectKey={projectKey} manage={manage} />
          </CardContent>
        </Card>
      )}
    </QueryState>
  )
}
