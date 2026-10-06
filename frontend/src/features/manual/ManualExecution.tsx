import { useState } from 'react'
import { Link } from 'react-router'

import type { TestRun, TestRunSummary } from '@/api/client'
import { useManualRun } from '@/api/queries'
import { ErrorAlert } from '@/components/QueryState'
import { StatusBadge } from '@/components/StatusBadge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'

type Outcome = TestRunSummary['testCases'][number]

const actions = [
  { status: 'passed', label: 'Pass' },
  { status: 'failed', label: 'Fail' },
  { status: 'error', label: 'Blocked' },
  { status: 'skipped', label: 'Skip' },
] as const

/** One expected test case of a running manual run: its current result and the buttons that record the next one. */
function CaseRow({ runId, outcome }: { runId: number; outcome: Outcome }) {
  const { record } = useManualRun(runId)
  const [note, setNote] = useState('')
  const [step, setStep] = useState('')
  const key = outcome.testCaseKey ?? String(outcome.testCaseId)
  return (
    <TableRow data-testid={`manual-${key}`}>
      <TableCell className="font-mono">
        <Link to={`/test-cases/${outcome.testCaseId}`} target="_blank" className="underline">
          {key}
        </Link>
      </TableCell>
      <TableCell>
        <StatusBadge status={outcome.status} />
      </TableCell>
      <TableCell>
        <div className="grid gap-1">
          <div className="flex flex-wrap gap-1">
            <Input
              aria-label={`Note for ${key}`}
              placeholder="Note (optional)"
              className="h-8 w-48"
              value={note}
              onChange={(e) => setNote(e.target.value)}
            />
            <Input
              aria-label={`Failed step of ${key}`}
              placeholder="Step"
              type="number"
              min={1}
              className="h-8 w-20"
              value={step}
              onChange={(e) => setStep(e.target.value)}
            />
          </div>
          {record.error ? <ErrorAlert error={record.error} title={`Could not record ${key}`} /> : null}
        </div>
      </TableCell>
      <TableCell>
        <div className="flex flex-wrap gap-1">
          {actions.map((a) => (
            <Button
              key={a.status}
              size="sm"
              variant={a.status === 'passed' ? 'default' : 'outline'}
              disabled={record.isPending}
              onClick={() =>
                record.mutate(
                  {
                    testCaseId: outcome.testCaseId,
                    status: a.status,
                    ...(note.trim() ? { note: note.trim() } : {}),
                    ...((a.status === 'failed' || a.status === 'error') && step
                      ? { failedStep: Number(step) }
                      : {}),
                  },
                  {
                    onSuccess: () => {
                      setNote('')
                      setStep('')
                    },
                  },
                )
              }
            >
              {a.label}
            </Button>
          ))}
        </div>
      </TableCell>
    </TableRow>
  )
}

/** The panel of a running manual run: record each expected test case, then complete (or cancel) the run. */
export function ManualExecution({ run, summary }: { run: TestRun; summary: TestRunSummary }) {
  const { finish } = useManualRun(run.id)
  const left = summary.testCases.filter((c) => c.status === 'untested').length
  return (
    <Card data-testid="manual-execution">
      <CardHeader>
        <CardTitle as="h2">Manual execution</CardTitle>
        <CardDescription>
          Started by {run.startedBy ?? '—'}. Record the result of each test case (recording it again is a
          re-test: the last result counts).{' '}
          {left === 0 ? 'Every test case has a result.' : `${left} still untested.`}
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>TC-ID</TableHead>
              <TableHead>Result</TableHead>
              <TableHead>Note and failed step</TableHead>
              <TableHead>Record</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {summary.testCases.length === 0 && (
              <TableRow>
                <TableCell colSpan={4} className="text-muted-foreground">
                  This run expects no test case.
                </TableCell>
              </TableRow>
            )}
            {summary.testCases.map((c) => (
              <CaseRow key={c.testCaseId} runId={run.id} outcome={c} />
            ))}
          </TableBody>
        </Table>
        {finish.error ? <ErrorAlert error={finish.error} title="Could not finish the run" /> : null}
        <div className="flex flex-wrap gap-2">
          <Button disabled={finish.isPending} onClick={() => finish.mutate('completed')}>
            Complete run
          </Button>
          <Button variant="outline" disabled={finish.isPending} onClick={() => finish.mutate('cancelled')}>
            Cancel run
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
