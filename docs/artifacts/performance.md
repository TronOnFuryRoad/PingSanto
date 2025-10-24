# Artifact Storage Performance Notes

## Current Benchmarks
Tests executed on the dev workstation (`12th Gen Intel Core i7-12700`, NVMe SSD).

```bash
cd /opt/pingsanto/controller
go test -run=^$ -bench BenchmarkFileStoreSaveLarge -benchmem ./internal/artifacts
```

| Benchmark | Size | Result | Throughput | Allocations |
| --- | --- | --- | --- | --- |
| `BenchmarkFileStoreSaveLarge-4` | 32 MiB | 173 ms/op | ~194 MiB/s | ~93 KiB, 33 allocs/op |

Agent-side signature verification cost (MiniSign):

```bash
cd /opt/pingsanto/agent
go test -run=^$ -bench BenchmarkMinisignVerifierVerify -benchmem ./internal/upgrade/verify
```

| Benchmark | Result | Allocations |
| --- | --- | --- |
| `BenchmarkMinisignVerifierVerify-4` | ~96 µs/op | ~2.8 KiB, 17 allocs/op |

## Tuning Guidance
- **Copy buffer size:** default 512 KiB (`ARTIFACT_COPY_BUFFER_BYTES`). Increase in 512 KiB increments if benchmarking reveals disk underutilization. Larger buffers can help on high-latency storage but may increase memory use per upload.
- **Parallel uploads:** current implementation processes uploads sequentially per request; ensure reverse proxies throttle concurrent uploads to avoid saturating disk.
- **Verification cost:** Minisign verification is sub-millisecond for small artifacts. Expect proportional increases with larger payloads (pre-hash). Re-benchmark once full release bundles are available.

## Recommended Operational Checks
1. Run the controller benchmark on representative infrastructure quarterly or after hardware changes.
2. Track upload log lines (`admin upload: artifact=… throughput=…`) in observability stack; alert if throughput drops below historical baseline (e.g., 100 MiB/s).
3. Ensure GitHub release jobs specify `version` and `signature` fields so end-to-end plan validation succeeds.
