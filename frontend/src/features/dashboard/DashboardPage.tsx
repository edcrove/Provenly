import { useState } from 'react'
import { Link } from 'react-router'

import type { Issue, Requirement, TestRun } from '@/api/client'
import { useIssues, useQuality, useRequirements, useTestRuns } from '@/api/queries'
import { PageTitle } from '@/components/PageTitle'
import { QueryState } from '@/components/QueryState'
import { VerdictBadge } from '@/components/StatusBadge'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/select'
import { useCurrentProject } from '@/features/projects/currentProject'
import { formatDateTime, formatPercent, plural } from '@/lib/format'
import { verificationLabel, verificationVariant } from '@/lib/issues'
import { coverageLabel, coverageVariant } from '@/lib/requirements'
import { verdictLabel } from '@/lib/status'

function Stat({
  label,
  value,
  hint,
  testId,
}: {
  label: string
  value: string
  hint?: string
  testId: string
}) {
  return (
    <div className="rounded-md border p-3">
      <div className="text-muted-foreground text-sm">{label}</div>
      <div className="text-2xl font-semibold" data-testid={testId}>
        {value}
      </div>
      {hint ? <div className="text-muted-foreground text-xs">{hint}</div> : null}
    </div>
  )
}

/** The run's verdict in words for screen readers; a running run's verdict is provisional. */
const runVerdict = (r: TestRun) =>
  `${verdictLabel(r.outcome.verdict)}${r.executionStatus === 'running' ? ' so far (running)' : ''}`

/** Pass rate of the latest runs, oldest first: one bar per run, red when its verdict is not passed. */
function Trend({ runs }: { runs: TestRun[] }) {
  const ordered = [...runs].reverse()
  if (ordered.length === 0) return <p className="text-muted-foreground text-sm">No runs yet.</p>
  return (
    <div className="grid gap-2">
      <ul className="flex h-32 items-end gap-1" aria-label="Pass rate of the latest runs">
        {ordered.map((r) => {
          // A run that executed nothing has no pass rate: a grey stub, not a 0% red bar.
          const rate =
            r.outcome.executed > 0
              ? `${formatPercent(r.outcome.passRate)} of executed passed`
              : '0 of 0 executed'
          const colour =
            r.outcome.executed === 0
              ? 'bg-muted-foreground/40'
              : r.outcome.verdict === 'passed'
                ? 'bg-emerald-600'
                : 'bg-red-600'
          return (
            <li key={r.id} className="flex h-full max-w-12 flex-1 items-end">
              <Link
                to={`/test-runs/${r.id}`}
                title={`#${r.id} · ${r.outcome.verdict} · ${rate}`}
                aria-label={`Run #${r.id}: ${runVerdict(r)}, ${rate}`}
                data-testid={`trend-${r.id}`}
                className={`min-h-1 w-full rounded-t ${colour}`}
                style={{ height: `${Math.max(r.outcome.passRate, 2)}%` }}
              />
            </li>
          )
        })}
      </ul>
      <p className="text-muted-foreground text-xs">
        Pass rate (% of executed) of the latest {ordered.length} runs, oldest first. Red: the run did not
        pass; grey: it executed no test case.
      </p>
    </div>
  )
}

/** Counts per status, in a fixed order, as badges. */
function Breakdown<S extends string>({
  statuses,
  items,
  label,
  variant,
  testId,
}: {
  statuses: S[]
  items: S[]
  label: (s: S) => string
  variant: (s: S) => 'outline' | 'secondary' | 'destructive' | 'warning' | 'success'
  testId: string
}) {
  return (
    <div className="flex flex-wrap gap-2" data-testid={testId}>
      {statuses
        .filter((s) => items.includes(s))
        .map((s) => (
          <Badge key={s} variant={variant(s)}>
            {label(s)}: {items.filter((i) => i === s).length}
          </Badge>
        ))}
    </div>
  )
}

const coverageOrder: Requirement['coverage']['status'][] = [
  'failing',
  'partial',
  'not_run',
  'uncovered',
  'passing',
]
const verificationOrder: Issue['verification']['status'][] = [
  'reopen',
  'known_issue',
  'unverified',
  'unlinked',
  'not_reproducible',
  'validated_fixed',
]

function ProjectDashboard({ project }: { project: string }) {
  const [staleDays, setStaleDays] = useState(14)
  const [window, setWindow] = useState(20)
  const runs = useTestRuns(1, project)
  const quality = useQuality(project, staleDays, window)
  const requirements = useRequirements(project)
  const issues = useIssues(project)
  const latest = runs.data?.items[0]
  return (
    <div className="grid gap-4">
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <div className="rounded-md border p-3">
          <div className="text-muted-foreground text-sm">Latest run</div>
          {latest ? (
            <div className="flex items-center gap-2" data-testid="latest-run">
              <Link to={`/test-runs/${latest.id}`} className="text-2xl font-semibold underline">
                #{latest.id}
              </Link>
              <VerdictBadge verdict={latest.outcome.verdict} running={latest.executionStatus === 'running'} />
            </div>
          ) : (
            <div className="text-2xl font-semibold" data-testid="latest-run">
              —
            </div>
          )}
          {/* "—" alone cannot tell loading, a failed read and no runs apart. */}
          <div className="text-muted-foreground text-xs" data-testid="latest-run-hint">
            {latest
              ? formatDateTime(latest.createdAt)
              : runs.isPending
                ? 'Loading…'
                : runs.error
                  ? 'Could not load the runs'
                  : 'No runs yet'}
          </div>
        </div>
        <QueryState query={quality}>
          {(q) => (
            <>
              <Stat
                label="Automation"
                value={formatPercent(q.testCases.automationRate)}
                hint={`${q.testCases.automated} of ${q.testCases.active} active test cases`}
                testId="automation-rate"
              />
              <Stat
                label="Not executed recently"
                value={String(q.execution.neverExecuted + q.execution.stale)}
                hint={`${q.execution.neverExecuted} never, ${q.execution.stale} not in ${q.execution.staleDays} days`}
                testId="stale-count"
              />
              <Stat
                label="Flaky test cases"
                value={String(q.flaky.testCases.length)}
                hint={`passed on a retry in the latest ${q.flaky.window} runs`}
                testId="flaky-count"
              />
            </>
          )}
        </QueryState>
      </div>
      <Card>
        <CardHeader>
          <CardTitle as="h2">Run trend</CardTitle>
        </CardHeader>
        <CardContent>
          <QueryState query={runs}>{(data) => <Trend runs={data.items} />}</QueryState>
        </CardContent>
      </Card>
      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle as="h2">Requirement coverage</CardTitle>
          </CardHeader>
          <CardContent>
            <QueryState query={requirements}>
              {(data) =>
                data.items.length === 0 ? (
                  <p className="text-muted-foreground text-sm">No requirements yet.</p>
                ) : (
                  <Breakdown
                    statuses={coverageOrder}
                    items={data.items.filter((r) => !r.archivedAt).map((r) => r.coverage.status)}
                    label={coverageLabel}
                    variant={coverageVariant}
                    testId="coverage-breakdown"
                  />
                )
              }
            </QueryState>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle as="h2">Issue verification</CardTitle>
          </CardHeader>
          <CardContent>
            <QueryState query={issues}>
              {(data) =>
                data.items.length === 0 ? (
                  <p className="text-muted-foreground text-sm">No issues yet.</p>
                ) : (
                  <Breakdown
                    statuses={verificationOrder}
                    items={data.items.map((i) => i.verification.status)}
                    label={verificationLabel}
                    variant={verificationVariant}
                    testId="verification-breakdown"
                  />
                )
              }
            </QueryState>
          </CardContent>
        </Card>
      </div>
      <QueryState query={quality}>
        {(q) => (
          <div className="grid gap-4 md:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle as="h2">Not executed recently</CardTitle>
                <CardDescription className="flex flex-wrap items-center gap-2">
                  <span>By execution start date.</span>
                  <Label htmlFor="stale-days">Older than</Label>
                  <NativeSelect
                    id="stale-days"
                    value={staleDays}
                    onChange={(e) => setStaleDays(Number(e.target.value))}
                  >
                    {[7, 14, 30, 90].map((d) => (
                      <option key={d} value={d}>
                        {d} days
                      </option>
                    ))}
                  </NativeSelect>
                </CardDescription>
              </CardHeader>
              <CardContent>
                {q.execution.testCases.length === 0 ? (
                  <p className="text-muted-foreground text-sm">Every active test case ran recently.</p>
                ) : (
                  <ul className="grid gap-1 text-sm" data-testid="stale-cases">
                    {q.execution.testCases.map((c) => (
                      <li key={c.testCaseId}>
                        <Link to={`/test-cases/${c.testCaseId}`} className="font-mono underline">
                          {c.testCaseKey}
                        </Link>{' '}
                        <span className="text-muted-foreground">
                          {c.lastExecutedAt ? `last ${formatDateTime(c.lastExecutedAt)}` : 'never executed'}
                        </span>
                      </li>
                    ))}
                  </ul>
                )}
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle as="h2">Flaky test cases</CardTitle>
                <CardDescription className="flex items-center gap-2">
                  <Label htmlFor="flaky-window">Over the latest</Label>
                  <NativeSelect
                    id="flaky-window"
                    value={window}
                    onChange={(e) => setWindow(Number(e.target.value))}
                  >
                    {[10, 20, 50].map((n) => (
                      <option key={n} value={n}>
                        {n} runs
                      </option>
                    ))}
                  </NativeSelect>
                </CardDescription>
              </CardHeader>
              <CardContent>
                {q.flaky.testCases.length === 0 ? (
                  <p className="text-muted-foreground text-sm">No flaky test cases.</p>
                ) : (
                  <ul className="grid gap-1 text-sm" data-testid="flaky-cases">
                    {q.flaky.testCases.map((c) => (
                      <li key={c.testCaseId}>
                        <Link to={`/test-cases/${c.testCaseId}`} className="font-mono underline">
                          {c.testCaseKey}
                        </Link>{' '}
                        <span className="text-muted-foreground">flaky in {plural(c.runs, 'run')}</span>
                      </li>
                    ))}
                  </ul>
                )}
              </CardContent>
            </Card>
          </div>
        )}
      </QueryState>
    </div>
  )
}

/** The current project's quality at a glance: latest run, trend, automation, stale and flaky tests, coverage, issues. */
export function DashboardPage() {
  const { project } = useCurrentProject()
  return (
    <div className="grid gap-4">
      <PageTitle title="Dashboard" />
      <h1 className="text-2xl font-semibold">Dashboard</h1>
      {!project ? (
        <p className="text-muted-foreground text-sm">Choose a project (top right) to see its quality.</p>
      ) : (
        <ProjectDashboard project={project} />
      )}
    </div>
  )
}
