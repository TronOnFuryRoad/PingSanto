# Resilience & Backfill Plan (Priority 3)

Objective: extend the agent runtime to meet resilience requirements in `AGENTS.md` §7 (Queueing & Spill-to-Disk) and related NFRs, ensuring durable buffering, controlled backfill, and observability for gaps/events.

## Scope
1. **Queue Spill-to-Disk:** Integrate disk-backed persistence when in-memory queue approaches capacity or connectivity is lost.
2. **Reconnect & Backfill:** Govern how persisted data is replayed, honoring rate limits and avoiding central overload.
3. **Gap & Alert Events:** Emit explicit gap markers when samples are dropped, produce telemetry for spill/backfill events.
4. **Queue Telemetry:** Surface queue depth, spill counts, backfill rates via metrics and logs.

## Components & Changes

### 1. Queue Persistence Layer
- New `internal/queue/persist` package with:
  - Append-only segment files (rotating with configurable size cap).
  - Index metadata to track read/write offsets.
  - Fsync on write when spill triggered to ensure crash safety.
- Integrate with `ResultQueue`:
  - When queue length exceeds 80% of capacity or `spill_to_disk=true`, enqueue to disk buffer.
  - Maintain counters for spilled items, disk usage, and drop counts.

### 2. Connectivity & Backfill Controller
- Introduce `internal/backfill` manager:
  - Tracks online/offline state (hooked into transmitter/transport).
  - On reconnect, drains disk-backed queue at governed rate (default ≤1.5× steady-state).
  - Provides callbacks to runtime to supply backfill batches.
- Implements jittered retry/backoff for reconnect attempts (reusing config NFRs: exponential backoff 1–60s).

### 3. Gap Events & Logging
- Extend `types` to include `Event` struct for `QueueSpill`, `BackfillStart/End`, `Gap`.
- Workers emit gap event when drop occurs (with monitor IDs, count).
- Logs: structured JSON entries for spill/backfill; ensure tokens/IDs redacted per plan.

### 4. Metrics & Health
- `/metrics`: counters for queue depth, spill counts, dropped samples, backfill queue size, loop slip (already planned).
- `/readyz`: include checks for disk usage (within `disk_bytes_cap`).

## Testing Strategy
- Unit tests for spill manager (simulate threshold crossing, crash recovery via reload).
- Integration test using fake transmitter: disconnect (writing to disk), reconnect (replaying within governed rate), verifying no more than 0.1% loss.
- Benchmark harness extension (later) to validate throughput with disk spill.

## Dependencies & Sequencing
1. Implement disk persistence base (segments + index).
2. Integrate with runtime queue; add event notifications.
3. Implement backfill controller and rate limit logic.
4. Update telemetry/health and add tests.

This plan ensures Priority 3 delivers the resilience features required before rate governance and upgrades work.
