# PingSanto Agent

Foundational workspace for the Ping Monitoring & Diagnostics agent. The repository currently provides:

- Go module scaffold (command entry point under `cmd/agent`).
- Internal packages for configuration parsing and shared domain types.
- Rust probe crate placeholder for the future FFI-based probe engine.
- Diagnostics CLI (`pingsanto-agent diag`) to bundle configs/logs/spill metadata and optional metrics/journal snapshots for support cases.
- Upgrade CLI (`pingsanto-agent upgrades`) to pause/resume auto-upgrades and switch channels.
- Basic development tooling hooks (Makefile targets for lint/test to be wired up in subsequent stages).

This codebase is planning-aligned with `AGENTS.md` and the master plan. Further implementation will build on this foundation for enrollment, scheduling, resilience, telemetry, and upgrades.
