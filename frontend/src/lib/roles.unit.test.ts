import { describe, expect, it } from 'vitest'

import { can, memberRoles, roleDescriptions } from './roles'

describe('roles', () => {
  it('orders roles: admin > maintainer > member > viewer; no role allows nothing', () => {
    expect(can('admin', 'maintainer')).toBe(true)
    expect(can('maintainer', 'maintainer')).toBe(true)
    expect(can('member', 'maintainer')).toBe(false)
    expect(can('member', 'member')).toBe(true)
    expect(can('viewer', 'member')).toBe(false)
    expect(can('viewer', 'viewer')).toBe(true)
    expect(can(undefined, 'viewer')).toBe(false)
  })

  it('describes every grantable role', () => {
    expect(memberRoles).toEqual(['maintainer', 'member', 'viewer'])
    for (const r of memberRoles) expect(roleDescriptions[r]).toBeTruthy()
  })
})
