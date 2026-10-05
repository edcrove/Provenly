import type { Suite } from '@/api/client'

/** What a suite selects, in words. */
export function selectionLabel(suite: Suite): string {
  if (suite.kind === 'static') return `${suite.caseCount} listed test case${suite.caseCount === 1 ? '' : 's'}`
  const parts = [...(suite.query?.tag ? [`#${suite.query.tag}`] : []), ...(suite.query?.classification ?? [])]
  return parts.join(' and ')
}
