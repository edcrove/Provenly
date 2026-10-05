# Security policy

## Reporting a vulnerability

Please do **not** open a public issue. Report it privately through GitHub's "Report a vulnerability" (Security
advisories) on this repository, with steps to reproduce and the affected version. You will get an answer within a
week; fixes are released as a new tag and credited unless you prefer otherwise.

## Supported versions

Provenly is pre-1.0: only the latest release receives fixes.

## How Provenly protects your data

- Passwords (bcrypt), API keys and invitation tokens (SHA-256) are hashed; webhook secrets and connector tokens are
  encrypted with AES-256-GCM under `PROVENLY_SECRETS_KEY` and never returned after creation.
- Sessions are signed tokens that end when the password changes; five failed sign-ins lock a username for 15 minutes.
- Roles per project (viewer, member, maintainer) and administrators; API keys only report runs into their project.
- Every change made through the API is in the append-only audit log (never request bodies).
- Webhooks and connectors cannot reach private, loopback or link-local addresses in production (SSRF).
- Inputs are validated before reaching the database; an edge-case sweep (`scripts/probe/edge_cases.py`) runs in CI.

See `docs/self-hosting.md` for a safe deployment.
