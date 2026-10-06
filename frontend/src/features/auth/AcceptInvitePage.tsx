import { useState, type FormEvent } from 'react'
import { useLocation, useNavigate } from 'react-router'

import { useAcceptInvitation } from '@/api/queries'
import { PageTitle } from '@/components/PageTitle'
import { ErrorAlert } from '@/components/QueryState'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ApiError } from '@/lib/problem'

import { invitationToken } from './links'

const empty = { username: '', displayName: '', email: '', password: '', confirm: '' }

/** Creates an account from an invitation link (/accept-invite#token=…) and signs in. */
export function AcceptInvitePage() {
  const location = useLocation()
  const token = invitationToken(location.hash, location.search)
  const navigate = useNavigate()
  const accept = useAcceptInvitation()
  const [values, setValues] = useState(empty)
  const [mismatch, setMismatch] = useState(false)
  const set = (field: keyof typeof empty) => (e: { target: { value: string } }) =>
    setValues((v) => ({ ...v, [field]: e.target.value }))
  const fieldErrors = accept.error instanceof ApiError ? accept.error.fieldErrors : {}

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const differs = values.password !== values.confirm
    setMismatch(differs)
    if (differs) return
    accept.mutate(
      {
        token,
        username: values.username,
        displayName: values.displayName,
        email: values.email || null,
        password: values.password,
      },
      { onSuccess: () => navigate('/', { replace: true }) },
    )
  }

  return (
    <div className="mx-auto grid min-h-screen max-w-md content-center px-4">
      <Card>
        <CardHeader>
          <PageTitle title="Join Provenly" />
          <CardTitle as="h1" className="text-xl">
            Join Provenly
          </CardTitle>
          <CardDescription>You were invited. Choose your username and password.</CardDescription>
        </CardHeader>
        <CardContent>
          {!token ? (
            <ErrorAlert
              error={new Error('This link has no invitation token.')}
              title="Invalid invitation link"
            />
          ) : (
            <form onSubmit={submit} className="grid gap-4">
              {accept.error ? (
                <ErrorAlert error={accept.error} title="Could not create your account" />
              ) : null}
              <div className="grid gap-2">
                <Label htmlFor="invite-username">Username</Label>
                <Input
                  id="invite-username"
                  autoComplete="username"
                  value={values.username}
                  onChange={set('username')}
                  required
                  aria-invalid={fieldErrors.username ? true : undefined}
                />
                <p className="text-muted-foreground text-xs">
                  3 to 32 lower-case letters, digits, &apos;.&apos;, &apos;-&apos; or &apos;_&apos;. It never
                  changes.
                </p>
              </div>
              <div className="grid gap-2">
                <Label htmlFor="invite-display-name">Display name</Label>
                <Input
                  id="invite-display-name"
                  value={values.displayName}
                  onChange={set('displayName')}
                  required
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="invite-email">Email (optional)</Label>
                <Input id="invite-email" type="email" value={values.email} onChange={set('email')} />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="invite-password">Password</Label>
                <Input
                  id="invite-password"
                  type="password"
                  autoComplete="new-password"
                  value={values.password}
                  onChange={set('password')}
                  required
                  minLength={10}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="invite-confirm">Repeat password</Label>
                <Input
                  id="invite-confirm"
                  type="password"
                  autoComplete="new-password"
                  value={values.confirm}
                  onChange={set('confirm')}
                  required
                  aria-invalid={mismatch ? true : undefined}
                  aria-describedby={mismatch ? 'invite-confirm-error' : undefined}
                />
                {mismatch ? (
                  <p id="invite-confirm-error" className="text-destructive text-sm">
                    The passwords do not match.
                  </p>
                ) : null}
              </div>
              <Button type="submit" disabled={accept.isPending}>
                Create account
              </Button>
            </form>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
