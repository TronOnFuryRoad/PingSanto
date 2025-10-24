package scheduler

import (
	"testing"
	"time"

	"github.com/pingsantohq/agent/internal/worker"
)

func TestSchedulerTickFiresJobs(t *testing.T) {
	jobCh := make(chan worker.Job, 10)
	current := time.Unix(0, 0).UTC()

	s := New(jobCh, WithNow(func() time.Time { return current }))

	spec := MonitorSpec{
		MonitorID: "mon1",
		Protocol:  "icmp",
		Targets:   []string{"203.0.113.1"},
		Cadence:   50 * time.Millisecond,
		Timeout:   5 * time.Second,
	}

	s.Update([]MonitorSpec{spec})

	current = current.Add(40 * time.Millisecond)
	s.tick(current)

	select {
	case <-jobCh:
		t.Fatalf("unexpected job before cadence elapsed")
	default:
	}

	current = current.Add(10 * time.Millisecond)
	s.tick(current)

	select {
	case job := <-jobCh:
		if job.MonitorID != "mon1" {
			t.Fatalf("expected monitor mon1 got %s", job.MonitorID)
		}
		if job.ScheduledFor.IsZero() {
			t.Fatalf("expected scheduled time to be set")
		}
	default:
		t.Fatalf("expected job to fire")
	}

	current = current.Add(60 * time.Millisecond)
	s.tick(current)

	select {
	case <-jobCh:
	default:
		t.Fatalf("expected second job after reschedule")
	}
}

func TestSchedulerUpdateReplacesMonitors(t *testing.T) {
	jobCh := make(chan worker.Job, 10)
	current := time.Now()
	s := New(jobCh, WithNow(func() time.Time { return current }))

	spec1 := MonitorSpec{
		MonitorID: "mon1",
		Protocol:  "icmp",
		Cadence:   20 * time.Millisecond,
	}
	spec2 := MonitorSpec{
		MonitorID: "mon2",
		Protocol:  "tcp",
		Cadence:   20 * time.Millisecond,
	}

	s.Update([]MonitorSpec{spec1})
	current = current.Add(25 * time.Millisecond)
	s.tick(current)
	if len(jobCh) != 1 || (<-jobCh).MonitorID != "mon1" {
		t.Fatalf("expected job for mon1")
	}

	s.Update([]MonitorSpec{spec2})
	current = current.Add(25 * time.Millisecond)
	s.tick(current)

	select {
	case job := <-jobCh:
		if job.MonitorID != "mon2" {
			t.Fatalf("expected monitor mon2 got %s", job.MonitorID)
		}
	default:
		t.Fatalf("expected job for mon2")
	}
}
