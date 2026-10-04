#!/usr/bin/env bash
# Runs the README's "Try the POC flow with curl" block verbatim against a freshly started demo
# environment and checks what the README promises: TC-8 created, run 6 = github:42:1 created
# once (a resend of the same attempt returns 200, no duplicate), the PASS+FAIL test case counted
# as failed with both results kept, the missing TC-ID diagnostic, and the history of TC-8.
# Also checks the README's start command rebuilds the images (a pull must not run stale code).
# Usage: scripts/readme-flow.sh [API base, default http://localhost:8080]
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
api="${1:-http://localhost:8080}"
fail() { echo "readme-flow: $*" >&2; exit 1; }

grep -q '^docker compose up -d --build ' "$root/README.md" || fail "README start command must rebuild the images (--build)"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
awk '/^### Try the POC flow with curl/{s=1; next} s && /^```bash/{b=1; next} b && /^```/{exit} b' "$root/README.md" \
  | sed "s#localhost:8080#${api#http://}#g; s#curl -s #curl -s --noproxy '*' #g" >"$work/flow.sh"
[ -s "$work/flow.sh" ] || fail "README curl block not found"
(cd "$work" && bash -euo pipefail flow.sh >"$work/out.txt") || fail "README block failed: $(cat "$work/out.txt")"

python3 - "$api" "$work" <<'PY'
import json, subprocess, sys
api, work = sys.argv[1], sys.argv[2]

def call(method, path, data=None, ctype=None):
    cmd = ["curl", "-s", "--noproxy", "*", "-o", f"{work}/body", "-w", "%{http_code}", "-X", method, api + "/api/v1" + path]
    if data is not None:
        cmd += ["-H", f"Content-Type: {ctype}", "--data-binary", data]
    status = int(subprocess.run(cmd, capture_output=True, text=True, check=True).stdout)
    return status, json.load(open(f"{work}/body"))

def check(name, got, want):
    if got != want:
        sys.exit(f"readme-flow: {name}: got {got!r}, want {want!r}")

st, tc = call("GET", "/test-cases/8")
check("TC-8 created by the README", (st, tc["key"], tc["automated"]), (200, "TC-8", True))
st, run = call("GET", "/test-runs/6")
check("run 6 is github:42:1", (st, run["externalRunId"], run["resultCount"]), (200, "github:42:1", 3))
st, resend = call("POST", "/ingestion/junit?provider=github&runId=42&runAttempt=1&branch=main&commit=abc123",
                  f"@{work}/report.xml", "application/xml")
check("resending the same attempt", (st, resend["created"], resend["testRun"]["id"]), (200, False, 6))
st, runs = call("GET", "/test-runs?pageSize=100")
check("no duplicate run", sum(r["externalRunId"] == "github:42:1" for r in runs["items"]), 1)
st, s = call("GET", "/test-runs/6/summary")
tc8 = [t for t in s["testCases"] if t["testCaseId"] == 8]
check("TC-8 PASS+FAIL aggregated as failed", tc8, [{"testCaseId": 8, "status": "failed", "resultCount": 2}])
check("executed / untested", (s["executedTotal"], s["counts"]["untested"]), (1, s["expectedTotal"] - 1))
check("missing TC-ID diagnostic", s["diagnostics"]["missing"], 1)
st, h = call("GET", "/test-cases/8/results")
check("history of TC-8", sorted(i["result"]["status"] for i in h["items"]), ["failed", "passed"])
check("history run", {i["run"]["externalRunId"] for i in h["items"]}, {"github:42:1"})
PY
echo "readme-flow: ok ($api)"
