import { useState, type FormEvent } from 'react'
import { Link } from 'react-router'

import { useIssueMutations, useIssuesPage } from '@/api/queries'
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
import { verificationLabel, verificationVariant } from '@/lib/issues'
import { requirementRef as issueRef } from '@/lib/requirements'
import { can } from '@/lib/roles'
import { usePage } from '@/lib/usePage'
import { ProjectChooser, ScopeLabel } from '@/features/projects/ProjectScope'

type Provider = 'provenly' | 'jira' | 'github' | 'azure_devops'

function NewIssue({ projectKey }: { projectKey: string }) {
  const m = useIssueMutations(projectKey)
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
    <form onSubmit={submit} className="grid gap-3" aria-label="New issue">
      {m.create.error ? <ErrorAlert error={m.create.error} title="Could not add the issue" /> : null}
      <div className="flex flex-wrap items-end gap-3">
        <div className="grid gap-2">
          <Label htmlFor="issue-title">Title</Label>
          <Input
            id="issue-title"
            className="w-72"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            required
          />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="issue-provider">Tracker</Label>
          <NativeSelect
            id="issue-provider"
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
            <Label htmlFor="issue-external">Id in the tracker</Label>
            <Input
              id="issue-external"
              className="w-32 font-mono"
              placeholder="PAY-123"
              value={externalId}
              onChange={(e) => setExternalId(e.target.value)}
              required
            />
          </div>
        ) : null}
        <div className="grid gap-2">
          <Label htmlFor="issue-url">Link (optional)</Label>
          <Input id="issue-url" className="w-56" value={url} onChange={(e) => setUrl(e.target.value)} />
        </div>
        <Button type="submit" disabled={m.create.isPending}>
          Add issue
        </Button>
      </div>
    </form>
  )
}

/** The current project's issues and their QA verification from the test results. */
export function IssuesPage() {
  const { project } = useCurrentProject()
  const [state, setState] = useState<'' | 'open' | 'closed'>('')
  const [page, setPage] = usePage(`${project}:${state}`)
  const issues = useIssuesPage(project, state ? { state } : {}, page)
  const edit = can(useProjectRole(project), 'member')
  return (
    <Card>
      <CardHeader>
        <PageTitle title="Issues" />
        <CardTitle as="h1" className="text-xl">
          Issues
        </CardTitle>
        <ScopeLabel />
        <CardDescription>
          Defects reported here or mirrored from Jira, GitHub or Azure DevOps (maintainers import them with
          their state through the API). Link the test cases that reproduce an issue: its verification combines
          the issue&apos;s state with their latest conclusive results.
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-6">
        {!project ? (
          <ProjectChooser what="issues" />
        ) : (
          <>
            <div className="flex items-center gap-2">
              <Label htmlFor="issue-state-filter">State</Label>
              <NativeSelect
                id="issue-state-filter"
                value={state}
                onChange={(e) => setState(e.target.value as '' | 'open' | 'closed')}
              >
                <option value="">All</option>
                <option value="open">Open</option>
                <option value="closed">Closed</option>
              </NativeSelect>
            </div>
            <QueryState query={issues}>
              {(data) => (
                <>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>Issue</TableHead>
                        <TableHead>Title</TableHead>
                        <TableHead>State</TableHead>
                        <TableHead>Verification</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {data.items.length === 0 && (
                        <TableRow>
                          <TableCell colSpan={4} className="text-muted-foreground">
                            {state ? `No ${state} issues.` : 'No issues here.'}
                          </TableCell>
                        </TableRow>
                      )}
                      {data.items.map((i) => (
                        <TableRow key={i.id} data-testid={`issue-${i.externalId}`}>
                          <TableCell className="font-mono whitespace-nowrap">
                            <Link to={`/issues/${project}/${i.id}`} className="underline">
                              {issueRef(i)}
                            </Link>
                          </TableCell>
                          <TableCell>{i.title}</TableCell>
                          <TableCell>
                            <Badge variant="outline">{i.state}</Badge>
                          </TableCell>
                          <TableCell>
                            <Badge variant={verificationVariant(i.verification.status)}>
                              {verificationLabel(i.verification.status)}
                            </Badge>
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
            {edit ? <NewIssue projectKey={project} /> : null}
          </>
        )}
      </CardContent>
    </Card>
  )
}
