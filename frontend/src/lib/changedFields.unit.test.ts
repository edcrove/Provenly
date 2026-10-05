import { describe, expect, it } from 'vitest'

import { changedFields } from './changedFields'

describe('changedFields', () => {
  it('keeps only the fields that differ from the starting values of the form', () => {
    const base = { title: 'a', description: 'd', automated: false }
    expect(changedFields(base, { title: 'b', description: 'd', automated: true })).toEqual({
      title: 'b',
      automated: true,
    })
    expect(changedFields(base, { ...base })).toEqual({})
  })
})
