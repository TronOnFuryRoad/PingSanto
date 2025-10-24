# PingSanto Controller (Scaffolding)

This directory contains the scaffolding for the PingSanto central controller. It now includes:

- Upgrade plan/report API matching `docs/agent_upgrade_api.md`
- Pluggable store with PostgreSQL support (`DATABASE_URL`) or in-memory fallback
- Optional mTLS-based agent authentication (`AGENT_AUTH_MODE=mtls`)
- Admin endpoints for managing plans/history secured via bearer token

## Layout

```
controller/
├── cmd/controller      # Entry point executable
├── internal/server     # HTTP server wiring and handlers
├── internal/store      # Store abstractions (memory + PostgreSQL)
├── migrations          # Database migration scripts
├── go.mod / go.sum     # Go module definition
```

## Quick Start

```bash
cd controller

# Optional: run migrations before starting (example with golang-migrate)
migrate -database "$DATABASE_URL" -path migrations up

ADMIN_BEARER_TOKEN=changeme go run ./cmd/controller
```

Environment variables:

| Variable | Description | Default |
| --- | --- | --- |
| `DATABASE_URL` | PostgreSQL connection string; if unset, in-memory store is used. | *(unset)* |
| `AGENT_AUTH_MODE` | `mtls` or `header`. `mtls` extracts agent ID from client certificate CN. | `header` |
| `ADMIN_BEARER_TOKEN` | Required token for admin endpoints; requests must send `Authorization: Bearer <token>`. | *(unset → admin disabled)* |
| `LISTEN_ADDR` | HTTP listen address. | `:8080` |

Agent requests (temporary) may supply `X-Agent-ID` when `AGENT_AUTH_MODE=header`. Admin APIs are available at:

- `POST /api/admin/v1/upgrade/plan` — create/update plan
- `GET /api/admin/v1/upgrade/history/{agent_id}?limit=50`
- `GET /api/admin/v1/settings/notifications` — fetch notification toggle
- `POST /api/admin/v1/settings/notifications` — update notification toggle (`{"notify_on_publish":true}`)

CLI helpers:

- `go run ./cmd/upgradectl` — manually upsert an upgrade plan (see `docs/release_pipeline.md`)
- `go run ./cmd/settingsctl` — read or update the notification toggle (`--set true|false`)

## Next Steps
- Integrate with real authentication (mTLS root CA management, certificate issuance).
- Flesh out artifact publishing/signature verification workflow.
- Build admin UI/CLI for plan management beyond the raw APIs.
- Add observability (metrics, structured logs, alerting) around upgrade rollouts.
