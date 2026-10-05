import type { Issue } from '@/api/client'

type VerificationStatus = Issue['verification']['status']

const labels: Record<VerificationStatus, string> = {
  unlinked: 'No linked test',
  unverified: 'Unverified',
  known_issue: 'Known issue',
  reopen: 'Reopen candidate',
  not_reproducible: 'Not reproducible',
  validated_fixed: 'Validated fixed',
}

const variants: Record<VerificationStatus, 'outline' | 'secondary' | 'destructive' | 'warning' | 'success'> =
  {
    unlinked: 'outline',
    unverified: 'secondary',
    known_issue: 'warning',
    reopen: 'destructive',
    not_reproducible: 'secondary',
    validated_fixed: 'success',
  }

/** How an issue's QA verification reads in the UI (DEC-8). */
export function verificationLabel(status: VerificationStatus): string {
  return labels[status]
}

export function verificationVariant(status: VerificationStatus) {
  return variants[status]
}
