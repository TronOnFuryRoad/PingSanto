package upgrade

import (
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/pingsantohq/agent/internal/config"
)

func TestManagerReloadsState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	loadCalls := 0
	loader := func(context.Context, string) (config.State, error) {
		loadCalls++
		return config.State{Upgrade: config.UpgradeState{Channel: "canary", Paused: loadCalls == 1}}, nil
	}

	var logBuf strings.Builder
	logger := log.New(&logBuf, "", 0)
	mgr := NewManager(Config{DataDir: "/fake", PollInterval: 10 * time.Millisecond}, Dependencies{Logger: logger, LoadState: loader})

	mgr.reload(ctx)
	if mgr.Channel() != "canary" {
		t.Fatalf("expected channel canary, got %s", mgr.Channel())
	}
	if !mgr.Paused() {
		t.Fatalf("expected paused true on first load")
	}

	mgr.reload(ctx)
	if mgr.Paused() {
		t.Fatalf("expected paused false after second load")
	}
}

func TestManagerRunStopsOnContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	loader := func(context.Context, string) (config.State, error) {
		return config.State{Upgrade: config.UpgradeState{Channel: "stable"}}, nil
	}
	mgr := NewManager(Config{DataDir: "/fake", PollInterval: 5 * time.Millisecond}, Dependencies{LoadState: loader})

	done := make(chan struct{})
	go func() {
		err := mgr.Run(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
		close(done)
	}()

	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("manager did not stop after cancel")
	}
}
