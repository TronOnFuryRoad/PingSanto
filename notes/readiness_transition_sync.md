# Readiness Telemetry Sync Notes

## Current Agent Emissions
- `pingsanto_agent_ready` gauge (0/1) reflects the most recent readiness evaluation in `internal/metrics.Store`.
- `pingsanto_agent_ready_info{reason}` synthetic gauge carries the textual reason from `health.Checker`.
- `pingsanto_agent_ready_transitions_total{state="ready|not_ready"}` increments whenever readiness flips, derived from `Store.ObserveReadiness`.
- `pingsanto_agent_ready_alerts_total` mirrors `not_ready` transitions and captures alert-worthy degradations.
- `pingsanto_agent_ready_categories_info{category,severity}` exposes the active readiness reason categories (deduped) with severity labels.
- `pingsanto_agent_ready_category_transitions_total{category,severity}` increments per category when the agent transitions from ready to not ready and retains severity information.

## Assumptions Backing Dashboard Draft
- Central scraper ingests Prometheus text from `/metrics` per agent on 15 s cadence.
- State transitions are aggregated per agent; dashboards layer org/site metadata via agent labels.
- `reason` is stored verbatim for last not-ready evaluation and displayed as-is in dashboards/alerts.

## Decisions & Follow-ups
1. `ready_transitions_total` stays split by resulting state; existing dashboards already accommodate the gauge pair.
2. Severity metadata is required for alert dedupe. Implemented via `{category,severity}` labels and normalized taxonomy; continue to surface the raw `reason` for debugging only.
3. `ready_alerts_total` plus per-category severity counters satisfies alerting. If future severities emerge, extend the taxonomy and regression tests.
4. Reconfirmed 15 s scrape cadence with no additional retention constraints; note to revisit if controller-side storage policies change.
5. Monitor assignment `revision` tagging remains optional—no action until we introduce correlation panels that need it.

## Pending Items
- Monitor for feedback once dashboards consume the new severity-aware metrics.
- Reevaluate metric labels if we later add revision tagging or additional severities.
