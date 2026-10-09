import { useProject } from '@/api/queries'
import type { ProjectRole } from '@/lib/roles'

/** The signed-in user's role in a project (undefined while loading or when not a member). */
export function useProjectRole(projectKey: string | undefined): ProjectRole | undefined {
  return useProject(projectKey).data?.myRole
}
