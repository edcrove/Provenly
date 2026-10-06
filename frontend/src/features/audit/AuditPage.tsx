import { useState, type FormEvent } from 'react'
import { useSearchParams } from 'react-router'

import { useAuditEvents } from '@/api/queries'
import { PageTitle } from '@/components/PageTitle'
import { Pagination } from '@/components/Pagination'
import { QueryState } from '@/components/QueryState'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { formatDateTime } from '@/lib/format'
import { positiveInt } from '@/lib/status'

/** The audit log (administrators): who changed what through the API, newest first, by project, actor and test case. */
export function AuditPage() {
  const [params, setParams] = useSearchParams()
  const page = positiveInt(params.get('page'), 1)
  const project = params.get('project') ?? ''
  const actor = params.get('actor') ?? ''
  const testCase = params.get('testCase') ?? ''
  const [draft, setDraft] = useState({ project, actor, testCase })
  const events = useAuditEvents(
    { ...(project ? { project } : {}), ...(actor ? { actor } : {}), ...(testCase ? { testCase } : {}) },
    page,
  )
  const apply = (e: FormEvent) => {
    e.preventDefault()
    const next: Record<string, string> = {}
    if (draft.project.trim()) next.project = draft.project.trim().toUpperCase()
    if (draft.actor.trim()) next.actor = draft.actor.trim()
    if (draft.testCase.trim()) next.testCase = draft.testCase.trim().toUpperCase()
    setParams(next)
  }
  return (
    <Card>
      <CardHeader>
        <PageTitle title="Audit log" />
        <CardTitle as="h1" className="text-xl">
          Audit log
        </CardTitle>
        <CardDescription>
          Every change made through the API or the UI, with who made it (a user or a CI API key). Request
          contents are never recorded.
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-6">
        <form onSubmit={apply} className="flex flex-wrap items-end gap-3" aria-label="Filter the audit log">
          <div className="grid gap-2">
            <Label htmlFor="audit-project">Project</Label>
            <Input
              id="audit-project"
              value={draft.project}
              placeholder="CHK"
              onChange={(e) => setDraft({ ...draft, project: e.target.value })}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="audit-actor">Actor</Label>
            <Input
              id="audit-actor"
              value={draft.actor}
              placeholder="username"
              onChange={(e) => setDraft({ ...draft, actor: e.target.value })}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="audit-test-case">Test case</Label>
            <Input
              id="audit-test-case"
              value={draft.testCase}
              placeholder="CHK-4"
              onChange={(e) => setDraft({ ...draft, testCase: e.target.value })}
            />
          </div>
          <Button type="submit">Filter</Button>
        </form>
        <QueryState query={events} page>
          {(data) => (
            <>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>When</TableHead>
                    <TableHead>Actor</TableHead>
                    <TableHead>Change</TableHead>
                    <TableHead>Project</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.items.length === 0 && (
                    <TableRow>
                      <TableCell colSpan={4} className="text-muted-foreground">
                        {project || actor || testCase
                          ? 'No changes match these filters.'
                          : 'No changes recorded.'}
                      </TableCell>
                    </TableRow>
                  )}
                  {data.items.map((e) => (
                    <TableRow key={e.id} data-testid={`audit-${e.id}`}>
                      <TableCell className="whitespace-nowrap">{formatDateTime(e.occurredAt)}</TableCell>
                      <TableCell>
                        <div>{e.actor}</div>
                        {e.ip ? (
                          <div
                            className="text-muted-foreground font-mono text-xs"
                            title={e.userAgent ?? undefined}
                          >
                            {e.ip}
                          </div>
                        ) : null}
                      </TableCell>
                      <TableCell>
                        {/* What it did in words; older events have only the route. The path is the detail. */}
                        <div>{e.summary ?? <span className="font-mono text-xs">{e.action}</span>}</div>
                        <div className="text-muted-foreground font-mono text-xs break-all">{e.path}</div>
                      </TableCell>
                      <TableCell>{e.project ?? '—'}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              <Pagination
                page={data.page}
                totalPages={data.totalPages}
                totalItems={data.totalItems}
                onPageChange={(p, replace) =>
                  setParams({ ...Object.fromEntries(params), page: String(p) }, { replace })
                }
              />
            </>
          )}
        </QueryState>
      </CardContent>
    </Card>
  )
}
