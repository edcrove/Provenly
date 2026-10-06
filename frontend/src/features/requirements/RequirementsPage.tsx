import { useState, type FormEvent } from 'react'
import { Link } from 'react-router'

import { useRequirementMutations, useRequirementsPage } from '@/api/queries'
import { PageTitle } from '@/components/PageTitle'
import { Pagination } from '@/components/Pagination'
import { ErrorAlert, QueryState } from '@/components/QueryState'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useCurrentProject } from '@/features/projects/currentProject'
import { useProjectRole } from '@/features/projects/useProjectRole'
import { coverageLabel, coverageVariant, requirementRef } from '@/lib/requirements'
import { can } from '@/lib/roles'
import { usePage } from '@/lib/usePage'

type Provider = 'provenly' | 'jira' | 'github' | 'azure_devops'

function NewRequirement({ projectKey }: { projectKey: string }) {
  const m = useRequirementMutations(projectKey)
  const [title, setTitle] = useState('')
  const [provider, setProvider] = useState<Provider>('provenly')
  const [externalId, setExternalId] = useState('')
  const [url, setUrl] = useState('')
  const submit = (e: FormEvent) => {
    e.preventDefault()
    m.create.mutate(
      {
        title: title.trim(),
        provider,
        ...(provider !== 'provenly' ? { externalId: externalId.trim() } : {}),
        ...(url.trim() ? { url: url.trim() } : {}),
      },
      {
        onSuccess: () => {
          setTitle('')
          setExternalId('')
          setUrl('')
        },
      },
    )
  }
  return (
    <form onSubmit={submit} className="grid gap-3" aria-label="New requirement">
      {m.create.error ? <ErrorAlert error={m.create.error} title="Could not add the requirement" /> : null}
      <div className="flex flex-wrap items-end gap-3">
        <div className="grid gap-2">
          <Label htmlFor="req-title">Title</Label>
          <Input
            id="req-title"
            className="w-72"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            required
          />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="req-provider">Source</Label>
          <NativeSelect
            id="req-provider"
            value={provider}
            onChange={(e) => setProvider(e.target.value as Provider)}
          >
            <option value="provenly">Provenly (native)</option>
            <option value="jira">Jira</option>
            <option value="github">GitHub</option>
            <option value="azure_devops">Azure DevOps</option>
          </NativeSelect>
        </div>
        {provider !== 'provenly' ? (
          <div className="grid gap-2">
            <Label htmlFor="req-external">Id in the source</Label>
            <Input
              id="req-external"
              className="w-32 font-mono"
              placeholder="PAY-123"
              value={externalId}
              onChange={(e) => setExternalId(e.target.value)}
              required
            />
          </div>
        ) : null}
        <div className="grid gap-2">
          <Label htmlFor="req-url">Link (optional)</Label>
          <Input id="req-url" className="w-56" value={url} onChange={(e) => setUrl(e.target.value)} />
        </div>
        <Button type="submit" disabled={m.create.isPending}>
          Add requirement
        </Button>
      </div>
    </form>
  )
}

/** The current project's requirements and how well their test cases cover them. */
export function RequirementsPage() {
  const { project } = useCurrentProject()
  const [page, setPage] = usePage(project)
  const requirements = useRequirementsPage(project, page)
  const edit = can(useProjectRole(project), 'member')
  return (
    <Card>
      <CardHeader>
        <PageTitle title="Requirements" />
        <CardTitle as="h1" className="text-xl">
          Requirements
        </CardTitle>
        <CardDescription>
          What the product must do, written here or mirrored from Jira, GitHub or Azure DevOps (maintainers
          import them through the API). Coverage comes from the latest result of each test case that covers a
          requirement.
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-6">
        {!project ? (
          <p className="text-muted-foreground text-sm">
            Choose a project (top right) to see its requirements.
          </p>
        ) : (
          <>
            <QueryState query={requirements}>
              {(data) => (
                <>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>Requirement</TableHead>
                        <TableHead>Title</TableHead>
                        <TableHead>Coverage</TableHead>
                        <TableHead>Test cases</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {data.items.length === 0 && (
                        <TableRow>
                          <TableCell colSpan={4} className="text-muted-foreground">
                            No requirements in {project} yet.
                          </TableCell>
                        </TableRow>
                      )}
                      {data.items.map((r) => (
                        <TableRow key={r.id} data-testid={`requirement-${r.externalId}`}>
                          <TableCell className="font-mono whitespace-nowrap">
                            <Link to={`/requirements/${project}/${r.id}`} className="underline">
                              {requirementRef(r)}
                            </Link>
                          </TableCell>
                          <TableCell>
                            {r.title}
                            {r.archivedAt ? (
                              <Badge variant="outline" className="ml-2">
                                archived
                              </Badge>
                            ) : null}
                          </TableCell>
                          <TableCell>
                            <Badge variant={coverageVariant(r.coverage.status)}>
                              {coverageLabel(r.coverage.status)}
                            </Badge>
                          </TableCell>
                          <TableCell className="tabular-nums">
                            {r.coverage.passed}/{r.coverage.linked} passing
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                  <Pagination
                    page={data.page}
                    totalPages={data.totalPages}
                    totalItems={data.totalItems}
                    onPageChange={setPage}
                  />
                </>
              )}
            </QueryState>
            {edit ? <NewRequirement projectKey={project} /> : null}
          </>
        )}
      </CardContent>
    </Card>
  )
}
