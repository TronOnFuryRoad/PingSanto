package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndLoadState(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	state := State{
		AgentID:    "agt_123",
		Server:     "https://central.example.com",
		Labels:     map[string]string{"site": "ATL-1"},
		EnrolledAt: time.Unix(1730000000, 0).UTC(),
		CertPath:   "client.crt",
		KeyPath:    "client.key",
		CAPath:     "ca.pem",
		ConfigPath: "/etc/pingsanto/agent.yaml",
		Upgrade:    UpgradeState{Channel: "stable", Paused: true},
	}

	if err := SaveState(ctx, dir, state); err != nil {
		t.Fatalf("SaveState returned error: %v", err)
	}

	info, err := os.Stat(StatePath(dir))
	if err != nil {
		t.Fatalf("stat state file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("unexpected perms: %v", perm)
	}

	loaded, err := LoadState(ctx, dir)
	if err != nil {
		t.Fatalf("LoadState returned error: %v", err)
	}

	if loaded.AgentID != state.AgentID {
		t.Fatalf("expected agent_id %q got %q", state.AgentID, loaded.AgentID)
	}
	if loaded.Labels["site"] != "ATL-1" {
		t.Fatalf("expected site label ATL-1, got %q", loaded.Labels["site"])
	}
	if loaded.Upgrade.Channel != "stable" || !loaded.Upgrade.Paused {
		t.Fatalf("unexpected upgrade state: %+v", loaded.Upgrade)
	}
}

func TestSaveStateExisting(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	state := State{AgentID: "agt_existing"}
	if err := SaveState(ctx, dir, state); err != nil {
		t.Fatalf("first SaveState: %v", err)
	}

	if err := SaveState(ctx, dir, state); err == nil {
		t.Fatalf("expected error on second SaveState when file exists")
	}
}

func TestUpdateState(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	state := State{AgentID: "agt", Upgrade: UpgradeState{Channel: "stable", Paused: false}}
	if err := SaveState(ctx, dir, state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	state.Upgrade.Channel = "canary"
	state.Upgrade.Paused = true
	if err := UpdateState(ctx, dir, state); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}
	loaded, err := LoadState(ctx, dir)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if loaded.Upgrade.Channel != "canary" || !loaded.Upgrade.Paused {
		t.Fatalf("unexpected upgrade after update: %+v", loaded.Upgrade)
	}
}

func TestStatePath(t *testing.T) {
	dir := "/var/lib/pingsanto/agent"
	expected := filepath.Join(dir, StateFileName)
	if got := StatePath(dir); got != expected {
		t.Fatalf("expected %q got %q", expected, got)
	}
}
