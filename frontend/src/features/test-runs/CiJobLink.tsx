import { ExternalLink } from 'lucide-react'

/** Opens the CI job a run came from in a new tab (the URL CI sent with the report). */
export function CiJobLink({ href, label = 'Open the CI job' }: { href: string; label?: string }) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className="inline-flex items-center gap-1 underline"
      data-testid="ci-job-link"
    >
      {label}
      <ExternalLink aria-hidden className="size-3.5" />
    </a>
  )
}
