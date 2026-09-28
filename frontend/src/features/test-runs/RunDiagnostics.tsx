import type { TestRunSummary } from '@/api/client'
import { CorrelationBadge } from '@/components/StatusBadge'
import { correlationExplanation, invalidCorrelations } from '@/lib/status'

/** Results excluded from the summary because their TC-ID is not valid. */
export function RunDiagnostics({ summary }: { summary: TestRunSummary }) {
  const { diagnostics } = summary
  return (
    <div className="grid gap-2 text-sm">
      {diagnostics.total === 0 ? (
        <p className="text-muted-foreground">Every result declared a valid TC-ID.</p>
      ) : (
        <ul className="grid gap-2" aria-label="Diagnostics">
          {invalidCorrelations
            .filter((c) => diagnostics[c] > 0)
            .map((c) => (
              <li key={c} className="flex items-center gap-2">
                <CorrelationBadge correlation={c} />
                <span data-testid={`diagnostic-${c}`}>{diagnostics[c]}</span>
                <span className="text-muted-foreground">{correlationExplanation(c)}</span>
              </li>
            ))}
        </ul>
      )}
      {summary.outsideUniverse > 0 && (
        <p className="text-muted-foreground" data-testid="outside-universe">
          {summary.outsideUniverse} result(s) point to valid test cases outside this run&apos;s expected
          universe (e.g. not automated when the run was created); they are listed below but not counted in the
          summary.
        </p>
      )}
    </div>
  )
}
