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
