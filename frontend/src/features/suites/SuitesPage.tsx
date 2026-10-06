import { useState, type FormEvent } from 'react'
import { Link } from 'react-router'

import type { Suite } from '@/api/client'
import { useDimensions, useSuiteMutations, useSuitesPage } from '@/api/queries'
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
import { can } from '@/lib/roles'
import { usePage } from '@/lib/usePage'

import { selectionLabel } from '@/lib/suites'

function NewSuite({ projectKey }: { projectKey: string }) {
  const m = useSuiteMutations(projectKey)
  const dimensions = useDimensions(projectKey).data?.items ?? []
  const [name, setName] = useState('')
  const [key, setKey] = useState('')
  const [kind, setKind] = useState<'static' | 'query'>('query')
  const [tag, setTag] = useState('')
  const [pair, setPair] = useState('')
  const submit = (e: FormEvent) => {
    e.preventDefault()
    m.create.mutate(
      {
        key: key.trim(),
        name: name.trim(),
        kind,
        ...(kind === 'query'
          ? { query: { tag: tag.trim() || null, classification: pair ? [pair] : [] } }
          : {}),
      },
      {
        onSuccess: () => {
          setName('')
          setKey('')
          setTag('')
          setPair('')
        },
      },
    )
  }
  return (
    <form onSubmit={submit} className="grid gap-3" aria-label="New suite">
      {m.create.error ? <ErrorAlert error={m.create.error} title="Could not create the suite" /> : null}
      <div className="flex flex-wrap items-end gap-3">
        <div className="grid gap-2">
          <Label htmlFor="suite-name">Name</Label>
          <Input id="suite-name" value={name} onChange={(e) => setName(e.target.value)} required />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="suite-key">Key</Label>
          <Input
            id="suite-key"
            className="w-36 font-mono"
            placeholder="smoke"
            value={key}
            onChange={(e) => setKey(e.target.value)}
            required
          />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="suite-kind">Kind</Label>
          <NativeSelect
            id="suite-kind"
            value={kind}
            onChange={(e) => setKind(e.target.value as 'static' | 'query')}
          >
            <option value="query">Query (by tag and classification)</option>
            <option value="static">Static (a list of test cases)</option>
          </NativeSelect>
        </div>
        {kind === 'query' ? (
          <>
            <div className="grid gap-2">
              <Label htmlFor="suite-tag">Tag</Label>
              <Input
                id="suite-tag"
                className="w-32"
                placeholder="smoke"
                value={tag}
                onChange={(e) => setTag(e.target.value)}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="suite-pair">Classification</Label>
              <NativeSelect id="suite-pair" value={pair} onChange={(e) => setPair(e.target.value)}>
                <option value="">Any</option>
                {dimensions
                  .filter((d) => d.archivedAt === null)
                  .map((d) => (
                    <optgroup key={d.key} label={d.name}>
                      {d.values
                        .filter((v) => v.archivedAt === null)
                        .map((v) => (
                          <option key={v.key} value={`${d.key}:${v.key}`}>
                            {d.name}: {v.name}
                          </option>
                        ))}
                    </optgroup>
                  ))}
              </NativeSelect>
            </div>
          </>
        ) : null}
        <Button type="submit" disabled={m.create.isPending}>
          Create suite
        </Button>
      </div>
    </form>
  )
}

function SuiteRow({ suite, projectKey }: { suite: Suite; projectKey: string }) {
  return (
    <TableRow data-testid={`suite-${suite.key}`}>
      <TableCell>
        <Link to={`/suites/${projectKey}/${suite.key}`} className="underline">
          {suite.name}
        </Link>
        {suite.archivedAt ? (
          <Badge variant="outline" className="ml-2">
            archived
          </Badge>
        ) : null}
      </TableCell>
      <TableCell className="font-mono">{suite.key}</TableCell>
      <TableCell>{suite.kind}</TableCell>
      <TableCell>{selectionLabel(suite)}</TableCell>
    </TableRow>
  )
}

/** The suites of the current project: named selections of test cases that CI reports partial runs for. */
export function SuitesPage() {
  const { project } = useCurrentProject()
  const [page, setPage] = usePage(project)
  const suites = useSuitesPage(project, page)
  const manage = can(useProjectRole(project), 'maintainer')
  return (
    <Card>
      <CardHeader>
        <PageTitle title="Suites" />
        <CardTitle as="h1" className="text-xl">
          Suites
        </CardTitle>
        <CardDescription>
          A suite is a named selection of a project&apos;s test cases: a static list, or a query by tag and
          classification. CI reports a run for a suite with <code>&amp;suite=&lt;key&gt;</code>: only the
          suite&apos;s active automated test cases are expected, so a smoke run is not full of untested cases.
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-6">
        {!project ? (
          <p className="text-muted-foreground text-sm">Choose a project (top right) to see its suites.</p>
        ) : (
          <>
            <QueryState query={suites}>
              {(data) => (
                <>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>Name</TableHead>
                        <TableHead>Key</TableHead>
                        <TableHead>Kind</TableHead>
                        <TableHead>Selection</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {data.items.length === 0 && (
                        <TableRow>
                          <TableCell colSpan={4} className="text-muted-foreground">
                            No suites in {project} yet.
                          </TableCell>
                        </TableRow>
                      )}
                      {data.items.map((s) => (
                        <SuiteRow key={s.key} suite={s} projectKey={project} />
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
            {manage ? <NewSuite projectKey={project} /> : null}
          </>
        )}
      </CardContent>
    </Card>
  )
}
