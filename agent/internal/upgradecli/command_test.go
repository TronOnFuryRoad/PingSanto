package upgradecli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pingsantohq/agent/internal/config"
	"gopkg.in/yaml.v3"
)

func writeConfig(t *testing.T, path, dataDir string) {
	t.Helper()
	cfg := map[string]any{
		"agent": map[string]any{
			"data_dir": dataDir,
		},
	}
	b, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestRunPauseResumeAndChannel(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}

	configPath := filepath.Join(tmp, "agent.yaml")
	writeConfig(t, configPath, dataDir)

	state := config.State{AgentID: "agt", Upgrade: config.UpgradeState{Channel: "stable", Paused: false}}
	if err := config.SaveState(ctx, dataDir, state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	out := &bytes.Buffer{}
	deps := Dependencies{Now: time.Now, Out: out}

	if err := Run(ctx, []string{"--config", configPath, "--pause"}, deps); err != nil {
		t.Fatalf("pause: %v", err)
	}
	loaded, err := config.LoadState(ctx, dataDir)
	if err != nil {
		t.Fatalf("load state after pause: %v", err)
	}
	if !loaded.Upgrade.Paused {
		t.Fatalf("expected paused true")
	}

	out.Reset()
	if err := Run(ctx, []string{"--config", configPath, "--channel", "canary"}, deps); err != nil {
		t.Fatalf("channel: %v", err)
	}
	loaded, err = config.LoadState(ctx, dataDir)
	if err != nil {
		t.Fatalf("load state after channel: %v", err)
	}
	if loaded.Upgrade.Channel != "canary" {
		t.Fatalf("expected channel canary, got %s", loaded.Upgrade.Channel)
	}

	out.Reset()
	if err := Run(ctx, []string{"--config", configPath, "--resume"}, deps); err != nil {
		t.Fatalf("resume: %v", err)
	}
	loaded, err = config.LoadState(ctx, dataDir)
	if err != nil {
		t.Fatalf("load state after resume: %v", err)
	}
	if loaded.Upgrade.Paused {
		t.Fatalf("expected paused false")
	}

	out.Reset()
	if err := Run(ctx, []string{"--config", configPath, "--status"}, deps); err != nil {
		t.Fatalf("status: %v", err)
	}
	statusOutput := out.String()
	if !strings.Contains(statusOutput, "Upgrade channel") || !strings.Contains(statusOutput, "Auto-upgrades paused") {
		t.Fatalf("unexpected status output: %s", statusOutput)
	}
}

func TestRunErrors(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	configPath := filepath.Join(tmp, "agent.yaml")
	writeConfig(t, configPath, dataDir)
	// Missing state should error
	deps := Dependencies{Now: time.Now, Out: &bytes.Buffer{}}
	if err := Run(ctx, []string{"--config", configPath}, deps); err == nil {
		t.Fatalf("expected error loading state when absent")
	}
}
