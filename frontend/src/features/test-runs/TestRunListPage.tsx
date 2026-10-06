import { Fragment } from 'react'
import { Play } from 'lucide-react'
import { Link, useSearchParams } from 'react-router'

import { useProjects, useTestRuns } from '@/api/queries'
import { Pagination } from '@/components/Pagination'
import { QueryState } from '@/components/QueryState'
import { EditedBadge, ExecutionBadge, FlakyBadge, SuiteBadge, VerdictBadge } from '@/components/StatusBadge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { PageTitle } from '@/components/PageTitle'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { formatDateTime, formatPercent, outcomeBreakdown, shortCommit } from '@/lib/format'
import { useCurrentProject } from '@/features/projects/currentProject'
import { can } from '@/lib/roles'
import { positiveInt } from '@/lib/status'

export function TestRunListPage() {
  const [params, setParams] = useSearchParams()
  const page = positiveInt(params.get('page'), 1)
  const { project: current } = useCurrentProject()
  // ?project=<key>&suite=<key>: the runs of one suite (linked from the suite page). Suite keys are per project, so
  // the link's project wins over the one chosen in the header.
  const suite = params.get('suite') ?? ''
  const project = (suite && params.get('project')) || current
  const query = useTestRuns(page, project || undefined, suite || undefined)
  const projects = useProjects()
  const projectKey = (id: number) => projects.data?.items.find((p) => p.id === id)?.key ?? '—'
  return (
    <Card>
      <CardHeader>
        <PageTitle title="Test Runs" />
        <div className="flex flex-wrap items-center justify-between gap-2">
          <CardTitle as="h1" className="text-xl">
            Test Runs
          </CardTitle>
          {(projects.data?.items ?? []).some((p) => can(p.myRole, 'member')) ? (
            <Button asChild variant="outline">
              <Link to="/test-runs/manual">
                <Play /> Start manual run
              </Link>
            </Button>
          ) : null}
        </div>
        {suite ? (
          <p className="text-muted-foreground text-sm" data-testid="suite-filter">
            Runs of suite{' '}
            <span className="font-mono">
              {project ? `${project}/` : ''}
              {suite}
            </span>{' '}
            ·{' '}
            <Link to="/test-runs" className="underline">
              All runs
            </Link>
          </p>
        ) : null}
      </CardHeader>
      <CardContent>
        <QueryState query={query}>
          {(data) => (
            <>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Run</TableHead>
                    <TableHead>Project</TableHead>
                    <TableHead>Verdict</TableHead>
                    <TableHead>Pass rate</TableHead>
                    <TableHead>Test cases</TableHead>
                    <TableHead>Execution</TableHead>
                    <TableHead>Branch</TableHead>
                    <TableHead>Commit</TableHead>
                    <TableHead>Pipeline</TableHead>
                    <TableHead>External run id</TableHead>
                    <TableHead>Created</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.items.length === 0 && (
                    <TableRow>
                      <TableCell colSpan={11} className="text-muted-foreground">
                        {suite
                          ? `No runs of suite ${suite} yet.`
                          : project
                            ? `No test runs in ${project} yet. CI sends JUnit reports to POST /api/v1/ingestion/junit.`
                            : 'No test runs yet. CI sends JUnit reports to POST /api/v1/ingestion/junit.'}
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
                      <TableCell className="font-mono" data-testid="run-project">
                        {projectKey(run.projectId)}
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-wrap gap-1">
                          <VerdictBadge
                            verdict={run.outcome.verdict}
                            running={run.executionStatus === 'running'}
                          />
                          <EditedBadge amendments={run.amendmentCount} />
                          <FlakyBadge count={run.outcome.flaky} />
                          <SuiteBadge suite={run.suite} />
                        </div>
                      </TableCell>
                      <TableCell className="tabular-nums" data-testid="pass-rate">
                        {run.outcome.executed > 0 ? formatPercent(run.outcome.passRate) : '—'}
                        {run.executionStatus === 'running' && run.outcome.executed > 0 ? ' so far' : ''}
                      </TableCell>
                      <TableCell data-testid="outcome-breakdown">
                        {outcomeBreakdown(run.outcome)
                          .split(' · ')
                          .map((part, i) => (
                            <Fragment key={part}>
                              {i > 0 ? ' · ' : null}
                              <span className="whitespace-nowrap">{part}</span>
                            </Fragment>
                          ))}
                        <span className="text-muted-foreground">
                          {' · '}
                          <span className="whitespace-nowrap">{run.expectedCount} expected</span>
                        </span>
                      </TableCell>
                      <TableCell>
                        {run.executionStatus === 'completed' ? (
                          <span className="text-muted-foreground">completed</span>
                        ) : (
                          <ExecutionBadge status={run.executionStatus} />
                        )}
                      </TableCell>
                      <TableCell>{run.branch || '—'}</TableCell>
                      <TableCell className="font-mono">{shortCommit(run.commit)}</TableCell>
                      <TableCell>{run.pipeline || '—'}</TableCell>
                      <TableCell className="font-mono">{run.externalRunId}</TableCell>
                      <TableCell className="whitespace-nowrap">{formatDateTime(run.createdAt)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              <Pagination
                page={data.page}
                totalPages={data.totalPages}
                totalItems={data.totalItems}
                onPageChange={(p, replace) =>
                  setParams(suite ? { suite, page: String(p) } : { page: String(p) }, { replace })
                }
              />
            </>
          )}
        </QueryState>
      </CardContent>
    </Card>
  )
}
