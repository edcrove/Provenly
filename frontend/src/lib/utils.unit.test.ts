import { describe, expect, it } from 'vitest'

import { cn } from './utils'

describe('cn', () => {
  it('merges class names and resolves tailwind conflicts', () => {
    const hidden = false
    expect(cn('px-2', hidden && 'hidden', 'px-4', ['text-sm'])).toBe('px-4 text-sm')
  })
})
