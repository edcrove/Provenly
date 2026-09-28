import { Badge } from '@/components/ui/badge'
import { correlationVariant, statusVariant, type Correlation, type SummaryStatus } from '@/lib/status'

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
