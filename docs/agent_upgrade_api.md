# Agent Upgrade API Contract (Draft)

This document captures the controller-facing contract that enables PingSanto Agents to fetch upgrade directives, download signed artifacts, and report upgrade status back to the central service. It mirrors the implementation scaffolding in `controller/`.

---

## 1. Overview
- All requests authenticated with mTLS; agent certificate subject CN = `agent_id`.
- Requests made over HTTPS/HTTP2 (same stack as enrollment/uplink).
- Endpoints live under `/api/agent/v1/upgrade`.

### 1.1 Controller Data Model (Relational Schema)

#### `agent_upgrade_plans`
| Column | Type | Notes |
| --- | --- | --- |
| `agent_id` | text PK | References logical agent identifier. |
| `channel` | text | Target channel (`stable`, `canary`, etc.). |
| `version` | text | SemVer string of target release. |
| `artifact_url` | text | Download URL. |
| `artifact_sha256` | char(64) | Hex checksum. |
| `artifact_signature_url` | text | URL for detached signature. |
| `force_apply` | boolean | Overrides local pause when `true`. |
| `schedule_earliest` | timestamptz | Optional rollout window start. |
| `schedule_latest` | timestamptz | Optional rollout window end. |
| `paused` | boolean | Controller-side pause flag. |
| `notes` | text | Optional operator notes. |
| `etag` | text | Hash of current plan for conditional requests. |
| `updated_at` | timestamptz | Last modification time. |

#### `agent_upgrade_history`
| Column | Type | Notes |
| --- | --- | --- |
| `id` | uuid PK | Generated via `gen_random_uuid()`. |
| `agent_id` | text | Agent identifier. |
| `channel` | text | Channel at time of attempt. |
| `target_version` | text | Version being applied. |
| `previous_version` | text | Agent’s prior version (nullable). |
| `status` | text | `success`, `failed`, `skipped`. |
| `message` | text | Short summary / error. |
| `details` | jsonb | Structured context (phase, checksum mismatch, etc.). |
| `started_at` | timestamptz | Start timestamp. |
| `completed_at` | timestamptz | Completion timestamp. |
| `created_at` | timestamptz | Insert time. |

Indexes:
- `agent_upgrade_history(agent_id, completed_at DESC)` for quick recent lookups.

---

## 2. Polling for Upgrade Instructions

### `GET /api/agent/v1/upgrade/plan`

Agents include their current channel via a query parameter. Example request:

```
GET /api/agent/v1/upgrade/plan?channel=stable HTTP/2
Host: central.example.com
User-Agent: pingsanto-agent/<version>
Accept: application/json
```

If `channel` is omitted the controller assumes `stable`.

**Successful Response (200)**
```json
{
  "agent_id": "agt_0123",
  "generated_at": "2025-10-23T17:00:00Z",
  "channel": "stable",
  "artifact": {
    "version": "1.2.4",
    "url": "https://artifacts.example.com/pingsanto/agent/1.2.4/pingsanto-agent-x86_64.tgz",
    "sha256": "8d27...b4c0",
    "signature_url": "https://artifacts.example.com/pingsanto/agent/1.2.4/pingsanto-agent-x86_64.sig",
    "force_apply": false
  },
  "schedule": {
    "earliest": "2025-10-23T18:00:00Z",
    "latest": "2025-10-23T23:00:00Z"
  },
  "paused": false,
  "notes": "rollout window for stable ring"
}
```

Fields map directly to `agent_upgrade_plans`. The `agent_id` in the response is the identifier associated with the stored plan. For channel-wide rollouts the controller returns a synthetic key such as `channel:stable` even though the requesting agent ID differs. Controllers return an `ETag` header derived from the serialized payload and honour `If-None-Match` for efficient polling. Recommended polling interval: 60s with jitter; agents back off exponentially on `503`.

When no agent-specific plan exists the controller falls back to the latest plan for the requested channel before returning `404`.

Error responses:
| Status | Meaning |
| --- | --- |
| `403` | Certificate invalid / agent revoked. |
| `404` | No plan found for agent/channel. |
| `503` | Controller unavailable; agent retries with backoff. |

---

## 3. Reporting Upgrade Status

### `POST /api/agent/v1/upgrade/report`

**Request Body**
```json
{
  "agent_id": "agt_0123",
  "current_version": "1.2.4",
  "previous_version": "1.2.3",
  "channel": "stable",
  "status": "success",
  "started_at": "2025-10-23T19:05:00Z",
  "completed_at": "2025-10-23T19:05:54Z",
  "message": "upgraded successfully"
}
```

`status` enumerations: `success`, `failed`, `skipped`. For failures, controllers encourage agents to provide `details.phase`, checksum info, or error codes for debugging.

**Handler Sketch** (`internal/server/server.go` implements this logic)
```go
channel := r.URL.Query().Get("channel")
plan, etag, err := deps.Store.FetchUpgradePlan(ctx, agentID, channel)
// ...
if err := deps.Store.RecordUpgradeReport(ctx, req); err != nil { ... }
```

Reports populate `agent_upgrade_history` and feed alerting/dashboards.

---

## 4. Artifact Upload (Admin)

`POST /api/admin/v1/artifacts`

- Auth: bearer token (`ADMIN_BEARER_TOKEN`).
- Multipart form fields:
  - `file` *(required)* – tarball produced by the release pipeline.
  - `signature` *(optional)* – detached signature corresponding to the artifact.
  - `version` *(optional)* – version label used when generating filenames.

**Successful Response**

```json
{
  "artifact": {
    "download_url": "https://controller.example.com/artifacts/pingsanto-agent-20251024.tar.gz",
    "signature_url": "https://controller.example.com/artifacts/pingsanto-agent-20251024.tar.gz.sig",
    "sha256": "3c6d...",
    "size": 10485760
  }
}
```

The returned URLs and checksum are passed to `POST /api/admin/v1/upgrade/plan`. Download requests are served from `/artifacts/{name}`.

---

## 5. Artifact Distribution
- Artifacts are served via HTTPS/CDN or the controller’s artifact endpoint.
- Agent downloads bundle from `artifact.url`, validates SHA-256, then verifies signature using controller’s root of trust.
- Controller can host an optional `manifest.json` describing rollback fallbacks or delta patches.

---

## 6. Upgrade Flow Summary
1. Agent polls `/upgrade/plan` (conditional requests) on startup and every minute.
2. If controller and local state both indicate pause (unless `force_apply`), agent skips.
3. If a newer artifact is available within rollout window, agent downloads, verifies, stages, updates, and restarts.
4. Agent posts `/upgrade/report` with outcome.
5. Controller monitors failure rates and can pause channels or request diagnostics.

---

## 7. Security Considerations
- All APIs require mTLS in production—current scaffolding also supports `X-Agent-ID` header for local dev.
- Artifacts must be signed and hashed; agent refuses to apply if validation fails.
- `force_apply` reserved for emergency patches; controllers should audit these events.
- Reports must not contain sensitive data; diagnostics are requested separately.

---

## 8. Future Enhancements
- Multi-stage rollouts (pre/post hooks, phased waves).
- Streaming/long-poll channel to reduce GET load.
- Automatic diagnostics collection on repeated failures.
- Integration with artifact publisher to expose release notes & compatibility gates.

---

## 9. Controller Admin APIs (Scaffolding Reference)
Temporary admin endpoints are available in the scaffolding to manage plans until a proper UI/CLI exists:

| Method & Path | Description | Auth |
| --- | --- | --- |
| `POST /api/admin/v1/upgrade/plan` | Upsert agent-specific plan. | `Authorization: Bearer <ADMIN_BEARER_TOKEN>` |
| `GET /api/admin/v1/upgrade/history/{agent_id}?limit=50` | Fetch recent upgrade reports for an agent. | Bearer token |
| `GET /api/admin/v1/settings/notifications` | Retrieve notification toggle (`notify_on_publish`). | Bearer token |
| `POST /api/admin/v1/settings/notifications` | Update notification toggle (`{"notify_on_publish": true}`) | Bearer token |

These should be replaced with RBAC-aware tooling before production deployment.

---

## 10. Controller Implementation Notes
- `internal/store/postgres.go` contains the production store leveraging the schema above.
- `migrations/0001_create_upgrade_tables.sql` creates the tables and `pgcrypto` extension.
- See `controller/README.md` for environment variables and startup instructions.
