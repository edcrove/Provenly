import { useState, type FormEvent } from 'react'

import type { GitHubConnection } from '@/api/client'
import { useGitHubConnection, useGitHubMutations } from '@/api/queries'
import { ErrorAlert, QueryState } from '@/components/QueryState'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { formatDateTime } from '@/lib/format'

function ConnectForm({ projectKey, current }: { projectKey: string; current: GitHubConnection | null }) {
  const { connect } = useGitHubMutations(projectKey)
  const [repository, setRepository] = useState(current?.repository ?? '')
  const [labels, setLabels] = useState(current?.labels ?? '')
  const [token, setToken] = useState('')
  // The stored token stays with its repository: another repository needs the token again (the server says so).
  const tokenRequired = !current || repository.trim().toLowerCase() !== current.repository.toLowerCase()
  const submit = (e: FormEvent) => {
    e.preventDefault()
    connect.mutate(
      { repository: repository.trim(), labels: labels.trim(), ...(token ? { token } : {}) },
      { onSuccess: () => setToken('') },
    )
  }
  return (
    <form onSubmit={submit} className="grid gap-3" aria-label="GitHub connection">
      {connect.error ? <ErrorAlert error={connect.error} title="Could not save the connection" /> : null}
      <div className="flex flex-wrap items-end gap-3">
        <div className="grid gap-2">
          <Label htmlFor="github-repository">Repository</Label>
          <Input
            id="github-repository"
            value={repository}
            placeholder="owner/name"
            onChange={(e) => setRepository(e.target.value)}
            required
          />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="github-labels">Labels (optional)</Label>
          <Input
            id="github-labels"
            value={labels}
            maxLength={200}
            placeholder="bug,qa"
            onChange={(e) => setLabels(e.target.value)}
          />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="github-token">{tokenRequired ? 'Token' : 'New token (optional)'}</Label>
          <Input
            id="github-token"
            type="password"
            autoComplete="off"
            value={token}
            maxLength={500}
            onChange={(e) => setToken(e.target.value)}
            required={tokenRequired}
          />
        </div>
        <Button type="submit" disabled={connect.isPending}>
          {current ? 'Save' : 'Connect'}
        </Button>
      </div>
    </form>
  )
}

function Connected({ projectKey, connection }: { projectKey: string; connection: GitHubConnection }) {
  const { sync, disconnect } = useGitHubMutations(projectKey)
  const error = sync.error ?? disconnect.error
  return (
    <div className="grid gap-3" data-testid="github-connection">
      <p className="text-sm">
        Mirrors the issues of <span className="font-mono">{connection.repository}</span>
        {connection.labels ? ` labelled ${connection.labels}` : ''} with token{' '}
        <span className="font-mono">{connection.tokenHint || '(unreadable: save a new one)'}</span>. Last
        sync: {connection.lastSyncedAt ? formatDateTime(connection.lastSyncedAt) : 'never'}.
      </p>
      {connection.lastError ? (
        <p className="text-destructive text-sm" role="alert">
          The last sync failed: {connection.lastError}
        </p>
      ) : null}
      {sync.data ? (
        <p className="text-sm" role="status">
          Synced: {sync.data.created} created, {sync.data.updated} updated.
        </p>
      ) : null}
      {error ? <ErrorAlert error={error} title="GitHub request failed" /> : null}
      <div className="flex flex-wrap gap-2">
        <Button size="sm" disabled={sync.isPending} onClick={() => sync.mutate()}>
          Sync issues now
        </Button>
        <Button
          size="sm"
          variant="outline"
          disabled={disconnect.isPending}
          onClick={() => disconnect.mutate()}
        >
          Disconnect
        </Button>
      </div>
    </div>
  )
}

/** A project's GitHub Issues connector (maintainers and administrators). */
export function GitHubSection({ projectKey }: { projectKey: string }) {
  const connection = useGitHubConnection(projectKey)
  return (
    <Card>
      <CardHeader>
        <CardTitle as="h2" className="text-lg">
          GitHub Issues
        </CardTitle>
        <CardDescription>
          Mirror a repository&apos;s issues (not pull requests) into this project&apos;s issues, read-only, to
          link them to test cases. The token is stored encrypted and never shown again.
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-6">
        <QueryState query={connection}>
          {(data) => (
            <>
              {data ? <Connected projectKey={projectKey} connection={data} /> : null}
              <ConnectForm key={data?.updatedAt ?? 'new'} projectKey={projectKey} current={data} />
            </>
          )}
        </QueryState>
      </CardContent>
    </Card>
  )
}
