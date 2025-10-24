package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMinisignVerifierSuccess(t *testing.T) {
	pubKeyBytes, err := os.ReadFile(filepath.Clean("testdata/test.pub"))
	if err != nil {
		t.Fatalf("read public key: %v", err)
	}
	verifier, err := NewMinisignVerifier(string(pubKeyBytes))
	if err != nil {
		t.Fatalf("NewMinisignVerifier: %v", err)
	}
	ctx := context.Background()
	if err := verifier.Verify(ctx,
		filepath.Clean("testdata/artifact.bin"),
		filepath.Clean("testdata/artifact.bin.minisig"),
	); err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
}

func TestMinisignVerifierRejectsTamperedArtifact(t *testing.T) {
	pubKeyBytes, err := os.ReadFile(filepath.Clean("testdata/test.pub"))
	if err != nil {
		t.Fatalf("read public key: %v", err)
	}
	verifier, err := NewMinisignVerifier(string(pubKeyBytes))
	if err != nil {
		t.Fatalf("NewMinisignVerifier: %v", err)
	}

	tmp := t.TempDir()
	tampered := filepath.Join(tmp, "artifact.bin")
	content := []byte("tampered contents")
	if err := os.WriteFile(tampered, content, 0o644); err != nil {
		t.Fatalf("write tampered file: %v", err)
	}

	ctx := context.Background()
	err = verifier.Verify(ctx, tampered, filepath.Clean("testdata/artifact.bin.minisig"))
	if err == nil {
		t.Fatalf("expected verification failure for tampered artifact")
	}
}
