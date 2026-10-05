import type { Requirement } from '@/api/client'

type CoverageStatus = Requirement['coverage']['status']

const labels: Record<CoverageStatus, string> = {
  uncovered: 'Not covered',
  not_run: 'Not run',
  failing: 'Failing',
  partial: 'Partially passing',
  passing: 'Passing',
}

const variants: Record<CoverageStatus, 'outline' | 'secondary' | 'destructive' | 'warning' | 'success'> = {
  uncovered: 'outline',
  not_run: 'secondary',
  failing: 'destructive',
  partial: 'warning',
  passing: 'success',
}

/** How a requirement's coverage reads in the UI. */
export function coverageLabel(status: CoverageStatus): string {
  return labels[status]
}

export function coverageVariant(status: CoverageStatus) {
  return variants[status]
}

const providers: Record<Requirement['provider'], string> = {
  provenly: 'Provenly',
  jira: 'Jira',
  github: 'GitHub',
  azure_devops: 'Azure DevOps',
}

/** "Jira PAY-12" or "R-3" for native requirements. */
export function requirementRef(r: Pick<Requirement, 'provider' | 'externalId'>): string {
  return r.provider === 'provenly' ? r.externalId : `${providers[r.provider]} ${r.externalId}`
}
