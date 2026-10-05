import { afterEach, describe, expect, it, vi } from 'vitest'

import { loadProject, noProvider, saveProject } from './currentProject'

describe('current project storage', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('remembers the chosen project and forgets "every project"', () => {
    const store = new Map<string, string>()
    vi.stubGlobal('localStorage', {
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => store.set(k, v),
      removeItem: (k: string) => store.delete(k),
    })
    expect(loadProject()).toBe('')
    saveProject('CHK')
    expect(loadProject()).toBe('CHK')
    saveProject('')
    expect(loadProject()).toBe('')
  })

  it('outside a provider every project is listed and choosing does nothing', () => {
    expect(noProvider.project).toBe('')
    expect(() => noProvider.setProject('CHK')).not.toThrow()
  })

  it('works without storage (private mode, blocked site data)', () => {
    const broken = {
      getItem: () => {
        throw new Error('denied')
      },
      setItem: () => {
        throw new Error('denied')
      },
      removeItem: () => {
        throw new Error('denied')
      },
    }
    vi.stubGlobal('localStorage', broken)
    expect(loadProject()).toBe('')
    expect(() => saveProject('CHK')).not.toThrow()
    vi.stubGlobal('localStorage', undefined)
    expect(loadProject()).toBe('')
    expect(() => saveProject('')).not.toThrow()
  })
})
