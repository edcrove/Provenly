import { useProjects } from '@/api/queries'
import { NativeSelect } from '@/components/ui/select'

import { useCurrentProject } from './currentProject'

/** Narrows the test case and run lists to one project (remembered in this browser). */
export function ProjectSwitcher() {
  const { project, setProject } = useCurrentProject()
  const projects = useProjects()
  return (
    <NativeSelect
      aria-label="Current project"
      className="max-w-full"
      value={project}
      onChange={(e) => setProject(e.target.value)}
    >
      <option value="">All projects</option>
      {projects.data?.items.map((p) => (
        <option key={p.key} value={p.key}>
          {p.key} · {p.name}
        </option>
      ))}
    </NativeSelect>
  )
}
