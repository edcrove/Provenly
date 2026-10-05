import { useProjects } from '@/api/queries'
import type { ProjectRole } from '@/lib/roles'

/** The signed-in user's role in a project (undefined while loading or when not a member). */
export function useProjectRole(projectKey: string | undefined): ProjectRole | undefined {
  const projects = useProjects()
  return projects.data?.items.find((p) => p.key === projectKey)?.myRole
}
