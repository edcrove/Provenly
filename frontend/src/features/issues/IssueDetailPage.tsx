import { useState } from 'react'
import { Link, useParams } from 'react-router'

import type { Issue } from '@/api/client'
import { useIssue, useIssueMutations, useTestCases } from '@/api/queries'
import { PageTitle } from '@/components/PageTitle'
import { ErrorAlert, QueryState } from '@/components/QueryState'
import { StatusBadge } from '@/components/StatusBadge'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { NativeSelect } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useProjectRole } from '@/features/projects/useProjectRole'
import { formatDateTime } from '@/lib/format'
import { verificationLabel, verificationVariant } from '@/lib/issues'
import { requirementRef as issueRef } from '@/lib/requirements'
import { can } from '@/lib/roles'
import { positiveInt } from '@/lib/status'

function LinkedTests({ issue, projectKey, edit }: { issue: Issue; projectKey: string; edit: boolean }) {
  const m = useIssueMutations(projectKey)
  const cases = useTestCases(1, { project: projectKey, pageSize: 100 })
  const [picked, setPicked] = useState('')
  const keyOf = (id: number) => cases.data?.items.find((tc) => tc.id === id)?.key ?? `#${id}`
  const candidates = (cases.data?.items ?? []).filter((tc) => !issue.testCaseIds.includes(tc.id))
  return (
    <div className="grid gap-3">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Test case</TableHead>
            <TableHead>Evidence</TableHead>
            <TableHead>Verification</TableHead>
            {edit ? <TableHead /> : null}
          </TableRow>
        </TableHeader>
        <TableBody>
          {issue.verification.testCases.length === 0 && (
            <TableRow>
              <TableCell colSpan={4} className="text-muted-foreground">
                No test case reproduces this issue yet.
              </TableCell>
            </TableRow>
          )}
          {issue.verification.testCases.map((c) => (
            <TableRow key={c.testCaseId} data-testid={`reproducing-${c.testCaseId}`}>
              <TableCell className="font-mono">
                <Link to={`/test-cases/${c.testCaseId}`} className="underline">
                  {keyOf(c.testCaseId)}
                </Link>
              </TableCell>
              <TableCell className="flex flex-wrap items-center gap-2">
                {c.evidence ? (
                  <>
                    <StatusBadge status={c.evidence} />
                    <Link to={`/test-runs/${c.evidenceRunId}`} className="text-sm underline">
                      run #{c.evidenceRunId}
                    </Link>
                  </>
                ) : (
                  <span className="text-muted-foreground">no conclusive result</span>
                )}
                {c.latestInconclusive ? (
                  <span className="text-muted-foreground text-xs">latest run skipped it</span>
                ) : null}
              </TableCell>
              <TableCell>
                <Badge variant={verificationVariant(c.status)}>{verificationLabel(c.status)}</Badge>
              </TableCell>
              {edit ? (
                <TableCell>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={m.link.isPending}
                    onClick={() =>
                      m.link.mutate({
                        issueId: issue.id,
                        testCaseIds: issue.testCaseIds.filter((id) => id !== c.testCaseId),
                      })
                    }
                  >
                    Unlink
                  </Button>
                </TableCell>
              ) : null}
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {edit ? (
        <div className="flex flex-wrap items-center gap-2">
          <NativeSelect
            aria-label="Test case to link"
            value={picked}
            onChange={(e) => setPicked(e.target.value)}
          >
            <option value="">Choose a test case…</option>
            {candidates.map((tc) => (
              <option key={tc.id} value={tc.id}>
                {tc.key} · {tc.title}
              </option>
            ))}
          </NativeSelect>
          <Button
            size="sm"
            disabled={!picked || m.link.isPending}
            onClick={() =>
              m.link.mutate(
                { issueId: issue.id, testCaseIds: [...issue.testCaseIds, Number(picked)] },
                { onSuccess: () => setPicked('') },
              )
            }
          >
            Link test case
          </Button>
        </div>
      ) : null}
      {m.link.error ? <ErrorAlert error={m.link.error} title="Could not change the linked tests" /> : null}
    </div>
  )
}

/** One issue: its state, the tests that reproduce it and what their latest conclusive results say. */
export function IssueDetailPage() {
  const { projectKey = '', issueId } = useParams()
  const id = positiveInt(issueId, 0)
  const issue = useIssue(projectKey, id)
  const m = useIssueMutations(projectKey)
  const edit = can(useProjectRole(projectKey), 'member')
  return (
    <QueryState page query={issue}>
      {(i) => (
        <Card>
          <CardHeader>
            <PageTitle title={`${issueRef(i)} · ${i.title}`} />
            <CardTitle as="h1" className="flex flex-wrap items-center gap-2 text-xl">
              <span className="font-mono">{issueRef(i)}</span> · {i.title}
              <Badge variant="outline" data-testid="issue-state">
                {i.state}
              </Badge>
              <Badge variant={verificationVariant(i.verification.status)} data-testid="verification-badge">
                {verificationLabel(i.verification.status)}
              </Badge>
            </CardTitle>
            <CardDescription className="grid gap-1">
              {i.description ? <span className="whitespace-pre-wrap">{i.description}</span> : null}
              <span>
                {i.providerStatus ? `Status in the tracker: ${i.providerStatus} · ` : ''}
                {i.closedAt ? `Closed ${formatDateTime(i.closedAt)} · ` : ''}
                {i.lastSyncedAt ? `Last synced ${formatDateTime(i.lastSyncedAt)} · ` : ''}
                {i.url ? (
                  <a href={i.url} className="underline" target="_blank" rel="noreferrer">
                    Open in the tracker
                  </a>
                ) : null}{' '}
                <Link to="/issues" className="underline">
                  All issues
                </Link>
              </span>
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-4">
            {edit ? (
              <div className="flex flex-wrap gap-2">
                <Button
                  size="sm"
                  variant="outline"
                  disabled={m.update.isPending}
                  onClick={() =>
                    m.update.mutate({ issueId: i.id, state: i.state === 'open' ? 'closed' : 'open' })
                  }
                >
                  {i.state === 'open' ? 'Close issue' : 'Reopen issue'}
                </Button>
                {m.update.error ? (
                  <ErrorAlert error={m.update.error} title="Could not update the issue" />
                ) : null}
              </div>
            ) : null}
            <p className="text-muted-foreground text-sm">
              Open + failing = known issue · closed + failing = reopen candidate · open + passing = not
              reproducible · closed + passing = validated fixed. Skipped results are inconclusive and keep the
              previous evidence.
            </p>
            <LinkedTests issue={i} projectKey={projectKey} edit={edit} />
          </CardContent>
        </Card>
      )}
    </QueryState>
  )
}
