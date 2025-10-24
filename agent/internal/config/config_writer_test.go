package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteSignedConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")

	data := []byte("agent:\n  server: https://central.example.com\n")

	if err := WriteSignedConfig(path, data); err != nil {
		t.Fatalf("WriteSignedConfig returned error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o640 {
		t.Fatalf("expected perms 0640 got %v", perm)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(written) != string(data) {
		t.Fatalf("expected config contents %q got %q", string(data), string(written))
	}
}

func TestWriteSignedConfigNoData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")

	if err := WriteSignedConfig(path, nil); err != nil {
		t.Fatalf("expected nil error")
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("expected no file created")
	}
}
