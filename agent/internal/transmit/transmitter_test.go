package transmit

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/pingsantohq/agent/internal/backfill"
	"github.com/pingsantohq/agent/internal/queue"
	"github.com/pingsantohq/agent/internal/queue/persist"
	"github.com/pingsantohq/agent/pkg/types"
)

func TestTransmitterReplaysBackfill(t *testing.T) {
	dir := t.TempDir()
	store, err := persist.Open(filepath.Join(dir, "spill"), 1<<20, 256)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	result := types.ProbeResult{MonitorID: "bf-1"}
	if err := store.Append(result); err != nil {
		t.Fatalf("append result: %v", err)
	}

	ctrl := backfill.New(store, backfill.WithRate(1000, 1000))
	q := queue.NewResultQueue(4)
	sink := newRecordingSink()

	tx := New(q, sink, WithBackfill(ctrl), WithIdleSleep(10*time.Millisecond), WithRetrySleep(10*time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- tx.Run(ctx)
	}()

	batch, ok := sink.waitForBatch(1, time.Second)
	if !ok {
		t.Fatalf("expected backfill batch delivered")
	}
	if len(batch) != 1 || batch[0].MonitorID != "bf-1" {
		t.Fatalf("unexpected batch contents: %+v", batch)
	}

	waitUntil(t, time.Second, func() bool {
		return ctrl.PendingBytes() == 0
	})

	cancel()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestTransmitterRetriesBackfillUntilSuccess(t *testing.T) {
	dir := t.TempDir()
	store, err := persist.Open(filepath.Join(dir, "spill"), 1<<20, 256)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.Append(types.ProbeResult{MonitorID: "retry"}); err != nil {
		t.Fatalf("append result: %v", err)
	}

	ctrl := backfill.New(store, backfill.WithRate(1000, 1000))
	q := queue.NewResultQueue(4)
	sink := newFailOnceSink()

	tx := New(q, sink, WithBackfill(ctrl), WithIdleSleep(10*time.Millisecond), WithRetrySleep(10*time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- tx.Run(ctx)
	}()

	first := <-sink.first
	if len(first) != 1 || first[0].MonitorID != "retry" {
		t.Fatalf("unexpected first batch: %+v", first)
	}

	if ctrl.PendingBytes() == 0 {
		t.Fatalf("expected pending bytes after failed send")
	}

	close(sink.allow)

	waitUntil(t, time.Second, func() bool {
		return len(sink.Results()) >= 1
	})

	waitUntil(t, time.Second, func() bool {
		return ctrl.PendingBytes() == 0
	})

	cancel()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestTransmitterPrefersLiveQueue(t *testing.T) {
	dir := t.TempDir()
	store, err := persist.Open(filepath.Join(dir, "spill"), 1<<20, 256)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.Append(types.ProbeResult{MonitorID: "backfill"}); err != nil {
		t.Fatalf("append result: %v", err)
	}

	ctrl := backfill.New(store, backfill.WithRate(1000, 1000))
	q := queue.NewResultQueue(4)
	q.Enqueue(types.ProbeResult{MonitorID: "live-1"})
	q.Enqueue(types.ProbeResult{MonitorID: "live-2"})

	sink := newRecordingSink()
	tx := New(q, sink, WithBackfill(ctrl), WithIdleSleep(10*time.Millisecond), WithRetrySleep(10*time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- tx.Run(ctx)
	}()

	first, ok := sink.waitForBatch(1, time.Second)
	if !ok {
		t.Fatalf("expected first batch")
	}
	if len(first) != 2 {
		t.Fatalf("expected two live results, got %d", len(first))
	}
	if first[0].MonitorID != "live-1" || first[1].MonitorID != "live-2" {
		t.Fatalf("expected live results first, got %+v", first)
	}

	cancel()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

type recordingSink struct {
	mu      sync.Mutex
	batches [][]types.ProbeResult
	notify  chan struct{}
}

func newRecordingSink() *recordingSink {
	return &recordingSink{
		notify: make(chan struct{}, 16),
	}
}

func (r *recordingSink) Send(ctx context.Context, results []types.ProbeResult) error {
	cpy := cloneResults(results)
	r.mu.Lock()
	r.batches = append(r.batches, cpy)
	r.mu.Unlock()
	select {
	case r.notify <- struct{}{}:
	default:
	}
	return nil
}

func (r *recordingSink) waitForBatch(n int, timeout time.Duration) ([]types.ProbeResult, bool) {
	deadline := time.After(timeout)
	for {
		r.mu.Lock()
		if len(r.batches) >= n {
			batch := cloneResults(r.batches[n-1])
			r.mu.Unlock()
			return batch, true
		}
		r.mu.Unlock()

		select {
		case <-deadline:
			return nil, false
		case <-r.notify:
		}
	}
}

func (r *recordingSink) Results() [][]types.ProbeResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]types.ProbeResult, len(r.batches))
	for i, b := range r.batches {
		out[i] = cloneResults(b)
	}
	return out
}

type failOnceSink struct {
	first chan []types.ProbeResult
	allow chan struct{}
	mu    sync.Mutex
	res   [][]types.ProbeResult
}

func newFailOnceSink() *failOnceSink {
	return &failOnceSink{
		first: make(chan []types.ProbeResult, 1),
		allow: make(chan struct{}),
	}
}

func (f *failOnceSink) Send(ctx context.Context, results []types.ProbeResult) error {
	cpy := cloneResults(results)

	select {
	case f.first <- cpy:
	default:
	}

	select {
	case <-f.allow:
		f.mu.Lock()
		f.res = append(f.res, cpy)
		f.mu.Unlock()
		return nil
	default:
		return errors.New("fail once")
	}
}

func (f *failOnceSink) Results() [][]types.ProbeResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]types.ProbeResult, len(f.res))
	for i, b := range f.res {
		out[i] = cloneResults(b)
	}
	return out
}

func cloneResults(in []types.ProbeResult) []types.ProbeResult {
	out := make([]types.ProbeResult, len(in))
	copy(out, in)
	return out
}

func waitUntil(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !fn() {
		t.Fatalf("condition not met within %s", timeout)
	}
}
