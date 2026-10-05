import { FlaskConical } from 'lucide-react'
import { NavLink, Outlet } from 'react-router'

import { ProjectProvider } from '@/features/projects/ProjectProvider'
import { ProjectSwitcher } from '@/features/projects/ProjectSwitcher'
import { cn } from '@/lib/utils'

const links = [
  { to: '/test-cases', label: 'Test Cases' },
  { to: '/test-runs', label: 'Test Runs' },
  { to: '/projects', label: 'Projects' },
]

export function Layout() {
  return (
    <ProjectProvider>
      <div className="min-h-screen">
        <header className="border-b">
          <div className="mx-auto flex max-w-6xl flex-wrap items-center gap-x-6 gap-y-2 px-6 py-3">
            <span className="flex items-center gap-2 font-semibold">
              <FlaskConical className="size-5" /> Provenly
            </span>
            <nav className="flex gap-4 text-sm">
              {links.map((l) => (
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
            <div className="ml-auto max-w-full">
              <ProjectSwitcher />
            </div>
          </div>
        </header>
        <main className="mx-auto max-w-6xl px-6 py-6">
          <Outlet />
        </main>
      </div>
    </ProjectProvider>
  )
}
