import { useState, type FormEvent } from 'react'
import { useSearchParams } from 'react-router'

import type { Invitation } from '@/api/client'
import { useCreateInvitation, useInvitations, useRevokeInvitation, useUsers } from '@/api/queries'
import { PageTitle } from '@/components/PageTitle'
import { Pagination } from '@/components/Pagination'
import { ErrorAlert, QueryState } from '@/components/QueryState'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { formatDateTime } from '@/lib/format'
import { positiveInt } from '@/lib/status'

import { useCurrentUser } from './currentUser'
import { invitationLink } from './links'

const statusVariant = {
  pending: 'default',
  accepted: 'success',
  revoked: 'outline',
  expired: 'secondary',
} as const

function NewInvitation() {
  const create = useCreateInvitation()
  const [email, setEmail] = useState('')
  const [note, setNote] = useState('')
  const [link, setLink] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const submit = (e: FormEvent) => {
    e.preventDefault()
    setCopied(false)
    create.mutate(
      { email: email || null, note },
      {
        onSuccess: (res) => {
          setLink(invitationLink(res.token))
          setEmail('')
          setNote('')
        },
      },
    )
  }
  const copy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
    } catch {
      setCopied(false)
    }
  }
  return (
    <form onSubmit={submit} className="grid gap-3" aria-label="New invitation">
      {create.error ? <ErrorAlert error={create.error} title="Could not create the invitation" /> : null}
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="grid gap-2">
          <Label htmlFor="invite-email-new">Email (optional)</Label>
          <Input
            id="invite-email-new"
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="invite-note">Note (optional)</Label>
          <Input id="invite-note" value={note} maxLength={200} onChange={(e) => setNote(e.target.value)} />
        </div>
      </div>
      <div>
        <Button type="submit" disabled={create.isPending}>
          Create invitation link
        </Button>
      </div>
      {link ? (
        <div className="grid gap-2 rounded-md border p-3" role="status">
          <p className="text-sm">
            Send this link to the person you invite. It works once, for 7 days, and is not shown again.
          </p>
          <code className="bg-muted rounded px-2 py-1 text-xs break-all" data-testid="invitation-link">
            {link}
          </code>
          <div className="flex items-center gap-2">
            <Button type="button" size="sm" variant="outline" onClick={() => void copy(link)}>
              Copy link
            </Button>
            {copied ? <span className="text-muted-foreground text-sm">Copied</span> : null}
          </div>
        </div>
      ) : null}
    </form>
  )
}

function InvitationRow({ invitation }: { invitation: Invitation }) {
  const revoke = useRevokeInvitation()
  return (
    <TableRow data-testid={`invitation-${invitation.id}`}>
      <TableCell>
        <Badge variant={statusVariant[invitation.status]}>{invitation.status}</Badge>
      </TableCell>
      <TableCell>{invitation.email ?? '—'}</TableCell>
      <TableCell>{invitation.note || '—'}</TableCell>
      <TableCell className="whitespace-nowrap">{formatDateTime(invitation.createdAt)}</TableCell>
      <TableCell className="whitespace-nowrap">{formatDateTime(invitation.expiresAt)}</TableCell>
      <TableCell>
        {invitation.status === 'pending' ? (
          <Button
            size="sm"
            variant="outline"
            disabled={revoke.isPending}
            onClick={() => revoke.mutate(invitation.id)}
          >
            Revoke
          </Button>
        ) : null}
        {revoke.error ? <ErrorAlert error={revoke.error} title="Could not revoke" /> : null}
      </TableCell>
    </TableRow>
  )
}

/** Administrators: who has an account, and invitation links for new people. */
export function UsersPage() {
  const me = useCurrentUser()
  const [params, setParams] = useSearchParams()
  const page = positiveInt(params.get('page'), 1)
  const invPage = positiveInt(params.get('invitations'), 1)
  const admin = me.isAdmin
  const users = useUsers(page, admin)
  const invitations = useInvitations(invPage, admin)
  const setParam = (name: string) => (p: number, replace?: boolean) => {
    const merged = new URLSearchParams(params)
    merged.set(name, String(p))
    setParams(merged, { replace })
  }

  if (!admin)
    return (
      <Card>
        <CardHeader>
          <PageTitle title="Users" />
          <CardTitle as="h1" className="text-xl">
            Users
          </CardTitle>
        </CardHeader>
        <CardContent>
          <ErrorAlert
            error={new Error('Only administrators can manage users and invitations.')}
            title="Not allowed"
          />
        </CardContent>
      </Card>
    )

  return (
    <div className="grid gap-6">
      <Card>
        <CardHeader>
          <PageTitle title="Users" />
          <CardTitle as="h1" className="text-xl">
            Users
          </CardTitle>
          <CardDescription>
            Accounts are never deleted. New people join with an invitation link.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <QueryState query={users}>
            {(data) => (
              <>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Username</TableHead>
                      <TableHead>Name</TableHead>
                      <TableHead>Email</TableHead>
                      <TableHead>Role</TableHead>
                      <TableHead>Since</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {data.items.map((u) => (
                      <TableRow key={u.id} data-testid={`user-${u.username}`}>
                        <TableCell className="font-mono">{u.username}</TableCell>
                        <TableCell>{u.displayName}</TableCell>
                        <TableCell>{u.email ?? '—'}</TableCell>
                        <TableCell>{u.isAdmin ? <Badge>admin</Badge> : 'member'}</TableCell>
                        <TableCell className="whitespace-nowrap">{formatDateTime(u.createdAt)}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
                <Pagination
                  page={data.page}
                  totalPages={data.totalPages}
                  totalItems={data.totalItems}
                  onPageChange={setParam('page')}
                />
              </>
            )}
          </QueryState>
        </CardContent>
      </Card>
      <Card>
        <CardHeader>
          <CardTitle as="h2">Invitations</CardTitle>
        </CardHeader>
        <CardContent className="grid gap-6">
          <NewInvitation />
          <QueryState query={invitations}>
            {(data) => (
              <>
                <Table aria-label="Invitations">
                  <TableHeader>
                    <TableRow>
                      <TableHead>Status</TableHead>
                      <TableHead>Email</TableHead>
                      <TableHead>Note</TableHead>
                      <TableHead>Created</TableHead>
                      <TableHead>Expires</TableHead>
                      <TableHead />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {data.items.length === 0 && (
                      <TableRow>
                        <TableCell colSpan={6} className="text-muted-foreground">
                          No invitations yet.
                        </TableCell>
                      </TableRow>
                    )}
                    {data.items.map((i) => (
                      <InvitationRow key={i.id} invitation={i} />
                    ))}
                  </TableBody>
                </Table>
                <Pagination
                  page={data.page}
                  totalPages={data.totalPages}
                  totalItems={data.totalItems}
                  onPageChange={setParam('invitations')}
                />
              </>
            )}
          </QueryState>
        </CardContent>
      </Card>
    </div>
  )
}
