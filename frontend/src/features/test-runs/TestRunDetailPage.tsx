import { useParams } from 'react-router'

import type { TestRun } from '@/api/client'
import { useProjects, useTestRun, useTestRunSummary } from '@/api/queries'
import { NotFoundPage } from '@/app/NotFoundPage'
import { QueryState } from '@/components/QueryState'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { EditedBadge, ExecutionBadge, FlakyBadge, SuiteBadge, VerdictBadge } from '@/components/StatusBadge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { PageTitle } from '@/components/PageTitle'
import { formatDateTime, formatPercent } from '@/lib/format'
import { can } from '@/lib/roles'
import { isInterruptedRun, positiveInt } from '@/lib/status'
import { RunKnownIssues } from '@/features/issues/RunKnownIssues'
import { LivePanel } from '@/features/live/LivePanel'
import { ManualExecution } from '@/features/manual/ManualExecution'

import { RunAmendments } from './RunAmendments'
import { RunDiagnostics } from './RunDiagnostics'
import { RunParseErrors } from './RunParseErrors'
import { RunResults } from './RunResults'
import { RunSummary } from './RunSummary'

function Metadata({ run }: { run: TestRun }) {
  const rows: [string, string][] = [
    ['External run id', run.externalRunId],
    ['Provider', run.provider],
    ['Run id', run.providerRunId],
    ['Attempt', String(run.runAttempt)],
    ['Pipeline', run.pipeline || '—'],
    ['Branch', run.branch || '—'],
    ['Commit', run.commit || '—'],
    ['Execution status', run.executionStatus],
    ['Created', formatDateTime(run.createdAt)],
    ['Started', formatDateTime(run.startedAt)],
    ['Completed', formatDateTime(run.completedAt)],
  ]
  return (
    <dl className="grid grid-cols-2 gap-x-6 gap-y-1 text-sm md:grid-cols-4">
      {rows.map(([k, v]) => (
        <div key={k}>
          <dt className="text-muted-foreground">{k}</dt>
          <dd className="font-mono wrap-anywhere">{v}</dd>
        </div>
      ))}
    </dl>
  )
}

type Shards = NonNullable<TestRun['shards']>

/** How far a sharded run got: the shards received, and the ones it waits for or that never arrived. */
function ShardsProgress({ shards, running }: { shards: Shards; running: boolean }) {
  const missing = shards.missing.join(', ')
  return (
    <p className="text-sm" data-testid="run-shards">
      <span className="font-medium">
        {shards.received.length} of {shards.total} shards received
      </span>
      {shards.missing.length === 0
        ? null
        : running
          ? ` · waiting for shard${shards.missing.length > 1 ? 's' : ''} ${missing}`
          : ` · shard${shards.missing.length > 1 ? 's' : ''} ${missing} never arrived`}
    </p>
  )
}

export function TestRunDetailPage() {
  const { testRunId } = useParams()
  const id = positiveInt(testRunId, 0)
  const run = useTestRun(id)
  const running = run.data?.executionStatus === 'running'
  const summary = useTestRunSummary(id, running)
  const projects = useProjects().data?.items ?? []
  if (!id) return <NotFoundPage />
  return (
    <QueryState page query={run}>
      {(r) => (
        <div className="grid gap-4">
          <div className="flex flex-wrap items-center gap-3">
            <PageTitle title={`Test run #${r.id}`} />
            <h1 className="text-2xl font-semibold">Test run #{r.id}</h1>
            <VerdictBadge verdict={r.outcome.verdict} running={r.executionStatus === 'running'} />
            <EditedBadge amendments={r.amendmentCount} />
            <FlakyBadge count={r.outcome.flaky} />
            <SuiteBadge suite={r.suite} />
            {isInterruptedRun(r.executionStatus) || r.executionStatus === 'running' ? (
              <ExecutionBadge status={r.executionStatus} />
            ) : null}
            {r.mode !== 'batch' ? <Badge variant="outline">{r.mode}</Badge> : null}
            <span className="text-muted-foreground text-sm" data-testid="run-pass-rate">
              {r.outcome.executed > 0
                ? `${formatPercent(r.outcome.passRate)} of executed passed${running ? ' so far' : ''}`
                : 'No test case executed'}
            </span>
          </div>
          {r.shards ? <ShardsProgress shards={r.shards} running={running} /> : null}
          {isInterruptedRun(r.executionStatus) ? (
            <Alert variant="destructive" data-testid="interrupted-run">
              <AlertTitle>
                The CI execution {r.executionStatus === 'interrupted' ? 'was interrupted' : 'was cancelled'}
              </AlertTitle>
              <AlertDescription>
                The report may be incomplete: untested test cases may simply not have run.
              </AlertDescription>
            </Alert>
          ) : null}
          <Card>
            <CardHeader>
              <CardTitle as="h2">Run metadata</CardTitle>
            </CardHeader>
            <CardContent>
              <Metadata run={r} />
            </CardContent>
          </Card>
          <QueryState query={summary}>
            {(s) => (
              <>
                <Card>
                  <CardHeader>
                    <CardTitle as="h2">Summary</CardTitle>
                  </CardHeader>
                  <CardContent>
                    <RunSummary summary={s} running={running} />
                    <RunKnownIssues
                      projectKey={projects.find((p) => p.id === r.projectId)?.key ?? ''}
                      summary={s}
                    />
                  </CardContent>
                </Card>
                <Card>
                  <CardHeader>
                    <CardTitle as="h2">TC-ID diagnostics</CardTitle>
                    <CardDescription>Excluded from the universe and the percentages.</CardDescription>
                  </CardHeader>
                  <CardContent>
                    <RunDiagnostics summary={s} testRunId={r.id} suite={r.suite?.name} />
                  </CardContent>
                </Card>
                {r.mode === 'live' ? <LivePanel run={r} /> : null}
                {r.mode === 'manual' &&
                r.executionStatus === 'running' &&
                can(projects.find((p) => p.id === r.projectId)?.myRole, 'member') ? (
                  <ManualExecution run={r} summary={s} />
                ) : null}
                {r.amendmentCount > 0 ? (
                  <RunAmendments testRunId={r.id} snapshotTotal={s.snapshotTotal} />
                ) : null}
              </>
            )}
          </QueryState>
          <RunParseErrors testRunId={r.id} />
          <Card>
            <CardHeader>
              <CardTitle as="h2">Results</CardTitle>
              <CardDescription>Every individual result as ingested (e.g. one per browser).</CardDescription>
            </CardHeader>
            <CardContent>
              <RunResults testRunId={r.id} shards={r.shards?.total} />
            </CardContent>
          </Card>
        </div>
      )}
    </QueryState>
  )
}
