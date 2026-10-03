---
name: edge-case-probe
description: Sweep a running Provenly API with every input class that has broken before (overflowing pages, invalid ids, NUL/invalid UTF-8, Content-Type, empty/repeated params, cross-resource ids, concurrency) using scripts/probe/edge_cases.py. Use during card validation, after API changes, or when Ed asks to look for edge cases.
---

# Edge-case probe

1. Disposable stack (the probe writes "probe-" data):
   `docker rm -f pv-manual; docker run -d --name pv-manual -e POSTGRES_USER=provenly -e POSTGRES_PASSWORD=provenly -e POSTGRES_DB=provenly -p 5450:5432 postgres:16-alpine`,
   then the API from the branch:
   `PROVENLY_DATABASE_URL='postgres://provenly:provenly@localhost:5450/provenly?sslmode=disable' PROVENLY_AUTO_MIGRATE=true go run ./cmd/provenly`
   (from `backend/`, in the background with a timeout; free the port with `fuser -k 8080/tcp`).
2. `scripts/probe/edge_cases.py --base http://localhost:8080` — prints `FAIL` lines and exits 1 on any 5xx or
   unexpected status.
3. For each FAIL decide: probe bug (fix the probe) or API defect. **Any 500 is a finding.** API defects follow the
   validate-card fix rules: failing test first in every layer touched, fix, `make lint`, `make coverage`, a row in
   `docs/review.md`, re-run the probe until it passes.
4. Every case you try by hand against the API goes into the probe (or a contract/integration test) before the card
   is closed: the probe runs in CI (`docker` job, qa environment through nginx), so it is regression, not a one-off.
5. New endpoint, parameter or text field → add its cases to the probe in the same change (expected statuses come
   from the rules in `CLAUDE.md` / `docs/architecture.md`, not from what the API happens to return).
