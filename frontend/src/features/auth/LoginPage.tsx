import { FlaskConical } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { Navigate, useNavigate, useSearchParams } from 'react-router'

import { useLogin, useMe } from '@/api/queries'
import { PageTitle } from '@/components/PageTitle'
import { ErrorAlert } from '@/components/QueryState'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

import { safeNext } from './links'

export function LoginPage() {
  const [params] = useSearchParams()
  const next = safeNext(params.get('next'))
  const navigate = useNavigate()
  const me = useMe()
  const login = useLogin()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')

  if (me.data) return <Navigate to={next} replace />

  const submit = (e: FormEvent) => {
    e.preventDefault()
    login.mutate({ username, password }, { onSuccess: () => navigate(next, { replace: true }) })
  }
  return (
    <div className="mx-auto grid min-h-screen max-w-sm content-center px-4">
      <Card>
        <CardHeader>
          <PageTitle title="Sign in" />
          <CardTitle as="h1" className="flex items-center gap-2 text-xl">
            <FlaskConical className="size-5" /> Sign in to Provenly
          </CardTitle>
          <CardDescription>Accounts are created by invitation from an administrator.</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={submit} className="grid gap-4">
            {login.error ? <ErrorAlert error={login.error} title="Could not sign in" /> : null}
            <div className="grid gap-2">
              <Label htmlFor="login-username">Username</Label>
              <Input
                id="login-username"
                autoComplete="username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                required
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="login-password">Password</Label>
              <Input
                id="login-password"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
              />
            </div>
            <Button type="submit" disabled={login.isPending}>
              Sign in
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
