# Agent Enrollment Flow (Priority 1 Scope)

This document outlines the initial implementation scope for the PingSanto Agent enrollment experience. It is derived from `AGENTS.md` (Security & Enrollment) and the master plan requirements.

## CLI Entry Point

```
pingsanto-agent enroll \
  --server https://central.example.com \
  --token <ENROLL_TOKEN> \
  --labels "site=ATL-1,isp=Comcast,env=prod" \
  --data-dir /var/lib/pingsanto/agent
```

### Responsibilities
- Validate required inputs (server, token, data-dir, config path).
- Normalize and persist label key/value pairs.
- Ensure the data directory exists with `0700` perms.
- Perform HTTPS POST to Central (`/api/agent/v1/enroll`) exchanging the token + labels for:
  - Agent identifier (string).
  - Client certificate, private key, CA bundle (PEM).
  - Initial signed agent configuration (YAML).
- Verify the returned client cert by performing an mTLS handshake against the Central host (connection-only check) before committing state.
- Persist returned artifacts to disk:
  - `client.crt`, `client.key`, `ca.pem` (0600 perms) inside the data dir.
  - `/etc/pingsanto/agent.yaml` (or provided `--config-path`) with 0640 perms.
- Bootstrap `state.yaml` (0600 perms) recording metadata, paths, token hash, and enrollment timestamp.

### Deferred (Future Stages)
- Implement certificate rotation and renewal prior to expiry.
- Harden transport (HTTP/2, pinned CA fingerprints, better error telemetry).
- Resilient retry/backoff logic and Prometheus counters for enrollment attempts.

## File Layout

```
/var/lib/pingsanto/agent/
  state.yaml          # bootstrap state (Priority 1)
  client.crt          # minted in future stage
  client.key          # minted in future stage (0600)
  ca.pem              # trusted central CA bundle
  queue/              # probe queue spill area
  logs/               # optional local diagnostics
```

## Interfaces & Packages

- `internal/enroll`: CLI handler + orchestration for the enrollment command.
- `internal/certs`: Interface definition for future certificate issuance/rotation logic (Priority 1 makes this a stub).
- `internal/config/state.go`: Load/save helpers for `state.yaml` (YAML encoded).

## Validation / Tests

- Unit tests cover state file round-trip and CLI flag validation.
- Manual sanity: run `go run ./cmd/agent enroll ...` and confirm `state.yaml` exists with correct metadata.

This scope unblocks later work on secure transport and config delivery while preserving a clean separation for the eventual mTLS implementation.
