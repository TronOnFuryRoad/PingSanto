# Readiness Metrics Update Notes

## Summary
- **New metrics emitted**:
  - `pingsanto_agent_ready_categories_info{category,severity}` — active readiness categories (deduped per evaluation) annotated with normalized severity.
  - `pingsanto_agent_ready_category_transitions_total{category,severity}` — counts per category when the agent shifts from ready → not_ready, preserving severity for alert dedupe.
  - Existing series retained: `pingsanto_agent_ready`, `pingsanto_agent_ready_info`, `pingsanto_agent_ready_transitions_total`, `pingsanto_agent_ready_alerts_total`.
- **Categories surfaced**:
  - `QUEUE_PRESSURE`, `MONITOR_PENDING`, `MONITOR_STALE`, `MONITOR_ERROR`, `CERT_EXPIRING`, `CERT_EXPIRED`.
  - Category list is stable and matches the aggregation taxonomy in `agent/docs/readiness_alert_aggregation.md`; severities are `warning`, `info`, or `critical` as documented.
- **Ingestion expectations**:
  - Prometheus scrape interval remains 15 s (configurable).
  - Category counters increment only on ready→not_ready transitions to avoid double counting.
  - `ready_info{reason}` still carries the free-form message for manual debugging; automated flows should rely on category+severity pairs.

## To-Do
1. Build dashboard panels that consume `{category,severity}` metrics once the visualization stack is ready.
2. Evaluate whether readiness metrics need monitor assignment `revision` tags when correlation features land.
3. Add regression tests if the taxonomy expands beyond the current six categories or introduces new severity levels.
