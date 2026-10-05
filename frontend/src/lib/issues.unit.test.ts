import { describe, expect, it } from 'vitest'

import { verificationLabel, verificationVariant } from './issues'

describe('issues', () => {
  it('labels verification', () => {
    expect(verificationLabel('unlinked')).toBe('No linked test')
    expect(verificationLabel('unverified')).toBe('Unverified')
    expect(verificationLabel('known_issue')).toBe('Known issue')
    expect(verificationLabel('reopen')).toBe('Reopen candidate')
    expect(verificationLabel('not_reproducible')).toBe('Not reproducible')
    expect(verificationLabel('validated_fixed')).toBe('Validated fixed')
    expect(verificationVariant('reopen')).toBe('destructive')
    expect(verificationVariant('known_issue')).toBe('warning')
    expect(verificationVariant('validated_fixed')).toBe('success')
  })
})
