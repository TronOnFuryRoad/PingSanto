package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const sampleYAML = `
agent:
  server: https://central.example.com
  data_dir: /var/lib/pingsanto/agent
  labels: ["site=ATL-1","isp=Comcast","env=prod"]
  heartbeat_sec: 15
queue:
  mem_items_cap: 200000
  spill_to_disk: true
  disk_bytes_cap: 2GiB
probes:
  workers: auto
  dns_resolvers: [system]
`

func TestLoad(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")

	if err := os.WriteFile(path, []byte(sampleYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(ctx, path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Agent.Server != "https://central.example.com" {
		t.Fatalf("unexpected server: %s", cfg.Agent.Server)
	}
	if cfg.Queue.MemItemsCap != 200000 {
		t.Fatalf("unexpected queue mem cap: %d", cfg.Queue.MemItemsCap)
	}
	if len(cfg.Probes.DNSResolvers) != 1 || cfg.Probes.DNSResolvers[0] != "system" {
		t.Fatalf("unexpected dns resolvers: %#v", cfg.Probes.DNSResolvers)
	}
}

func TestLoadFromEnv(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")

	if err := os.WriteFile(path, []byte(sampleYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv(envConfigPath, path)

	cfg, err := LoadFromEnv(ctx)
	if err != nil {
		t.Fatalf("LoadFromEnv returned error: %v", err)
	}

	if cfg.Agent.DataDir != "/var/lib/pingsanto/agent" {
		t.Fatalf("unexpected data dir: %s", cfg.Agent.DataDir)
	}
}
