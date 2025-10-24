# Monitor Assignment API Contract

This reference captures the agent-facing JSON schema for the central `/api/agent/v1/monitors` endpoint. It is derived from the central service OpenAPI draft shared in planning and validated via the unit tests in `pkg/types/monitor_test.go`.

## Response Envelope

```json
{
  "revision": "rev-123",
  "generated_at": "2025-10-22T20:11:33Z",
  "incremental": true,
  "removed": ["mon_old_1"],
  "monitors": [
    {
      "monitor_id": "mon_new",
      "protocol": "icmp",
      "targets": ["203.0.113.7"],
      "cadence_ms": 3000,
      "timeout_ms": 1200,
      "configuration": "{}",
      "disabled": false
    }
  ]
}
```

### Fields

- `revision` *(string, required)* — Monotonic identifier used for ETag comparison.
- `generated_at` *(RFC3339 timestamp, required)* — Server time when the snapshot was assembled.
- `incremental` *(bool, optional)* — When `true`, the payload contains only the monitors that changed plus explicit removals.
- `removed` *(array[string], optional)* — Monitor IDs that should be deleted from the current schedule.
- `monitors` *(array[MonitorAssignment], required)* — Monitor definitions to upsert.

`MonitorAssignment` elements share the same shape documented in `pkg/types/monitor.go`. The agent ignores entries with `disabled: true` or blank `monitor_id`.

## Contract Validation

- `pkg/types/monitor_test.go` exercises JSON marshal/unmarshal against the schema above.
- `cmd/agent/main.go` applies incremental diffs using `applyIncrementalSnapshot` while preserving full snapshots when `incremental` is omitted.

Any change to the central API should update this document and the tests to keep the contract explicit.
