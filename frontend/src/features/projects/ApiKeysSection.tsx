import { useState, type FormEvent } from 'react'

import type { ApiKey } from '@/api/client'
import { useApiKeyMutations, useApiKeys } from '@/api/queries'
import { Pagination } from '@/components/Pagination'
import { ErrorAlert, QueryState } from '@/components/QueryState'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ciSnippet } from '@/lib/ciSnippet'
import { formatDateTime } from '@/lib/format'

function KeyRow({ apiKey, projectKey }: { apiKey: ApiKey; projectKey: string }) {
  const m = useApiKeyMutations(projectKey)
  const active = apiKey.status === 'active'
  return (
    <TableRow data-testid={`api-key-${apiKey.id}`}>
      <TableCell>{apiKey.name}</TableCell>
      <TableCell className="font-mono whitespace-nowrap">{apiKey.prefix}…</TableCell>
      <TableCell>
        <Badge variant={active ? 'secondary' : 'outline'}>{apiKey.status}</Badge>
      </TableCell>
      <TableCell className="whitespace-nowrap">{formatDateTime(apiKey.createdAt)}</TableCell>
      <TableCell className="whitespace-nowrap">
        {apiKey.lastUsedAt ? formatDateTime(apiKey.lastUsedAt) : 'Never'}
      </TableCell>
      <TableCell>
        {active ? (
          <Button
            size="sm"
            variant="outline"
            disabled={m.revoke.isPending}
            onClick={() => m.revoke.mutate(apiKey.id)}
          >
            Revoke
          </Button>
        ) : null}
        {m.revoke.error ? <ErrorAlert error={m.revoke.error} title="Could not revoke the key" /> : null}
      </TableCell>
    </TableRow>
  )
}

function NewKey({ projectKey }: { projectKey: string }) {
  const { create } = useApiKeyMutations(projectKey)
  const [name, setName] = useState('')
  const [token, setToken] = useState<string>()
  const submit = (e: FormEvent) => {
    e.preventDefault()
    create.mutate(name.trim(), {
      onSuccess: (res) => {
        setToken(res.token)
        setName('')
      },
    })
  }
  return (
    <form onSubmit={submit} className="grid gap-3" aria-label="New API key">
      {create.error ? <ErrorAlert error={create.error} title="Could not create the key" /> : null}
      <div className="flex flex-wrap items-end gap-3">
        <div className="grid gap-2">
          <Label htmlFor="api-key-name">Key name</Label>
          <Input
            id="api-key-name"
            value={name}
            maxLength={100}
            placeholder="GitHub Actions"
            onChange={(e) => setName(e.target.value)}
            required
          />
        </div>
        <Button type="submit" disabled={create.isPending}>
          Create API key
        </Button>
      </div>
      {token ? (
        <div className="grid gap-2 rounded-md border p-3" role="status">
          <p className="text-sm">
            Store this key as a CI secret (e.g. <code>PROVENLY_API_KEY</code>). It is not shown again; revoke
            it and create another if it is lost.
          </p>
          <code className="bg-muted rounded px-2 py-1 text-xs break-all" data-testid="api-key-token">
            {token}
          </code>
          <p className="text-sm">Report each run from CI with:</p>
          <pre className="bg-muted overflow-x-auto rounded px-2 py-1 text-xs" data-testid="api-key-snippet">
            {ciSnippet(window.location.origin, projectKey)}
          </pre>
        </div>
      ) : null}
    </form>
  )
}

/** A project's CI API keys (maintainers and administrators): create, list and revoke. */
export function ApiKeysSection({ projectKey }: { projectKey: string }) {
  const [page, setPage] = useState(1)
  const keys = useApiKeys(projectKey, page)
  return (
    <Card>
      <CardHeader>
        <CardTitle as="h2" className="text-lg">
          API keys
        </CardTitle>
        <CardDescription>
          CI reports runs into this project with a key. A key opens nothing else, and only this project.
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-6">
        <QueryState query={keys}>
          {(data) => (
            <>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Name</TableHead>
                    <TableHead>Key</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Created</TableHead>
                    <TableHead>Last used</TableHead>
                    <TableHead />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.items.length === 0 && (
                    <TableRow>
                      <TableCell colSpan={6} className="text-muted-foreground">
                        No API keys yet.
                      </TableCell>
                    </TableRow>
                  )}
                  {data.items.map((k) => (
                    <KeyRow key={k.id} apiKey={k} projectKey={projectKey} />
                  ))}
                </TableBody>
              </Table>
              <Pagination
                page={data.page}
                totalPages={data.totalPages}
                totalItems={data.totalItems}
                onPageChange={(p) => setPage(p)}
              />
            </>
          )}
        </QueryState>
        <NewKey projectKey={projectKey} />
      </CardContent>
    </Card>
  )
}
