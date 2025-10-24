package runtime

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/pingsantohq/agent/internal/queue/persist"
	"github.com/pingsantohq/agent/internal/scheduler"
)

func TestRuntimeGeneratesResults(t *testing.T) {
	rt := New(
		WithQueueCapacity(10),
		WithJobBuffer(10),
		WithTickResolution(10*time.Millisecond),
	)

	spec := scheduler.MonitorSpec{
		MonitorID: "mon1",
		Protocol:  "icmp",
		Targets:   []string{"203.0.113.1"},
		Cadence:   20 * time.Millisecond,
	}
	rt.UpdateMonitors([]scheduler.MonitorSpec{spec})

	ctx, cancel := context.WithCancel(context.Background())
	wait := rt.Start(ctx)

	deadline := time.NewTimer(200 * time.Millisecond)
	defer deadline.Stop()

	var success bool
	for !success {
		if rt.ResultsQueue().Len() > 0 {
			success = true
			break
		}
		select {
		case <-deadline.C:
			cancel()
			wait()
			t.Fatalf("timeout waiting for results")
		case <-time.After(10 * time.Millisecond):
		}
	}

	cancel()
	wait()

	results := rt.ResultsQueue().Drain(0)
	if len(results) == 0 {
		t.Fatalf("expected results in queue")
	}
	if results[0].MonitorID != "mon1" {
		t.Fatalf("unexpected monitor id %s", results[0].MonitorID)
	}
}

func TestRuntimeSpillIntegration(t *testing.T) {
	dir := t.TempDir()
	store, err := persist.Open(filepath.Join(dir, "spill"), 1<<20, 256)
	if err != nil {
		t.Fatalf("open spill store: %v", err)
	}
	defer store.Close()

	rt := New(
		WithQueueCapacity(2),
		WithSpill(store, 0.5),
		WithTickResolution(10*time.Millisecond),
	)

	spec := scheduler.MonitorSpec{
		MonitorID: "mon-spill",
		Protocol:  "icmp",
		Cadence:   10 * time.Millisecond,
	}
	rt.UpdateMonitors([]scheduler.MonitorSpec{spec})

	ctx, cancel := context.WithCancel(context.Background())
	wait := rt.Start(ctx)

	time.Sleep(150 * time.Millisecond)

	cancel()
	wait()

	batch, err := store.ReadBatch(10)
	if err != nil {
		t.Fatalf("ReadBatch: %v", err)
	}
	if len(batch.Results) == 0 {
		t.Fatalf("expected spilled results")
	}
}
