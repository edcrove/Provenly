/** Where to go after signing in: only a path inside the app (never another origin). */
export function safeNext(next: string | null): string {
  if (!next || !next.startsWith('/') || next.startsWith('//') || next.startsWith('/\\')) return '/'
  return next
}

/** The link an invited person opens; the token is only known right after creating the invitation. */
export function invitationLink(token: string, origin = globalThis.location.origin): string {
  return `${origin}/accept-invite?token=${encodeURIComponent(token)}`
}
