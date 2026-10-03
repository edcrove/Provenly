---
name: implement-card
description: Implement a Provenly Trello card end to end following the project's Definition of Done (contract first, migrations, tests in every layer, 100% gates, docs, screenshots) and hand it to Validation. Use when starting work on a card from Por hacer or when Ed asks to build a feature.
---

# Implement a card

1. **Start**: read the card, its comments and the related Notion decisions. If an acceptance criterion depends on an
   undecided product question, ask Ed before coding (one concise question with a recommendation). Move the card to
   **En progreso**.
1b. Every scenario you try by hand while building (curl, browser, psql) becomes an automated test in its layer
   before the card goes to Validation (CLAUDE.md, Card workflow 4).
2. **Contract first**: change `api/openapi.yaml`, then `cd frontend && npm run gen:api`. Problem+json for errors.
3. **Schema**: a new goose migration in `backend/migrations` (never edit a committed one); test it up *and* down,
   also against `seeds/demo.sql`. Queries in `backend/queries/*.sql`, then `make generate`.
4. **Code**: logic in the module `Service`; handlers stay thin; a module never reads another module's tables.
5. **Tests in every layer touched, both sides**: unit (`*_test.go`, `*.unit.test.ts`), integration (`BE-INT-xxx`,
   `FE-INT-xxx`), contract (backend `test/contract`, frontend `src/contract`), E2E (`[BE-E2E-xxx]`,
   `[FE-E2E-xxx]`). Register new ids in `coverage/inventories/*.yaml`. A behavior test must fail without the change.
6. **Gates**: `make lint`, `make check-generated`, `make coverage` → `Overall: PASS` (no new coverage exceptions
   without telling Ed why).
7. **Docs**: `docs/architecture.md` (conventions), `docs/implementation-decisions.md` (choices made),
   `docs/review.md` (criterion → tests), README if commands changed. UI change → `make screenshots` and update
   `docs/screenshots/README.md`.
8. **Ship**: commit (clear message, no model ids), push to the session branch, wait for CI green.
9. **Hand off**: move the card to **Validation** with an evidence comment: what was done, how to verify, test ids,
   CI run, code paths, decisions to review. Then follow `validate-card`.
