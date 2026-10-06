# Self-hosting Provenly

Provenly is one Go API, one static web app behind nginx and one PostgreSQL database. The `prod` environment of
`docker-compose.yml` runs all three on one host; this guide sets it up, keeps it safe and upgrades it.

## 1. Requirements

- Docker with Compose v2.24+, 1 vCPU and 1 GB of RAM are enough for a team; PostgreSQL 16 (the compose file runs it).
- A TLS-terminating reverse proxy (Caddy, nginx, Traefik…) in front of the web port. Provenly listens on
  `127.0.0.1` by default (`BIND_ADDR`) and must not be exposed without TLS: sessions are bearer tokens and cookies.
  The proxy must send `X-Forwarded-Proto: https` (Caddy, Traefik and nginx's usual config do): the web container
  passes it to the API, which then marks the session cookie `Secure`.

## 2. Configure

```bash
cp envs/prod.env.example envs/prod.env   # git-ignored: never commit it
```

Set in `envs/prod.env`:

| Variable | What | How |
|---|---|---|
| `POSTGRES_PASSWORD` | Database password | `openssl rand -hex 24` |
| `PROVENLY_JWT_SECRET` | Signs sessions (≥ 32 characters); changing it signs everyone out | `openssl rand -hex 32` |
| `PROVENLY_SECRETS_KEY` | Encrypts webhook secrets and connector tokens (32 bytes, base64). **Back it up**: losing it makes them unreadable (reconnect GitHub, recreate webhooks) | `openssl rand -base64 32` |
| `PROVENLY_ADMIN_USERNAME` / `PROVENLY_ADMIN_PASSWORD` | First administrator, created only while there are no users | A strong password (the demo one is refused in prod) |
| `WEB_PORT`, `API_PORT`, `BIND_ADDR` | Where the proxy reaches the web app (it proxies `/api` to the API) | Defaults: 3200, 8280, 127.0.0.1 |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Optional: export traces over OTLP/HTTP | e.g. `http://otel-collector:4318` |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`, `OTEL_EXPORTER_OTLP_HEADERS`, `OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES` | Optional, standard OpenTelemetry SDK settings (passed through) | e.g. `OTEL_EXPORTER_OTLP_HEADERS=x-api-key=…` for a hosted collector |
| `PROVENLY_WEBHOOKS_ALLOW_PRIVATE` | Leave empty in prod: webhooks and connectors may not reach private addresses | `true` only for an internal-only deployment |
| `PROVENLY_GITHUB_API_URL` | GitHub Enterprise Server API, if not github.com | `https://github.example.com/api/v3` |
| `PROVENLY_WEBHOOK_DELIVERY_RETENTION_DAYS` | Days finished webhook deliveries are kept (default 90; `0` keeps them). The API purges older ones hourly, in batches, and logs how many. Audit events and run results are never purged | `30` |

The API refuses to start in `prod` without `PROVENLY_JWT_SECRET` and `PROVENLY_SECRETS_KEY`.

## 3. Run

Run from a checkout of the release tag you deploy (`git checkout v0.2.0`): the compose file, the env example and the
`make` targets must match the images. Released images (recommended; tags are listed in the repository's releases —
**none is published yet**: until the first release, build from source as below):

```bash
IMAGE_PREFIX=ghcr.io/edcrove/ IMAGE_TAG=v0.2.0 \
  docker compose --env-file envs/prod.env up -d --no-build --wait
```

or set `IMAGE_PREFIX` and `IMAGE_TAG` in `envs/prod.env`. To build from source instead: `make up ENV=prod`.

Migrations run before the API starts (the `migrate` service); the API answers `GET /readyz` once the database is
reachable. Sign in at the web port with the administrator, then invite your team from **Users**.

Point CI at Provenly with a project API key (project page → API keys) and either the curl step shown there or the
Playwright reporter:

```bash
# Not on npm yet (the @provenly scope is not registered to this project): install from a checkout.
(cd Provenly/reporters/playwright && npm ci && npm run build) && npm i -D ./Provenly/reporters/playwright
```

See [`reporters/playwright/README.md`](../reporters/playwright/README.md) for the configuration.

## 4. Back up and restore

```bash
make db-dump ENV=prod                                         # backups/prod-<timestamp>.sql (git-ignored)
make db-restore ENV=prod CONFIRM=prod FILE=backups/<file>.sql # takes a dump first
```

Back up `envs/prod.env` too (it holds `PROVENLY_SECRETS_KEY`), separately from the dumps.

## 5. Upgrade

1. Read the release notes; take a dump (`make db-dump ENV=prod`).
2. Set the new `IMAGE_TAG` and run the `up` command again: the `migrate` service applies new migrations (they are
   forward-only and never edited once released), then the API restarts.
3. Check `GET /readyz` and sign in. To roll back, restore the dump and start the previous tag.

## 6. Operate

- **Health**: `GET /healthz` (process), `GET /readyz` (database). Logs are JSON on stdout with `trace_id`.
- **Audit**: administrators see every change in **Audit** (who, what, when).
- **Sign-in throttle**: five failed sign-ins of a username lock it for 15 minutes (429), also under parallel attempts.
  It lives in the API's memory: restarting the API clears it, and it does not span several API replicas. Put rate
  limiting per client address in the reverse proxy as well.
- **Environment**: `PROVENLY_ENV` must be one of `development`, `ci`, `demo`, `qa`, `prod` (anything else refuses to
  start). Only `prod` gets the production guards (required secrets, no demo password, webhooks and connectors refused
  on private, loopback, link-local and other non-global addresses).
- **People who leave**: an administrator deactivates them in **Users** (their sessions end at once; they cannot sign
  in; project API keys belong to the project and keep working) and can reactivate them later. A forgotten password
  gets a single-use reset link from **Users** (24 hours). If the only administrator is locked out, run on the server
  `docker compose --env-file envs/prod.env exec api provenly reset-password <username>`: it prints a reset link token
  (open `<web address>/reset-password?token=...`) and reactivates the account.
- **Agents**: MCP clients connect to `/api/v1/mcp` with a user's session token (Account page).
- **Security reports**: see `SECURITY.md`.
