import { useState, type FormEvent } from 'react'
import { Link } from 'react-router'

import type { TestRunSummary } from '@/api/client'
import { useAmendRun, useTestCase, useUpdateTestCase } from '@/api/queries'
import { ErrorAlert } from '@/components/QueryState'
import { CorrelationBadge } from '@/components/StatusBadge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useProjectRole } from '@/features/projects/useProjectRole'
import { can } from '@/lib/roles'
import { correlationExplanation, diagnosticCount, invalidCorrelations } from '@/lib/status'

function outsideUniverseMessage(n: number): string {
  return n === 1
    ? '1 result points to a test case that was not automated when this run was created, so it is not counted in this summary. If it should be automated, mark it so future runs include it; a maintainer can also include it in this run.'
    : `${n} results point to test cases that were not automated when this run was created, so they are not counted in this summary. If they should be automated, mark them so future runs include them; a maintainer can also include them in this run.`
}

/** Maintainers include the TC in this run's universe, with a reason (DEC-42): the run is then marked as edited. */
function IncludeInRun({ id, testRunId }: { id: number; testRunId: number }) {
  const amend = useAmendRun(testRunId)
  const [reason, setReason] = useState('')
  const submit = (e: FormEvent) => {
    e.preventDefault()
    amend.mutate({ testCaseId: id, reason: reason.trim() })
  }
  return (
    <form
      onSubmit={submit}
      className="flex w-full flex-wrap items-center gap-2"
      aria-label={`Include ${id} in this run`}
    >
      <Input
        aria-label="Why it belongs in this run"
        placeholder="Why it belongs in this run"
        value={reason}
        maxLength={500}
        required
        className="max-w-sm"
        onChange={(e) => setReason(e.target.value)}
      />
      <Button size="sm" variant="outline" type="submit" disabled={amend.isPending}>
        Include in this run
      </Button>
      {amend.error ? <ErrorAlert error={amend.error} title="Could not include the test case" /> : null}
    </form>
  )
}

/** A TC with valid results that was not in this run's snapshot, with shortcuts to mark it automated and, for
 * maintainers, to include it in this run. */
function OutsideUniverseCase({ id, testRunId }: { id: number; testRunId: number }) {
  const tc = useTestCase(id)
  const update = useUpdateTestCase(id)
  const role = useProjectRole(tc.data?.projectKey)
  const canEdit = can(role, 'member')
  return (
    <li className="flex flex-wrap items-center gap-2" data-testid={`outside-${id}`}>
      <Link to={`/test-cases/${id}`} className="font-mono underline">
        {tc.data?.key ?? `#${id}`}
      </Link>
      <span>{tc.data?.title}</span>
      {tc.data?.automated ? (
        <span className="text-muted-foreground">
          Now automated: future runs include it; this run&apos;s snapshot is unchanged.
        </span>
      ) : canEdit ? (
        <Button
          size="sm"
          variant="outline"
          disabled={update.isPending}
          onClick={() => update.mutate({ automated: true })}
        >
          Mark as automated
        </Button>
      ) : null}
      {update.error ? <ErrorAlert error={update.error} title="Could not update the test case" /> : null}
      {can(role, 'maintainer') ? <IncludeInRun id={id} testRunId={testRunId} /> : null}
    </li>
  )
}

/** Results excluded from the summary because their TC-ID is not valid or not in the snapshot. */
export function RunDiagnostics({ summary, testRunId }: { summary: TestRunSummary; testRunId: number }) {
  const { diagnostics } = summary
  return (
    <div className="grid gap-2 text-sm">
      {diagnostics.total === 0 ? (
        <p className="text-muted-foreground">Every result declared a valid TC-ID.</p>
      ) : (
        <ul className="grid gap-2" aria-label="Diagnostics">
          {invalidCorrelations
            .filter((c) => diagnosticCount(diagnostics, c) > 0)
            .map((c) => (
              <li key={c} className="flex items-center gap-2">
                <CorrelationBadge correlation={c} />
                <span data-testid={`diagnostic-${c}`}>{diagnosticCount(diagnostics, c)}</span>
                <span className="text-muted-foreground">{correlationExplanation(c)}</span>
              </li>
            ))}
        </ul>
      )}
      {summary.outsideUniverse > 0 && (
        <div className="grid gap-2 rounded-md border p-3" role="status">
          <p data-testid="outside-universe">{outsideUniverseMessage(summary.outsideUniverse)}</p>
          <ul className="grid gap-2" aria-label="Test cases outside the expected universe">
            {summary.outsideUniverseTestCaseIds.map((id) => (
              <OutsideUniverseCase key={id} id={id} testRunId={testRunId} />
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}
