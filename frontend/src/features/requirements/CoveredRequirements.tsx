import { Link } from 'react-router'

import { useRequirements } from '@/api/queries'
import { QueryState } from '@/components/QueryState'
import { Badge } from '@/components/ui/badge'
import { coverageLabel, coverageVariant, requirementRef } from '@/lib/requirements'

/** The requirements a test case covers (on the test case page). */
export function CoveredRequirements({ projectKey, testCaseId }: { projectKey: string; testCaseId: number }) {
  const requirements = useRequirements(projectKey, testCaseId)
  return (
    <QueryState query={requirements}>
      {(data) =>
        data.items.length === 0 ? (
          <p className="text-muted-foreground text-sm">This test case covers no requirement.</p>
        ) : (
          <ul className="grid gap-1 text-sm" data-testid="covered-requirements">
            {data.items.map((r) => (
              <li key={r.id} className="flex flex-wrap items-center gap-2">
                <Link to={`/requirements/${projectKey}/${r.id}`} className="font-mono underline">
                  {requirementRef(r)}
                </Link>
                {r.title}
                <Badge variant={coverageVariant(r.coverage.status)}>{coverageLabel(r.coverage.status)}</Badge>
              </li>
            ))}
          </ul>
        )
      }
    </QueryState>
  )
}
