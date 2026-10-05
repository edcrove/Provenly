import { Link } from 'react-router'

import { useRunAmendments } from '@/api/queries'
import { QueryState } from '@/components/QueryState'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { formatDateTime } from '@/lib/format'

/** Who included which TC-IDs in the run's universe after its creation, when and why (DEC-42). */
export function RunAmendments({ testRunId, snapshotTotal }: { testRunId: number; snapshotTotal: number }) {
  const amendments = useRunAmendments(testRunId)
  return (
    <Card data-testid="amendments">
      <CardHeader>
        <CardTitle as="h2">Edited after creation</CardTitle>
        <CardDescription>
          The snapshot frozen when CI reported this run had {snapshotTotal} test case
          {snapshotTotal === 1 ? '' : 's'}. These were included afterwards and are counted in the summary.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <QueryState query={amendments}>
          {(data) => (
            <ul className="grid gap-2 text-sm" aria-label="Amendments">
              {data.items.map((a) => (
                <li key={a.id} className="flex flex-wrap gap-x-2">
                  <Link to={`/test-cases/${a.testCaseId}`} className="font-mono underline">
                    {a.testCaseKey}
                  </Link>
                  <span>
                    included by <span className="font-mono">{a.amendedByUsername}</span> on{' '}
                    {formatDateTime(a.createdAt)}:
                  </span>
                  <span className="text-muted-foreground break-all">{a.reason}</span>
                </li>
              ))}
            </ul>
          )}
        </QueryState>
      </CardContent>
    </Card>
  )
}
