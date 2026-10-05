import { describe, expect, it } from 'vitest'

import { coverageLabel, coverageVariant, requirementRef } from './requirements'

describe('requirements', () => {
  it('labels coverage and references', () => {
    expect(coverageLabel('uncovered')).toBe('Not covered')
    expect(coverageLabel('not_run')).toBe('Not run')
    expect(coverageLabel('failing')).toBe('Failing')
    expect(coverageLabel('partial')).toBe('Partially passing')
    expect(coverageLabel('passing')).toBe('Passing')
    expect(coverageVariant('failing')).toBe('destructive')
    expect(coverageVariant('passing')).toBe('success')
    expect(requirementRef({ provider: 'provenly', externalId: 'R-3' })).toBe('R-3')
    expect(requirementRef({ provider: 'jira', externalId: 'PAY-12' })).toBe('Jira PAY-12')
    expect(requirementRef({ provider: 'azure_devops', externalId: '77' })).toBe('Azure DevOps 77')
    expect(requirementRef({ provider: 'github', externalId: '9' })).toBe('GitHub 9')
  })
})
