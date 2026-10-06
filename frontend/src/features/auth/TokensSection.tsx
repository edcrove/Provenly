import { useState, type FormEvent } from 'react'

import type { PersonalAccessToken } from '@/api/client'
import { useProjects, useTokenMutations, useTokens } from '@/api/queries'
import { InlineConfirm } from '@/components/InlineConfirm'
import { Pagination } from '@/components/Pagination'
import { ErrorAlert, QueryState } from '@/components/QueryState'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { formatDateTime } from '@/lib/format'
import { mcpSnippet } from '@/lib/mcpSnippet'

const statusVariant = { active: 'secondary', expired: 'outline', revoked: 'outline' } as const
const lifetimes = [30, 90, 180, 365]

function TokenRow({ token }: { token: PersonalAccessToken }) {
  const { revoke } = useTokenMutations()
  return (
    <TableRow data-testid={`token-${token.id}`}>
      <TableCell>{token.name}</TableCell>
      <TableCell className="font-mono whitespace-nowrap">{token.prefix}…</TableCell>
      <TableCell>{token.projects.join(', ')}</TableCell>
      <TableCell>
        <Badge variant={statusVariant[token.status]}>{token.status}</Badge>
      </TableCell>
      <TableCell className="whitespace-nowrap">{formatDateTime(token.expiresAt)}</TableCell>
      <TableCell className="whitespace-nowrap">
        {token.lastUsedAt ? formatDateTime(token.lastUsedAt) : 'Never'}
      </TableCell>
      <TableCell>
        {token.status === 'active' ? (
          <InlineConfirm
            label={`Confirm revoking ${token.name}`}
            question={`Revoke token ${token.name}? Scripts and agents using it will get 401.`}
            confirmLabel="Revoke"
            pending={revoke.isPending}
            onConfirm={(close) => revoke.mutate(token.id, { onSettled: close })}
            trigger={(open) => (
              <Button size="sm" variant="outline" disabled={revoke.isPending} onClick={open}>
                Revoke…
              </Button>
            )}
          />
        ) : null}
        {revoke.error ? <ErrorAlert error={revoke.error} title="Could not revoke the token" /> : null}
      </TableCell>
    </TableRow>
  )
}

function NewToken() {
  const { create } = useTokenMutations()
  const projects = useProjects()
  const [name, setName] = useState('')
  const [picked, setPicked] = useState<string[]>([])
  const [days, setDays] = useState(90)
  const [secret, setSecret] = useState<string>()
  const toggle = (key: string) =>
    setPicked((p) => (p.includes(key) ? p.filter((k) => k !== key) : [...p, key]))
  const submit = (e: FormEvent) => {
    e.preventDefault()
    create.mutate(
      { name: name.trim(), projects: picked, expiresInDays: days },
      {
        onSuccess: (res) => {
          setSecret(res.token)
          setName('')
          setPicked([])
        },
      },
    )
  }
  return (
    <form onSubmit={submit} className="grid gap-3" aria-label="New personal access token">
      {create.error ? <ErrorAlert error={create.error} title="Could not create the token" /> : null}
      <div className="flex flex-wrap items-end gap-3">
        <div className="grid gap-2">
          <Label htmlFor="token-name">Token name</Label>
          <Input
            id="token-name"
            value={name}
            maxLength={100}
            placeholder="Claude Desktop"
            onChange={(e) => setName(e.target.value)}
            required
          />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="token-days">Expires in</Label>
          <NativeSelect id="token-days" value={days} onChange={(e) => setDays(Number(e.target.value))}>
            {lifetimes.map((d) => (
              <option key={d} value={d}>
                {d} days
              </option>
            ))}
          </NativeSelect>
        </div>
      </div>
      <fieldset className="grid gap-2">
        <legend className="mb-1 text-sm font-medium">Projects it can read</legend>
        <QueryState query={projects}>
          {(data) => (
            <div className="flex flex-wrap gap-x-4 gap-y-2">
              {data.items.map((p) => (
                <label key={p.key} className="flex items-center gap-2 text-sm">
                  <input type="checkbox" checked={picked.includes(p.key)} onChange={() => toggle(p.key)} />
                  {p.key} · {p.name}
                </label>
              ))}
            </div>
          )}
        </QueryState>
      </fieldset>
      <div>
        <Button type="submit" disabled={create.isPending || picked.length === 0}>
          Create token
        </Button>
      </div>
      {secret ? (
        <div className="grid gap-2 rounded-md border p-3" role="status">
          <p className="text-sm">
            Copy this token now: it is not shown again. Revoke it and create another if it is lost.
          </p>
          <code className="bg-muted rounded px-2 py-1 text-xs break-all" data-testid="token-secret">
            {secret}
          </code>
          <p className="text-sm">Connect an MCP client with it:</p>
          <pre className="bg-muted overflow-x-auto rounded px-2 py-1 text-xs" data-testid="token-mcp-snippet">
            {mcpSnippet(window.location.origin, secret)}
          </pre>
        </div>
      ) : null}
    </form>
  )
}

/** The signed-in person's read-only personal access tokens, for scripts and MCP clients (card #62). */
export function TokensSection() {
  const [page, setPage] = useState(1)
  const tokens = useTokens(page)
  return (
    <Card data-testid="tokens">
      <CardHeader>
        <CardTitle as="h2" className="text-lg">
          Personal access tokens
        </CardTitle>
        <CardDescription>
          Scripts and AI agents read Provenly as you with a token: only the projects you give it, never a
          change, and only until it expires (at most a year). Send it as{' '}
          <code>Authorization: Bearer &lt;token&gt;</code>.
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-6">
        <QueryState query={tokens}>
          {(data) => (
            <div className="grid gap-2">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Name</TableHead>
                    <TableHead>Token</TableHead>
                    <TableHead>Projects</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Expires</TableHead>
                    <TableHead>Last used</TableHead>
                    <TableHead />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {data.items.length === 0 && (
                    <TableRow>
                      <TableCell colSpan={7} className="text-muted-foreground">
                        No tokens yet.
                      </TableCell>
                    </TableRow>
                  )}
                  {data.items.map((t) => (
                    <TokenRow key={t.id} token={t} />
                  ))}
                </TableBody>
              </Table>
              <Pagination
                page={data.page}
                totalPages={data.totalPages}
                totalItems={data.totalItems}
                onPageChange={setPage}
              />
            </div>
          )}
        </QueryState>
        <NewToken />
      </CardContent>
    </Card>
  )
}
