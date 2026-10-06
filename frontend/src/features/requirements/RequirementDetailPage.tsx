import { useState } from 'react'
import { Link, useParams } from 'react-router'

import type { Requirement } from '@/api/client'
import { useRequirement, useRequirementMutations, useTestCases } from '@/api/queries'
import { NotFoundPage } from '@/app/NotFoundPage'
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
import { coverageLabel, coverageVariant, requirementRef } from '@/lib/requirements'
import { can } from '@/lib/roles'
import { positiveInt } from '@/lib/status'

function Coverage({ req, projectKey, edit }: { req: Requirement; projectKey: string; edit: boolean }) {
  const m = useRequirementMutations(projectKey)
  const cases = useTestCases(1, { project: projectKey, pageSize: 100 })
  const [picked, setPicked] = useState('')
  const keyOf = (id: number) => cases.data?.items.find((tc) => tc.id === id)?.key ?? `#${id}`
  const candidates = (cases.data?.items ?? []).filter((tc) => !req.testCaseIds.includes(tc.id))
  return (
    <div className="grid gap-3">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Test case</TableHead>
            <TableHead>Latest result</TableHead>
            {edit ? <TableHead /> : null}
          </TableRow>
        </TableHeader>
        <TableBody>
          {req.coverage.testCases.length === 0 && (
            <TableRow>
              <TableCell colSpan={3} className="text-muted-foreground">
                No test case covers this requirement yet.
              </TableCell>
            </TableRow>
          )}
          {req.coverage.testCases.map((c) => (
            <TableRow key={c.testCaseId} data-testid={`covering-${c.testCaseId}`}>
              <TableCell className="font-mono">
                <Link to={`/test-cases/${c.testCaseId}`} className="underline">
                  {keyOf(c.testCaseId)}
                </Link>
              </TableCell>
              <TableCell>
                {c.status ? (
                  <StatusBadge status={c.status} />
                ) : (
                  <span className="text-muted-foreground">not run</span>
                )}
              </TableCell>
              {edit ? (
                <TableCell>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={m.link.isPending}
                    onClick={() =>
                      m.link.mutate({
                        requirementId: req.id,
                        testCaseIds: req.testCaseIds.filter((id) => id !== c.testCaseId),
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
                { requirementId: req.id, testCaseIds: [...req.testCaseIds, Number(picked)] },
                { onSuccess: () => setPicked('') },
              )
            }
          >
            Link test case
          </Button>
        </div>
      ) : null}
      {m.link.error ? <ErrorAlert error={m.link.error} title="Could not change the coverage" /> : null}
    </div>
  )
}

/** One requirement: where it comes from, what covers it and how that coverage did last. */
export function RequirementDetailPage() {
  const { projectKey = '', requirementId } = useParams()
  const id = positiveInt(requirementId, 0)
  const req = useRequirement(projectKey, id)
  const m = useRequirementMutations(projectKey)
  const edit = can(useProjectRole(projectKey), 'member')
  if (!id) return <NotFoundPage />
  return (
    <QueryState page query={req}>
      {(r) => (
        <Card>
          <CardHeader>
            <PageTitle title={`${requirementRef(r)} · ${r.title}`} />
            <CardTitle as="h1" className="flex flex-wrap items-center gap-2 text-xl">
              <span className="font-mono">{requirementRef(r)}</span> · {r.title}
              <Badge variant={coverageVariant(r.coverage.status)} data-testid="coverage-badge">
                {coverageLabel(r.coverage.status)}
              </Badge>
              {r.archivedAt ? <Badge variant="outline">archived</Badge> : null}
            </CardTitle>
            <CardDescription className="grid gap-1">
              {r.description ? <span className="whitespace-pre-wrap">{r.description}</span> : null}
              <span>
                {r.providerStatus ? `Status in the source: ${r.providerStatus} · ` : ''}
                {r.lastSyncedAt ? `Last synced ${formatDateTime(r.lastSyncedAt)} · ` : ''}
                {r.url ? (
                  <a href={r.url} className="underline" target="_blank" rel="noreferrer">
                    Open in the source
                  </a>
                ) : null}
                {r.url ? ' · ' : null}
                <Link to="/requirements" className="underline">
                  All requirements
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
                  onClick={() => m.update.mutate({ requirementId: r.id, archived: r.archivedAt === null })}
                >
                  {r.archivedAt ? 'Restore' : 'Archive'}
                </Button>
                {m.update.error ? (
                  <ErrorAlert error={m.update.error} title="Could not update the requirement" />
                ) : null}
              </div>
            ) : null}
            <h2 className="font-medium">
              Coverage: {r.coverage.passed} of {r.coverage.linked} test cases passing
              {r.coverage.failed ? `, ${r.coverage.failed} failing` : ''}
            </h2>
            <Coverage req={r} projectKey={projectKey} edit={edit} />
          </CardContent>
        </Card>
      )}
    </QueryState>
  )
}
