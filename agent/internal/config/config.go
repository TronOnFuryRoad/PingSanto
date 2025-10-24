package config

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	envConfigPath     = "PINGSANTO_AGENT_CONFIG"
	DefaultConfigPath = "/etc/pingsanto/agent.yaml"
)

type Config struct {
	Agent  AgentConfig `yaml:"agent"`
	Queue  QueueConfig `yaml:"queue"`
	Probes ProbeConfig `yaml:"probes"`
	Run    RunConfig   `yaml:"run"`
}

type RunConfig struct {
	Workers        int           `yaml:"workers"`
	TickResolution time.Duration `yaml:"tick_resolution"`
}

type AgentConfig struct {
	Server         string                `yaml:"server"`
	DataDir        string                `yaml:"data_dir"`
	Labels         []string              `yaml:"labels"`
	HeartbeatSec   int                   `yaml:"heartbeat_sec"`
	RateGovernance *RateGovernanceConfig `yaml:"rate_governance"`
}

type RateGovernanceConfig struct {
	Enabled              bool `yaml:"enabled"`
	GlobalPPSCap         int  `yaml:"global_pps_cap"`
	PerDestinationPPSCap int  `yaml:"per_dest_pps_cap"`
	NotifyIfSustainedMin int  `yaml:"notify_if_sustained_minutes"`
}

type QueueConfig struct {
	MemItemsCap  int    `yaml:"mem_items_cap"`
	SpillToDisk  bool   `yaml:"spill_to_disk"`
	DiskBytesCap string `yaml:"disk_bytes_cap"`
}

type ProbeConfig struct {
	Workers      string   `yaml:"workers"`
	DNSResolvers []string `yaml:"dns_resolvers"`
}

func Load(ctx context.Context, path string) (Config, error) {
	var cfg Config

	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return cfg, fmt.Errorf("open config %q: %w", path, err)
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return cfg, fmt.Errorf("read config %q: %w", path, err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %q: %w", path, err)
	}

	return cfg, nil
}

func LoadFromEnv(ctx context.Context) (Config, error) {
	path := os.Getenv(envConfigPath)
	if path == "" {
		path = DefaultConfigPath
	}
	return Load(ctx, path)
}
