import { describe, expect, it } from 'vitest'

import type { Dimension } from '@/api/client'

import {
  assigned,
  classificationChanges,
  classificationLabels,
  createBody,
  formatTags,
  parseTags,
  selectableValues,
  updateBody,
} from './taxonomy'

const risk: Dimension = {
  key: 'risk',
  name: 'Risk',
  builtIn: true,
  archivedAt: null,
  values: [
    { key: 'critical', name: 'Critical', archivedAt: null },
    { key: 'low', name: 'Low', archivedAt: '2026-01-01T00:00:00Z' },
  ],
}

describe('taxonomy', () => {
  it('parses and formats tags', () => {
    expect(parseTags(' smoke, API  regression,,')).toEqual(['smoke', 'API', 'regression'])
    expect(parseTags('')).toEqual([])
    expect(formatTags(['a', 'b'])).toBe('a, b')
  })

  it('sends only the dimensions a form changed', () => {
    expect(
      classificationChanges({ risk: 'low', feature: 'pay' }, { risk: 'critical', feature: 'pay' }),
    ).toEqual({
      risk: 'critical',
    })
    expect(classificationChanges({ risk: 'low' }, { risk: '' })).toEqual({ risk: null })
    expect(classificationChanges({}, { risk: 'low', feature: '' })).toEqual({ risk: 'low' })
    expect(classificationChanges({ risk: 'low' }, {})).toEqual({ risk: null })
    expect(assigned({ risk: 'low', feature: '' })).toEqual({ risk: 'low' })
  })

  it('labels a classification by name', () => {
    expect(classificationLabels({ risk: 'critical', gone: 'x', ...{} }, [risk])).toEqual([
      { key: 'risk', label: 'Risk: Critical' },
      { key: 'gone', label: 'gone: x' },
    ])
    expect(classificationLabels({ risk: 'new' }, [risk])).toEqual([{ key: 'risk', label: 'Risk: new' }])
  })

  it('offers active values and the current one', () => {
    expect(selectableValues(risk, '').map((v) => v.key)).toEqual(['critical'])
    expect(selectableValues(risk, 'low').map((v) => v.key)).toEqual(['critical', 'low'])
  })

  it('builds create and update bodies', () => {
    const body = createBody(
      { title: 't', tags: 'a b', classification: { risk: 'low', feature: '', os: 'x' } },
      [risk, { ...risk, key: 'feature' }],
    )
    expect(body).toEqual({
      title: 't',
      tags: ['a', 'b'],
      classification: { risk: 'low' },
    })
    const base = { title: 't', tags: 'a, b', classification: { risk: 'low' } }
    expect(updateBody(base, { ...base })).toEqual({})
    expect(updateBody(base, { ...base, tags: 'a b' })).toEqual({})
    expect(updateBody(base, { title: 'u', tags: 'c', classification: { risk: '' } })).toEqual({
      title: 'u',
      tags: ['c'],
      classification: { risk: null },
    })
  })
})
