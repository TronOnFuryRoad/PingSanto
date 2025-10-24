# Scheduler & Probe Execution Design (Priority 2)

This document outlines the initial architecture for the agent’s scheduling and probe execution subsystems. The goal is to satisfy the requirements in `AGENTS.md` §5 (Scheduling & Execution) and related resilience sections, providing a solid foundation for later resilience/rate governance work.

## Objectives
1. Maintain per-monitor schedules with sub-second accuracy (default 3 s cadence) using a scalable timer mechanism.
2. Coordinate a worker pool that batches probe requests by protocol and invokes the Rust probe library (`probe_batch()`).
3. Buffer results in a bounded in-memory queue, emitting backpressure events when near capacity (drop-oldest policy).
4. Provide hooks for spill-to-disk and backfill (implemented in Priority 3).
5. Capture scheduling drift (“loop slip”) metrics for telemetry (Priority 4).

## Components

### 1. Scheduler (Timer Wheel)
- Maintain a hierarchical bucketed timer wheel keyed by “tick slots” (e.g., 10 ms or 100 ms resolution).
- Each monitor schedule entry includes:
  - `MonitorID`, `Protocol`, `Targets` (resolved IP/FQDN), `Cadence`, `Timeout`, `ConfigHash`.
  - Next scheduled tick (monotonic time).
- On each scheduler loop iteration:
  1. Advance the current time slot based on monotonic clock.
  2. Pop due monitor entries; enqueue `ProbeJob` to the worker queue.
  3. Calculate drift (`actual_fire_time - scheduled_time`) and record to metrics.
  4. Reschedule monitor with next tick (taking configuration changes into account).
- Configuration updates (from central) are applied by replacing the schedule table entries atomically (copy-on-write map) to avoid locking delays in the hot path.

### 2. Worker Pool
- Fixed-size pool (auto sizing based on CPU cores; override via config).
- Worker goroutines consume jobs from the queue.
- Batching strategy:
  - Workers gather jobs per protocol and time window (e.g., combine all ICMP jobs due in this tick) before calling into `probe_batch()`.
  - For Priority 2, batching logic is stubbed but the interface will reflect this future behavior.
- Worker executes:
  1. Convert jobs into FFI-friendly structures.
  2. Call Rust stub (`probe_batch`) returning synthetic results.
  3. Enqueue results into the result queue (`ResultQueue`).

### 3. Result Queue & Backpressure
- Bounded queue (size configured in YAML: `queue.mem_items_cap`).
- Enqueue semantics:
  - If queue length < cap, append.
  - If cap reached, drop oldest and raise `QueueDrop` event (Priority 3 will record event).
- Expose queue depth metrics for `/metrics`.
- Provide `Flush` hook for aggregator to consume and send results to central.

### 4. Interfaces & Packages
- `internal/scheduler`: scheduler loop, timer wheel, schedule management.
- `internal/worker`: worker pool, job queue, FFI integration.
- `internal/queue`: bounded queue + drop-oldest semantics.
- `internal/probe`: existing package extended to expose `Batch` stub.
- `internal/types`: shared job/result structs (existing `pkg/types` reused where possible).

## Execution Flow
1. Scheduler loop maintains active schedule, emits `ProbeJob` to `worker.JobQueue`.
2. Worker pool consumes jobs, groups by protocol, and invokes `probe.Batch`.
3. `probe.Batch` returns `[]types.ProbeResult` (stubbed). Worker publishes results to `queue.ResultQueue`.
4. Future stages: results drained by transmitter, rate governance applied, disk spill/backfill integrated.

## Testing Strategy (Priority 2 Scope)
- Unit tests:
  - Scheduler tick accuracy with simulated monotonic clock (fake time advancement).
  - Queue drop-oldest behavior when capacity reached.
  - Worker pool dispatch verifying job consumption and probe stub invocation.
- End-to-end tests (later) will integrate with actual Rust probe library.

This design anchors Priority 2 implementation; subsequent priorities will layer resilience, telemetry, and rate governance.
