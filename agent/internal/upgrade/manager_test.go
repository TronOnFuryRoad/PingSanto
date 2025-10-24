package upgrade

import (
	"context"
	"errors"
	"io"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/pingsantohq/agent/internal/config"
)

type fakePlanFetcher struct {
	mu       sync.Mutex
	calls    int
	lastETag string
	result   PlanResult
	err      error
}

func (f *fakePlanFetcher) FetchPlan(ctx context.Context, channel string, etag string) (PlanResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastETag = etag
	return f.result, f.err
}

type fakeStateStore struct {
	mu      sync.Mutex
	state   config.State
	updates []config.State
}

func (f *fakeStateStore) Load(ctx context.Context, _ string) (config.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state, nil
}

func (f *fakeStateStore) Update(ctx context.Context, _ string, state config.State) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = state
	f.updates = append(f.updates, state)
	return nil
}

type fakeApplier struct {
	mu     sync.Mutex
	calls  int
	result ApplyResult
	err    error
}

func (f *fakeApplier) Apply(ctx context.Context, plan Plan, state config.State) (ApplyResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.result, f.err
}

type fakeReporter struct {
	mu      sync.Mutex
	reports []Report
}

func (f *fakeReporter) ReportUpgrade(ctx context.Context, report Report) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reports = append(f.reports, report)
	return nil
}

func TestManagerReloadsStateAndPersistsETag(t *testing.T) {
	ctx := context.Background()
	store := &fakeStateStore{
		state: config.State{
			Upgrade: config.UpgradeState{
				Channel: "canary",
				Paused:  true,
				Plan: config.UpgradePlanState{
					ETag: `"etag-123"`,
				},
			},
		},
	}
	mgr := NewManager(
		Config{DataDir: "/fake", PollInterval: 50 * time.Millisecond},
		Dependencies{
			LoadState: store.Load,
		},
	)

	mgr.reload(ctx)

	if ch := mgr.Channel(); ch != "canary" {
		t.Fatalf("expected channel canary, got %s", ch)
	}
	if !mgr.Paused() {
		t.Fatalf("expected paused true after reload")
	}
	_, _, etag := mgr.snapshot()
	if etag != `"etag-123"` {
		t.Fatalf("expected etag %q, got %q", `"etag-123"`, etag)
	}
}

func TestManagerPollAppliesPlan(t *testing.T) {
	ctx := context.Background()
	store := &fakeStateStore{
		state: config.State{
			AgentID: "agt-1",
			Upgrade: config.UpgradeState{
				Channel: "stable",
				Plan: config.UpgradePlanState{
					ETag: `"etag-old"`,
				},
				Applied: config.UpgradeAppliedState{
					Version: "1.0.0",
				},
			},
		},
	}
	fetcher := &fakePlanFetcher{
		result: PlanResult{
			Plan: Plan{
				AgentID: "channel:stable",
				Channel: "stable",
				Artifact: PlanArtifact{
					Version:    "1.1.0",
					URL:        "https://example.com",
					SHA256:     "abc",
					ForceApply: true,
				},
			},
			ETag: `"etag-new"`,
		},
	}
	applier := &fakeApplier{
		result: ApplyResult{
			AppliedVersion:  "1.1.0",
			PreviousVersion: "1.0.0",
			AppliedAt:       time.Unix(1730001000, 0).UTC(),
			BundlePath:      "/opt/tmp/bundle",
		},
	}
	reporter := &fakeReporter{}

	mgr := NewManager(
		Config{DataDir: "/fake"},
		Dependencies{
			Logger:      log.New(io.Discard, "", 0),
			LoadState:   store.Load,
			UpdateState: store.Update,
			PlanFetcher: fetcher,
			Applier:     applier,
			Reporter:    reporter,
			Now: func() time.Time {
				return time.Unix(1730000000, 0)
			},
		},
	)

	mgr.reload(ctx)
	if err := mgr.poll(ctx); err != nil {
		t.Fatalf("poll returned error: %v", err)
	}

	if fetcher.calls != 1 {
		t.Fatalf("expected one plan fetch, got %d", fetcher.calls)
	}
	if applier.calls != 1 {
		t.Fatalf("expected applier invoked once, got %d", applier.calls)
	}
	if len(reporter.reports) != 1 {
		t.Fatalf("expected report emitted")
	}
	rep := reporter.reports[0]
	if rep.Status != "success" || rep.CurrentVersion != "1.1.0" || rep.PreviousVersion != "1.0.0" {
		t.Fatalf("unexpected report: %#v", rep)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.updates) < 2 {
		t.Fatalf("expected state persisted twice, got %d", len(store.updates))
	}
	final := store.state.Upgrade
	if final.Plan.ETag != `"etag-new"` {
		t.Fatalf("plan not updated: %#v", final.Plan)
	}
	if final.Applied.Version != "1.1.0" || final.Applied.Path != "/opt/tmp/bundle" || final.Applied.LastError != "" {
		t.Fatalf("applied state incorrect: %+v", final.Applied)
	}
}

func TestManagerPollRespectsLocalPause(t *testing.T) {
	ctx := context.Background()
	store := &fakeStateStore{
		state: config.State{
			Upgrade: config.UpgradeState{
				Channel: "stable",
				Paused:  true,
			},
		},
	}
	fetcher := &fakePlanFetcher{
		result: PlanResult{
			Plan: Plan{
				Channel: "stable",
				Artifact: PlanArtifact{
					Version: "1.1.0",
				},
			},
			ETag: `"etag"`,
		},
	}
	applier := &fakeApplier{}
	reporter := &fakeReporter{}

	mgr := NewManager(
		Config{DataDir: "/fake"},
		Dependencies{
			LoadState:   store.Load,
			UpdateState: store.Update,
			PlanFetcher: fetcher,
			Applier:     applier,
			Reporter:    reporter,
			Now:         time.Now,
		},
	)

	mgr.reload(ctx)
	if err := mgr.poll(ctx); err != nil {
		t.Fatalf("poll returned error: %v", err)
	}

	if fetcher.calls != 1 {
		t.Fatalf("expected fetch even when paused")
	}
	if applier.calls != 0 {
		t.Fatalf("expected applier not invoked when paused")
	}
	if len(reporter.reports) != 0 {
		t.Fatalf("expected no report when paused")
	}
}

func TestManagerPollForceApplyOverridesPause(t *testing.T) {
	ctx := context.Background()
	store := &fakeStateStore{
		state: config.State{
			Upgrade: config.UpgradeState{
				Channel: "stable",
				Paused:  true,
			},
		},
	}
	fetcher := &fakePlanFetcher{
		result: PlanResult{
			Plan: Plan{
				Channel: "stable",
				Artifact: PlanArtifact{
					Version:    "1.2.0",
					ForceApply: true,
				},
			},
			ETag: `"etag-2"`,
		},
	}
	applier := &fakeApplier{
		result: ApplyResult{
			AppliedVersion: "1.2.0",
		},
	}

	mgr := NewManager(
		Config{DataDir: "/fake"},
		Dependencies{
			LoadState:   store.Load,
			UpdateState: store.Update,
			PlanFetcher: fetcher,
			Applier:     applier,
			Now:         time.Now,
		},
	)

	mgr.reload(ctx)
	if err := mgr.poll(ctx); err != nil {
		t.Fatalf("poll returned error: %v", err)
	}
	if applier.calls != 1 {
		t.Fatalf("expected force apply to execute despite pause")
	}
}

func TestManagerPollHandlesMissingPlan(t *testing.T) {
	ctx := context.Background()
	store := &fakeStateStore{
		state: config.State{
			Upgrade: config.UpgradeState{
				Channel: "stable",
			},
		},
	}
	fetcher := &fakePlanFetcher{err: ErrPlanNotFound}
	mgr := NewManager(
		Config{DataDir: "/fake"},
		Dependencies{
			LoadState:   store.Load,
			UpdateState: store.Update,
			PlanFetcher: fetcher,
		},
	)

	mgr.reload(ctx)
	if err := mgr.poll(ctx); err != nil {
		t.Fatalf("expected nil error when plan missing, got %v", err)
	}
}

func TestManagerPollPropagatesFetchError(t *testing.T) {
	ctx := context.Background()
	store := &fakeStateStore{
		state: config.State{
			Upgrade: config.UpgradeState{Channel: "stable"},
		},
	}
	fetcher := &fakePlanFetcher{err: errors.New("network")}
	mgr := NewManager(
		Config{DataDir: "/fake"},
		Dependencies{
			LoadState:   store.Load,
			UpdateState: store.Update,
			PlanFetcher: fetcher,
		},
	)
	mgr.reload(ctx)
	if err := mgr.poll(ctx); err == nil {
		t.Fatalf("expected error when fetch fails")
	}
}
