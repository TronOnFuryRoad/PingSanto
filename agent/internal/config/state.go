package config

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const StateFileName = "state.yaml"

type State struct {
	AgentID     string            `yaml:"agent_id"`
	Server      string            `yaml:"server"`
	Labels      map[string]string `yaml:"labels"`
	EnrolledAt  time.Time         `yaml:"enrolled_at"`
	CertPath    string            `yaml:"cert_path"`
	KeyPath     string            `yaml:"key_path"`
	CAPath      string            `yaml:"ca_path"`
	ConfigPath  string            `yaml:"config_path"`
	Credentials struct {
		TokenHash string `yaml:"token_hash"`
	} `yaml:"credentials"`
	Upgrade UpgradeState `yaml:"upgrade"`
}

type UpgradeState struct {
	Channel string              `yaml:"channel"`
	Paused  bool                `yaml:"paused"`
	Plan    UpgradePlanState    `yaml:"plan"`
	Applied UpgradeAppliedState `yaml:"applied"`
}

type UpgradePlanState struct {
	Version      string              `yaml:"version"`
	Channel      string              `yaml:"channel"`
	Source       string              `yaml:"source"`
	Paused       bool                `yaml:"paused"`
	ArtifactURL  string              `yaml:"artifact_url"`
	SignatureURL string              `yaml:"signature_url"`
	SHA256       string              `yaml:"sha256"`
	ForceApply   bool                `yaml:"force_apply"`
	Notes        string              `yaml:"notes"`
	Schedule     UpgradePlanSchedule `yaml:"schedule"`
	RetrievedAt  time.Time           `yaml:"retrieved_at"`
	ETag         string              `yaml:"etag"`
}

type UpgradePlanSchedule struct {
	Earliest *time.Time `yaml:"earliest,omitempty"`
	Latest   *time.Time `yaml:"latest,omitempty"`
}

type UpgradeAppliedState struct {
	Version     string    `yaml:"version"`
	Path        string    `yaml:"path"`
	AppliedAt   time.Time `yaml:"applied_at"`
	LastAttempt time.Time `yaml:"last_attempt"`
	LastError   string    `yaml:"last_error"`
}

func StatePath(dir string) string {
	return filepath.Join(dir, StateFileName)
}

func LoadState(ctx context.Context, dir string) (State, error) {
	var state State
	path := StatePath(dir)

	data, err := os.ReadFile(path)
	if err != nil {
		return state, fmt.Errorf("read state file %q: %w", path, err)
	}

	if err := yaml.Unmarshal(data, &state); err != nil {
		return state, fmt.Errorf("parse state file %q: %w", path, err)
	}

	return state, nil
}

func SaveState(ctx context.Context, dir string, state State) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("ensure state dir %q: %w", dir, err)
	}

	path := StatePath(dir)
	_, err := os.Stat(path)
	if err == nil {
		return fmt.Errorf("state file %q already exists", path)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("check state file %q: %w", path, err)
	}

	data, err := yaml.Marshal(&state)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write temp state file %q: %w", tmp, err)
	}

	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("commit state file %q: %w", path, err)
	}

	return nil
}

func UpdateState(ctx context.Context, dir string, state State) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("ensure state dir %q: %w", dir, err)
	}

	path := StatePath(dir)
	data, err := yaml.Marshal(&state)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write temp state file %q: %w", tmp, err)
	}

	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("commit state file %q: %w", path, err)
	}

	return nil
}
