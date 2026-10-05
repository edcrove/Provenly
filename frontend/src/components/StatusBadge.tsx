import { Badge } from '@/components/ui/badge'
import {
  correlationVariant,
  executionVariant,
  statusVariant,
  verdictLabel,
  verdictVariant,
  type Correlation,
  type ExecutionStatus,
  type SummaryStatus,
  type Verdict,
} from '@/lib/status'

export function StatusBadge({ status }: { status: SummaryStatus }) {
  return (
    <Badge variant={statusVariant(status)} data-testid="status-badge">
      {status}
    </Badge>
  )
}

export function CorrelationBadge({ correlation }: { correlation: Correlation }) {
  return <Badge variant={correlationVariant(correlation)}>{correlation}</Badge>
}

/** Test outcome of a run: passed, failed, incomplete or no tests. */
export function VerdictBadge({ verdict }: { verdict: Verdict }) {
  return (
    <Badge variant={verdictVariant(verdict)} data-testid="verdict-badge">
      {verdictLabel(verdict)}
    </Badge>
  )
}

/** How the CI execution ended: completed, interrupted or cancelled. */
export function ExecutionBadge({ status }: { status: ExecutionStatus }) {
  return (
    <Badge variant={executionVariant(status)} data-testid="execution-badge">
      {status}
    </Badge>
  )
}

/** Marks a run whose universe was amended after its creation (DEC-42), so its numbers are not mistaken for the
 * original snapshot's. */
export function EditedBadge({ amendments }: { amendments: number }) {
  if (amendments === 0) return null
  return (
    <Badge variant="outline" data-testid="edited-badge" title={`${amendments} amendment(s) after creation`}>
      edited
    </Badge>
  )
}

/** Which attempt of its test a result was (D1); "retried" when a later attempt superseded it. */
export function AttemptBadge({ attempt, retried }: { attempt: number; retried: boolean }) {
  if (attempt === 1 && !retried) return null
  return (
    <Badge
      variant="outline"
      data-testid="attempt-badge"
      title={
        retried
          ? 'A later attempt of this test exists: this is not its logical result'
          : 'The last attempt of this test'
      }
    >
      attempt {attempt}
      {retried ? ' · retried' : ''}
    </Badge>
  )
}

/** TC-IDs that passed only on a retry (D1): counted as passed, flagged as flaky. */
export function FlakyBadge({ count }: { count: number }) {
  if (count === 0) return null
  return (
    <Badge variant="outline" data-testid="flaky-badge" title="Passed only on a retry after failed attempts">
      {count} flaky
    </Badge>
  )
}
