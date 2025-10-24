package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/pingsantohq/agent/internal/worker"
)

type MonitorSpec struct {
	MonitorID     string
	Protocol      string
	Targets       []string
	Cadence       time.Duration
	Timeout       time.Duration
	Configuration string
}

type Scheduler struct {
	jobCh          chan<- worker.Job
	tickResolution time.Duration

	now func() time.Time

	mu      sync.Mutex
	entries map[string]*entry
}

type entry struct {
	spec   MonitorSpec
	next   time.Time
	paused bool
}

type Option func(*Scheduler)

func WithTickResolution(d time.Duration) Option {
	return func(s *Scheduler) {
		if d > 0 {
			s.tickResolution = d
		}
	}
}

func WithNow(now func() time.Time) Option {
	return func(s *Scheduler) {
		if now != nil {
			s.now = now
		}
	}
}

func New(jobCh chan<- worker.Job, opts ...Option) *Scheduler {
	s := &Scheduler{
		jobCh:          jobCh,
		tickResolution: 100 * time.Millisecond,
		now:            time.Now,
		entries:        make(map[string]*entry),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Scheduler) Update(specs []MonitorSpec) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	nextEntries := make(map[string]*entry, len(specs))
	for _, spec := range specs {
		interval := spec.Cadence
		if interval <= 0 {
			interval = 3 * time.Second
		}
		next := now.Add(interval)
		nextEntries[spec.MonitorID] = &entry{
			spec: spec,
			next: next,
		}
	}
	s.entries = nextEntries
}

func (s *Scheduler) Start(ctx context.Context) {
	ticker := time.NewTicker(s.tickResolution)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(s.now())
		}
	}
}

func (s *Scheduler) tick(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, e := range s.entries {
		if e.paused {
			continue
		}
		if !now.Before(e.next) {
			job := worker.Job{
				MonitorID:     e.spec.MonitorID,
				Protocol:      e.spec.Protocol,
				Targets:       append([]string{}, e.spec.Targets...),
				Cadence:       e.spec.Cadence,
				Timeout:       e.spec.Timeout,
				ScheduledFor:  e.next,
				Configuration: e.spec.Configuration,
			}
			select {
			case s.jobCh <- job:
			default:
			}
			interval := e.spec.Cadence
			if interval <= 0 {
				interval = 3 * time.Second
			}
			for !now.Before(e.next) {
				e.next = e.next.Add(interval)
			}
			s.entries[id] = e
		}
	}
}
