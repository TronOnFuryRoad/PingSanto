package upgrade

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBinaryInstallerInstallAndRollback(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	target := filepath.Join(tmp, "pingsanto-agent")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatalf("write target: %v", err)
	}

	staged := filepath.Join(tmp, "staged")
	if err := os.WriteFile(staged, []byte("new"), 0o755); err != nil {
		t.Fatalf("write staged: %v", err)
	}

	installer := &BinaryInstaller{TargetPath: target}
	res, err := installer.Install(ctx, staged)
	if err != nil {
		t.Fatalf("install returned error: %v", err)
	}
	if res.TargetPath != target {
		t.Fatalf("unexpected target path: %s", res.TargetPath)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read installed: %v", err)
	}
	if string(data) != "new" {
		t.Fatalf("expected new binary content, got %s", data)
	}

	if err := installer.Rollback(ctx, res); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	data, err = os.ReadFile(target)
	if err != nil {
		t.Fatalf("read rolled back: %v", err)
	}
	if string(data) != "old" {
		t.Fatalf("expected rollback to restore old binary, got %s", data)
	}
}

func TestBinaryInstallerInstallWithoutExistingTarget(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	target := filepath.Join(tmp, "pingsanto-agent")
	staged := filepath.Join(tmp, "staged")
	if err := os.WriteFile(staged, []byte("fresh"), 0o755); err != nil {
		t.Fatalf("write staged: %v", err)
	}
	installer := &BinaryInstaller{TargetPath: target}
	res, err := installer.Install(ctx, staged)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if res.TargetPath != target {
		t.Fatalf("unexpected target path: %s", res.TargetPath)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read installed: %v", err)
	}
	if string(data) != "fresh" {
		t.Fatalf("expected fresh content, got %s", data)
	}
}
