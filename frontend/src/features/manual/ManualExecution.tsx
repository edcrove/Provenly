import { useState } from 'react'
import { Link } from 'react-router'

import type { TestRun, TestRunSummary } from '@/api/client'
import { useManualRun, useTestCase, useTestSteps } from '@/api/queries'
import { InlineConfirm } from '@/components/InlineConfirm'
import { ErrorAlert } from '@/components/QueryState'
import { StatusBadge } from '@/components/StatusBadge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { plural } from '@/lib/format'

type Outcome = TestRunSummary['testCases'][number]

const actions = [
  { status: 'passed', label: 'Pass' },
  { status: 'failed', label: 'Fail' },
  { status: 'error', label: 'Blocked' },
  { status: 'skipped', label: 'Skip' },
] as const

/** A row's unsaved note and failed step, kept for this tab so a reload or a detour does not lose them. */
interface Draft {
  note: string
  step: string
}
const draftKey = (runId: number, testCaseId: number) => `provenly.manual.${runId}.${testCaseId}`

function loadDraft(key: string): Draft {
  try {
    const d = JSON.parse(globalThis.sessionStorage?.getItem(key) ?? 'null') as Partial<Draft> | null
    return {
      note: typeof d?.note === 'string' ? d.note : '',
      step: typeof d?.step === 'string' ? d.step : '',
    }
  } catch {
    return { note: '', step: '' }
  }
}

function saveDraft(key: string, d: Draft) {
  try {
    if (d.note || d.step) globalThis.sessionStorage?.setItem(key, JSON.stringify(d))
    else globalThis.sessionStorage?.removeItem(key)
  } catch {
    // Not kept; the row still works.
  }
}

/**
 * One expected test case of a running manual run: what it is (title, steps on demand), its current result and the
 * buttons that record the next one. The failed step is picked from the test case's own steps.
 */
function CaseRow({ runId, outcome }: { runId: number; outcome: Outcome }) {
  const { record } = useManualRun(runId)
  const storeKey = draftKey(runId, outcome.testCaseId)
  const [draft, setDraftState] = useState<Draft>(() => loadDraft(storeKey))
  const setDraft = (d: Draft) => {
    setDraftState(d)
    saveDraft(storeKey, d)
  }
  const { note, step } = draft
  const [saved, setSaved] = useState<string | null>(null)
  const title = useTestCase(outcome.testCaseId).data?.title
  const steps = useTestSteps(outcome.testCaseId).data?.items ?? []
  const key = outcome.testCaseKey ?? String(outcome.testCaseId)
  // A failed step means the test failed or was blocked: never drop it silently on Pass or Skip.
  const stepOnly = step.trim() !== ''
  return (
    <TableRow data-testid={`manual-${key}`}>
      <TableCell className="max-w-80 align-top">
        <Link
          to={`/test-cases/${outcome.testCaseId}`}
          target="_blank"
          rel="noopener"
          className="font-mono underline"
        >
          {key}
          <span aria-hidden="true"> ↗</span>
          <span className="sr-only"> (opens in a new tab)</span>
        </Link>
        {title ? <div className="text-sm break-words">{title}</div> : null}
        {steps.length > 0 ? (
          <details className="mt-1 text-xs">
            <summary className="text-muted-foreground cursor-pointer">{plural(steps.length, 'step')}</summary>
            <ol className="mt-1 grid list-decimal gap-1 pl-4" data-testid={`steps-${key}`}>
              {steps.map((st) => (
                <li key={st.id} className="break-words">
                  {st.action}
                  {st.expectedResult ? (
                    <span className="text-muted-foreground"> → {st.expectedResult}</span>
                  ) : null}
                </li>
              ))}
            </ol>
          </details>
        ) : null}
      </TableCell>
      <TableCell className="align-top">
        <StatusBadge status={outcome.status} manual />
      </TableCell>
      <TableCell className="align-top">
        <div className="grid gap-1">
          <div className="flex flex-wrap gap-1">
            <Input
              aria-label={`Note for ${key}`}
              placeholder="Note (optional)"
              className="h-8 w-48"
              value={note}
              onChange={(e) => setDraft({ note: e.target.value, step })}
            />
            {steps.length > 0 ? (
              <NativeSelect
                aria-label={`Failed step of ${key}`}
                className="h-8 w-40"
                value={step}
                onChange={(e) => setDraft({ note, step: e.target.value })}
              >
                <option value="">No failed step</option>
                {steps.map((st) => (
                  <option key={st.id} value={String(st.position)}>
                    {st.position}. {st.action}
                  </option>
                ))}
              </NativeSelect>
            ) : null}
          </div>
          {stepOnly ? (
            <p className="text-muted-foreground text-xs" data-testid={`step-hint-${key}`}>
              A failed step goes with Fail or Blocked; clear it to pass or skip.
            </p>
          ) : null}
          {record.error ? (
            <ErrorAlert error={record.error} title={`Could not record ${key}`} />
          ) : saved ? (
            <p role="status" className="text-muted-foreground text-xs" data-testid={`saved-${key}`}>
              {saved}
            </p>
          ) : null}
        </div>
      </TableCell>
      <TableCell className="align-top">
        <div className="flex flex-wrap gap-1">
          {actions.map((a) => (
            <Button
              key={a.status}
              size="sm"
              // Only the recorded result is highlighted, so a row tells at a glance what was recorded.
              variant={a.status === outcome.status ? 'default' : 'outline'}
              aria-pressed={a.status === outcome.status}
              disabled={record.isPending || (stepOnly && (a.status === 'passed' || a.status === 'skipped'))}
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
                      const failedStep =
                        (a.status === 'failed' || a.status === 'error') && step ? ` at step ${step}` : ''
                      setSaved(`Saved · ${a.label === 'Blocked' ? 'blocked' : a.status}${failedStep}`)
                      setDraft({ note: '', step: '' })
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
              <TableHead>Test case</TableHead>
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
          {/* Finishing cannot be undone: completing with untested test cases and cancelling ask first. */}
          <InlineConfirm
            label="Confirm completing the run"
            question={`${plural(left, 'test case')} ${left === 1 ? 'is' : 'are'} still untested. Complete anyway?`}
            confirmLabel="Complete anyway"
            pending={finish.isPending}
            onConfirm={(close) => finish.mutate('completed', { onSettled: close })}
            trigger={(open) => (
              <Button
                disabled={finish.isPending}
                onClick={() => (left > 0 ? open() : finish.mutate('completed'))}
              >
                Complete run
              </Button>
            )}
          />
          <InlineConfirm
            label="Confirm cancelling the run"
            question={`Cancel this run? Recorded results are kept${left > 0 ? `; ${left} untested stay untested` : ''}.`}
            confirmLabel="Cancel run"
            dismissLabel="Keep the run"
            pending={finish.isPending}
            onConfirm={(close) => finish.mutate('cancelled', { onSettled: close })}
            trigger={(open) => (
              <Button variant="outline" disabled={finish.isPending} onClick={open}>
                Cancel run…
              </Button>
            )}
          />
        </div>
      </CardContent>
    </Card>
  )
}
