package queue

import (
	"path/filepath"
	"testing"

	"github.com/pingsantohq/agent/internal/queue/persist"
	"github.com/pingsantohq/agent/pkg/types"
)

func TestResultQueueEnqueueAndDrain(t *testing.T) {
	q := NewResultQueue(2)

	dropped := q.Enqueue(sampleResult("a"))
	if dropped {
		t.Fatalf("did not expect drop for first enqueue")
	}
	dropped = q.Enqueue(sampleResult("b"))
	if dropped {
		t.Fatalf("did not expect drop for second enqueue")
	}
	dropped = q.Enqueue(sampleResult("c"))
	if !dropped {
		t.Fatalf("expected drop when queue full")
	}

	if got := q.Len(); got != 2 {
		t.Fatalf("expected len 2 got %d", got)
	}

	drained := q.Drain(0)
	if len(drained) != 2 {
		t.Fatalf("expected 2 drained results got %d", len(drained))
	}
	if drained[0].MonitorID != "b" || drained[1].MonitorID != "c" {
		t.Fatalf("expected drop-oldest semantics, got %+v", drained)
	}

	if got := q.Len(); got != 0 {
		t.Fatalf("expected len 0 after drain got %d", got)
	}
}

func TestResultQueueSpillToDisk(t *testing.T) {
	dir := t.TempDir()
	store, err := persist.Open(filepath.Join(dir, "spill"), 1<<20, 256)
	if err != nil {
		t.Fatalf("open spill store: %v", err)
	}
	defer store.Close()

	q := NewResultQueue(2)
	q.AttachSpill(store, 0.5)

	q.Enqueue(sampleResult("a"))
	q.Enqueue(sampleResult("b"))
	q.Enqueue(sampleResult("c"))

	stats := q.Stats()
	if stats.Spilled == 0 {
		t.Fatalf("expected spills to occur")
	}

	batch, err := store.ReadBatch(10)
	if err != nil {
		t.Fatalf("ReadBatch: %v", err)
	}
	if len(batch.Results) == 0 {
		t.Fatalf("expected results spilled to disk")
	}
}

func TestResultQueueEvents(t *testing.T) {
	recorder := &captureRecorder{}
	q := NewResultQueue(1)
	q.SetEventRecorder(recorder)
	m := &captureMetrics{}
	q.SetMetricsRecorder(m)

	q.Enqueue(sampleResult("a"))
	q.Enqueue(sampleResult("b")) // triggers drop of "a"

	if len(recorder.events) == 0 {
		t.Fatalf("expected event to be recorded")
	}
	if recorder.events[0].Type != types.EventQueueDrop {
		t.Fatalf("expected QueueDrop event, got %s", recorder.events[0].Type)
	}
	if m.drops == 0 {
		t.Fatalf("expected metrics drops increment")
	}
}

type captureRecorder struct {
	events []types.Event
}

func (c *captureRecorder) Record(event types.Event) {
	c.events = append(c.events, event)
}

type captureMetrics struct {
	drops  int
	spills int
	depths []int
}

func (c *captureMetrics) ObserveQueueDepth(depth int) {
	c.depths = append(c.depths, depth)
}

func (c *captureMetrics) IncQueueDrops() {
	c.drops++
}

func (c *captureMetrics) IncQueueSpills() {
	c.spills++
}

func sampleResult(id string) types.ProbeResult {
	return types.ProbeResult{
		MonitorID: id,
	}
}
