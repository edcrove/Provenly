import { Link } from 'react-router'

import { useProject, useProjects } from '@/api/queries'
import { Button } from '@/components/ui/button'

import { projectLabel, useCurrentProject } from './currentProject'

/** Says which project a list shows ("TC · Default" or "All projects"): the lists follow the project switcher. */
export function ScopeLabel() {
  const { project } = useCurrentProject()
  const current = useProject(project || undefined).data
  return (
    <span className="text-muted-foreground text-sm font-normal" data-testid="scope-label">
      {current ? projectLabel(current) : 'All projects'}
    </span>
  )
}

/** How many projects the chooser offers before sending to the projects page. */
const CHOOSER_SIZE = 8

/** For pages that belong to one project (dashboard, suites, requirements, issues) while "All projects" is chosen. */
export function ProjectChooser({ what }: { what: string }) {
  const { setProject } = useCurrentProject()
  // The first few projects, and how many there are: an instance may have more than one page of them.
  const page = useProjects(1, CHOOSER_SIZE).data
  const projects = page?.items ?? []
  const more = (page?.totalItems ?? 0) - projects.length
  return (
    <div className="grid gap-3" data-testid="project-chooser">
      <p className="text-muted-foreground text-sm">Pick a project to see its {what}.</p>
      <div className="flex flex-wrap gap-2">
        {projects.map((p) => (
          <Button key={p.key} size="sm" variant="outline" onClick={() => setProject(p.key)}>
            {projectLabel(p)}
          </Button>
        ))}
      </div>
      {more > 0 ? (
        <p className="text-sm">
          {more} more:{' '}
          <Link to="/projects" className="underline">
            view all projects
          </Link>{' '}
          or use the project switcher.
        </p>
      ) : null}
    </div>
  )
}

/**
 * Where a detail page sits: its project, its section (linked) and itself. Opening something of another project does not
 * change the current one silently: the trail names the real project and offers to switch to it.
 */
export function Breadcrumb({
  projectKey,
  section,
  to,
  current,
}: {
  projectKey: string
  section: string
  to: string
  current: string
}) {
  const { project, setProject } = useCurrentProject()
  const owner = useProject(projectKey || undefined).data
  return (
    <nav
      aria-label="Breadcrumb"
      className="text-muted-foreground flex flex-wrap items-center gap-x-2 text-sm"
    >
      <span>{owner ? projectLabel(owner) : projectKey}</span>
      <span aria-hidden>/</span>
      <Link to={to} className="underline">
        {section}
      </Link>
      <span aria-hidden>/</span>
      <span aria-current="page" className="text-foreground">
        {current}
      </span>
      {projectKey && project !== projectKey ? (
        <Button size="sm" variant="link" className="h-auto p-0" onClick={() => setProject(projectKey)}>
          Switch to {projectKey}
        </Button>
      ) : null}
    </nav>
  )
}
