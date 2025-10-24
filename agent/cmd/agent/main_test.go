package main

import (
	"testing"
	"time"

	"github.com/pingsantohq/agent/pkg/types"
)

func TestSnapshotToSpecs(t *testing.T) {
	snapshot := types.MonitorSnapshot{
		Monitors: []types.MonitorAssignment{
			{
				MonitorID:     "m1",
				Protocol:      "icmp",
				Targets:       []string{"198.51.100.1"},
				CadenceMillis: 5000,
				TimeoutMillis: 1200,
				Configuration: "cfg",
			},
			{
				MonitorID: "m2",
				Disabled:  true,
			},
			{
				MonitorID: "",
				Protocol:  "tcp",
			},
			{
				MonitorID:     "m3",
				Protocol:      "tcp",
				Targets:       []string{"203.0.113.5"},
				CadenceMillis: 0,
				TimeoutMillis: 0,
			},
		},
	}

	specs := snapshotToSpecs(snapshot)
	if len(specs) != 2 {
		t.Fatalf("expected 2 specs, got %d", len(specs))
	}

	first := specs[0]
	if first.MonitorID != "m1" || first.Protocol != "icmp" {
		t.Fatalf("unexpected spec: %+v", first)
	}
	if first.Cadence != 5*time.Second {
		t.Fatalf("expected cadence 5s got %s", first.Cadence)
	}
	if first.Timeout != 1200*time.Millisecond {
		t.Fatalf("expected timeout 1.2s got %s", first.Timeout)
	}

	// Ensure targets are copied.
	snapshot.Monitors[0].Targets[0] = "modified"
	if specs[0].Targets[0] != "198.51.100.1" {
		t.Fatalf("targets slice was not copied")
	}

	second := specs[1]
	if second.MonitorID != "m3" {
		t.Fatalf("expected second spec m3 got %s", second.MonitorID)
	}
	if second.Cadence != 3*time.Second {
		t.Fatalf("expected default cadence 3s got %s", second.Cadence)
	}
	if second.Timeout != 1*time.Second {
		t.Fatalf("expected default timeout 1s got %s", second.Timeout)
	}
}

func TestApplyIncrementalSnapshot(t *testing.T) {
	base := types.MonitorSnapshot{
		Monitors: []types.MonitorAssignment{
			{
				MonitorID:     "m1",
				Protocol:      "icmp",
				CadenceMillis: 3000,
				TimeoutMillis: 1000,
			},
			{
				MonitorID:     "m2",
				Protocol:      "http",
				CadenceMillis: 4000,
				TimeoutMillis: 1000,
			},
		},
	}
	state := snapshotToSpecMap(base)
	if len(state) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(state))
	}
	incremental := types.MonitorSnapshot{
		Incremental: true,
		Monitors: []types.MonitorAssignment{
			{
				MonitorID:     "m1",
				Protocol:      "icmp",
				CadenceMillis: 5000,
				TimeoutMillis: 1500,
			},
			{
				MonitorID:     "m3",
				Protocol:      "tcp",
				Targets:       []string{"203.0.113.10"},
				CadenceMillis: 2000,
				TimeoutMillis: 800,
			},
			{
				MonitorID: "m2",
				Disabled:  true,
			},
		},
		Removed: []string{"m4"},
	}
	state, upserts, removed := applyIncrementalSnapshot(state, incremental)
	if upserts != 2 {
		t.Fatalf("expected 2 upserts, got %d", upserts)
	}
	if removed != 1 {
		t.Fatalf("expected 1 removal, got %d", removed)
	}
	specs := specsFromState(state)
	if len(specs) != 2 {
		t.Fatalf("expected 2 specs after update, got %d", len(specs))
	}
	updatedM1, ok := state["m1"]
	if !ok {
		t.Fatalf("expected m1 to remain")
	}
	if updatedM1.Cadence != 5*time.Second {
		t.Fatalf("expected m1 cadence 5s, got %s", updatedM1.Cadence)
	}
	if _, exists := state["m2"]; exists {
		t.Fatalf("expected m2 to be removed")
	}
	if _, exists := state["m3"]; !exists {
		t.Fatalf("expected m3 to be inserted")
	}
}
