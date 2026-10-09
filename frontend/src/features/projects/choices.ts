import { useProject, useProjects } from '@/api/queries'
import type { Project } from '@/api/client'

import { useCurrentProject } from './currentProject'

/**
 * The projects a form offers: the first page of the projects list plus the current project when it is not on it (an
 * instance may have more projects than a page). `more` counts the ones not offered: the header switcher finds them.
 */
export function useProjectChoices() {
  const { project } = useCurrentProject()
  const list = useProjects()
  const current = useProject(project || undefined).data
  const items: Project[] = list.data?.items ?? []
  const all = current && !items.some((p) => p.key === current.key) ? [current, ...items] : items
  return { query: list, items: all, more: Math.max(0, (list.data?.totalItems ?? 0) - all.length) }
}

/** The hint a form shows when it does not offer every project. */
export const moreProjectsHint = (more: number) =>
  `${more} more project${more === 1 ? '' : 's'} not listed: choose one in the header's project switcher to offer it here.`
