import { useState, type FormEvent } from 'react'

import { useChangePassword } from '@/api/queries'
import { PageTitle } from '@/components/PageTitle'
import { ErrorAlert } from '@/components/QueryState'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

import { useCurrentUser } from './currentUser'

const empty = { currentPassword: '', newPassword: '', confirm: '' }

/** The signed-in user's account: who they are and a password change (which signs other sessions out). */
export function AccountPage() {
  const user = useCurrentUser()
  const change = useChangePassword()
  const [values, setValues] = useState(empty)
  const [mismatch, setMismatch] = useState(false)
  const [done, setDone] = useState(false)
  const set = (field: keyof typeof empty) => (e: { target: { value: string } }) =>
    setValues((v) => ({ ...v, [field]: e.target.value }))

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const differs = values.newPassword !== values.confirm
    setMismatch(differs)
    setDone(false)
    if (differs) return
    change.mutate(
      { currentPassword: values.currentPassword, newPassword: values.newPassword },
      {
        onSuccess: () => {
          setValues(empty)
          setDone(true)
        },
      },
    )
  }
  return (
    <Card className="max-w-2xl">
      <CardHeader>
        <PageTitle title="Account" />
        <CardTitle as="h1" className="text-xl">
          Account
        </CardTitle>
        <CardDescription data-testid="account-identity">
          {user.displayName} · @{user.username}
          {user.email ? ` · ${user.email}` : ''}
          {user.isAdmin ? ' · administrator' : ''}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={submit} className="grid gap-4" aria-label="Change password">
          <h2 className="font-medium">Change password</h2>
          {change.error ? <ErrorAlert error={change.error} title="Could not change the password" /> : null}
          {done ? (
            <p role="status" className="text-sm">
              Password changed. Other sessions were signed out.
            </p>
          ) : null}
          <div className="grid gap-2">
            <Label htmlFor="current-password">Current password</Label>
            <Input
              id="current-password"
              type="password"
              autoComplete="current-password"
              value={values.currentPassword}
              onChange={set('currentPassword')}
              required
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="new-password">New password</Label>
            <Input
              id="new-password"
              type="password"
              autoComplete="new-password"
              value={values.newPassword}
              onChange={set('newPassword')}
              required
              minLength={10}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="confirm-password">Repeat new password</Label>
            <Input
              id="confirm-password"
              type="password"
              autoComplete="new-password"
              value={values.confirm}
              onChange={set('confirm')}
              required
              aria-invalid={mismatch ? true : undefined}
            />
            {mismatch ? <p className="text-destructive text-sm">The new passwords do not match.</p> : null}
          </div>
          <div>
            <Button type="submit" disabled={change.isPending}>
              Change password
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  )
}
