package worker

import (
	"context"
	"runtime"
	"sync"

	"github.com/pingsantohq/agent/internal/probe"
	"github.com/pingsantohq/agent/internal/queue"
	"github.com/pingsantohq/agent/pkg/types"
)

type ResultSink interface {
	Enqueue(types.ProbeResult) bool
}

type Pool struct {
	jobs        <-chan Job
	results     ResultSink
	workerCount int
	batcher     func(context.Context, []probe.Request) ([]types.ProbeResult, error)
}

type PoolOption func(*Pool)

func WithWorkerCount(n int) PoolOption {
	return func(p *Pool) {
		if n > 0 {
			p.workerCount = n
		}
	}
}

func WithBatcher(fn func(context.Context, []probe.Request) ([]types.ProbeResult, error)) PoolOption {
	return func(p *Pool) {
		if fn != nil {
			p.batcher = fn
		}
	}
}

func NewPool(jobs <-chan Job, results ResultSink, opts ...PoolOption) *Pool {
	p := &Pool{
		jobs:        jobs,
		results:     results,
		workerCount: runtime.NumCPU(),
		batcher:     probe.Batch,
	}
	for _, opt := range opts {
		opt(p)
	}
	if p.workerCount <= 0 {
		p.workerCount = 1
	}
	if p.results == nil {
		p.results = queue.NewResultQueue(1024)
	}
	return p
}

func (p *Pool) Start(ctx context.Context) *sync.WaitGroup {
	var wg sync.WaitGroup
	for i := 0; i < p.workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.runWorker(ctx)
		}()
	}
	return &wg
}

func (p *Pool) runWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-p.jobs:
			if !ok {
				return
			}
			p.handleJob(ctx, job)
		}
	}
}

func (p *Pool) handleJob(ctx context.Context, job Job) {
	req := probe.Request{
		MonitorID: job.MonitorID,
		Protocol:  job.Protocol,
		Targets:   append([]string{}, job.Targets...),
		Timeout:   job.Timeout,
	}

	results, err := p.batcher(ctx, []probe.Request{req})
	if err != nil {
		return
	}

	for _, res := range results {
		p.results.Enqueue(res)
	}
}
