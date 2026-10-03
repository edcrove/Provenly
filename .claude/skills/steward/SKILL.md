---
name: steward
description: Repo-specific guidance for driving a Provenly pull request to green and mergeable (CI failures, review comments, merge conflicts, scheduled check-ins). Read before acting on PR events for this repository.
---

# Stewarding Provenly PRs

- Pushes go only to the session's branch; `main` is protected and **only Ed merges**. Never force-push; resolve
  conflicts with a merge commit.
- Before every push: `make lint`, `make check-generated`, and the gates the change touches (`make coverage` for
  anything beyond docs). Reproduce a CI failure locally before fixing it.
- Generated code (sqlc, `frontend/src/api/schema.d.ts`) is regenerated with the pinned tools (`.tool-versions`),
  never edited by hand. sqlc drift in CI usually means a local sqlc with a different version.
- Flaky-looking failures in integration/contract/E2E are real until proven otherwise (testcontainers, timing); fix
  the test or the code, never skip or quarantine.
- Check-ins: report to Ed in UYT, one line when nothing changed; stop after three consecutive check-ins with no
  change and no activity from Ed.
- Review comments: implement small asks directly; architectural or product asks go to Ed with a recommendation.
