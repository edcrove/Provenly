import { Link } from 'react-router'

import { useIssues } from '@/api/queries'
import { QueryState } from '@/components/QueryState'
import { Badge } from '@/components/ui/badge'
import { verificationLabel, verificationVariant } from '@/lib/issues'
import { requirementRef as issueRef } from '@/lib/requirements'

/** The issues a test case reproduces (on the test case page). */
export function LinkedIssues({ projectKey, testCaseId }: { projectKey: string; testCaseId: number }) {
  const issues = useIssues(projectKey, { testCase: testCaseId })
  return (
    <QueryState query={issues}>
      {(data) =>
        data.items.length === 0 ? (
          <p className="text-muted-foreground text-sm">No issue is linked to this test case.</p>
        ) : (
          <ul className="grid gap-1 text-sm" data-testid="linked-issues">
            {data.items.map((i) => (
              <li key={i.id} className="flex flex-wrap items-center gap-2">
                <Link to={`/issues/${projectKey}/${i.id}`} className="font-mono underline">
                  {issueRef(i)}
                </Link>
                {i.title}
                <Badge variant="outline">{i.state}</Badge>
                <Badge variant={verificationVariant(i.verification.status)}>
                  {verificationLabel(i.verification.status)}
                </Badge>
              </li>
            ))}
          </ul>
        )
      }
    </QueryState>
  )
}
