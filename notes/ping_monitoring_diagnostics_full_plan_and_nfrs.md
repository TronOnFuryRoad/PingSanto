# Ping Monitoring & Diagnostics – Full Plan, NFRs, Benchmarks, Threat Model, and Acceptance Checklist

> **Scope:** Planning-only deliverable for a headless Ubuntu-based, PingPlotter-like monitoring and diagnostics system with React web UI, Go host + Rust probe integration (Option A via C-ABI), SQLite (small/easy) and TimescaleDB (large/advanced) storage profiles, multi-site agents, and shareable status pages.

---

## Table of Contents
1. [Product Overview](#product-overview)
2. [Feature Plan (Detailed)](#feature-plan-detailed)
   - [Core Monitoring](#core-monitoring)
   - [Settings & Configuration Model](#settings--configuration-model)
   - [Web GUI](#web-gui)
   - [Notifications & Alerting](#notifications--alerting)
   - [Metadata & Asset Tracking](#metadata--asset-tracking)
   - [Diagnostics (Manual Packs)](#diagnostics-manual-packs)
   - [Status Pages (Shareable/Embeddable)](#status-pages-shareableembeddable)
   - [Agents (Multi-site)](#agents-multi-site)
3. [Architecture](#architecture)
   - [Runtime](#runtime)
   - [Integration Strategy (Option A Now, Option B Later)](#integration-strategy-option-a-now-option-b-later)
   - [Storage Profiles: SQLite vs TimescaleDB](#storage-profiles-sqlite-vs-timescaledb)
   - [Deployment](#deployment)
4. [Observability & Operations](#observability--operations)
5. [Security & Compliance](#security--compliance)
6. [Configuration & Lifecycle](#configuration--lifecycle)
7. [UX & Accessibility](#ux--accessibility)
8. [Probing & Diagnostics Depth](#probing--diagnostics-depth)
9. [Alerting & Noise Control](#alerting--noise-control)
10. [Status Pages – Operational Details](#status-pages--operational-details)
11. [Agents – Operational Details](#agents--operational-details)
12. [Documentation & Enablement](#documentation--enablement)
13. [Legal & Licensing](#legal--licensing)
14. [Non-Functional Requirements (NFRs)](#non-functional-requirements-nfrs)
15. [Benchmark Test Plan](#benchmark-test-plan)
16. [Threat Model Table](#threat-model-table)
17. [Feature Acceptance Checklist (MVP)](#feature-acceptance-checklist-mvp)

---

## Product Overview
A modern, headless network monitoring and diagnostics tool inspired by PingPlotter. Runs on Ubuntu server; captures latency, loss, jitter, MOS; supports HTTP/HTTPS timing; exposes a rich web UI with live charts, stacked multi-monitor graphs, and shareable status pages. Supports multi-site agents reporting to a central server. Prioritizes raw sample retention (no rollups) with configurable auto-purge.

**Defaults & Policies**
- **Sampling**: 3s per monitor (configurable per Global → Group → Monitor).
- **Charts**: Background bands: Green 0–99 ms; Yellow 100–199 ms; Red ≥200 ms (configurable per level).
- **Retention**: Auto-purge older than X days (default 14 days) with manual purge action.
- **Storage**: SQLite for small/easy; TimescaleDB for large/advanced.
- **Integration**: Go host app + Rust probe library via C-ABI (single binary). Sidecar (gRPC) feature-flagged for future.

---

## Feature Plan (Detailed)

### Core Monitoring
- **Protocols**: ICMP echo; TCP SYN; UDP; HTTP/HTTPS (GET/HEAD) with stage-level timings (DNS, TCP, TLS, TTFB, total). No automatic protocol fallback.
- **Cadence**: Default 3s; per-monitor override. Per-attempt timeout and retry counts.
- **Metrics**: RTT, success, jitter (stddev of deltas or RFC3550), window loss %, MOS (E-model approximation). Track IP-at-time and sequence.
- **Dynamic DNS**: FQDN targets allowed with A/AAAA resolution; maintain IP history; **alert on IP change** (debounce window).

### Settings & Configuration Model
- **Layering**: Global → Group → Monitor precedence. “Effective config” inspector in UI.
- **Adjustable parameters**: retries, timeouts, sample rate, color bands, alert thresholds, sensitivity (K-of-N, hysteresis), retention days.
- **Profiles**: Template bundles (e.g., WAN Edge, HTTP Check) for quick creation.
- **Group-level overrides**: All core settings adjustable at Group level (inherit/override).

### Web GUI
- **Main list (sortable table)**: Status, Error Count, Ping Count, IP, FQDN, Friendly Name, Min/Max/Avg/Current latency, Jitter, % Loss, MOS, Group, Tags, inline **box-plot** cell (min/avg/max/current), optional **sparkline** preview.
- **Live view**: Streaming time-series; red vertical ticks for drops; gaps displayed for missing data; markers for IP change and diagnostics sessions.
- **Reporting window**: 1m, 5m, 10m, 20m, 30m, 1h, 3h, 6h, 12h, 1d, 2d, 5d, 7d.
- **Multi-monitor compare**: Open multiple monitors; each renders its **own stacked graph**; unlimited count (virtualized rendering).
- **Export**: CSV/PNG from current view; JSON via API.
- **Themes**: Dark & Light; per-user preference; system-theme aware.

### Notifications & Alerting
- **Triggers**: Latency bands, jitter, window loss %, MOS, **IP change**, **rate-limit sustained > 2 min**.
- **Sensitivity**: K-of-N failures; hysteresis recovery.
- **Severity bands**: Warning/Critical thresholds by loss% and p95/p99 latency.
- **Quiet hours & maintenance**: Schedule-based suppression and annotations on timeline.
- **Email**: Include last 10-min thumbnail graph; queue with retry/backoff; per-rule min-notify interval.

### Metadata & Asset Tracking
- **Monitor fields**: location (freeform), multiple ISPs/providers, account notes, contacts, groups, tags.

### Diagnostics (Manual Packs)
- **Advanced Diagnostics window** (per monitor): user-initiated, timed run (1–30 min) of diagnostic pack(s):
  - **Connectivity Pack**: MTR/traceroute (ICMP/UDP), hop RTT/loss trends, route-change markers, ASN/ISP enrichment.
  - **DNS Pack**: Resolver timing across system/1.1.1.1/8.8.8.8; response size; failure codes.
  - **HTTP/TLS Pack**: TLS chain, expiry, cipher; redirects; status/keyword assertions; per-stage timing.
- Export results with charts (PNG) and data (JSON/CSV).

### Status Pages (Shareable/Embeddable)
- **Scopes**: Single monitor, Group, or Saved View.
- **Visibility modes**: Private (login), Unlisted (signed token), Password-protected (bcrypt), optional expiry.
- **Embeds**: Iframe `?embed=1` and JS widget. CSP-friendly with `frame-ancestors` and origin allowlist.
- **Branding**: Logo, palette, theme sync; optional IP masking (/24 or /64).
- **Caching**: 10–30s server cache; ETag/Last-Modified; CDN-friendly.
- **Audit**: Create/update/rotate/revoke events; view counters.

### Agents (Multi-site)
- **Role**: Execute probes on remote sites and push results to central.
- **Security**: One-time enrollment token → mTLS cert issuance; per-agent labels (site, isp, tags); certificate revocation.
- **Resilience**: Local queue with size/time caps; spill-to-disk; backfill on reconnect; version skew tolerance.
- **Upgrades**: **Auto-upgrade ON** by default; signed binaries; staged rollout (canary → ring).
- **Dashboards**: Heartbeat age, queue depth, local rate-limit state, CPU/RAM, NIC metrics, recent failures.

---

## Architecture

### Runtime
- **Single binary** (systemd-managed) hosting: HTTP API + UI, scheduler, alert engine, storage layer, and probe engine via FFI.
- **Privileges**: `CAP_NET_RAW` for ICMP (no root).
- **Health endpoints**: `/livez`, `/readyz`, `/metrics`.

### Integration Strategy (Option A Now, Option B Later)
- **Option A (chosen)**: Rust probe compiled as static C-ABI library; invoked by Go via cgo. Narrow, versioned API; panic guards; batch probes per tick; zero-copy where feasible.
- **Option B (feature-flagged)**: Sidecar Rust probe via gRPC over Unix domain socket for fault isolation and independent upgrades (not enabled by default).

### Storage Profiles: SQLite vs TimescaleDB
- **SQLite (small/easy)**: Single-node; WAL mode; tuned pragmas; indices on `(monitor_id, ts)`; retention via DELETE + VACUUM.
- **TimescaleDB (large/advanced)**: Hypertables on `samples(ts, monitor_id)`; retention policy (default 14d); optional compression later; `time_bucket()` queries; `time_bucket_gapfill()` for gap rendering.

### Deployment
- **Now**: Single binary + systemd unit; journald logs; setcap `cap_net_raw+ep`.
- **Later**: Optional Docker Compose profile (central + TimescaleDB).

---

## Observability & Operations
- **Metrics**: Prometheus—collector loop slip (p50/p95/p99), send/recv per tick, success rate, ICMP/TCP/HTTP histograms; rule eval/sec; alert latency; dedup hits; suppressed counts; DB write latency; agent backlog; SMTP queue.
- **Logs**: Structured JSON; correlation IDs; sampling during storms; sensitive data redaction.
- **Tracing**: Optional OpenTelemetry spans for probe→persist→rule→notify.
- **Runbooks**: DB full/slow; Email backlog; Agent offline; Rate-limit triggered; Certificate expiring; Token leaked; Backup/restore.
- **Admin**: `/admin/info` (build, SBOM id, storage backend, config checksum), `/admin/config` (redacted), `/admin/tasks` (purge, reindex).

---

## Security & Compliance
- **Threat model**: ICMP/TCP abuse, status page leakage, token theft, SSRF via HTTP probe, agent impersonation, config leakage.
- **Mitigations**: Token buckets & per-destination caps; share modes (password, token expiry/rotation), origin allowlist, `frame-ancestors` CSP; `X-Robots-Tag`; IP masking; SSRF allow/deny CIDRs; redirect & size caps; mTLS for agents; secrets in env; masked logs.
- **AuthN/Z**: Local accounts (bcrypt/argon2), session cookies; roles Admin/Editor/Viewer; PATs with scopes & expiry; audit for sensitive actions.
- **Supply chain**: SBOM (CycloneDX), vulnerability scanning (OSV/Trivy), pinned deps, signed releases, reproducible container builds, third‑party license inventory.

---

## Configuration & Lifecycle
- **Layered config** with profiles; effective-config inspector. Bulk import/export (CSV/JSON), tag-based edits, dry-run diffs.
- **Migrations**: Storage abstraction; SQLite↔TimescaleDB CLI (export/import with verification). Config schema versioning.
- **Release mgmt**: SemVer, signed artifacts, changelog with breaking/feature/fix; rollback plan.

---

## UX & Accessibility
- **A11y**: Keyboard navigation, ARIA, contrast checks, reduced motion.
- **Mobile**: Responsive tables/cards; stacked graph scroll; sticky filters.
- **Onboarding**: First-run wizard (choose SQLite or TimescaleDB, SMTP, first monitor).
- **Saved Views**: Per-user saved filters/columns/sorts; shareable read-only links distinct from status pages.
- **Localization**: i18n keys; number/date localization; RTL-ready.

---

## Probing & Diagnostics Depth
- **HTTP deep timings**: Record DNS, TCP, TLS, TTFB, total; keyword/status assertions; size & redirect caps.
- **Diagnostics packs**: Connectivity, DNS, HTTP/TLS (manual, timed; exportable). Route-change markers.
- **Rate governance**: Optional feature—per-agent/interface token buckets; per-destination caps; events + email if sustained > 2 min.

---

## Alerting & Noise Control
- **Rules**: K-of-N, hysteresis; multi-signal boolean expressions (e.g., `loss_pct > 3% AND p95_rtt > 120ms FOR 60s`).
- **Severity**: Warning/Critical thresholds (loss %, p95/p99 latency, MOS).
- **Dedup/Correlation**: Correlate by agent/site, ISP, and ASN. When ≥M monitors breach within T seconds sharing a key, emit a parent incident, mark children as related/suppressed; banner shows likely upstream (e.g., ISP/ASN). Suppression window configurable.
- **Escalation**: Tiered recipients, min intervals, business hours awareness; auto-resolve with hysteresis.
- **Maintenance**: Recurring & one-off windows; auto-suppress + annotate in charts.

---

## Status Pages – Operational Details
- **Branding**: Logo upload, color overrides, dark/light sync.
- **Components**: Monitor card, group table, stacked graphs, uptime badge.
- **Caching**: 10–30s server cache; ETag; CDN-compatible headers.
- **Audit**: Share lifecycle events; view counters.

---

## Agents – Operational Details
- **Enrollment**: One-time token → mTLS cert issuance; label assignment.
- **Resilience**: Offline queue + spill-to-disk; backfill; version skew tolerance.
- **Upgrades**: Auto-upgrade ON; signed binaries; staged rollout/canary; pause/resume.
- **Dashboards**: Heartbeat, queue depth, local rate-limit state, CPU/RAM, NIC stats.

---

## Documentation & Enablement
- **Admin Guide**: Install (systemd), capabilities, SMTP, storage profiles, backups, upgrades.
- **User Guide**: Monitors, groups, alerts, status pages, exports, diagnostics.
- **Runbooks**: As listed; include commands and log locations.
- **API Docs**: OpenAPI spec; auth; pagination; filtering; examples.

---

## Legal & Licensing
- **Open source license**: **Apache-2.0** (recommended). Alternative: MPL-2.0 if file-level copyleft desired.
- **Third-party licenses**: Inventory generated in releases; ensure compatibility. Fonts/icons typically SIL OFL / CC-BY—include notices.

---

## Non-Functional Requirements (NFRs)

### Performance
- **Probe loop slip**: p95 ≤ 100 ms; p99 ≤ 200 ms at 1,000 monitors @ 3 s per node; alert if p95 > 150 ms for ≥2 min.
- **Alert latency**: Event → email enqueued ≤ 5 s (K-of-N satisfied).
- **CPU**: ≤ 5% of 1 vCPU per 100 ICMP monitors @ 3 s.
- **Memory**: ≤ 300 MB per 1,000 monitors (buffers + windows).
- **DB write latency**: p95 ≤ 20 ms (SQLite, WAL); p95 ≤ 10 ms (TimescaleDB).
- **Disk IOPS**: sustained writes < 40% device capacity.

### Scalability
- **SQLite profile**: up to ~100–150 monitors @ 3 s, 14-day retention, single agent.
- **Timescale profile**: 500–1,000+ monitors, multi-agent; 14+ days retention.
- **Agents**: ≤ 64 KB/s per busy agent to central.

### Reliability
- **Data loss**: 0 dropped samples in steady-state; <0.1% during storms with explicit gap events.
- **Recovery**: Agent backfill after reconnect; central restart without manual steps.

### Security
- **Auth**: Password hashing (bcrypt/argon2); session cookie flags; PAT scopes & expiry.
- **mTLS**: Agent mutual TLS required; cert revocation supported.
- **Least privilege**: CAP_NET_RAW only; non-root user for service.
- **Supply chain**: SBOM, signed releases, vulnerability scan pass.

### Operability
- **Metrics**: Prometheus endpoints ready; SLOs documented.
- **Logs**: Structured JSON with redaction; rotation policy documented.
- **Runbooks**: Completed and tested.

### UX/A11y
- **Keyboard nav** and ARIA for all interactive components; WCAG contrast on bands; mobile responsive layouts.

---

## Benchmark Test Plan

### Goals
Validate NFRs for performance, scalability, reliability, and alert latency under steady-state and failure conditions.

### Test Environments
- **Small**: 2 vCPU / 4 GB RAM VM; SQLite; single agent collocated.
- **Medium**: 4 vCPU / 8 GB RAM; TimescaleDB on separate VM; 1–3 agents.

### Workloads
1. **ICMP-Only Steady**: 100, 300, 500, 1,000 monitors @ 3 s for 30 min.
2. **Mixed Probe**: 70% ICMP, 30% HTTP/HTTPS with stage timings; same monitor counts.
3. **Storm**: Inject 2% random loss + +200 ms spikes on 20% monitors for 10 min; simulate ISP outage on one agent.
4. **Diagnostics Load**: 50 concurrent diagnostic sessions (5-min each) while steady-state continues.

### Measurements
- Loop slip (p50/p95/p99), CPU%, RSS, GC pause times (Go), Rust worker utilization.
- DB insert p95 latency; rows/sec; queue depth; storage growth.
- Alert rule eval/sec; alert latency percentiles; SMTP queue depth; success/failure rates.
- Agent backlog size; spill-to-disk events; backfill duration.

### Tooling & Method
- Synthetic echo target (programmable delay/loss). DNS/HTTP fixture endpoints.
- Time-synced nodes (chrony/systemd-timesyncd).
- Prometheus scrape + Grafana dashboards; log capture with jq scripts.

### Acceptance Criteria
- Meet all NFR thresholds (Performance, Reliability, Security where measurable).
- Zero fatal errors; no panics at FFI boundary; no memory leaks across 60-min runs.
- Alert latency ≤ 5 s p95 across all runs.

### Artifacts
- Benchmark report (charts, tables), configs, seeds, and replay scripts.

---

## Threat Model Table

| Threat | Vector | Impact | Likelihood | Mitigations | Testing/Validation |
|---|---|---:|:---:|---|---|
| ICMP/TCP abuse/reflection | Misconfig creates high-rate probes to 3rd parties | Legal/abuse complaints, egress cost | Med | Per-agent/global rate caps; per-destination caps; CAP_NET_RAW only; audit monitor creation | Load test caps; verify cap events + rate-limit email after 2 min |
| Status page data leakage | Token leaked or brute-forced | Infra intel exposure | Med | Unlisted tokenized links; password-protected mode; token expiry/rotation; IP masking; origin allowlist; `frame-ancestors` CSP; rate limiting; `X-Robots-Tag` | Attempt embed from disallowed origins; token rotation revokes prior |
| Token theft (embeds/widgets) | Site XSS or referrer leakage | Unauthorized viewing | Med | Short-lived tokens; rotating signatures; origin allowlist; no referrers; revoke in UI | Simulate leak; ensure revocation within cache TTL |
| SSRF via HTTP probe | Probe internal metadata endpoints | Data exfiltration | Low–Med | CIDR allow/deny lists (default deny RFC1918/link-local); redirect & max-size caps; method/port restrictions | Unit tests + e2e with blocked targets |
| Agent impersonation | Attacker posts fake data | Corrupted metrics | Low | mTLS per-agent; enrollment tokens; cert revocation; label scoping | Cert revocation test; reject without valid client cert |
| Secrets/config leakage | Logs/UI expose secrets | Credential compromise | Low | Secrets in env; masked logs; redacted config views; least-priv fs perms | Static analysis of logs; redaction unit tests |
| Supply chain | Malicious/buggy deps | RCE/License risk | Low–Med | SBOM; OSV/Trivy scans; pin deps; signed releases | CI gates; signature verification in install guide |

---

## Feature Acceptance Checklist (MVP)

### Core & Storage
- [ ] Sampling at 3 s default; per-group and per-monitor overrides.
- [ ] Protocols: ICMP, TCP SYN, UDP, HTTP/HTTPS (GET/HEAD) with stage timings.
- [ ] SQLite profile works to 100–150 monitors; Timescale profile works to 500+.
- [ ] Auto-purge retention (default 14d); manual purge action.

### Web GUI
- [ ] Monitors table with all specified columns + sortable headers.
- [ ] Inline box-plot and optional sparkline per row.
- [ ] Live updating charts with failure ticks and gap rendering; IP change markers.
- [ ] Reporting window selector with all options.
- [ ] Stacked multi-graphs for multiple open monitors.
- [ ] CSV/PNG export in UI; JSON via API.
- [ ] Dark/Light modes.

### Alerts & Notifications
- [ ] K-of-N + hysteresis rules; severity bands; quiet hours; maintenance windows.
- [ ] Email alerts include last 10-min graph.
- [ ] IP change alerts with debounce; rate-limit alert after > 2 min sustained.
- [ ] Multi-signal rule support.
- [ ] Dedup/correlation producing parent incident with child suppression.
- [ ] Escalation chains with tiers & intervals.

### Diagnostics & Packs
- [ ] Advanced Diagnostics modal; timed runs (1–30 min).
- [ ] Connectivity Pack (MTR/route changes + ASN/ISP); DNS Pack; HTTP/TLS Pack.
- [ ] Export diagnostics as JSON/CSV + PNG charts.

### Status Pages
- [ ] Share scopes (monitor/group/view); unlisted/password/private modes.
- [ ] Embeds (iframe + JS widget); origin allowlist; CSP `frame-ancestors`.
- [ ] Branding (logo/palette/theme); IP masking toggle.
- [ ] Server-side cache 10–30s; ETag headers.
- [ ] Audit of share lifecycle + view counters.

### Agents
- [ ] Enrollment with one-time token → mTLS cert issuance; labels.
- [ ] Offline queue + spill-to-disk; backfill on reconnect.
- [ ] Auto-upgrade ON; signed binaries; staged rings.
- [ ] Agent dashboard: heartbeat age, queue depth, rate-limit state, CPU/RAM, NIC stats.

### Observability & Ops
- [ ] Prometheus metrics exported; baseline Grafana dashboards provided.
- [ ] Structured JSON logs with redaction; sampling under storm.
- [ ] Optional OpenTelemetry tracing plumbed.
- [ ] Runbooks authored and tested.
- [ ] Health/admin endpoints: `/livez`, `/readyz`, `/metrics`, `/admin/info`, `/admin/config`, `/admin/tasks`.

### Security & Compliance
- [ ] Role model (Admin/Editor/Viewer) enforced across API/UI.
- [ ] PATs with scopes, expiry, and last-used tracking.
- [ ] SSRF protections active; CIDR allow/deny lists default-safe.
- [ ] Supply chain gates: SBOM, signed releases, vuln scans passing.

### Documentation & Licensing
- [ ] Admin guide, user guide, runbooks, API docs (OpenAPI) completed.
- [ ] License: Apache-2.0 applied; third‑party license inventory included.

---

**End of document**

