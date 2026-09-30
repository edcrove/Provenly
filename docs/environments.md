# Environments (Docker)

`docker compose up -d` builds and starts the whole stack. Every environment is a separate compose project
(`provenly-<env>`) with its own PostgreSQL volume and ports, so environments run side by side and never share
data.

```
postgres (healthy) ──► seed (once, only into an EMPTY database) ──► migrate (goose up) ──► api (healthy) ──► web
   volume pgdata          seeds/<SEED>.sql                          one-shot              :8080            nginx :80
```

| Service | Image | Role |
|---|---|---|
| `postgres` | `postgres:16-alpine` | Database; data in the project's `pgdata` volume. |
| `seed` | `postgres:16-alpine` | `scripts/db/seed.sh`: restores `seeds/$SEED.sql` when the database has never been migrated. |
| `migrate` | `provenly-api` | `provenly migrate up`, then exits; applies migrations newer than the seed. |
| `api` | `provenly-api` (`backend/Dockerfile`, alpine) | REST API; healthcheck on `/healthz`. |
| `web` | `provenly-web` (`frontend/Dockerfile`, nginx) | Production UI build; proxies `/api` and `/healthz` to `api`, SPA fallback. |

Ports are bound to `127.0.0.1` (`BIND_ADDR`) because the POC has no authentication.

## The environments

| | `demo` | `qa` | `prod` |
|---|---|---|---|
| Purpose | Showing Provenly | Manual testing | Real data of a project |
| Config | `envs/demo.env` | `envs/qa.env` | `envs/prod.env` (git-ignored; copy `envs/prod.env.example`) |
| UI / API / DB | 3000 / 8080 / 5432 | 3100 / 8180 / 5433 | 3200 / 8280 / 5434 |
| Starts with | `demo` seed | `demo` seed (any seed) | empty (`SEED=`) |
| Reset | `make demo-reset` | `make db-reset ENV=qa` | `make db-reset ENV=prod CONFIRM=prod` (dumps first) |
| Log level | info | debug | info |

`docker compose up` without an env file starts `demo` (the compose defaults).

## Commands

| Command | What it does |
|---|---|
| `make up ENV=<env>` | Build images and start the environment; waits until everything is healthy. |
| `make dev ENV=<env>` | Same data with hot reload: `api` runs air (rebuild + restart on `.go`/`.sql` changes), `web` runs the Vite dev server with HMR; `./backend` and `./frontend` are bind-mounted (`docker-compose.dev.yml`). |
| `make down ENV=<env>` | Stop; the data stays in the volume. |
| `make ps` / `make logs ENV=<env> [SERVICE=api]` | Status and logs. |
| `make db-dump ENV=<env>` | `pg_dump` to `backups/<env>-<timestamp>.sql` (git-ignored). |
| `make db-restore ENV=<env> FILE=<dump>` | Replace the environment's data with a dump (prod: `CONFIRM=prod`, dumps first). |
| `make db-reset ENV=<env>` | Delete the data and start again from the environment's seed (prod: `CONFIRM=prod`, dumps first). |
| `make demo-reset` | Bring `demo` back to exactly the demo snapshot. |
| `make seed-snapshot FROM=<env> NAME=<name>` | Save an environment's current data as `seeds/<name>.sql`, a new seed (from prod: `CONFIRM=prod`). |
| `make seed-rebuild-demo` | Regenerate `seeds/demo.sql` from `scripts/seed/demo-data.sh` (loads the dataset through the public API). |
| `make infra ENV=qa` | Only the database (seeded + migrated), for running the API/UI without Docker. |

## Seeds and snapshots

A seed is a plain `pg_dump` (schema + data + goose version) in `seeds/`. It is restored only into a database that
was never migrated, and `migrate` then applies any newer migration on top, so an old seed keeps working after the
schema evolves. A database with data is never overwritten by `seed`.

To use another environment's state as the starting point:

```bash
make seed-snapshot FROM=qa NAME=sprint-12   # seeds/sprint-12.sql
# set SEED=sprint-12 in envs/qa.env (or: SEED=sprint-12 make db-reset ENV=qa for a one-off)
make db-reset ENV=qa
```

Commit only seeds without real data. Snapshots of `prod` require `CONFIRM=prod` for that reason.

## Automated suites never use these environments

| Suite | Database |
|---|---|
| Backend Integration / Contract | Throwaway Postgres per run (testcontainers-go) |
| E2E journeys, screenshots | `docker-compose.e2e.yml`: in-memory (tmpfs) Postgres on `127.0.0.1:5439`, destroyed when the script exits. CI passes `E2E_DATABASE_URL` to use its service container instead. |

## Builds behind a corporate proxy

If `npm ci` or `go mod download` fail with certificate errors (TLS-intercepting proxy), point `EXTRA_CA_CERT` at the
proxy's CA bundle (PEM). It is passed to the builds as a BuildKit secret and never stored in an image layer:

```bash
EXTRA_CA_CERT=/path/to/corporate-ca.pem make up ENV=qa
```

## Data safety

- `docker compose down -v` deletes an environment's volume. Prefer `make db-reset`, which guards `prod`.
- Back up `prod` with `make db-dump ENV=prod` before upgrading; `migrate` applies new migrations automatically on
  the next `make up`.
