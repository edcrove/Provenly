import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Link, useSearchParams } from 'react-router'

import type { Project } from '@/api/client'
import { useArchiveProject, useCreateProject, useProjects, useUpdateProject } from '@/api/queries'
import { InlineConfirm } from '@/components/InlineConfirm'
import { PageTitle } from '@/components/PageTitle'
import { Pagination } from '@/components/Pagination'
import { ErrorAlert, QueryState } from '@/components/QueryState'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
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

/**
 * Administrators archive a project (after confirming) or unarchive it. The default project TC is never archived:
 * reports without ?project= go to it.
 */
function ArchiveControl({ project, onDone }: { project: Project; onDone: (message: string) => void }) {
  const archive = useArchiveProject(project.key)
  const done = (archived: boolean) => () =>
    onDone(
      `${project.key} ${archived ? 'archived: it left the lists and is read-only' : 'unarchived: it is listed and writable again'}.`,
    )
  const error = archive.error ? (
    <ErrorAlert
      error={archive.error}
      title={`Could not ${project.archivedAt ? 'unarchive' : 'archive'} the project`}
    />
  ) : null
  if (project.archivedAt)
    return (
      <>
        <Button
          size="sm"
          variant="outline"
          disabled={archive.isPending}
          onClick={() => archive.mutate(false, { onSuccess: done(false) })}
        >
          Unarchive
        </Button>
        {error}
      </>
    )
  if (project.key === 'TC') return null
  return (
    <>
      <InlineConfirm
        label={`Archive ${project.key}`}
        question={`Archive ${project.key}? It leaves every list and becomes read-only; nothing is deleted.`}
        confirmLabel="Archive"
        pending={archive.isPending}
        onConfirm={(close) => archive.mutate(true, { onSettled: close, onSuccess: done(true) })}
        trigger={(open) => (
          <Button size="sm" variant="ghost" onClick={open}>
            Archive…
          </Button>
        )}
      />
      {error}
    </>
  )
}

function ProjectRow({
  project,
  isAdmin,
  onArchived,
}: {
  project: Project
  isAdmin: boolean
  onArchived: (message: string) => void
}) {
  const update = useUpdateProject(project.key)
  const { setProject } = useCurrentProject()
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
            <Link to="/dashboard" className="underline" onClick={() => setProject(project.key)}>
              {project.name}
            </Link>
            {can(project.myRole, 'maintainer') && !project.archivedAt ? (
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
      <TableCell className="whitespace-nowrap">
        {formatDateTime(project.archivedAt ?? project.createdAt)}
      </TableCell>
      <TableCell>
        <span className="flex flex-wrap items-center gap-2">
          <Link to={`/projects/${project.key}`} className="underline">
            Settings
          </Link>
          {isAdmin ? <ArchiveControl project={project} onDone={onArchived} /> : null}
        </span>
      </TableCell>
    </TableRow>
  )
}

/** Projects own test cases and runs; each numbers its test cases with its own key (CHK-1, CHK-2…). */
export function ProjectsPage() {
  const [params, setParams] = useSearchParams()
  const page = positiveInt(params.get('page'), 1)
  const q = params.get('q')?.trim() ?? ''
  const archived = params.get('archived') === '1'
  const query = useProjects(page, PAGE_SIZE, q, archived)
  const me = useCurrentUser()
  const [draft, setDraft] = useState(q)
  // Back/forward changes the address without retyping: the box follows it (state adjusted while rendering).
  const [seenQ, setSeenQ] = useState(q)
  if (seenQ !== q) {
    setSeenQ(q)
    setDraft(q)
  }
  // Archiving removes the row (and the focused button with it): say what happened where focus lands.
  const [notice, setNotice] = useState('')
  const noticeRef = useRef<HTMLParagraphElement>(null)
  useEffect(() => {
    if (notice) noticeRef.current?.focus()
  }, [notice])
  // The search and the archived view travel together in the address; a new one starts on page 1.
  const show = (next: { q: string; archived: boolean }) =>
    setParams({ ...(next.q ? { q: next.q } : {}), ...(next.archived ? { archived: '1' } : {}) })
  const find = (e: FormEvent) => {
    e.preventDefault()
    show({ q: draft.trim(), archived })
  }
  return (
    <div className="grid gap-6">
      <Card>
        <CardHeader>
          <PageTitle title="Projects" />
          <CardTitle as="h1" className="text-xl">
            Projects
          </CardTitle>
          <CardDescription>
            A project holds test cases, runs, suites, requirements and issues; each numbers its test cases
            with its own key (e.g. CHK-12). Keys never change and projects are never deleted. TC is the
            default project. Open a project by its name, or its settings for members, classification and
            integrations. Administrators archive the projects nobody works on: they leave the lists and become
            read-only.
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          <form onSubmit={find} role="search" aria-label="Find a project" className="flex gap-2">
            <Input
              aria-label="Search projects"
              placeholder="Key or name"
              className="max-w-xs"
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
            />
            <Button type="submit" variant="outline">
              Find
            </Button>
          </form>
          <label className="flex items-center gap-2 text-sm">
            <Checkbox checked={archived} onChange={(e) => show({ q, archived: e.target.checked })} />
            Show archived projects instead
          </label>
          {notice ? (
            <p
              role="status"
              tabIndex={-1}
              ref={noticeRef}
              className="text-sm outline-none"
              data-testid="archive-notice"
            >
              {notice}
            </p>
          ) : null}
          <QueryState query={query}>
            {(data) => (
              <>
                {data.items.length === 0 ? (
                  <p className="text-muted-foreground text-sm">
                    {q ? `No ${archived ? 'archived ' : ''}project matches “${q}”.` : 'No archived projects.'}
                  </p>
                ) : null}
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Key</TableHead>
                      <TableHead>Name</TableHead>
                      <TableHead>Description</TableHead>
                      <TableHead>Your role</TableHead>
                      <TableHead>{archived ? 'Archived' : 'Created'}</TableHead>
                      <TableHead />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {data.items.map((p) => (
                      <ProjectRow key={p.key} project={p} isAdmin={me.isAdmin} onArchived={setNotice} />
                    ))}
                  </TableBody>
                </Table>
                <Pagination
                  page={data.page}
                  totalPages={data.totalPages}
                  totalItems={data.totalItems}
                  onPageChange={(p, replace) =>
                    setParams(
                      { ...(q ? { q } : {}), ...(archived ? { archived: '1' } : {}), page: String(p) },
                      { replace },
                    )
                  }
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
