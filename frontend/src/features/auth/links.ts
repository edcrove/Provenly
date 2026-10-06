/** Where to go after signing in: only a path inside the app (never another origin). */
export function safeNext(next: string | null): string {
  if (!next || !next.startsWith('/') || next.startsWith('//') || next.startsWith('/\\')) return '/'
  return next
}

/**
 * The link an invited person opens; the token is only known right after creating the invitation. It travels in the
 * fragment, which browsers never send to a server, so it stays out of web server logs and proxies.
 */
export function invitationLink(token: string, origin = globalThis.location.origin): string {
  return `${origin}/accept-invite#token=${encodeURIComponent(token)}`
}

/** The link to set a new password; like invitations, the token travels in the fragment. */
export function passwordResetLink(token: string, origin = globalThis.location.origin): string {
  return `${origin}/reset-password#token=${encodeURIComponent(token)}`
}

/** The invitation token of the current location: the fragment (current links) or the query (older links). */
export function invitationToken(hash: string, search: string): string {
  return (
    new URLSearchParams(hash.replace(/^#/, '')).get('token') ?? new URLSearchParams(search).get('token') ?? ''
  )
}
