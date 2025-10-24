# Artifact Pipeline Runbook

This runbook helps operators diagnose and remediate issues with the controller’s artifact ingestion path (`POST /api/admin/v1/artifacts`) and downstream distribution.

## 1. Quick Reference
- **Controller log context:** `admin upload: artifact=<name> size=<bytes> duration=<dur> throughput=<MiB/s>`
- **Configuration knobs:**
  - `ARTIFACTS_DIR` – filesystem root for stored artifacts.
  - `ARTIFACT_COPY_BUFFER_BYTES` – copy buffer size used during upload (defaults to 512 KiB).
  - `PUBLIC_BASE_URL` / `ARTIFACT_PATH` – external download URLs returned to callers.
- **Benchmarks:** `go test -run=^$ -bench BenchmarkFileStoreSaveLarge -benchmem ./internal/artifacts`

## 2. Common Symptoms
| Symptom | Likely Cause | Actions |
| --- | --- | --- |
| **400 “artifact required”** | Upload omitted `file` field | Ensure GitHub workflow / CLI sends multipart `file=@...` |
| **500 “unable to save artifact”** with log `create temp artifact` | Permission/space issue in `ARTIFACTS_DIR` | Check directory ownership, free disk. |
| **Low throughput (<50 MiB/s)** | Insufficient buffer, slow disk, noisy neighbors | Increase `ARTIFACT_COPY_BUFFER_BYTES`, run benchmark, confirm storage health. |
| **Agents cannot download artifact** | Incorrect `PUBLIC_BASE_URL`, firewall, or missing file | Curl the `download_url`, verify controller logs and artifact existence. |

## 3. Triage Steps
1. **Capture request metadata:** Ask operator for version, timestamp, workflow run URL.
2. **Inspect controller logs:**
   ```bash
   journalctl -u pingsanto-controller --since "10 min ago" | grep 'admin upload'
   ```
   Confirm the duration/throughput logged for the failing upload.
3. **Validate filesystem:**
   ```bash
   df -h $(systemctl show -p FragmentPath pingsanto-controller.service | cut -d= -f2)
   ls -lh ${ARTIFACTS_DIR:-./artifacts}
   ```
4. **Replay request (if safe):** Use `controller/cmd/upgradectl --upload-artifact` to retry with debug logging enabled.
5. **Benchmark locally (optional):**
   ```bash
   cd /opt/pingsanto/controller
   go test -run=^$ -bench BenchmarkFileStoreSaveLarge -benchmem ./internal/artifacts
   ```
   Compare throughput to historic values (~190 MiB/s on dev hardware).

## 4. Remediation
### Increase Copy Buffer
1. Decide on target buffer (e.g., `1048576` for 1 MiB).
2. Export env or update systemd unit:
   ```ini
   Environment="ARTIFACT_COPY_BUFFER_BYTES=1048576"
   ```
3. Restart controller and confirm log line reflects new buffer.

### Clear Stale Artifacts
If disk pressure is caused by unused artifacts:
```bash
find ${ARTIFACTS_DIR} -type f -mtime +14 -print
# ensure safe to delete, then:
sudo find ${ARTIFACTS_DIR} -type f -mtime +30 -delete
```

### Validate Download URLs
1. Get `download_url` from upload response or plan.
2. Curl from agent network segment:
   ```bash
   curl -I https://controller.example.com/artifacts/<name>
   ```
3. If 404, ensure file exists; if SSL error, verify TLS configuration.

## 5. Escalation
- **Platform team:** repeated 500s after buffer/disk remediation.
- **Security team:** signature mismatches or unexpected artifact tampering.
- **Release engineering:** GitHub workflow misconfiguration (missing `file` field, wrong token).
