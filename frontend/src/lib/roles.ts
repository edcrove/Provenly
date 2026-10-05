import type { components } from '@/api/schema'

export type ProjectRole = components['schemas']['Project']['myRole']
export type MemberRole = components['schemas']['ProjectRole']

const rank: Record<ProjectRole, number> = { viewer: 1, member: 2, maintainer: 3, admin: 4 }

/** Whether a role (undefined: not a member) allows what needs min. Mirrors the API's checks to hide actions. */
export function can(role: ProjectRole | undefined, min: MemberRole): boolean {
  return role !== undefined && rank[role] >= rank[min]
}

export const memberRoles: MemberRole[] = ['maintainer', 'member', 'viewer']

export const roleDescriptions: Record<MemberRole, string> = {
  maintainer: 'manages the project, its members and deprecations',
  member: 'creates and edits test cases and steps',
  viewer: 'reads only',
}
