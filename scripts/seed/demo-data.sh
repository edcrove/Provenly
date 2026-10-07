#!/usr/bin/env bash
# Loads the demo dataset through the public API of a running, empty environment (a run that stopped halfway can be
# run again: test cases are matched by title and reports are idempotent replays):
#   scripts/seed/demo-data.sh http://localhost:8080
# `make seed-rebuild-demo` runs it on a fresh demo environment and saves the
# result as seeds/demo.sql.
set -euo pipefail
api="${1:-http://localhost:8080}/api/v1"

# shellcheck source=../lib/session.sh
source "$(dirname "$0")/../lib/session.sh"
jar=$(provenly_login "${1:-http://localhost:8080}")
json() { curl -fsS -b "$jar" -H 'Content-Type: application/json' "$@"; }
existing=$(mktemp) # ids of test cases already there (a resumed run): their steps are not added again
tc() { # title automated [expectedResult] -> TC-ID (an existing test case with the same title is reused)
  local id
  id=$(json -G "$api/test-cases" --data-urlencode "q=$1" --data-urlencode pageSize=100 |
    jq -r --arg t "$1" '[.items[] | select(.title == $t) | .id][0] // empty')
  if [ -n "$id" ]; then
    echo "$id" | tee -a "$existing"
    return
  fi
  json -X POST "$api/test-cases" -d "{\"title\":\"$1\",\"automated\":$2,\"expectedResult\":\"${3:-}\"}" |
    sed -n 's/^{"id":\([0-9]*\).*/\1/p'
}
step() {
  grep -qx "$1" "$existing" && return
  json -X POST "$api/test-cases/$1/steps" -d "{\"action\":\"$2\",\"expectedResult\":\"${3:-}\"}" >/dev/null
}
ingest() { # runId status branch xml
  curl -fsS -b "$jar" -o /dev/null -X POST -H 'Content-Type: application/xml' --data-binary "$4" \
    "$api/ingestion/junit?provider=github&runId=$1&runAttempt=1&pipeline=e2e-nightly&branch=$3&commit=9f3c2a1e7b&status=$2"
}
suite() { echo "<?xml version=\"1.0\"?><testsuites><testsuite name=\"web\" timestamp=\"$1\">$2</testsuite></testsuites>"; }
case_() { # name tc-id [body] [time]
  echo "<testcase name=\"$1\" classname=\"web\" time=\"${4:-1.284}\"><properties><property name=\"tc-id\" value=\"$2\"/></properties>${3:-}</testcase>"
}
fail() { echo "<failure message=\"$1\">Error: $1</failure>"; }

login=$(tc "User can log in with email and password" true "The dashboard is shown with the user name")
step "$login" "Open /login" "Login form is shown"
step "$login" "Enter valid email and password"
step "$login" "Click \\\"Sign in\\\"" "Dashboard is shown"
logout=$(tc "User can log out" true "The login page is shown")
checkout=$(tc "Checkout with credit card" true "Order is confirmed")
step "$checkout" "Add a product to the cart"
step "$checkout" "Pay with a test Visa card" "Payment is approved"
search=$(tc "Search products by name" true "Matching products are listed")
legacy=$(tc "Export orders to CSV (legacy)" true "A CSV file is downloaded")
reset=$(tc "Password reset email is sent" false "An email with a reset link arrives")
tc "Accessibility audit of the checkout" false "No critical WCAG issues" >/dev/null

ingest 1001 completed main "$(suite 2026-09-28T09:00:00Z "$(case_ 'login [chromium]' "$login")$(case_ 'login [firefox]' "TC-$login" "$(fail 'Sign in button not found')")$(case_ logout "$logout" '' 0)$(case_ 'checkout with visa' "$checkout" '<skipped message="payment sandbox down"/>')$(case_ 'export csv' "$legacy" '<error message="timeout after 30s">TimeoutError</error>')$(case_ 'password reset' "$reset")<testcase name=\"header renders\" classname=\"web\" time=\"0.2\"/>$(case_ wishlist 987654321)$(case_ 'cart badge' TC-12x)$(case_ 'search by name' "$search" '' abc)<testcase name=\"\" classname=\"web\"/>")"
ingest 1002 interrupted feature/checkout "$(suite 2026-09-28T11:00:00Z "$(case_ 'login [chromium]' "$login")")"
ingest 1003 cancelled feature/search "$(suite 2026-09-28T12:00:00Z "$(case_ logout "$logout")")"
grep -qx "$legacy" "$existing" || json -X POST "$api/test-cases/$legacy/deprecate" >/dev/null
ingest 1004 completed main "$(suite 2026-09-28T13:00:00Z "$(case_ 'export csv' "$legacy")")"
ingest 1005 completed main "$(suite 2026-09-28T14:00:00.000Z "$(case_ login "$login")$(case_ logout "$logout" "$(fail 'session cookie still present')")$(case_ checkout "$checkout" '<skipped/>')")"
echo "demo data loaded: TC-$login..TC-$reset, runs github:1001..1005"
