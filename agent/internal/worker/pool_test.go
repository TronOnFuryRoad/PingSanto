package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pingsantohq/agent/internal/probe"
	"github.com/pingsantohq/agent/internal/queue"
	"github.com/pingsantohq/agent/pkg/types"
)

func TestPoolProcessesJob(t *testing.T) {
	jobs := make(chan Job, 1)
	resultQueue := queue.NewResultQueue(10)
	processed := atomic.Int32{}

	batcher := func(ctx context.Context, reqs []probe.Request) ([]types.ProbeResult, error) {
		processed.Add(int32(len(reqs)))
		results := make([]types.ProbeResult, len(reqs))
		for i, req := range reqs {
			results[i] = types.ProbeResult{MonitorID: req.MonitorID, Proto: req.Protocol, Success: true}
		}
		return results, nil
	}

	p := NewPool(jobs, resultQueue, WithWorkerCount(1), WithBatcher(batcher))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wg := p.Start(ctx)

	jobs <- Job{MonitorID: "mon1", Protocol: "icmp"}

	deadline := time.NewTimer(200 * time.Millisecond)
	defer deadline.Stop()

	for {
		if processed.Load() > 0 {
			break
		}
		select {
		case <-deadline.C:
			t.Fatalf("timeout waiting for job to process")
		case <-time.After(10 * time.Millisecond):
		}
	}

	cancel()
	close(jobs)
	wg.Wait()

	results := resultQueue.Drain(0)
	if len(results) != 1 {
		t.Fatalf("expected 1 result got %d", len(results))
	}
	if results[0].MonitorID != "mon1" {
		t.Fatalf("unexpected monitor id %s", results[0].MonitorID)
	}
}
