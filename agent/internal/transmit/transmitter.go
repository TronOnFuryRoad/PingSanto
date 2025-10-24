package transmit

import (
	"context"
	"errors"
	"time"

	"github.com/pingsantohq/agent/internal/backfill"
	"github.com/pingsantohq/agent/internal/queue"
	"github.com/pingsantohq/agent/pkg/types"
)

// Sink defines the downstream consumer for probe results (e.g. HTTPS uploader).
type Sink interface {
	Send(ctx context.Context, results []types.ProbeResult) error
}

// Option configures a Transmitter instance.
type Option func(*Transmitter)

// WithBackfill connects a backfill controller for replaying persisted results.
func WithBackfill(ctrl *backfill.Controller) Option {
	return func(t *Transmitter) {
		t.backfill = ctrl
	}
}

// WithBatchSize overrides the number of probe results flushed per send.
func WithBatchSize(size int) Option {
	return func(t *Transmitter) {
		if size > 0 {
			t.batchSize = size
		}
	}
}

// WithIdleSleep customises the sleep interval when no data is available.
func WithIdleSleep(d time.Duration) Option {
	return func(t *Transmitter) {
		if d > 0 {
			t.idleSleep = d
		}
	}
}

// WithRetrySleep customises the backoff applied after a failed send attempt.
func WithRetrySleep(d time.Duration) Option {
	return func(t *Transmitter) {
		if d > 0 {
			t.retrySleep = d
		}
	}
}

// Transmitter drains live results from the in-memory queue and replays buffered
// data from the backfill controller, handing both streams to a downstream sink.
type Transmitter struct {
	queue      *queue.ResultQueue
	backfill   *backfill.Controller
	sink       Sink
	batchSize  int
	idleSleep  time.Duration
	retrySleep time.Duration
}

// New constructs a Transmitter. The queue and sink are required.
func New(queue *queue.ResultQueue, sink Sink, opts ...Option) *Transmitter {
	t := &Transmitter{
		queue:      queue,
		sink:       sink,
		batchSize:  256,
		idleSleep:  100 * time.Millisecond,
		retrySleep: 200 * time.Millisecond,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Run blocks until the context is cancelled or an unrecoverable error occurs.
// It prioritises draining the live queue, falling back to replaying persisted
// batches through the backfill controller when idle.
func (t *Transmitter) Run(ctx context.Context) error {
	if t.queue == nil {
		return errors.New("transmitter queue is nil")
	}
	if t.sink == nil {
		return errors.New("transmitter sink is nil")
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		sent := t.flushQueue(ctx)
		if sent {
			continue
		}

		replayed, err := t.flushBackfill(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			// Backfill errors are surfaced to the caller to simplify retry semantics.
			return err
		}
		if replayed {
			continue
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(t.idleSleep):
		}
	}
}

func (t *Transmitter) flushQueue(ctx context.Context) bool {
	results := t.queue.Drain(t.batchSize)
	if len(results) == 0 {
		return false
	}

	if err := t.sink.Send(ctx, results); err != nil {
		for _, res := range results {
			t.queue.Enqueue(res)
		}
		t.sleep(ctx, t.retrySleep)
		return true
	}

	return true
}

func (t *Transmitter) flushBackfill(ctx context.Context) (bool, error) {
	if t.backfill == nil {
		return false, nil
	}

	batch, err := t.backfill.Next(ctx, t.batchSize)
	if err != nil {
		return false, err
	}
	if len(batch.Results) == 0 {
		return false, nil
	}

	if err := t.sink.Send(ctx, batch.Results); err != nil {
		t.sleep(ctx, t.retrySleep)
		return true, nil
	}

	if err := t.backfill.Ack(batch); err != nil {
		return true, err
	}

	return true, nil
}

func (t *Transmitter) sleep(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
