import { useState, type FormEvent } from 'react'
import { Link, useSearchParams } from 'react-router'

import type { Project } from '@/api/client'
import { useCreateProject, useProjects, useUpdateProject } from '@/api/queries'
import { PageTitle } from '@/components/PageTitle'
import { Pagination } from '@/components/Pagination'
import { ErrorAlert, QueryState } from '@/components/QueryState'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { formatDateTime } from '@/lib/format'
import { positiveInt } from '@/lib/status'

import { useCurrentUser } from '@/features/auth/currentUser'
import { can } from '@/lib/roles'

import { useCurrentProject } from './currentProject'

const PAGE_SIZE = 20

function NewProjectForm() {
  const create = useCreateProject()
  const { setProject } = useCurrentProject()
  const [values, setValues] = useState({ key: '', name: '', description: '' })
  const set = (field: keyof typeof values) => (e: { target: { value: string } }) =>
    setValues((v) => ({ ...v, [field]: e.target.value }))
  const submit = (e: FormEvent) => {
    e.preventDefault()
    create.mutate(values, {
      onSuccess: (p) => {
        setValues({ key: '', name: '', description: '' })
        setProject(p.key)
      },
    })
  }
  return (
    <form onSubmit={submit} className="grid gap-3" aria-label="New project">
      {create.error ? <ErrorAlert error={create.error} title="Could not create the project" /> : null}
      <div className="grid gap-3 sm:grid-cols-[10rem_1fr]">
        <div className="grid gap-2">
          <Label htmlFor="project-key">Key</Label>
          <Input
            id="project-key"
            value={values.key}
            onChange={set('key')}
            required
            maxLength={10}
            placeholder="CHK"
            className="font-mono uppercase"
          />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="project-name">Name</Label>
          <Input id="project-name" value={values.name} onChange={set('name')} required maxLength={100} />
        </div>
      </div>
      <div className="grid gap-2">
        <Label htmlFor="project-description">Description</Label>
        <Input
          id="project-description"
          value={values.description}
          onChange={set('description')}
          maxLength={2000}
        />
      </div>
      <div>
        <Button type="submit" disabled={create.isPending}>
          Create project
        </Button>
      </div>
    </form>
  )
}

function ProjectRow({ project }: { project: Project }) {
  const update = useUpdateProject(project.key)
  // null while not renaming.
  const [name, setName] = useState<string | null>(null)
  const save = (e: FormEvent, value: string) => {
    e.preventDefault()
    update.mutate({ name: value }, { onSuccess: () => setName(null) })
  }
  return (
    <TableRow data-testid={`project-${project.key}`}>
      <TableCell className="font-mono">{project.key}</TableCell>
      <TableCell>
        {name === null ? (
          <span className="flex items-center gap-2">
            {project.name}
            {can(project.myRole, 'maintainer') ? (
              <Button size="sm" variant="ghost" onClick={() => setName(project.name)}>
                Rename
              </Button>
            ) : null}
          </span>
        ) : (
          <form onSubmit={(e) => save(e, name)} className="flex items-center gap-2">
            <Input
              aria-label={`Name of ${project.key}`}
              value={name}
              onChange={(e) => setName(e.target.value)}
              maxLength={100}
            />
            <Button size="sm" type="submit" disabled={update.isPending}>
              Save
            </Button>
            <Button size="sm" type="button" variant="ghost" onClick={() => setName(null)}>
              Cancel
            </Button>
          </form>
        )}
        {update.error ? <ErrorAlert error={update.error} title="Could not rename the project" /> : null}
      </TableCell>
      <TableCell className="text-muted-foreground">{project.description || '—'}</TableCell>
      <TableCell data-testid="my-role">{project.myRole}</TableCell>
      <TableCell className="whitespace-nowrap">{formatDateTime(project.createdAt)}</TableCell>
      <TableCell>
        <Link to={`/projects/${project.key}`} className="underline">
          Members
        </Link>
      </TableCell>
    </TableRow>
  )
}

/** Projects own test cases and runs; each numbers its test cases with its own key (CHK-1, CHK-2…). */
export function ProjectsPage() {
  const [params, setParams] = useSearchParams()
  const page = positiveInt(params.get('page'), 1)
  const query = useProjects(page, PAGE_SIZE)
  const me = useCurrentUser()
  return (
    <div className="grid gap-6">
      <Card>
        <CardHeader>
          <PageTitle title="Projects" />
          <CardTitle as="h1" className="text-xl">
            Projects
          </CardTitle>
          <CardDescription>
            Each project numbers its test cases with its own key (e.g. CHK-12). Keys never change and projects
            are never deleted. TC is the default project.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <QueryState query={query}>
            {(data) => (
              <>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Key</TableHead>
                      <TableHead>Name</TableHead>
                      <TableHead>Description</TableHead>
                      <TableHead>Your role</TableHead>
                      <TableHead>Created</TableHead>
                      <TableHead />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {data.items.map((p) => (
                      <ProjectRow key={p.key} project={p} />
                    ))}
                  </TableBody>
                </Table>
                <Pagination
                  page={data.page}
                  totalPages={data.totalPages}
                  totalItems={data.totalItems}
                  onPageChange={(p, replace) => setParams({ page: String(p) }, { replace })}
                />
              </>
            )}
          </QueryState>
        </CardContent>
      </Card>
      {me.isAdmin ? (
        <Card className="max-w-2xl">
          <CardHeader>
            <CardTitle as="h2">New project</CardTitle>
          </CardHeader>
          <CardContent>
            <NewProjectForm />
          </CardContent>
        </Card>
      ) : null}
    </div>
  )
}
