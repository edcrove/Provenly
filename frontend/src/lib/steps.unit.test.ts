import { describe, expect, it } from 'vitest'

import { moveId } from './steps'

describe('moveId', () => {
  it('moves an id up or down', () => {
    expect(moveId([1, 2, 3], 1, -1)).toEqual([2, 1, 3])
    expect(moveId([1, 2, 3], 1, 1)).toEqual([1, 3, 2])
  })

  it('keeps the order when the move is out of range', () => {
    const ids = [1, 2, 3]
    expect(moveId(ids, 0, -1)).toBe(ids)
    expect(moveId(ids, 2, 1)).toBe(ids)
    expect(moveId(ids, -1, 1)).toBe(ids)
    expect(moveId(ids, 5, -1)).toBe(ids)
  })
})
