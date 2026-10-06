import { Plus } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { Link, useSearchParams } from 'react-router'

import { useDimensions, useProjects, useTestCases } from '@/api/queries'
import { Pagination } from '@/components/Pagination'
import { QueryState } from '@/components/QueryState'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { PageTitle } from '@/components/PageTitle'
import { NativeSelect } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { useCurrentProject } from '@/features/projects/currentProject'
import { can } from '@/lib/roles'
import { pickEnum, positiveInt } from '@/lib/status'

const statuses = ['active', 'deprecated'] as const

/** A tag filter applied on submit (Enter), not on every keystroke. */
function TagFilter({ value, onApply }: { value: string; onApply: (tag: string) => void }) {
  const [text, setText] = useState(value)
  const submit = (e: FormEvent) => {
    e.preventDefault()
    onApply(text.trim().toLowerCase())
  }
  return (
    <form onSubmit={submit} role="search" aria-label="Filter by tag">
      <Input
        aria-label="Tag"
        placeholder="Tag (Enter)"
        className="w-36"
        value={text}
        onChange={(e) => setText(e.target.value)}
      />
    </form>
  )
}

/** The format of a test case key, as the API takes it in ?key=. */
const TEST_CASE_KEY = /^[A-Z][A-Z0-9]{1,9}-[1-9][0-9]{0,17}$/

/** Finds a test case by its key (CHK-12) on submit; anything else is explained, not sent. */
function KeyFilter({ value, onApply }: { value: string; onApply: (key: string) => void }) {
  const [text, setText] = useState(value)
  const [invalid, setInvalid] = useState(false)
  const submit = (e: FormEvent) => {
    e.preventDefault()
    const key = text.trim().toUpperCase()
    const ok = key === '' || TEST_CASE_KEY.test(key)
    setInvalid(!ok)
    if (ok) onApply(key)
  }
  return (
    <form onSubmit={submit} role="search" aria-label="Find by TC-ID">
      <Input
        aria-label="TC-ID"
        placeholder="TC-ID (Enter)"
        className="w-36"
        value={text}
        aria-invalid={invalid}
        aria-describedby={invalid ? 'tc-key-error' : undefined}
        onChange={(e) => setText(e.target.value)}
      />
      {invalid ? (
        <p id="tc-key-error" role="alert" className="text-destructive mt-1 text-xs">
          Enter a TC-ID like CHK-12
        </p>
      ) : null}
    </form>
  )
}

export function TestCaseListPage() {
  const [params, setParams] = useSearchParams()
  const page = positiveInt(params.get('page'), 1)
  const status = pickEnum(params.get('status'), statuses)
  const { project } = useCurrentProject()
  const projects = useProjects()
  // Members can create test cases in their projects (in the current one when a project is chosen).
  const canCreate = (projects.data?.items ?? []).some(
    (p) => (!project || p.key === project) && can(p.myRole, 'member'),
  )
  const tag = params.get('tag') ?? ''
  const classification = params.get('classification') ?? ''
  const tcKey = params.get('key') ?? ''
  // Classification filters need a project: dimensions and their values are per project.
  const dimensions = useDimensions(project).data?.items ?? []
  const query = useTestCases(page, {
    status,
    project: project || undefined,
    tag: tag || undefined,
    classification: (project && classification) || undefined,
    key: TEST_CASE_KEY.test(tcKey) ? tcKey : undefined,
  })

  const update = (next: Record<string, string | undefined>, replace = false) => {
    const merged = new URLSearchParams(params)
    for (const [k, v] of Object.entries(next)) {
      if (v) merged.set(k, v)
      else merged.delete(k)
    }
    setParams(merged, { replace })
  }

  return (
    <Card>
      <CardHeader className="flex-row flex-wrap items-center justify-between gap-2">
        <PageTitle title="Test Cases" />
        <CardTitle as="h1" className="text-xl">
          Test Cases
        </CardTitle>
        <div className="flex flex-wrap items-center gap-2">
          <NativeSelect
            aria-label="Filter by status"
            value={status ?? ''}
            onChange={(e) => update({ status: e.target.value || undefined, page: undefined })}
          >
            <option value="">All statuses</option>
            <option value="active">Active</option>
            <option value="deprecated">Deprecated</option>
          </NativeSelect>
          <KeyFilter
            key={`key-${tcKey}`}
            value={tcKey}
            onApply={(k) => update({ key: k || undefined, page: undefined })}
          />
          <TagFilter
            key={tag}
            value={tag}
            onApply={(t) => update({ tag: t || undefined, page: undefined })}
          />
          {project && dimensions.length > 0 ? (
            <NativeSelect
              aria-label="Filter by classification"
              value={classification}
              onChange={(e) => update({ classification: e.target.value || undefined, page: undefined })}
            >
              <option value="">Any classification</option>
              {dimensions
                .filter((d) => d.archivedAt === null)
                .map((d) => (
                  <optgroup key={d.key} label={d.name}>
                    {d.values
                      .filter((v) => v.archivedAt === null || `${d.key}:${v.key}` === classification)
                      .map((v) => (
                        <option key={v.key} value={`${d.key}:${v.key}`}>
                          {d.name}: {v.name}
                        </option>
                      ))}
                  </optgroup>
                ))}
            </NativeSelect>
          ) : null}
          {canCreate ? (
            <Button asChild>
              <Link to="/test-cases/new">
                <Plus /> New test case
              </Link>
            </Button>
          ) : null}
        </div>
      </CardHeader>
      <CardContent>
        <QueryState query={query}>
          {(data) => (
            <>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>TC-ID</TableHead>
                    <TableHead>Title</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Automated</TableHead>
                    <TableHead>Tags</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.items.length === 0 && (
                    <TableRow>
                      <TableCell colSpan={5} className="text-muted-foreground">
                        {tcKey
                          ? `No test case ${tcKey}`
                          : tag || classification
                            ? 'No test cases match the filters'
                            : status
                              ? `No ${status} test cases`
                              : 'No test cases yet'}
                        {project ? ` in ${project}.` : '.'}
                      </TableCell>
                    </TableRow>
                  )}
                  {data.items.map((tc) => (
                    <TableRow key={tc.id}>
                      <TableCell className="font-mono whitespace-nowrap">
                        <Link to={`/test-cases/${tc.id}`} className="underline">
                          {tc.key}
                        </Link>
                      </TableCell>
                      <TableCell>{tc.title}</TableCell>
                      <TableCell>
                        <Badge variant={tc.status === 'active' ? 'secondary' : 'outline'}>{tc.status}</Badge>
                      </TableCell>
                      <TableCell>{tc.automated ? 'Yes' : 'No'}</TableCell>
                      <TableCell>
                        <div className="flex flex-wrap gap-1">
                          {tc.tags.map((t) => (
                            <Badge key={t} variant="outline" className="font-mono">
                              #{t}
                            </Badge>
                          ))}
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              <Pagination
                page={data.page}
                totalPages={data.totalPages}
                totalItems={data.totalItems}
                onPageChange={(p, replace) => update({ page: String(p) }, replace)}
              />
            </>
          )}
        </QueryState>
      </CardContent>
    </Card>
  )
}
