# Local Monitoring Stack (Prometheus + Grafana)

This guide shows how to spin up a lightweight Prometheus + Grafana stack on the same machine as the PingSanto agent so you can use the readiness dashboard without an existing monitoring environment.

## Prerequisites
- Docker Engine and Docker Compose plugin installed.
- PingSanto agent running locally with the default metrics endpoint (`127.0.0.1:9310`).

## File Layout
```
agent/
├── deploy/
│   ├── docker-compose.monitoring.yml
│   └── prometheus/
│       └── prometheus.yml
└── docs/
    └── dashboards/
        └── readiness_overview_dashboard.md
```

The provided compose file brings up Prometheus (scraping the agent metrics endpoint) and Grafana on standard ports.

## Steps
1. Change to the project directory:
   ```bash
   cd /opt/pingsanto/agent
   ```
2. Start Prometheus and Grafana:
   ```bash
   docker compose -f deploy/docker-compose.monitoring.yml up -d
   ```
   - Prometheus UI: http://localhost:9090
   - Grafana UI: http://localhost:3000 (default admin/admin credentials; change after first login).
3. Verify Prometheus is scraping the agent: in Prometheus, run `pingsanto_agent_ready` in the expression browser; you should see the metric from your agent.
4. Import the readiness dashboard in Grafana:
   - Dashboards → Import → upload `docs/dashboards/json/readiness_overview.json`.
   - Select the Prometheus datasource created automatically (defaults to `Prometheus`).
   - Adjust dashboard variables (`site`, `severity`, `agent`) as needed.
5. (Optional) Persist data:
   - To keep data across container restarts, mount host directories for Prometheus and Grafana data. Edit the compose file to map volumes (not enabled by default to keep the example simple).
6. Stop the stack when finished:
   ```bash
   docker compose -f deploy/docker-compose.monitoring.yml down
   ```

## Customizing Scrape Targets
If the agent runs on a different host or port:
1. Update `deploy/prometheus/prometheus.yml`:
   ```yaml
   scrape_configs:
     - job_name: pingsanto-agent
       static_configs:
         - targets:
             - <agent-hostname>:<port>
   ```
2. Restart Prometheus:
   ```bash
   docker compose -f deploy/docker-compose.monitoring.yml restart prometheus
   ```

### AppArmor note
The compose file sets `security_opt: apparmor=unconfined` for both services to support environments where the default AppArmor profile cannot be loaded (common when running Docker inside another container). If AppArmor is fully available you can remove those lines.

## Next Steps
- Build the per-agent drill-down dashboard once readiness data is flowing.
- Consider adding alertmanager and notification routes if you want local alerting while developing.
