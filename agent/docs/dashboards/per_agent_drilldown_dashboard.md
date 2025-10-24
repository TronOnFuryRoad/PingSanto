# Per-Agent Drilldown Dashboard (Draft)

## Purpose
Provide an in-depth view for a single agent, displaying readiness history, queue/backfill metrics, and recent events. Complements the readiness overview dashboard by focusing on one agent at a time.

## Datasource Assumptions
- Prometheus (same dataplane as readiness overview).
- Optional: future integrations with log/query backends.

## Variables
- `agent`: `label_values(pingsanto_agent_ready, agent_id)`; prefixed by linking dashboard (e.g., from overview stat panel).
- `site`: `label_values(pingsanto_agent_ready{agent_id="$agent"}, site)`.
- `severity`: optional filter for readiness categories.

## Panel Layout
1. **Summary Stats**
   - `pingsanto_agent_ready{agent_id="$agent"}` (boolean stat).
   - `pingsanto_agent_queue_depth_number{agent_id="$agent"}` (queue depth).
   - `pingsanto_agent_backfill_pending_bytes{agent_id="$agent"}`.
   - `pingsanto_agent_ready_category_transitions_total{state="not_ready"}` recent increase (short window).

2. **Readiness Timeline**
   - State-timeline of `pingsanto_agent_ready` for the selected agent.
   - Overlay annotations when category transitions occur (requires Grafana alert annotations using `ready_category_transitions_total`).

3. **Category Snapshot**
   - Bar chart: `sum by(category, severity)(pingsanto_agent_ready_categories_info{agent_id="$agent", severity=~"$severity"})`.
   - Table: `increase(pingsanto_agent_ready_category_transitions_total{agent_id="$agent"}[24h])` with severity.

4. **Queue & Spill Details**
   - Time series: `queue_depth`, `queue_dropped_total`, `queue_spilled_total` (rate for totals).
   - Single stat summarizing spill file count (requires diagnostics ingestion; note as future item).

5. **Backfill & Transmit**
   - Time series of `pingsanto_agent_backfill_pending_bytes`.
   - Time series of transmitter success/failure (placeholder metric to be added when available).

6. **Diagnostics Links**
   - Text panel describing how to run `pingsanto-agent diag` (link to docs).
   - Table of recent diagnostic bundles (future integration with artifact storage).

7. **Logs / Journal** (future work)
   - When central logging is available, embed Loki/Elastic log panel filtered by `agent_id`.

## Grafana Implementation Notes
- Reuse dashboard style from readiness overview for consistency.
- Provide navigation link (view link) back to readiness overview.
- When exporting JSON, place file at `docs/dashboards/json/per_agent_drilldown.json`.

## Next Steps
1. Build Grafana dashboard following this spec.
2. Export JSON and document import instructions once metrics are live.
3. Extend diagnostics collection to push summarized spill stats to Prometheus for richer panels.
