# Readiness Overview Dashboard (Draft)

## Purpose
Provide an at-a-glance view of agent readiness health across deployments, surface the dominant degradation categories/severities, and highlight agents requiring intervention. The dashboard assumes a Prometheus data source scraping each agent’s `/metrics` endpoint every 15 seconds.

## Data Sources
- Prometheus datasource: `PromDS` (replace with deployment-specific name).
- Metrics consumed (all emitted by the agent):
  - `pingsanto_agent_ready`
  - `pingsanto_agent_ready_info`
  - `pingsanto_agent_ready_categories_info`
  - `pingsanto_agent_ready_category_transitions_total`
  - `pingsanto_agent_ready_transitions_total`
  - `pingsanto_agent_ready_alerts_total`

Required labels for correlation:
- `agent_id` (exported as `instance` or relabeled external label)
- Enrollment labels via Prometheus relabeling: `site`, `isp`, `env`

## Layout Overview
1. **Top Row (Status Summary)**
   - *Stat – Agents Ready (%)*  
     ```promql
     100 * avg(pingsanto_agent_ready) by () 
     ```
     Display as percentage, green threshold ≥ 95%, yellow 80–95%, red < 80%.
   - *Stat – Agents Not Ready (Count)*  
     ```promql
     count_values("state", (1 - pingsanto_agent_ready)) > 0
     ```
     (Grafana stat with `reduce` → sum).
   - *Stat – Readiness Alerts Δ (24h)*  
     ```promql
     increase(pingsanto_agent_ready_alerts_total[24h])
     ```

2. **Row: Current Readiness**
   - *Table – Agents with Readiness Issues*  
     Query:
     ```promql
     pingsanto_agent_ready == 0
     ```
     Columns: `agent_id`, `site`, `isp`, `env`, `ready_categories`, `ready_reason` (via transformations combining results from `pingsanto_agent_ready_info` and `pingsanto_agent_ready_categories_info`).
   - *State Timeline – Agent Readiness Over Time*  
     Panel type: Status history / state timeline.  
     Query:
     ```promql
     pingsanto_agent_ready{agent_id=~"$agent"}
     ```
     Use per-agent repeating panels or multi-series stacked state timeline.

3. **Row: Category Breakdown**
   - *Bar Chart – Active Categories by Severity*  
     Query:
     ```promql
     sum by (category, severity) (pingsanto_agent_ready_categories_info)
     ```
     Configure stacked bars grouped by `category` with color mapping: `info` (blue), `warning` (orange), `critical` (red).
   - *Table – Transition Counters (24h)*  
     Query:
     ```promql
     increase(pingsanto_agent_ready_category_transitions_total[24h])
     ```
     Display columns for `category`, `severity`, `count`. Sort desc by count.

4. **Row: Trend & Alertability**
   - *Time Series – Readiness Alerts Rate*  
     ```promql
     rate(pingsanto_agent_ready_alerts_total[5m])
     ```
   - *Time Series – Ready vs Not Ready Transitions*  
     ```promql
     rate(pingsanto_agent_ready_transitions_total{state="ready"}[5m])
     rate(pingsanto_agent_ready_transitions_total{state="not_ready"}[5m])
     ```
     Plot both on same graph for context.

5. **Row: Drill-down Utilities**
   - *Panel Link – Agent Detail Dashboard* (optional)  
     Provide navigation link to per-agent dashboard (future).
   - *Panel Link – Monitor Assignment Revision View* (future)  
     Placeholder if/when readiness metrics carry revision tags.

## Variables
- `$site` (multi-select, optional)  
  ```promql
  label_values(pingsanto_agent_ready, site)
  ```
- `$severity` (multi-select, default = all)  
  Values: `info`, `warning`, `critical`.
- `$agent` (multi-select)  
  ```promql
  label_values(pingsanto_agent_ready, agent_id)
  ```

Filter queries with `site=~"$site"` etc. Example:
```promql
sum by (category, severity) (pingsanto_agent_ready_categories_info{site=~"$site", severity=~"$severity"})
```

## Alert Suggestions
- **Critical Agent Not Ready**  
  Trigger when `pingsanto_agent_ready{severity="critical"} == 0` persists > 2 scrape intervals.
- **Sustained Queue Pressure**  
  ```promql
  increase(pingsanto_agent_ready_category_transitions_total{category="QUEUE_PRESSURE", severity="warning"}[15m]) > 2
  ```
- **Certificate Expiring Soon**  
  Alert if `pingsanto_agent_ready_categories_info{category="CERT_EXPIRING"}` persists > 1 hour.

## Implementation Notes
- Use Grafana transformations to join `ready` and `ready_info` series; e.g., `Outer join` on `agent_id` then `Add field from calculation` to display reason text.
- For tables, configure value mappings to translate severity strings into colored badges.
- Consider adding annotations for readiness alerts using `pingsanto_agent_ready_alerts_total` increases.
- Dashboard JSON export should be saved under `docs/dashboards/json/readiness_overview.json` once implemented. (Placeholder until actual Grafana export is produced.)

## Next Steps
1. Build Grafana dashboard using the layout above and export JSON snapshot.
2. Validate queries against staging Prometheus with sample agent metrics.
3. Extend per-agent dashboard to include probe queue depth, spill counters, and monitor revision context.
4. Draft per-agent drilldown dashboard spec (`docs/dashboards/per_agent_drilldown_dashboard.md`) and link overview panels to it.

## Import Instructions
1. Open Grafana → `Dashboards` → `Import`.
2. Upload `docs/dashboards/json/readiness_overview.json` (or paste file contents).
3. When prompted, select the Prometheus datasource to bind to `DS_PROMETHEUS`.
4. Adjust variable defaults (`site`, `severity`, `agent`) to match deployment labels.
5. Save the dashboard under the desired folder and share UID/link with operations.

> Need a local Prometheus/Grafana stack? See `docs/dashboards/local_monitoring_stack.md` for a Docker Compose recipe that scrapes the agent’s metrics endpoint and hosts Grafana on localhost.
