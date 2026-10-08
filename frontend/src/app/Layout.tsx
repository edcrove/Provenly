import { FlaskConical, Menu, X } from 'lucide-react'
import { useState } from 'react'
import { Link, Navigate, NavLink, Outlet, useLocation, useNavigate } from 'react-router'

import { useLogout, useMe } from '@/api/queries'
import { ErrorAlert } from '@/components/QueryState'
import { Button } from '@/components/ui/button'
import { useCurrentProject } from '@/features/projects/currentProject'
import { ProjectProvider } from '@/features/projects/ProjectProvider'
import { ProjectSwitcher } from '@/features/projects/ProjectSwitcher'
import { ApiError } from '@/lib/problem'
import { cn } from '@/lib/utils'

interface Item {
  to: string
  label: string
}

/**
 * What a project holds, in the order of a working day: how it is doing, what CI and testers reported, the test cases
 * behind it, how they are grouped, what they cover and the issues found; then the project's own settings (only when one
 * project is chosen: "All projects" has no settings).
 */
function projectItems(project: string): Item[] {
  return [
    { to: '/dashboard', label: 'Dashboard' },
    { to: '/test-runs', label: 'Test Runs' },
    { to: '/test-cases', label: 'Test Cases' },
    { to: '/suites', label: 'Suites' },
    { to: '/requirements', label: 'Requirements' },
    { to: '/issues', label: 'Issues' },
    ...(project ? [{ to: `/projects/${project}`, label: 'Settings' }] : []),
  ]
}

const linkClass = ({ isActive }: { isActive: boolean }) =>
  cn(
    'hover:text-foreground whitespace-nowrap',
    isActive ? 'text-foreground font-medium' : 'text-muted-foreground',
  )

/** The signed-in shell: without a session every page sends to sign-in and comes back afterwards. */
export function Layout() {
  const me = useMe()
  const location = useLocation()

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
  return (
    <ProjectProvider>
      <div className="min-h-screen">
        <Header displayName={me.data.displayName} isAdmin={me.data.isAdmin} />
        <main className="mx-auto max-w-7xl px-4 py-6 xl:px-6">
          <Outlet context={me.data} />
        </main>
      </div>
    </ProjectProvider>
  )
}

/**
 * Left: the project switcher, then what belongs to the chosen project. Right: the workspace (every project, and for
 * administrators users and the audit log) and the account. Below xl (1280 px) the items move into a Menu so the header stays one
 * row on a phone.
 */
function Header({ displayName, isAdmin }: { displayName: string; isAdmin: boolean }) {
  const { project } = useCurrentProject()
  const navigate = useNavigate()
  const logout = useLogout()
  const [menu, setMenu] = useState(false)
  const items = projectItems(project)
  const workspace: Item[] = [
    { to: '/projects', label: 'Projects' },
    ...(isAdmin
      ? [
          { to: '/users', label: 'Users' },
          { to: '/audit', label: 'Audit' },
        ]
      : []),
  ]
  const signOut = () => logout.mutate(undefined, { onSettled: () => navigate('/login', { replace: true }) })
  const close = () => setMenu(false)
  return (
    <header className="border-b">
      <div className="mx-auto flex h-14 max-w-7xl items-center gap-3 px-4 xl:gap-5 xl:px-6">
        <Link
          to="/dashboard"
          className="flex shrink-0 items-center gap-2 font-semibold"
          aria-label="Provenly home"
        >
          <FlaskConical className="size-5" /> <span className="hidden sm:inline">Provenly</span>
        </Link>
        <ProjectSwitcher onPicked={close} />
        <nav aria-label="Project pages" className="hidden gap-4 text-sm xl:flex">
          {items.map((l) => (
            <NavLink key={l.to} to={l.to} end={l.label === 'Settings'} className={linkClass}>
              {l.label}
            </NavLink>
          ))}
        </nav>
        <div className="ml-auto hidden items-center gap-4 text-sm xl:flex">
          <nav aria-label="Workspace" className="flex gap-4 border-r pr-4">
            {workspace.map((l) => (
              <NavLink key={l.to} to={l.to} end className={linkClass}>
                {l.label}
              </NavLink>
            ))}
          </nav>
          <Link to="/account" className="whitespace-nowrap underline" data-testid="current-user">
            {displayName}
          </Link>
          <Button size="sm" variant="ghost" disabled={logout.isPending} onClick={signOut}>
            Sign out
          </Button>
        </div>
        <Button
          size="sm"
          variant="ghost"
          className="ml-auto xl:hidden"
          aria-expanded={menu}
          aria-controls="main-menu"
          onClick={() => setMenu(!menu)}
        >
          {menu ? <X className="size-4" aria-hidden /> : <Menu className="size-4" aria-hidden />} Menu
        </Button>
      </div>
      {menu ? (
        <div id="main-menu" className="grid gap-3 border-t px-4 py-3 text-sm xl:hidden">
          <nav aria-label="Project pages" className="grid gap-2">
            {items.map((l) => (
              <NavLink
                key={l.to}
                to={l.to}
                end={l.label === 'Settings'}
                className={linkClass}
                onClick={close}
              >
                {l.label}
              </NavLink>
            ))}
          </nav>
          <nav aria-label="Workspace" className="grid gap-2 border-t pt-3">
            {workspace.map((l) => (
              <NavLink key={l.to} to={l.to} end className={linkClass} onClick={close}>
                {l.label}
              </NavLink>
            ))}
            <Link to="/account" className="underline" onClick={close}>
              {displayName}
            </Link>
          </nav>
          <div>
            <Button size="sm" variant="ghost" disabled={logout.isPending} onClick={signOut}>
              Sign out
            </Button>
          </div>
        </div>
      ) : null}
    </header>
  )
}
