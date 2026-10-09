import { useState } from 'react'
import { Link } from 'react-router'

import type { Issue, Requirement, TestRun } from '@/api/client'
import { useIssuesPage, useQuality, useRequirementsPage, useTestRuns } from '@/api/queries'
import { PageTitle } from '@/components/PageTitle'
import { QueryState } from '@/components/QueryState'
import { VerdictBadge } from '@/components/StatusBadge'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/select'
import { useCurrentProject } from '@/features/projects/currentProject'
import { executedLabel, formatDateTime, formatPercent, plural } from '@/lib/format'
import { verificationLabel, verificationVariant } from '@/lib/issues'
import { coverageLabel, coverageVariant } from '@/lib/requirements'
import { PrintButton } from '@/components/PrintButton'
import { verdictLabel } from '@/lib/status'
import { ProjectChooser, ScopeLabel } from '@/features/projects/ProjectScope'

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

/** The colour of a run's bar, by verdict: incomplete is amber and striped, never the red of a failure. */
const verdictFill: Record<TestRun['outcome']['verdict'], { className: string; striped?: boolean }> = {
  passed: { className: 'bg-emerald-600' },
  failed: { className: 'bg-red-600' },
  incomplete: { className: 'bg-amber-500', striped: true },
  no_tests: { className: 'bg-muted-foreground/40' },
}

const stripes = {
  backgroundImage: 'repeating-linear-gradient(45deg, rgb(255 255 255 / 0.45) 0 3px, transparent 3px 7px)',
}

/** The legend of the trend: identity is never colour alone (the incomplete swatch is striped). */
function TrendLegend() {
  return (
    <ul className="text-muted-foreground flex flex-wrap gap-x-4 gap-y-1 text-xs" aria-label="Legend">
      {(Object.keys(verdictFill) as TestRun['outcome']['verdict'][]).map((v) => (
        <li key={v} className="flex items-center gap-1.5">
          <span
            className={`inline-block size-3 rounded-sm ${verdictFill[v].className}`}
            style={verdictFill[v].striped ? stripes : undefined}
            aria-hidden
          />
          {verdictLabel(v)}
        </li>
      ))}
    </ul>
  )
}

/**
 * Pass rate of the latest runs, oldest first: one bar per run (height: % of executed that passed, on a 0-100% axis),
 * coloured by its verdict and labelled with its number and local date. An incomplete run at 100% is a tall amber bar,
 * not a red one.
 */
function Trend({ runs }: { runs: TestRun[] }) {
  const ordered = [...runs].reverse()
  if (ordered.length === 0) return <p className="text-muted-foreground text-sm">No runs yet.</p>
  return (
    <div className="grid gap-2">
      <div className="flex gap-2">
        <div
          className="text-muted-foreground flex h-32 flex-col justify-between text-right text-[10px] tabular-nums"
          aria-hidden
        >
          <span className="-translate-y-1/2">100%</span>
          <span>50%</span>
          <span className="translate-y-1/2">0%</span>
        </div>
        <div className="min-w-0 flex-1">
          <div className="relative h-32">
            {/* Recessive grid at 0, 50 and 100%. */}
            {[0, 50, 100].map((y) => (
              <div
                key={y}
                className="border-border absolute inset-x-0 border-t"
                style={{ bottom: `${y}%` }}
                aria-hidden
              />
            ))}
            <ul className="relative flex h-full items-end gap-1" aria-label="Pass rate of the latest runs">
              {ordered.map((r) => {
                // A run that executed nothing has no pass rate: a grey stub, not a 0% bar.
                const rate =
                  r.outcome.executed > 0
                    ? `${formatPercent(r.outcome.passRate)} of executed passed`
                    : 'no pass rate'
                const executed = `executed ${executedLabel(r.outcome.executed, r.expectedCount)}`
                const fill = verdictFill[r.outcome.verdict]
                return (
                  <li key={r.id} className="flex h-full max-w-12 flex-1 items-end">
                    <Link
                      to={`/test-runs/${r.id}`}
                      title={`#${r.id} · ${formatDateTime(r.createdAt)} · ${verdictLabel(r.outcome.verdict)} · ${rate} · ${executed}`}
                      aria-label={`Run #${r.id}: ${runVerdict(r)}, ${rate}, ${executed}`}
                      data-testid={`trend-${r.id}`}
                      className={`min-h-1 w-full rounded-t ${fill.className}`}
                      style={{
                        height: `${Math.max(r.outcome.passRate, 2)}%`,
                        ...(fill.striped ? stripes : {}),
                      }}
                    />
                  </li>
                )
              })}
            </ul>
          </div>
          <ul className="mt-1 flex gap-1" aria-hidden>
            {ordered.map((r) => (
              <li
                key={r.id}
                className="text-muted-foreground max-w-12 min-w-0 flex-1 truncate text-center text-[10px] leading-tight tabular-nums"
                data-testid={`trend-label-${r.id}`}
              >
                #{r.id}
                <br />
                {formatDateTime(r.createdAt).slice(5, 10)}
              </li>
            ))}
          </ul>
        </div>
      </div>
      <TrendLegend />
      <p className="text-muted-foreground text-xs">
        Pass rate (% of executed) of the latest {ordered.length} runs, oldest first; hover a bar for how much
        of the run executed.
      </p>
    </div>
  )
}

/** Counts per status (from the server, over every page), in a fixed order, as badges. */
function Breakdown<S extends string>({
  statuses,
  counts,
  label,
  variant,
  testId,
}: {
  statuses: S[]
  counts: Partial<Record<string, number>>
  label: (s: S) => string
  variant: (s: S) => 'outline' | 'secondary' | 'destructive' | 'warning' | 'success'
  testId: string
}) {
  return (
    <div className="flex flex-wrap gap-2" data-testid={testId}>
      {statuses
        .filter((s) => (counts[s] ?? 0) > 0)
        .map((s) => (
          <Badge key={s} variant={variant(s)}>
            {label(s)}: {counts[s]}
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
  // One item per page: the dashboard only needs the totals and the counts over every page (DEC-78).
  const requirements = useRequirementsPage(project, 1, 1)
  const issues = useIssuesPage(project, {}, 1, 1)
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
          {latest ? (
            <div className="text-sm tabular-nums" data-testid="latest-run-rates">
              Executed {executedLabel(latest.outcome.executed, latest.expectedCount)} · Passed{' '}
              {latest.outcome.executed > 0 ? formatPercent(latest.outcome.passRate) : '—'}
            </div>
          ) : null}
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
                data.totalItems === 0 ? (
                  <p className="text-muted-foreground text-sm">No requirements yet.</p>
                ) : (
                  <Breakdown
                    statuses={coverageOrder}
                    counts={data.coverageCounts}
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
                data.totalItems === 0 ? (
                  <p className="text-muted-foreground text-sm">No issues yet.</p>
                ) : (
                  <Breakdown
                    statuses={verificationOrder}
                    counts={data.verificationCounts}
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
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="grid gap-1">
          <h1 className="text-2xl font-semibold">Dashboard</h1>
          <ScopeLabel />
        </div>
        {project ? <PrintButton what={`dashboard of ${project}`} /> : null}
      </div>
      {!project ? <ProjectChooser what="quality" /> : <ProjectDashboard project={project} />}
    </div>
  )
}
