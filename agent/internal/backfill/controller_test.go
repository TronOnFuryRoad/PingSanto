package backfill

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/pingsantohq/agent/internal/metrics"
	"github.com/pingsantohq/agent/internal/queue/persist"
	"github.com/pingsantohq/agent/pkg/types"
)

func TestControllerNextAndAck(t *testing.T) {
	dir := t.TempDir()
	store, err := persist.Open(filepath.Join(dir, "spill"), 1<<20, 256)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	results := []types.ProbeResult{
		{MonitorID: "m1"},
		{MonitorID: "m2"},
	}
	for _, res := range results {
		if err := store.Append(res); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	ctrl := New(store, WithRate(1000, 1000), WithMaxBatch(5))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	batch, err := ctrl.Next(ctx, 10)
	if err != nil {
		t.Fatalf("Next returned error: %v", err)
	}
	if len(batch.Results) != len(results) {
		t.Fatalf("expected %d results got %d", len(results), len(batch.Results))
	}

	if err := ctrl.Ack(batch); err != nil {
		t.Fatalf("Ack returned error: %v", err)
	}

	if pending := ctrl.PendingBytes(); pending != 0 {
		t.Fatalf("expected pending bytes 0 got %d", pending)
	}
}

func TestControllerRateLimit(t *testing.T) {
	dir := t.TempDir()
	store, err := persist.Open(filepath.Join(dir, "spill"), 1<<20, 256)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	for i := 0; i < 3; i++ {
		if err := store.Append(types.ProbeResult{MonitorID: "m"}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	ctrl := New(store, WithRate(1, 1), WithMaxBatch(1))
	start := time.Now()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for i := 0; i < 3; i++ {
		batch, err := ctrl.Next(ctx, 1)
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if len(batch.Results) != 1 {
			t.Fatalf("expected 1 result")
		}
		ctrl.Ack(batch)
	}

	elapsed := time.Since(start)
	if elapsed < 2*time.Second {
		t.Fatalf("expected rate limiter to throttle, elapsed %v", elapsed)
	}
}

func TestControllerMetricsRecorder(t *testing.T) {
	dir := t.TempDir()
	store, err := persist.Open(filepath.Join(dir, "spill"), 1<<20, 256)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	for i := 0; i < 2; i++ {
		if err := store.Append(types.ProbeResult{MonitorID: "m"}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	mstore := metrics.NewStore()
	ctrl := New(store, WithMetrics(mstore.BackfillRecorder()))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	batch, err := ctrl.Next(ctx, 2)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}

	if got := mstore.Snapshot().BackfillPendingBytes; got == 0 {
		t.Fatalf("expected pending bytes >0 after Next")
	}

	if err := ctrl.Ack(batch); err != nil {
		t.Fatalf("Ack: %v", err)
	}

	if got := mstore.Snapshot().BackfillPendingBytes; got != 0 {
		t.Fatalf("expected pending bytes 0 after ack, got %d", got)
	}
}
