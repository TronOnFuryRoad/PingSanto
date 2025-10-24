package types

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMonitorSnapshotJSONContract(t *testing.T) {
	payload := []byte(`{
        "revision": "rev-42",
        "generated_at": "2025-10-22T20:11:33Z",
        "incremental": true,
        "removed": ["mon-abandoned"],
        "monitors": [
            {
                "monitor_id": "mon-new",
                "protocol": "icmp",
                "targets": ["203.0.113.7"],
                "cadence_ms": 3000,
                "timeout_ms": 1200,
                "configuration": "{}"
            },
            {
                "monitor_id": "mon-disabled",
                "disabled": true,
                "protocol": "http",
                "targets": [],
                "cadence_ms": 0,
                "timeout_ms": 0
            }
        ]
    }`)

	var snapshot MonitorSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatalf("unmarshal monitor snapshot: %v", err)
	}

	if snapshot.Revision != "rev-42" {
		t.Fatalf("unexpected revision: %s", snapshot.Revision)
	}
	if !snapshot.Incremental {
		t.Fatalf("expected incremental to be true")
	}
	if len(snapshot.Removed) != 1 || snapshot.Removed[0] != "mon-abandoned" {
		t.Fatalf("unexpected removed list: %+v", snapshot.Removed)
	}
	if !snapshot.GeneratedAt.Equal(time.Date(2025, 10, 22, 20, 11, 33, 0, time.UTC)) {
		t.Fatalf("unexpected generated_at: %s", snapshot.GeneratedAt)
	}

	if len(snapshot.Monitors) != 2 {
		t.Fatalf("expected two monitor entries, got %d", len(snapshot.Monitors))
	}
	first := snapshot.Monitors[0]
	if first.MonitorID != "mon-new" || first.Protocol != "icmp" {
		t.Fatalf("unexpected first monitor: %+v", first)
	}
	if first.CadenceMillis != 3000 || first.TimeoutMillis != 1200 {
		t.Fatalf("unexpected cadence/timeout on first: %+v", first)
	}

	// Ensure disabled flag is preserved for removed monitors even if other fields present.
	second := snapshot.Monitors[1]
	if !second.Disabled {
		t.Fatalf("expected second monitor to be disabled")
	}
}

func TestMonitorSnapshotMarshalRoundTrip(t *testing.T) {
	original := MonitorSnapshot{
		Revision:    "rev-9",
		GeneratedAt: time.Unix(1730000000, 0).UTC(),
		Incremental: true,
		Removed:     []string{"mon-old"},
		Monitors: []MonitorAssignment{
			{
				MonitorID:     "mon-123",
				Protocol:      "tcp",
				Targets:       []string{"example.com"},
				CadenceMillis: 4000,
				TimeoutMillis: 1500,
				Configuration: "cfg",
			},
		},
	}

	payload, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}

	var decoded MonitorSnapshot
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}

	if decoded.Revision != original.Revision || decoded.Incremental != original.Incremental {
		t.Fatalf("round-trip mismatch: %+v", decoded)
	}
	if len(decoded.Removed) != 1 || decoded.Removed[0] != "mon-old" {
		t.Fatalf("round-trip removed mismatch: %+v", decoded.Removed)
	}
}
