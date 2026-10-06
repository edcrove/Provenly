import { useState, type FormEvent } from 'react'

import type { Webhook } from '@/api/client'
import { useWebhookDeliveries, useWebhookMutations, useWebhooks } from '@/api/queries'
import { Pagination } from '@/components/Pagination'
import { ErrorAlert, QueryState } from '@/components/QueryState'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { formatDateTime } from '@/lib/format'

const deliveryVariant = { succeeded: 'secondary', failed: 'destructive', pending: 'outline' } as const

function Deliveries({ projectKey, webhookId }: { projectKey: string; webhookId: number }) {
  const [page, setPage] = useState(1)
  const deliveries = useWebhookDeliveries(projectKey, webhookId, page, true)
  return (
    <QueryState query={deliveries}>
      {(data) => (
        <div className="grid gap-2" data-testid={`deliveries-${webhookId}`}>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Event</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Attempts</TableHead>
                <TableHead>Answer</TableHead>
                <TableHead>Created</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.items.length === 0 && (
                <TableRow>
                  <TableCell colSpan={5} className="text-muted-foreground">
                    No deliveries yet.
                  </TableCell>
                </TableRow>
              )}
              {data.items.map((d) => (
                <TableRow key={d.id}>
                  <TableCell className="font-mono">{d.event}</TableCell>
                  <TableCell>
                    <Badge variant={deliveryVariant[d.status]}>{d.status}</Badge>
                  </TableCell>
                  <TableCell>{d.attempts}</TableCell>
                  <TableCell>{d.lastError ?? d.lastStatusCode ?? '—'}</TableCell>
                  <TableCell className="whitespace-nowrap">{formatDateTime(d.createdAt)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          <Pagination
            page={data.page}
            totalPages={data.totalPages}
            totalItems={data.totalItems}
            onPageChange={(p) => setPage(p)}
          />
        </div>
      )}
    </QueryState>
  )
}

function WebhookRow({ webhook, projectKey }: { webhook: Webhook; projectKey: string }) {
  const m = useWebhookMutations(projectKey)
  const [open, setOpen] = useState(false)
  const last = webhook.lastDelivery
  const error = m.update.error ?? m.ping.error
  return (
    <>
      <TableRow data-testid={`webhook-${webhook.id}`}>
        <TableCell className="font-mono break-all">{webhook.url}</TableCell>
        <TableCell>
          <Badge variant={webhook.active ? 'secondary' : 'outline'}>
            {webhook.active ? 'active' : 'paused'}
          </Badge>
        </TableCell>
        <TableCell>
          {last ? (
            <Badge variant={deliveryVariant[last.status]}>{`${last.event}: ${last.status}`}</Badge>
          ) : (
            'None yet'
          )}
        </TableCell>
        <TableCell>
          <div className="flex flex-wrap gap-2">
            <Button
              size="sm"
              variant="outline"
              disabled={m.ping.isPending}
              onClick={() => m.ping.mutate(webhook.id)}
            >
              Send ping
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={m.update.isPending}
              onClick={() => m.update.mutate({ webhookId: webhook.id, body: { active: !webhook.active } })}
            >
              {webhook.active ? 'Pause' : 'Resume'}
            </Button>
            <Button size="sm" variant="ghost" aria-expanded={open} onClick={() => setOpen(!open)}>
              Deliveries
            </Button>
            {error ? <ErrorAlert error={error} title="Could not update the webhook" /> : null}
          </div>
        </TableCell>
      </TableRow>
      {open ? (
        <TableRow>
          <TableCell colSpan={4}>
            <Deliveries projectKey={projectKey} webhookId={webhook.id} />
          </TableCell>
        </TableRow>
      ) : null}
    </>
  )
}

function NewWebhook({ projectKey }: { projectKey: string }) {
  const { create } = useWebhookMutations(projectKey)
  const [url, setUrl] = useState('')
  const [secret, setSecret] = useState<string>()
  const submit = (e: FormEvent) => {
    e.preventDefault()
    create.mutate(url.trim(), {
      onSuccess: (res) => {
        setSecret(res.secret)
        setUrl('')
      },
    })
  }
  return (
    <form onSubmit={submit} className="grid gap-3" aria-label="New webhook">
      {create.error ? <ErrorAlert error={create.error} title="Could not add the webhook" /> : null}
      <div className="flex flex-wrap items-end gap-3">
        <div className="grid gap-2">
          <Label htmlFor="webhook-url">Endpoint URL</Label>
          <Input
            id="webhook-url"
            type="url"
            className="w-96 max-w-full"
            value={url}
            maxLength={2000}
            placeholder="https://hooks.example.com/provenly"
            onChange={(e) => setUrl(e.target.value)}
            required
          />
        </div>
        <Button type="submit" disabled={create.isPending}>
          Add webhook
        </Button>
      </div>
      {secret ? (
        <div className="grid gap-2 rounded-md border p-3" role="status">
          <p className="text-sm">
            Verify each delivery with this signing secret: <code>X-Provenly-Signature</code> is{' '}
            <code>sha256=</code> + the hex HMAC-SHA256 of <code>{'<X-Provenly-Timestamp>.<body>'}</code>. It
            is not shown again.
          </p>
          <code className="bg-muted rounded px-2 py-1 text-xs break-all" data-testid="webhook-secret">
            {secret}
          </code>
        </div>
      ) : null}
    </form>
  )
}

/** A project's webhooks (maintainers and administrators): completed runs are POSTed, signed and retried. */
export function WebhooksSection({ projectKey }: { projectKey: string }) {
  const hooks = useWebhooks(projectKey)
  return (
    <Card>
      <CardHeader>
        <CardTitle as="h2" className="text-lg">
          Webhooks
        </CardTitle>
        <CardDescription>
          Every completed run (CI, live or manual) is sent to these endpoints as JSON, signed, and retried for
          about 40 minutes until it gets a 2xx.
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-6">
        <QueryState query={hooks}>
          {(data) => (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Endpoint</TableHead>
                  <TableHead>State</TableHead>
                  <TableHead>Last delivery</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.items.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={4} className="text-muted-foreground">
                      No webhooks yet.
                    </TableCell>
                  </TableRow>
                )}
                {data.items.map((w) => (
                  <WebhookRow key={w.id} webhook={w} projectKey={projectKey} />
                ))}
              </TableBody>
            </Table>
          )}
        </QueryState>
        <NewWebhook projectKey={projectKey} />
      </CardContent>
    </Card>
  )
}
