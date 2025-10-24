package persist

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pingsantohq/agent/pkg/types"
)

func TestStoreAppendReadAck(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir, 1<<20, 256)
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	defer store.Close()

	results := []types.ProbeResult{
		{MonitorID: "m1", Proto: "icmp"},
		{MonitorID: "m2", Proto: "tcp"},
		{MonitorID: "m3", Proto: "udp"},
	}
	for _, res := range results {
		if err := store.Append(res); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	batch, err := store.ReadBatch(2)
	if err != nil {
		t.Fatalf("ReadBatch: %v", err)
	}
	if len(batch.Results) != 2 {
		t.Fatalf("expected 2 results got %d", len(batch.Results))
	}
	if batch.Results[0].MonitorID != "m1" || batch.Results[1].MonitorID != "m2" {
		t.Fatalf("unexpected order: %+v", batch.Results)
	}

	if err := store.Ack(batch); err != nil {
		t.Fatalf("Ack: %v", err)
	}

	batch2, err := store.ReadBatch(5)
	if err != nil {
		t.Fatalf("ReadBatch second: %v", err)
	}
	if len(batch2.Results) != 1 || batch2.Results[0].MonitorID != "m3" {
		t.Fatalf("expected last result, got %+v", batch2.Results)
	}
	if err := store.Ack(batch2); err != nil {
		t.Fatalf("Ack2: %v", err)
	}

	if size := store.SizeBytes(); size != 0 {
		t.Fatalf("expected store empty size=0 got %d", size)
	}
}

func TestStorePersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir, 1<<20, 256)
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	defer store.Close()

	if err := store.Append(types.ProbeResult{MonitorID: "m1"}); err != nil {
		t.Fatalf("append: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	store2, err := Open(dir, 1<<20, 256)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer store2.Close()

	batch, err := store2.ReadBatch(1)
	if err != nil {
		t.Fatalf("ReadBatch after reopen: %v", err)
	}
	if len(batch.Results) != 1 || batch.Results[0].MonitorID != "m1" {
		t.Fatalf("unexpected result after reopen: %+v", batch.Results)
	}
	if err := store2.Ack(batch); err != nil {
		t.Fatalf("Ack after reopen: %v", err)
	}
}

func TestStoreEnforcesMaxBytes(t *testing.T) {
	dir := t.TempDir()
	maxBytes := int64(512)
	store, err := Open(dir, maxBytes, 256)
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	defer store.Close()

	for i := 0; i < 10; i++ {
		if err := store.Append(types.ProbeResult{MonitorID: "m"}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	if store.SizeBytes() > maxBytes {
		t.Fatalf("expected size <= maxBytes got %d", store.SizeBytes())
	}

	// ensure state file exists
	if _, err := os.Stat(filepath.Join(dir, stateFileName)); err != nil {
		t.Fatalf("state file missing: %v", err)
	}
}
