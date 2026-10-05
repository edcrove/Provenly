import { describe, expect, it, vi } from 'vitest'

import { invitationLink, safeNext } from './links'

describe('auth links', () => {
  it('only follows next paths inside the app', () => {
    expect(safeNext('/test-runs?page=2')).toBe('/test-runs?page=2')
    for (const bad of [null, '', 'test-runs', 'https://evil.example', '//evil.example/x', '/\\evil.example'])
      expect(safeNext(bad)).toBe('/')
  })

  it('builds the invitation link from the web origin', () => {
    expect(invitationLink('a b/c', 'https://provenly.example')).toBe(
      'https://provenly.example/accept-invite?token=a%20b%2Fc',
    )
    vi.stubGlobal('location', { origin: 'http://localhost:3000' })
    expect(invitationLink('tok')).toBe('http://localhost:3000/accept-invite?token=tok')
    vi.unstubAllGlobals()
  })
})
