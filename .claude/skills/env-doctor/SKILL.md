---
name: env-doctor
description: Diagnose and fix the Provenly dev environment (pinned Go/sqlc/golangci-lint, npm packages, dockerd, Playwright Chromium, busy ports) with scripts/doctor.sh. Use when lint/generate/coverage/E2E fail for environmental reasons (sqlc drift, golangci-lint refusing go.mod, Docker down, port in use, Chromium not found) or at the start of a long session.
---

# Environment doctor

1. `scripts/doctor.sh` — report only; exits 1 when something is wrong.
2. `scripts/doctor.sh --fix` — runs `.claude/hooks/session-start.sh` (pinned tools into `$(go env GOPATH)/bin`,
   go modules, npm packages, dockerd) and frees the dev ports (8080, 5173, 3000) with `fuser -k`.
3. Still failing:
   - tools: put `$(go env GOPATH)/bin` first in PATH (older copies live in `/usr/local/bin`); use
     `GOTOOLCHAIN=go<.tool-versions>`.
   - Docker: `docker info || (setsid nohup dockerd >/tmp/dockerd.log 2>&1 &)`; read `/tmp/dockerd.log`; disk space
     (delete build artifacts/caches) before anything else.
   - Playwright: never `playwright install`; point `PLAYWRIGHT_CHROMIUM_EXECUTABLE` at `/opt/pw-browsers/chromium-*`.
   - Never `pkill -f` (it can kill the shell).
4. A new gotcha found → add the check to `scripts/doctor.sh` and the note to `CLAUDE.md` in the same change.
