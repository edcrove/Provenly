import { useState, type FormEvent } from 'react'
import { useLocation, useNavigate } from 'react-router'

import { useResetPassword } from '@/api/queries'
import { PageTitle } from '@/components/PageTitle'
import { ErrorAlert } from '@/components/QueryState'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ApiError } from '@/lib/problem'

import { invitationToken } from './links'

/** Sets a new password from a reset link (/reset-password#token=…) an administrator gave, and signs in. */
export function ResetPasswordPage() {
  const location = useLocation()
  const token = invitationToken(location.hash, location.search)
  const navigate = useNavigate()
  const reset = useResetPassword()
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [mismatch, setMismatch] = useState(false)
  const passwordError = reset.error instanceof ApiError ? reset.error.fieldErrors.password : undefined

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const differs = password !== confirm
    setMismatch(differs)
    if (differs) return
    reset.mutate({ token, password }, { onSuccess: () => navigate('/', { replace: true }) })
  }

  return (
    <div className="mx-auto grid min-h-screen max-w-md content-center px-4">
      <Card>
        <CardHeader>
          <PageTitle title="Choose a new password" />
          <CardTitle as="h1" className="text-xl">
            Choose a new password
          </CardTitle>
          <CardDescription>The link works once. Your other sessions end.</CardDescription>
        </CardHeader>
        <CardContent>
          {!token ? (
            <ErrorAlert error={new Error('This link has no reset token.')} title="Invalid reset link" />
          ) : (
            <form onSubmit={submit} className="grid gap-4">
              {reset.error && !passwordError ? (
                <ErrorAlert error={reset.error} title="Could not set your password" />
              ) : null}
              <div className="grid gap-2">
                <Label htmlFor="reset-password">New password</Label>
                <Input
                  id="reset-password"
                  type="password"
                  autoComplete="new-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  required
                  minLength={10}
                  aria-invalid={passwordError ? true : undefined}
                  aria-describedby={passwordError ? 'reset-password-error' : 'reset-password-hint'}
                />
                <p className="text-muted-foreground text-xs" id="reset-password-hint">
                  At least 10 characters, at most 72 bytes (letters outside ASCII count as 2 to 4).
                </p>
                {passwordError ? (
                  <p id="reset-password-error" className="text-destructive text-sm">
                    Password {passwordError}
                  </p>
                ) : null}
              </div>
              <div className="grid gap-2">
                <Label htmlFor="reset-confirm">Repeat new password</Label>
                <Input
                  id="reset-confirm"
                  type="password"
                  autoComplete="new-password"
                  value={confirm}
                  onChange={(e) => setConfirm(e.target.value)}
                  required
                  aria-invalid={mismatch ? true : undefined}
                  aria-describedby={mismatch ? 'reset-confirm-error' : undefined}
                />
                {mismatch ? (
                  <p id="reset-confirm-error" className="text-destructive text-sm">
                    The passwords do not match.
                  </p>
                ) : null}
              </div>
              <Button type="submit" disabled={reset.isPending}>
                Set password and sign in
              </Button>
            </form>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
