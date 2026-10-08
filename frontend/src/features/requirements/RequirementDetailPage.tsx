import { Link, useParams } from 'react-router'

import type { Requirement } from '@/api/client'
import { useRequirement, useRequirementMutations } from '@/api/queries'
import { NotFoundPage } from '@/app/NotFoundPage'
import { PageTitle } from '@/components/PageTitle'
import { ErrorAlert, QueryState } from '@/components/QueryState'
import { TestCasePicker } from '@/components/TestCasePicker'
import { StatusBadge } from '@/components/StatusBadge'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useProjectRole } from '@/features/projects/useProjectRole'
import { formatDateTime } from '@/lib/format'
import { coverageLabel, coverageVariant, requirementRef } from '@/lib/requirements'
import { can } from '@/lib/roles'
import { positiveInt } from '@/lib/status'
import { Breadcrumb } from '@/features/projects/ProjectScope'

function Coverage({ req, projectKey, edit }: { req: Requirement; projectKey: string; edit: boolean }) {
  const m = useRequirementMutations(projectKey)
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
                  {c.testCaseKey ?? `#${c.testCaseId}`}
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
        <TestCasePicker
          projectKey={projectKey}
          label="Test case to link"
          action="Link test case"
          exclude={req.testCaseIds}
          pending={m.link.isPending}
          onPick={(id, done) =>
            m.link.mutate(
              { requirementId: req.id, testCaseIds: [...req.testCaseIds, id] },
              { onSuccess: done },
            )
          }
        />
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
            <Breadcrumb
              projectKey={projectKey}
              section="Requirements"
              to="/requirements"
              current={requirementRef(r)}
            />
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
