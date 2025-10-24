package runtime

import (
	"context"
	"sync"
	"time"

	"github.com/pingsantohq/agent/internal/backfill"
	"github.com/pingsantohq/agent/internal/metrics"
	"github.com/pingsantohq/agent/internal/queue"
	"github.com/pingsantohq/agent/internal/queue/persist"
	"github.com/pingsantohq/agent/internal/scheduler"
	"github.com/pingsantohq/agent/internal/transmit"
	"github.com/pingsantohq/agent/internal/upgrade"
	"github.com/pingsantohq/agent/internal/worker"
)

type Option func(*config)

type config struct {
	queueCapacity  int
	jobBuffer      int
	schedulerOpts  []scheduler.Option
	workerOpts     []worker.PoolOption
	spillStore     *persist.Store
	spillThreshold float64
	backfillCtrl   *backfill.Controller
	metricsStore   *metrics.Store
	upgradeManager *upgrade.Manager
}

func WithQueueCapacity(cap int) Option {
	return func(c *config) {
		if cap > 0 {
			c.queueCapacity = cap
		}
	}
}

func WithJobBuffer(size int) Option {
	return func(c *config) {
		if size > 0 {
			c.jobBuffer = size
		}
	}
}

func WithSchedulerOptions(opts ...scheduler.Option) Option {
	return func(c *config) {
		c.schedulerOpts = append(c.schedulerOpts, opts...)
	}
}

func WithWorkerOptions(opts ...worker.PoolOption) Option {
	return func(c *config) {
		c.workerOpts = append(c.workerOpts, opts...)
	}
}

func WithSpill(store *persist.Store, threshold float64) Option {
	return func(c *config) {
		c.spillStore = store
		c.spillThreshold = threshold
	}
}

func WithBackfillController(ctrl *backfill.Controller) Option {
	return func(c *config) {
		c.backfillCtrl = ctrl
	}
}

func WithMetricsStore(store *metrics.Store) Option {
	return func(c *config) {
		c.metricsStore = store
	}
}

func WithUpgradeManager(mgr *upgrade.Manager) Option {
	return func(c *config) {
		c.upgradeManager = mgr
	}
}

type Runtime struct {
	jobs      chan worker.Job
	results   *queue.ResultQueue
	scheduler *scheduler.Scheduler
	pool      *worker.Pool
	backfill  *backfill.Controller
	upgrader  *upgrade.Manager
}

func New(opts ...Option) *Runtime {
	cfg := config{
		queueCapacity: 1024,
		jobBuffer:     1024,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	jobs := make(chan worker.Job, cfg.jobBuffer)
	results := queue.NewResultQueue(cfg.queueCapacity)
	if cfg.spillStore != nil {
		results.AttachSpill(cfg.spillStore, cfg.spillThreshold)
	}
	if cfg.metricsStore != nil {
		results.SetMetricsRecorder(cfg.metricsStore.QueueRecorder())
	}
	_sched := scheduler.New(jobs, cfg.schedulerOpts...)
	_pool := worker.NewPool(jobs, results, cfg.workerOpts...)

	if cfg.backfillCtrl != nil && cfg.metricsStore != nil {
		cfg.backfillCtrl.SetMetrics(cfg.metricsStore.BackfillRecorder())
	}

	return &Runtime{
		jobs:      jobs,
		results:   results,
		scheduler: _sched,
		pool:      _pool,
		backfill:  cfg.backfillCtrl,
		upgrader:  cfg.upgradeManager,
	}
}

func (r *Runtime) Start(ctx context.Context) func() {
	workerWG := r.pool.Start(ctx)
	var schedWG sync.WaitGroup
	schedWG.Add(1)
	go func() {
		defer schedWG.Done()
		r.scheduler.Start(ctx)
	}()

	var upgradeWG sync.WaitGroup
	if r.upgrader != nil {
		upgradeWG.Add(1)
		go func() {
			defer upgradeWG.Done()
			_ = r.upgrader.Run(ctx)
		}()
	}

	return func() {
		workerWG.Wait()
		schedWG.Wait()
		upgradeWG.Wait()
	}
}

func (r *Runtime) UpdateMonitors(specs []scheduler.MonitorSpec) {
	r.scheduler.Update(specs)
}

func (r *Runtime) ResultsQueue() *queue.ResultQueue {
	return r.results
}

func (r *Runtime) BackfillController() *backfill.Controller {
	return r.backfill
}

func (r *Runtime) JobsChannel() chan<- worker.Job {
	return r.jobs
}

func (r *Runtime) NewTransmitter(sink transmit.Sink, opts ...transmit.Option) *transmit.Transmitter {
	options := append([]transmit.Option(nil), opts...)
	if r.backfill != nil {
		options = append(options, transmit.WithBackfill(r.backfill))
	}
	return transmit.New(r.results, sink, options...)
}

func WithTickResolution(d time.Duration) Option {
	return WithSchedulerOptions(scheduler.WithTickResolution(d))
}

func WithNow(now func() time.Time) Option {
	return WithSchedulerOptions(scheduler.WithNow(now))
}
