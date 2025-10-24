package backfill

import (
	"context"
	"time"

	"golang.org/x/time/rate"

	"github.com/pingsantohq/agent/internal/metrics"
	"github.com/pingsantohq/agent/internal/queue/persist"
	"github.com/pingsantohq/agent/pkg/types"
)

type Controller struct {
	store    *persist.Store
	limiter  *rate.Limiter
	maxBatch int
	metrics  metrics.BackfillRecorder
}

type Option func(*Controller)

func WithRate(opsPerSecond float64, burst int) Option {
	return func(c *Controller) {
		if opsPerSecond > 0 {
			if burst <= 0 {
				burst = int(opsPerSecond)
			}
			c.limiter = rate.NewLimiter(rate.Limit(opsPerSecond), burst)
		}
	}
}

func WithMaxBatch(size int) Option {
	return func(c *Controller) {
		if size > 0 {
			c.maxBatch = size
		}
	}
}

func WithMetrics(rec metrics.BackfillRecorder) Option {
	return func(c *Controller) {
		if rec != nil {
			c.metrics = rec
		}
	}
}

func New(store *persist.Store, opts ...Option) *Controller {
	limiter := rate.NewLimiter(rate.Limit(50), 100)
	c := &Controller{
		store:    store,
		limiter:  limiter,
		maxBatch: 256,
		metrics:  metrics.NoopBackfillRecorder{},
	}
	for _, opt := range opts {
		opt(c)
	}
	c.recordPending()
	return c
}

type Batch struct {
	Results []types.ProbeResult
	ack     func() error
}

func (c *Controller) Next(ctx context.Context, max int) (Batch, error) {
	if c.store == nil {
		return Batch{}, nil
	}
	if max <= 0 || max > c.maxBatch {
		max = c.maxBatch
	}

	storeBatch, err := c.store.ReadBatch(max)
	if err != nil {
		return Batch{}, err
	}
	c.recordPending()
	if len(storeBatch.Results) == 0 {
		return Batch{}, nil
	}
	if err := c.limiter.WaitN(ctx, len(storeBatch.Results)); err != nil {
		return Batch{}, err
	}

	return Batch{
		Results: storeBatch.Results,
		ack: func() error {
			return c.store.Ack(storeBatch)
		},
	}, nil
}

func (c *Controller) Ack(batch Batch) error {
	if batch.ack == nil {
		return nil
	}
	if err := batch.ack(); err != nil {
		return err
	}
	c.recordPending()
	return nil
}

func (c *Controller) PendingBytes() int64 {
	if c.store == nil {
		return 0
	}
	return c.store.SizeBytes()
}

func (c *Controller) SetLimiter(ratePerSecond float64, burst int) {
	if ratePerSecond <= 0 {
		ratePerSecond = 1
	}
	if burst <= 0 {
		burst = int(ratePerSecond)
	}
	c.limiter = rate.NewLimiter(rate.Limit(ratePerSecond), burst)
}

func (c *Controller) AllowAt(t time.Time, n int) bool {
	if c.limiter == nil {
		return true
	}
	return c.limiter.AllowN(t, n)
}

func (c *Controller) SetMetrics(rec metrics.BackfillRecorder) {
	if rec == nil {
		c.metrics = metrics.NoopBackfillRecorder{}
	} else {
		c.metrics = rec
	}
	c.recordPending()
}

func (c *Controller) recordPending() {
	if c.metrics == nil || c.store == nil {
		return
	}
	c.metrics.ObservePendingBytes(c.store.SizeBytes())
}
