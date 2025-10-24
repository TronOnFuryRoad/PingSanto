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

	earliest := time.Unix(1730003600, 0).UTC()
	latest := time.Unix(1730007200, 0).UTC()
	state := State{
		AgentID:    "agt_123",
		Server:     "https://central.example.com",
		Labels:     map[string]string{"site": "ATL-1"},
		EnrolledAt: time.Unix(1730000000, 0).UTC(),
		CertPath:   "client.crt",
		KeyPath:    "client.key",
		CAPath:     "ca.pem",
		ConfigPath: "/etc/pingsanto/agent.yaml",
		Upgrade: UpgradeState{
			Channel: "stable",
			Paused:  true,
			Plan: UpgradePlanState{
				Version:      "1.2.3",
				Channel:      "stable",
				Source:       "channel:stable",
				Paused:       false,
				ArtifactURL:  "https://example.com/pingsanto-agent.tgz",
				SignatureURL: "https://example.com/pingsanto-agent.sig",
				SHA256:       "deadbeef",
				ForceApply:   true,
				Notes:        "rollout window for stable",
				Schedule: UpgradePlanSchedule{
					Earliest: &earliest,
					Latest:   &latest,
				},
				RetrievedAt: time.Unix(1730000000, 0).UTC(),
				ETag:        `"etag-123"`,
			},
			Applied: UpgradeAppliedState{
				Version:     "1.2.2",
				Path:        "/var/lib/pingsanto/agent/upgrades/1.2.2",
				AppliedAt:   time.Unix(1729999000, 0).UTC(),
				LastAttempt: time.Unix(1729999000, 0).UTC(),
				LastError:   "",
			},
		},
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
	if loaded.Upgrade.Plan.Version != "1.2.3" || loaded.Upgrade.Plan.Source != "channel:stable" {
		t.Fatalf("unexpected plan version: %+v", loaded.Upgrade.Plan)
	}
	if loaded.Upgrade.Plan.Schedule.Earliest == nil || !loaded.Upgrade.Plan.Schedule.Earliest.Equal(earliest) {
		t.Fatalf("unexpected plan schedule: %+v", loaded.Upgrade.Plan.Schedule)
	}
	if loaded.Upgrade.Applied.Version != "1.2.2" || loaded.Upgrade.Applied.Path == "" {
		t.Fatalf("unexpected applied state: %+v", loaded.Upgrade.Applied)
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
	state := State{
		AgentID: "agt",
		Upgrade: UpgradeState{
			Channel: "stable",
			Paused:  false,
			Plan: UpgradePlanState{
				Version:     "1.0.0",
				Channel:     "stable",
				Source:      "channel:stable",
				ArtifactURL: "https://example.com/v1.tgz",
				Paused:      false,
				ETag:        `"etag-original"`,
			},
			Applied: UpgradeAppliedState{
				Version:     "1.0.0",
				Path:        "/var/lib/pingsanto/agent/upgrades/1.0.0",
				AppliedAt:   time.Unix(1729990000, 0).UTC(),
				LastAttempt: time.Unix(1729990000, 0).UTC(),
			},
		},
	}
	if err := SaveState(ctx, dir, state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	state.Upgrade.Channel = "canary"
	state.Upgrade.Paused = true
	state.Upgrade.Plan.Version = "1.0.1"
	state.Upgrade.Plan.Channel = "canary"
	state.Upgrade.Plan.Source = "agent:agt"
	state.Upgrade.Plan.Paused = true
	state.Upgrade.Plan.ETag = `"etag-next"`
	state.Upgrade.Applied.Version = "1.0.1"
	state.Upgrade.Applied.Path = "/var/lib/pingsanto/agent/upgrades/1.0.1"
	state.Upgrade.Applied.AppliedAt = time.Unix(1730001000, 0).UTC()
	state.Upgrade.Applied.LastAttempt = time.Unix(1730001000, 0).UTC()
	state.Upgrade.Applied.LastError = "force apply"
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
	if loaded.Upgrade.Plan.Version != "1.0.1" || loaded.Upgrade.Plan.ETag != `"etag-next"` || !loaded.Upgrade.Plan.Paused {
		t.Fatalf("unexpected plan after update: %+v", loaded.Upgrade.Plan)
	}
	if loaded.Upgrade.Applied.Version != "1.0.1" || loaded.Upgrade.Applied.Path == "" || loaded.Upgrade.Applied.LastError != "force apply" {
		t.Fatalf("unexpected applied after update: %+v", loaded.Upgrade.Applied)
	}
}

func TestStatePath(t *testing.T) {
	dir := "/var/lib/pingsanto/agent"
	expected := filepath.Join(dir, StateFileName)
	if got := StatePath(dir); got != expected {
		t.Fatalf("expected %q got %q", expected, got)
	}
}
