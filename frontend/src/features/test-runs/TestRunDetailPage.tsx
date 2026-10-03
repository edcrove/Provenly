import { useParams } from 'react-router'

import type { TestRun } from '@/api/client'
import { useTestRun, useTestRunSummary } from '@/api/queries'
import { NotFoundPage } from '@/app/NotFoundPage'
import { QueryState } from '@/components/QueryState'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { ExecutionBadge, VerdictBadge } from '@/components/StatusBadge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { PageTitle } from '@/components/PageTitle'
import { formatDateTime, formatPercent } from '@/lib/format'
import { isInterruptedRun, positiveInt } from '@/lib/status'

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
          <dd className="font-mono break-all">{v}</dd>
        </div>
      ))}
    </dl>
  )
}

export function TestRunDetailPage() {
  const { testRunId } = useParams()
  const id = positiveInt(testRunId, 0)
  const run = useTestRun(id)
  const summary = useTestRunSummary(id)
  if (!id) return <NotFoundPage />
  return (
    <QueryState page query={run}>
      {(r) => (
        <div className="grid gap-4">
          <div className="flex flex-wrap items-center gap-3">
            <PageTitle title={`Test run #${r.id}`} />
            <h1 className="text-2xl font-semibold">Test run #{r.id}</h1>
            <VerdictBadge verdict={r.outcome.verdict} />
            {isInterruptedRun(r.executionStatus) ? <ExecutionBadge status={r.executionStatus} /> : null}
            <span className="text-muted-foreground text-sm" data-testid="run-pass-rate">
              {r.outcome.executed > 0
                ? `${formatPercent(r.outcome.passRate)} of executed passed`
                : 'No test case executed'}
            </span>
          </div>
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
                    <RunSummary summary={s} />
                  </CardContent>
                </Card>
                <Card>
                  <CardHeader>
                    <CardTitle as="h2">TC-ID diagnostics</CardTitle>
                    <CardDescription>Excluded from the universe and the percentages.</CardDescription>
                  </CardHeader>
                  <CardContent>
                    <RunDiagnostics summary={s} />
                  </CardContent>
                </Card>
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
              <RunResults testRunId={r.id} />
            </CardContent>
          </Card>
        </div>
      )}
    </QueryState>
  )
}
