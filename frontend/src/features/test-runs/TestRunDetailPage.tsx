import { useParams } from 'react-router'

import type { TestRun } from '@/api/client'
import { useTestRun, useTestRunSummary } from '@/api/queries'
import { QueryState } from '@/components/QueryState'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { formatDateTime } from '@/lib/format'
import { positiveInt } from '@/lib/status'

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
    ['Status', run.status],
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
  return (
    <QueryState query={run}>
      {(r) => (
        <div className="grid gap-4">
          <h1 className="text-2xl font-semibold">Test run #{r.id}</h1>
          <Card>
            <CardHeader>
              <CardTitle>Run metadata</CardTitle>
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
                    <CardTitle>Summary</CardTitle>
                  </CardHeader>
                  <CardContent>
                    <RunSummary summary={s} />
                  </CardContent>
                </Card>
                <Card>
                  <CardHeader>
                    <CardTitle>TC-ID diagnostics</CardTitle>
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
              <CardTitle>Results</CardTitle>
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
