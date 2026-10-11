import { useProject } from '@/api/queries'
import type { ProjectRole } from '@/lib/roles'

/**
 * The signed-in user's role in a project (undefined while loading or when not a member). An archived project is
 * read-only for everyone, so its pages offer what a viewer sees until an administrator unarchives it.
 */
export function useProjectRole(projectKey: string | undefined): ProjectRole | undefined {
  const project = useProject(projectKey).data
  return project?.archivedAt ? 'viewer' : project?.myRole
}
