// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { usePage } from './usePage'

describe('usePage', () => {
  it('keeps the page within a scope and goes back to the first one when the scope changes', () => {
    const { result, rerender } = renderHook(({ scope }) => usePage(scope), { initialProps: { scope: 'TC' } })
    expect(result.current[0]).toBe(1)
    act(() => result.current[1](3))
    expect(result.current[0]).toBe(3)
    rerender({ scope: 'TC' })
    expect(result.current[0]).toBe(3)
    rerender({ scope: 'CHK' })
    expect(result.current[0]).toBe(1)
    act(() => result.current[1](2))
    expect(result.current[0]).toBe(2)
  })
})
