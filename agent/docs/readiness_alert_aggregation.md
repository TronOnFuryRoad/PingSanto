# Readiness Alert Reason Aggregation (Draft)

## Goal
Provide a consistent pipeline for central to ingest per-agent readiness degradations, normalize reasons into actionable categories, and feed dashboards/alerts once the readiness schema is finalized.

## Inputs
- Agent Prometheus metrics:
  - `pingsanto_agent_ready` gauge (0/1).
  - `pingsanto_agent_ready_info{reason}` with the most recent readiness reason string.
  - `pingsanto_agent_ready_categories_info{category,severity}` gauge enumerating the current reason categories with severity labels.
  - `pingsanto_agent_ready_category_transitions_total{category,severity}` counter of degradations per category (increments only on transitions from ready → not_ready).
  - `pingsanto_agent_ready_transitions_total{state}` counters.
  - `pingsanto_agent_ready_alerts_total` counter.
- Agent metadata: `agent_id`, `site`, `isp`, `env` (from enrollment labels).
- Monitor assignment revision (`revision`) obtained during sync (optional future label).

## Reason Taxonomy
Map `health.Checker` free-form reasons to structured categories. The checker currently emits:
- `queue capacity exceeded`
- `monitors not yet synced`
- `monitor sync stale (<duration>)`
- `monitor sync failing: <error>`
- `client certificate expiring soon`
- `client certificate expired`

Normalized categories emitted by the agent (with default severities):
1. `QUEUE_PRESSURE` – severity `warning`
2. `MONITOR_PENDING` – severity `info`
3. `MONITOR_STALE` – severity `warning`
4. `MONITOR_ERROR` – severity `critical`
5. `CERT_EXPIRING` – severity `warning`
6. `CERT_EXPIRED` – severity `critical`

The agent already reports the active categories via the `ready_categories_info` gauge and increments category counters on ready→not_ready transitions, so central no longer needs to regex the free-form reason string. The raw string remains available for debugging/context.

## Aggregation Flow
1. **Scrape** agent metrics at the configured cadence (15 s default).
2. **Detect transitions** by comparing `ready` state and transition counters to the stored state for the agent. When `ready` drops to 0 or transition counters advance, fetch the `reason` gauge.
3. **Normalize reasons** using the taxonomy above, producing structured events:
   ```json
   {
     "agent_id": "agt_x",
     "site": "ATL-1",
     "state": "not_ready",
     "detected_at": "2025-10-23T12:58:00Z",
    "categories": [
      {"code": "QUEUE_PRESSURE", "severity": "warning"},
      {"code": "MONITOR_STALE", "severity": "warning", "data": {"age_seconds": 120}},
      {"code": "MONITOR_ERROR", "severity": "critical", "data": {"message": "config checksum mismatch"}}
    ]
   }
   ```
4. **Persist** structured events in central storage (TimescaleDB table keyed by agent/time) for dashboards and alert dedupe. Store the raw reason string for debugging.
5. **Alert**: tie into the incident pipeline so that any new `not_ready` event emits a deduplicated alert enriched with site/ISP metadata and current monitor revision. Re-arm when the agent transitions back to ready.
6. **Metrics export**: compute aggregate counters per category for org/site dashboards (e.g., `agents_not_ready_total{category="QUEUE_PRESSURE"}`).

## Open Schema Considerations
- Confirm with central whether they prefer agents to emit structured labels directly (future change) or keep normalization central-side.
- Determine if monitor assignment `revision` should tag readiness events for correlation.
- Decide on retention and roll-up policy for readiness events (raw vs summarized).

## Follow-up Tasks
- Update `internal/metrics.Store` once taxonomy or severity requirements change.
- Extend health integration tests if new readiness categories/severities are introduced.
- See `docs/dashboards/readiness_overview_dashboard.md` for the readiness dashboard layout; build and export the Grafana JSON once data is available.
