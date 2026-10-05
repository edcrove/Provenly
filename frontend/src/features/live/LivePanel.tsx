import { useEffect } from 'react'
import { Link } from 'react-router'
import { useQueryClient } from '@tanstack/react-query'

import type { TestRun } from '@/api/client'
import { keys, useLiveRun } from '@/api/queries'
import { QueryState } from '@/components/QueryState'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'

const mismatchLabels: Record<string, string> = {
  status_mismatch: 'Live and final status differ',
  live_only: 'Finished live, missing from the report',
  final_only: 'In the report, never seen live',
  started_without_finished: 'Started live, never finished',
  duplicate: 'Finished more than once live',
  invalid_correlation: 'Live event with an unknown TC-ID',
}

const stateVariant = (state: string) =>
  state === 'passed'
    ? 'success'
    : state === 'failed' || state === 'error'
      ? 'destructive'
      : state === 'running'
        ? 'warning'
        : 'outline'

/** A live run: provisional progress while CI streams events, then the reconciliation with the final report. */
export function LivePanel({ run }: { run: TestRun }) {
  const running = run.executionStatus === 'running'
  const live = useLiveRun(run.id, running)
  const qc = useQueryClient()
  const reconciled = live.data !== undefined && live.data.reconciliation !== 'pending'
  useEffect(() => {
    // The final report arrived while the page was open: reload the run, its summary and results.
    if (running && reconciled) void qc.invalidateQueries({ queryKey: keys.testRun(run.id) })
  }, [running, reconciled, qc, run.id])
  return (
    <Card data-testid="live-panel">
      <CardHeader>
        <CardTitle as="h2">Live execution</CardTitle>
        <CardDescription>
          {running
            ? 'Provisional: updated every two seconds from the events CI streams. The final report decides the results.'
            : 'The final report completed this run; its live events were reconciled with it.'}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <QueryState query={live}>
          {(l) => {
            const total = l.waiting + l.running + l.finished
            return (
              <div className="grid gap-4">
                <div className="flex flex-wrap items-center gap-3 text-sm">
                  <span data-testid="live-progress">
                    {running
                      ? `${l.finished} of ${total} finished · ${l.running} running · ${l.waiting} waiting`
                      : `As streamed live: ${l.finished} of ${total} finished · ${l.running} started but never finished · ${l.waiting} never started`}
                  </span>
                  <span className="text-muted-foreground">
                    {l.events} events{l.runFinished ? ' · runner finished' : ''}
                  </span>
                  <Badge
                    data-testid="reconciliation"
                    variant={
                      l.reconciliation === 'consistent'
                        ? 'success'
                        : l.reconciliation === 'mismatch'
                          ? 'destructive'
                          : 'secondary'
                    }
                  >
                    {l.reconciliation === 'pending' ? 'awaiting final report' : l.reconciliation}
                  </Badge>
                </div>
                <div className="bg-muted h-2 overflow-hidden rounded" aria-hidden>
                  <div
                    className="bg-primary h-2"
                    style={{ width: `${total ? Math.round((l.finished * 100) / total) : 0}%` }}
                  />
                </div>
                {running ? (
                  <ul className="flex flex-wrap gap-2 text-sm">
                    {l.testCases.map((c) => (
                      <li key={c.testCaseId} data-testid={`live-${c.testCaseKey}`}>
                        <Badge variant={stateVariant(c.state)}>
                          {c.testCaseKey} · {c.state}
                        </Badge>
                      </li>
                    ))}
                  </ul>
                ) : null}
                {l.mismatches.length > 0 ? (
                  <Table aria-label="Reconciliation mismatches">
                    <TableHeader>
                      <TableRow>
                        <TableHead>Test case</TableHead>
                        <TableHead>Mismatch</TableHead>
                        <TableHead>Live</TableHead>
                        <TableHead>Final report</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {l.mismatches.map((m, i) => (
                        <TableRow key={i} data-testid="mismatch">
                          <TableCell className="font-mono">
                            {m.testCaseId ? (
                              <Link to={`/test-cases/${m.testCaseId}`} className="underline">
                                {m.testCaseKey ?? `#${m.testCaseId}`}
                              </Link>
                            ) : (
                              m.requestedTestCaseId
                            )}
                          </TableCell>
                          <TableCell>{mismatchLabels[m.kind]}</TableCell>
                          <TableCell>{m.liveStatus ?? '—'}</TableCell>
                          <TableCell>{m.finalStatus ?? '—'}</TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                ) : null}
              </div>
            )
          }}
        </QueryState>
      </CardContent>
    </Card>
  )
}
