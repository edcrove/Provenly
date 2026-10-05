import { Link } from 'react-router'

import type { TestRunSummary } from '@/api/client'
import { useTestCase, useUpdateTestCase } from '@/api/queries'
import { ErrorAlert } from '@/components/QueryState'
import { CorrelationBadge } from '@/components/StatusBadge'
import { Button } from '@/components/ui/button'
import { useProjectRole } from '@/features/projects/useProjectRole'
import { can } from '@/lib/roles'
import { correlationExplanation, diagnosticCount, invalidCorrelations } from '@/lib/status'

function outsideUniverseMessage(n: number): string {
  return n === 1
    ? '1 result points to a test case that was not automated when this run was created, so it is not counted in this summary. If it should be automated, mark it so future runs include it.'
    : `${n} results point to test cases that were not automated when this run was created, so they are not counted in this summary. If they should be automated, mark them so future runs include them.`
}

/** A TC with valid results that was not in this run's snapshot, with a shortcut to mark it automated. */
function OutsideUniverseCase({ id }: { id: number }) {
  const tc = useTestCase(id)
  const update = useUpdateTestCase(id)
  const canEdit = can(useProjectRole(tc.data?.projectKey), 'member')
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
    </li>
  )
}

/** Results excluded from the summary because their TC-ID is not valid or not in the snapshot. */
export function RunDiagnostics({ summary }: { summary: TestRunSummary }) {
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
              <OutsideUniverseCase key={id} id={id} />
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}
