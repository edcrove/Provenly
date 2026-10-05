import { FlaskConical } from 'lucide-react'
import { Link, Navigate, NavLink, Outlet, useLocation, useNavigate } from 'react-router'

import { useLogout, useMe } from '@/api/queries'
import { ErrorAlert } from '@/components/QueryState'
import { Button } from '@/components/ui/button'
import { ProjectProvider } from '@/features/projects/ProjectProvider'
import { ProjectSwitcher } from '@/features/projects/ProjectSwitcher'
import { ApiError } from '@/lib/problem'
import { cn } from '@/lib/utils'

const links = [
  { to: '/dashboard', label: 'Dashboard' },
  { to: '/test-cases', label: 'Test Cases' },
  { to: '/test-runs', label: 'Test Runs' },
  { to: '/suites', label: 'Suites' },
  { to: '/requirements', label: 'Requirements' },
  { to: '/issues', label: 'Issues' },
  { to: '/projects', label: 'Projects' },
]

/** The signed-in shell: without a session every page sends to sign-in and comes back afterwards. */
export function Layout() {
  const me = useMe()
  const location = useLocation()
  const navigate = useNavigate()
  const logout = useLogout()

  if (me.isPending) return <p className="text-muted-foreground p-6 text-sm">Loading…</p>
  if (me.error instanceof ApiError && me.error.status === 401) {
    const next = location.pathname + location.search
    return <Navigate to={`/login?next=${encodeURIComponent(next)}`} replace />
  }
  if (!me.data)
    return (
      <div className="p-6">
        <ErrorAlert error={me.error} />
      </div>
    )
  const user = me.data
  const nav = user.isAdmin ? [...links, { to: '/users', label: 'Users' }] : links
  return (
    <ProjectProvider>
      <div className="min-h-screen">
        <header className="border-b">
          <div className="mx-auto flex max-w-6xl flex-wrap items-center gap-x-6 gap-y-2 px-6 py-3">
            <span className="flex items-center gap-2 font-semibold">
              <FlaskConical className="size-5" /> Provenly
            </span>
            <nav className="flex flex-wrap gap-4 text-sm">
              {nav.map((l) => (
                <NavLink
                  key={l.to}
                  to={l.to}
                  className={({ isActive }) =>
                    cn('hover:text-foreground', isActive ? 'font-medium' : 'text-muted-foreground')
                  }
                >
                  {l.label}
                </NavLink>
              ))}
            </nav>
            <div className="ml-auto flex max-w-full flex-wrap items-center gap-3">
              <ProjectSwitcher />
              <Link to="/account" className="text-sm underline" data-testid="current-user">
                {user.displayName}
              </Link>
              <Button
                size="sm"
                variant="ghost"
                disabled={logout.isPending}
                onClick={() =>
                  logout.mutate(undefined, { onSettled: () => navigate('/login', { replace: true }) })
                }
              >
                Sign out
              </Button>
            </div>
          </div>
        </header>
        <main className="mx-auto max-w-6xl px-6 py-6">
          <Outlet context={user} />
        </main>
      </div>
    </ProjectProvider>
  )
}
