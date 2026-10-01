# Contributing to Provenly

## Code conventions

**Backend (Go)**
- `gofmt`/`goimports` formatting, `go vet` and `golangci-lint` (config: `backend/.golangci.yml`) must be clean: `make lint`.
- Modular monolith: `internal/catalog`, `internal/execution`, `internal/ingestion`. A module never reads another
  module's tables; it calls the other module's service through a narrow interface declared by the consumer.
- Every product operation lives in a module `Service` (application layer). REST handlers are thin adapters, so
  future interfaces (e.g. MCP) reuse the same use cases.
- SQL lives in `backend/queries/*.sql` and is compiled by `sqlc` into `internal/<module>/<module>db` (typed, not an
  ORM). Schema changes are new `goose` migrations in `backend/migrations` — never edit an applied migration.

**Frontend (React)**
- ESLint + Prettier + `tsc` must be clean: `npm run lint && npm run format:check && npm run typecheck`.
- **Never hand-write API types.** Run `npm run gen:api` after changing `api/openapi.yaml`; CI fails on drift.
- UI primitives come from shadcn/ui (`src/components/ui`, vendored source, `components.json`).

## Local environments

Use `make dev ENV=qa` for hot reload on the qa data, or `make infra ENV=qa` plus `make dev-backend` /
`make dev-frontend` without containers. A new migration is applied by the `migrate` service on the next start; a
schema change that makes an older seed fail to migrate must refresh the seed (`make seed-rebuild-demo`). Never
commit seeds with real data. Details: [`docs/environments.md`](docs/environments.md).

## API changes

`api/openapi.yaml` is the contract and lives next to the code. A change to a public operation updates, in the same
change: the spec, the backend handler, the generated client, and both Contract suites (a new response status is a new
contract target automatically). The backend Contract suite also fails when the router and the spec disagree
(`TestRoutesMatchContract`): every registered route must be an operation of the contract, and vice versa.

## Tests: a feature updates every layer it touches, on both sides

| Layer | Backend | Frontend |
|---|---|---|
| Unit | `*_test.go` next to the code | `src/**/*.unit.test.ts` |
| Integration | `backend/test/integration` (tag `integration`), subtest names start with the `BE-INT-xxx` target id | `src/**/*.int.test.tsx`, test names contain the `FE-INT-xxx` id |
| Contract | `backend/test/contract` (tag `contract`) | `src/contract` |
| E2E | `e2e/tests/api.spec.ts` (`[BE-E2E-xxx]`) | `e2e/tests/ui.spec.ts` (`[FE-E2E-xxx]`) |

New integration behaviors and journeys are added to `coverage/inventories/*.yaml`. An empty layer is explicit debt;
100% never justifies duplicated tests without value.

## Coverage exceptions

Only `coverage/exceptions.yaml`, reviewed like code. Every entry needs id, gate, layer, side, target, reason,
category, evidence, owner, createdAt, reviewBy (expiry), link and `status: approved`. Expired, incomplete or stale
(matching nothing) entries fail CI. Exceptions are always reported separately and never reduce the reachable total.
See [`docs/testing-strategy.md`](docs/testing-strategy.md).

## Commits and pull requests

Small, focused commits with imperative subjects. Nothing merges with red CI.

## License of contributions

Provenly is licensed under the [Apache License 2.0](LICENSE). Unless you state otherwise, any contribution you submit
is licensed under the same terms (section 5 of the license); no separate CLA is required.
