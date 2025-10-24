# PingSanto

PingSanto provides the foundations for the Ping Monitoring & Diagnostics platform. The repository contains the site-local **agent** that executes network probes and a scaffold for the **controller** service that coordinates agents, manages upgrade plans, and records telemetry.

## Repository Layout

- `agent/` – Go-based site agent with enrollment, runtime, diagnostics, upgrade manager, and supporting docs.
- `controller/` – Controller service scaffold with Go modules, PostgreSQL store, and admin/agent APIs for upgrade plans.
- `docs/` – Planning and API contracts, including the agent upgrade API and release pipeline guidelines.
- `notes/` – Project notes and progress log (`notes/progress.md`).
- `logs/`, `screenshots/` – Reserved directories for diagnostics artifacts and visual references.

## Quick Start

```bash
# Run unit tests
cd agent && go test ./...
cd controller && go test ./...

# Build the agent binary
cd agent && go build ./cmd/agent

# Inspect upgrade plan CLI
cd agent && go run ./cmd/agent upgrades --status --data-dir <data_dir>
```

The agent expects configuration at `/etc/pingsanto/agent.yaml` (see `docs/` for examples) and maintains runtime state under `data_dir` (default `/var/lib/pingsanto/agent`).

## Upgrade Flow Highlights

- Agents poll the controller for upgrade plans via mTLS-secured APIs.
- The upgrade manager persists plan metadata, downloads and validates artifacts (checksum and optional signature), stages bundles under `<data_dir>/upgrades/`, and reports success/failure back to the controller.
- Channel-wide plans (e.g., `channel:stable`) are supported alongside per-agent directives, as described in `docs/agent_upgrade_api.md`.

## Contributing

1. Run `go test ./...` for both modules before submitting changes.
2. Update `notes/progress.md` with a UTC timestamped entry describing your work.
3. Keep documentation (`docs/` and `AGENTS.md`) in sync with new features, especially upgrade and operational behaviour.

## License

See [LICENSE](./LICENSE) for details.
