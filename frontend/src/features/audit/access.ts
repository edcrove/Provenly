import { useProjects } from '@/api/queries'
import { can } from '@/lib/roles'

/**
 * Who reads the audit log (P20-5, Ed 2026-10-09): administrators all of it, maintainers the events of the projects they
 * maintain. `maintained` is null for administrators; `ready` is false while a non-administrator's projects load.
 */
export function useAuditAccess(isAdmin: boolean) {
  const projects = useProjects()
  const maintained = (projects.data?.items ?? []).filter((p) => can(p.myRole, 'maintainer')).map((p) => p.key)
  return {
    ready: isAdmin || !projects.isPending,
    allowed: isAdmin || maintained.length > 0,
    maintained: isAdmin ? null : maintained,
  }
}
