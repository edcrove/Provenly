import { Link } from 'react-router'

import type { TestRunSummary } from '@/api/client'
import { useIssues } from '@/api/queries'
import { requirementRef as issueRef } from '@/lib/requirements'

/** Splits a run's failures into known issues (a linked open issue) and new failures, for triage. */
export function RunKnownIssues({ projectKey, summary }: { projectKey: string; summary: TestRunSummary }) {
  const issues = useIssues(projectKey, { state: 'open' })
  const failing = summary.testCases.filter((c) => c.status === 'failed' || c.status === 'error')
  if (failing.length === 0 || !issues.data) return null
  const known = failing.map((c) => ({
    c,
    issues: issues.data.items.filter((i) => i.testCaseIds.includes(c.testCaseId)),
  }))
  const fresh = known.filter((k) => k.issues.length === 0)
  return (
    <div className="grid gap-1 text-sm" data-testid="run-known-issues">
      {known
        .filter((k) => k.issues.length > 0)
        .map((k) => (
          <div key={k.c.testCaseId}>
            <span className="font-medium">Known issue: </span>
            <Link to={`/test-cases/${k.c.testCaseId}`} className="font-mono underline">
              {k.c.testCaseKey}
            </Link>{' '}
            fails with{' '}
            {k.issues.map((i, n) => (
              <span key={i.id}>
                {n > 0 && ', '}
                <Link to={`/issues/${projectKey}/${i.id}`} className="font-mono underline">
                  {issueRef(i)}
                </Link>{' '}
                ({i.title})
              </span>
            ))}
          </div>
        ))}
      {fresh.length > 0 ? (
        <div data-testid="run-new-failures">
          <span className="font-medium">New failures (no open issue): </span>
          {fresh.map((k, n) => (
            <span key={k.c.testCaseId}>
              {n > 0 && ', '}
              <Link to={`/test-cases/${k.c.testCaseId}`} className="font-mono underline">
                {k.c.testCaseKey}
              </Link>
            </span>
          ))}
        </div>
      ) : null}
    </div>
  )
}
