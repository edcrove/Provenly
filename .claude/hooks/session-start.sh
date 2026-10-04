#!/bin/bash
# SessionStart (Claude Code on the web): prepares the toolchain the repo pins in
# .tool-versions so `make lint`, `make generate`, the gates and E2E work without
# manual setup. Idempotent; the container state is cached after it completes.
set -euo pipefail

if [ "${CLAUDE_CODE_REMOTE:-}" != "true" ]; then
  exit 0
fi

root="${CLAUDE_PROJECT_DIR:-$(cd "$(dirname "$0")/../.." && pwd)}"
cd "$root"
log() { echo "[session-start] $*" >&2; }

ver() { awk -v t="$1" '$1 == t { print $2 }' .tool-versions; }
GO_VERSION="$(ver golang)"
SQLC_VERSION="$(ver sqlc)"
GOLANGCI_VERSION="$(ver golangci-lint)"
GOBIN="$(go env GOPATH)/bin"
export PATH="$GOBIN:$PATH" GOTOOLCHAIN="go${GO_VERSION}"

# Tools built with the pinned Go: an older golangci-lint refuses newer go.mod files,
# and an older sqlc produces generated-code drift that CI rejects.
if ! "$GOBIN/sqlc" version 2>/dev/null | grep -q "v${SQLC_VERSION}"; then
  log "installing sqlc v${SQLC_VERSION}"
  go install "github.com/sqlc-dev/sqlc/cmd/sqlc@v${SQLC_VERSION}"
fi
if ! "$GOBIN/golangci-lint" version 2>/dev/null | grep -q "${GOLANGCI_VERSION}.*go${GO_VERSION}"; then
  log "installing golangci-lint v${GOLANGCI_VERSION}"
  go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v${GOLANGCI_VERSION}"
fi

log "go modules"
(cd backend && go mod download)
(cd tools/covgate && go mod download)

log "npm packages"
(cd frontend && npm install --no-audit --no-fund --loglevel=error >&2)
(cd e2e && npm install --no-audit --no-fund --loglevel=error >&2)

# Docker is needed by integration/contract tests (testcontainers), E2E and screenshots.
if command -v dockerd >/dev/null && ! docker info >/dev/null 2>&1; then
  log "starting dockerd"
  setsid nohup dockerd >/tmp/dockerd.log 2>&1 < /dev/null &
  for _ in $(seq 1 30); do docker info >/dev/null 2>&1 && break; sleep 1; done
fi

chromium="$(ls -d /opt/pw-browsers/chromium-*/chrome-linux/chrome 2>/dev/null | sort -V | tail -1 || true)"
if [ -n "${CLAUDE_ENV_FILE:-}" ]; then
  {
    echo "export PATH=\"$GOBIN:\$PATH\""
    echo "export GOTOOLCHAIN=go${GO_VERSION}"
    [ -n "$chromium" ] && echo "export PLAYWRIGHT_CHROMIUM_EXECUTABLE=$chromium"
  } >> "$CLAUDE_ENV_FILE"
fi
log "ready: $("$GOBIN/sqlc" version), $("$GOBIN/golangci-lint" version | head -1), docker $(docker info >/dev/null 2>&1 && echo up || echo down)"
