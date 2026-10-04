import { describe, expect, it } from 'vitest'

import { previousPage } from './paging'

describe('previousPage', () => {
  const data = { items: [1] }

  it('keeps the previous data when only the page changed', () => {
    expect(previousPage(['test-runs', 'list', 2], data, ['test-runs', 'list', 5])).toBe(data)
    expect(
      previousPage(['runs', 7, 'results', undefined, undefined, 2], data, [
        'runs',
        7,
        'results',
        undefined,
        undefined,
        1,
      ]),
    ).toBe(data)
  })

  it('drops it when anything but the page changed, or there is no previous query', () => {
    expect(
      previousPage(['test-cases', 154, 'results', 1], data, ['test-cases', 153, 'results', 1]),
    ).toBeUndefined()
    expect(
      previousPage(['runs', 7, 'results', 'failed', undefined, 1], data, [
        'runs',
        7,
        'results',
        undefined,
        undefined,
        1,
      ]),
    ).toBeUndefined()
    expect(previousPage(['test-runs', 'list', 1], data, ['test-runs', 1])).toBeUndefined()
    expect(previousPage(['test-runs', 'list', 1], data, undefined)).toBeUndefined()
  })
})
