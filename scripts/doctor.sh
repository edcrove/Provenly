#!/bin/bash
# Diagnoses the local toolchain against .tool-versions and the environment gotchas in
# CLAUDE.md. Report only by default; --fix installs the pinned tools, npm packages and
# starts dockerd (via .claude/hooks/session-start.sh) and frees busy dev ports.
# Exit 1 when something is still wrong.
set -uo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
fix=false
[ "${1:-}" = "--fix" ] && fix=true

ver() { awk -v t="$1" '$1 == t { print $2 }' .tool-versions; }
GOBIN="$(go env GOPATH 2>/dev/null)/bin"
export PATH="$GOBIN:$PATH"
fail=0
ok() { printf '  ok    %s\n' "$*"; }
bad() { printf '  FAIL  %s\n' "$*"; fail=1; }
warn() { printf '  warn  %s\n' "$*"; }

if $fix; then
  echo "fix: toolchain, packages, docker"
  CLAUDE_CODE_REMOTE=true CLAUDE_PROJECT_DIR="$root" CLAUDE_ENV_FILE= .claude/hooks/session-start.sh || bad "session-start.sh failed"
fi

echo "toolchain (.tool-versions)"
go_want="$(ver golang)"
go_have="$(GOTOOLCHAIN="go${go_want}" go env GOVERSION 2>/dev/null)"
[ "$go_have" = "go${go_want}" ] && ok "go $go_want" || bad "go: want $go_want, have ${go_have:-none} (GOTOOLCHAIN=go$go_want)"
node_want="$(ver nodejs)"
node_have="$(node --version 2>/dev/null)"
[ "${node_have#v}" = "$node_want" ] && ok "node $node_want" || warn "node: want $node_want, have ${node_have:-none} (minor drift is fine locally; CI uses the pin)"
sqlc_want="$(ver sqlc)"
"$GOBIN/sqlc" version 2>/dev/null | grep -q "v${sqlc_want}" && ok "sqlc $sqlc_want ($GOBIN)" || bad "sqlc: want v$sqlc_want in $GOBIN (an older one drifts generated code) -> --fix"
lint_want="$(ver golangci-lint)"
"$GOBIN/golangci-lint" version 2>/dev/null | grep -q "$lint_want" && ok "golangci-lint $lint_want ($GOBIN)" || bad "golangci-lint: want $lint_want in $GOBIN -> --fix"
case "$(command -v sqlc) $(command -v golangci-lint)" in
  *"/usr/local/bin/"*) warn "an older sqlc/golangci-lint in /usr/local/bin shadows $GOBIN in PATH; prepend $GOBIN" ;;
esac

echo "packages"
for d in frontend e2e; do
  (cd "$d" && npm ls --depth=0 >/dev/null 2>&1) && ok "$d/node_modules" || bad "$d/node_modules missing or out of sync with package-lock.json -> --fix"
done

echo "docker"
if docker info >/dev/null 2>&1; then ok "dockerd up"; else bad "dockerd down -> setsid nohup dockerd >/tmp/dockerd.log 2>&1 & (or --fix)"; fi

echo "playwright"
chromium="${PLAYWRIGHT_CHROMIUM_EXECUTABLE:-$(ls -d /opt/pw-browsers/chromium-*/chrome-linux/chrome 2>/dev/null | sort -V | tail -1)}"
[ -x "$chromium" ] && ok "chromium $chromium" || bad "no Chromium under /opt/pw-browsers (never run 'playwright install')"

echo "ports (8080 API, 5173 Vite, 3000 E2E UI, 5433 dev DB, 5439 E2E DB)"
for p in 8080 5173 3000; do
  if pid="$(fuser "$p/tcp" 2>/dev/null | tr -s ' ')" && [ -n "$pid" ]; then
    if $fix; then fuser -k "$p/tcp" >/dev/null 2>&1; ok "$p freed (was pid$pid)"; else warn "$p busy (pid$pid): fuser -k $p/tcp before dev/E2E (never pkill -f)"; fi
  else
    ok "$p free"
  fi
done
for p in 5433 5439; do
  pid="$(fuser "$p/tcp" 2>/dev/null | tr -s ' ')"
  [ -n "$pid" ] && warn "$p in use (expected while its Postgres container runs)" || ok "$p free"
done

echo
[ "$fail" = 0 ] && echo "Doctor: OK" || echo "Doctor: problems found (run scripts/doctor.sh --fix)"
exit "$fail"
