#!/bin/bash
# PostToolUse: format the file Claude just wrote, with the formatter CI checks.
# Go -> gofmt; frontend/ sources -> the frontend's Prettier config. Never blocks.
set -uo pipefail
root="${CLAUDE_PROJECT_DIR:-$(pwd)}"
path="$(jq -r '.tool_response.filePath // .tool_input.file_path // empty')"
[ -n "$path" ] && [ -f "$path" ] || exit 0
rel="${path#"$root"/}"
case "$rel" in
  backend/internal/*/*db/*.go) ;;  # generated
  *.go) gofmt -w "$path" ;;
  frontend/src/api/schema.d.ts) ;;  # generated
  frontend/*.ts|frontend/*.tsx|frontend/*.css|frontend/*.json|frontend/*.md)
    (cd "$root/frontend" && npx --no-install prettier --write --log-level=warn "$path") ;;
esac
exit 0
