import { Check, ChevronDown } from 'lucide-react'
import { useDeferredValue, useEffect, useId, useRef, useState, type KeyboardEvent } from 'react'
import { Link } from 'react-router'

import { useProject, useProjects } from '@/api/queries'
import { errorMessage } from '@/lib/problem'
import { cn } from '@/lib/utils'

import { projectLabel, useCurrentProject } from './currentProject'

interface Option {
  key: string
  label: string
}

/**
 * Picks the project every list, dashboard and settings page works on (remembered in this browser). A button naming the
 * current project opens a searchable list (combobox): type to filter by key or name, arrows to move, Enter to pick,
 * Escape to close. "All projects" widens the lists to every project the user can see. Typing searches the server, so
 * every project is found however many there are (the list shows the first SHOWN).
 */
const SHOWN = 50

export function ProjectSwitcher({ onPicked }: { onPicked?: () => void }) {
  const { project, setProject } = useCurrentProject()
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const q = useDeferredValue(query.trim())
  const search = useProjects(1, SHOWN, q)
  const found = search.data
  const projects = found?.items ?? []
  const current = useProject(project || undefined).data
  const [active, setActive] = useState(0)
  const button = useRef<HTMLButtonElement>(null)
  const panel = useRef<HTMLDivElement>(null)
  const listId = useId()

  // A stored project whose read failed (server or network) keeps the scope and its key as the label.
  const label = current ? projectLabel(current) : project || 'All projects'
  const options: Option[] = [
    ...(q ? [] : [{ key: '', label: 'All projects' }]),
    ...projects.map((p) => ({ key: p.key, label: projectLabel(p) })),
  ]
  const hidden = (found?.totalItems ?? 0) - projects.length

  // A click anywhere else closes the list.
  useEffect(() => {
    if (!open) return
    const close = (e: MouseEvent) => {
      if (!panel.current?.contains(e.target as Node) && !button.current?.contains(e.target as Node))
        setOpen(false)
    }
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [open])

  const show = () => {
    setQuery('')
    setActive(
      Math.max(
        0,
        options.findIndex((o) => o.key === project),
      ),
    )
    setOpen(true)
  }
  const hide = () => {
    setOpen(false)
    button.current?.focus()
  }
  const pick = (key: string) => {
    setProject(key)
    hide()
    onPicked?.()
  }
  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'ArrowDown') setActive((i) => Math.min(i + 1, options.length - 1))
    else if (e.key === 'ArrowUp') setActive((i) => Math.max(i - 1, 0))
    else if (e.key === 'Enter' && options[active]) pick(options[active].key)
    else if (e.key === 'Escape') hide()
    else return
    e.preventDefault()
  }

  return (
    <div className="relative min-w-0">
      <button
        ref={button}
        type="button"
        aria-label={`Current project: ${label}`}
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => (open ? hide() : show())}
        className="hover:bg-accent flex max-w-56 min-w-0 items-center gap-1 rounded-md border px-2.5 py-1.5 text-sm font-medium"
        data-testid="project-switcher"
      >
        <span className="truncate">{label}</span>
        <ChevronDown className="size-4 shrink-0 opacity-60" aria-hidden />
      </button>
      {open ? (
        <div
          ref={panel}
          className="bg-background absolute left-0 z-20 mt-1 grid w-72 max-w-[calc(100vw-2rem)] gap-1 rounded-md border p-2 shadow-md"
        >
          <input
            autoFocus
            role="combobox"
            aria-label="Find a project"
            aria-expanded
            aria-controls={listId}
            aria-activedescendant={options[active] ? `${listId}-${active}` : undefined}
            placeholder="Find a project"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value)
              setActive(0)
            }}
            onKeyDown={onKeyDown}
            className="border-input h-8 rounded-md border px-2 text-sm outline-none"
          />
          <ul id={listId} role="listbox" aria-label="Projects" className="max-h-72 overflow-y-auto">
            {options.map((o, i) => (
              <li
                key={o.key || '*'}
                id={`${listId}-${i}`}
                role="option"
                aria-selected={o.key === project}
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => pick(o.key)}
                onMouseEnter={() => setActive(i)}
                className={cn(
                  'flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm',
                  i === active && 'bg-accent',
                )}
              >
                <Check className={cn('size-4 shrink-0', o.key !== project && 'invisible')} aria-hidden />
                <span className="truncate">{o.label}</span>
              </li>
            ))}
            {options.length === 0 && found ? (
              <li className="text-muted-foreground px-2 py-1.5 text-sm">No project matches.</li>
            ) : null}
            {/* The list says when it is still searching or could not load (it used to be silently empty). */}
            {!found && search.isPending ? (
              <li className="text-muted-foreground px-2 py-1.5 text-sm" data-testid="switcher-searching">
                Searching…
              </li>
            ) : null}
            {search.isError ? (
              <li role="alert" className="text-destructive px-2 py-1.5 text-sm">
                Could not load the projects: {errorMessage(search.error)}
              </li>
            ) : null}
          </ul>
          {hidden > 0 ? (
            <p className="text-muted-foreground px-2 text-xs" data-testid="switcher-more">
              Showing {projects.length} of {found!.totalItems}: type to find the others.
            </p>
          ) : null}
          <Link
            to="/projects"
            className="border-t px-2 pt-2 text-sm underline"
            onClick={() => setOpen(false)}
          >
            View all projects
          </Link>
        </div>
      ) : null}
    </div>
  )
}
