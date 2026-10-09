import { describe, expect, it } from 'vitest'

import { hasRunFilters, readRunFilters, toApiFilter } from './runFilters'

describe('run filters', () => {
  it('reads valid filters from the URL and drops invalid ones', () => {
    expect(
      readRunFilters(
        new URLSearchParams(
          'branch=main&executionStatus=completed&mode=manual&from=2026-10-01&to=2026-10-07',
        ),
      ),
    ).toEqual({
      branch: 'main',
      executionStatus: 'completed',
      mode: 'manual',
      from: '2026-10-01',
      to: '2026-10-07',
    })
    expect(
      readRunFilters(new URLSearchParams('executionStatus=passed&mode=nightly&from=yesterday&to=2026-1-7')),
    ).toEqual({ branch: '', executionStatus: undefined, mode: undefined, from: '', to: '' })
  })

  it('sends only the filters that are set, with days as local day bounds', () => {
    expect(toApiFilter(readRunFilters(new URLSearchParams()))).toEqual({})
    expect(
      toApiFilter({
        branch: 'main',
        executionStatus: 'running',
        mode: 'live',
        from: '2026-10-01',
        to: '2026-10-07',
      }),
    ).toEqual({
      branch: 'main',
      executionStatus: 'running',
      mode: 'live',
      from: '2026-10-01T00:00:00.000Z',
      to: '2026-10-07T23:59:59.999Z',
    })
  })

  it('tells whether any filter is set', () => {
    const none = { branch: '', from: '', to: '' }
    expect(hasRunFilters(none)).toBe(false)
    expect(hasRunFilters({ ...none, branch: 'main' })).toBe(true)
    expect(hasRunFilters({ ...none, executionStatus: 'cancelled' })).toBe(true)
    expect(hasRunFilters({ ...none, mode: 'batch' })).toBe(true)
    expect(hasRunFilters({ ...none, from: '2026-10-01' })).toBe(true)
    expect(hasRunFilters({ ...none, to: '2026-10-01' })).toBe(true)
  })
})
