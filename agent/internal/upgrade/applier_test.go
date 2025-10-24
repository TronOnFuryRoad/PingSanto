package upgrade

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pingsantohq/agent/internal/config"
)

func TestApplierApplySuccess(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()

	artifactBytes := buildTarGz(t, map[string]string{
		"README.txt": "hello upgrade",
	})
	sum := sha256.Sum256(artifactBytes)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/artifact" {
			w.Write(artifactBytes)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	applier := &Applier{
		DataDir:    dataDir,
		HTTPClient: server.Client(),
		Now: func() time.Time {
			return time.Unix(1730005000, 0)
		},
	}

	state := config.State{
		Upgrade: config.UpgradeState{
			Applied: config.UpgradeAppliedState{
				Version: "1.0.0",
			},
		},
	}

	plan := Plan{
		Artifact: PlanArtifact{
			Version:    "1.1.0",
			URL:        server.URL + "/artifact",
			SHA256:     hex.EncodeToString(sum[:]),
			ForceApply: true,
		},
	}

	result, err := applier.Apply(ctx, plan, state)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if result.AppliedVersion != "1.1.0" {
		t.Fatalf("unexpected version: %+v", result)
	}
	if result.BundlePath == "" {
		t.Fatalf("expected bundle path set")
	}
	if result.BinaryPath == "" {
		t.Fatalf("expected binary path set")
	}
	content, err := os.ReadFile(filepath.Join(result.BundlePath, "README.txt"))
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(content) != "hello upgrade" {
		t.Fatalf("unexpected content: %s", content)
	}
}

func TestApplierApplyChecksumMismatch(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()

	artifactBytes := buildTarGz(t, map[string]string{"file.txt": "data"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(artifactBytes)
	}))
	t.Cleanup(server.Close)

	applier := &Applier{
		DataDir:    dataDir,
		HTTPClient: server.Client(),
	}

	plan := Plan{
		Artifact: PlanArtifact{
			Version: "1.2.0",
			URL:     server.URL,
			SHA256:  "deadbeef",
		},
	}
	state := config.State{}

	if _, err := applier.Apply(ctx, plan, state); err == nil {
		t.Fatalf("expected checksum error, got nil")
	}
}

func TestApplierApplyMissingBinary(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	content := "no binary"
	if err := tw.WriteHeader(&tar.Header{
		Name: "docs.txt",
		Mode: 0o644,
		Size: int64(len(content)),
	}); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := io.Copy(tw, bytes.NewBufferString(content)); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	artifactBytes := buf.Bytes()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(artifactBytes)
	}))
	t.Cleanup(server.Close)

	applier := &Applier{
		DataDir:    dataDir,
		HTTPClient: server.Client(),
	}

	plan := Plan{
		Artifact: PlanArtifact{
			Version: "1.3.0",
			URL:     server.URL,
			SHA256:  "",
		},
	}
	state := config.State{}

	if _, err := applier.Apply(ctx, plan, state); err == nil {
		t.Fatalf("expected error when binary missing")
	}
}

type stubVerifier struct {
	err    error
	called bool
}

func (s *stubVerifier) Verify(ctx context.Context, artifactPath, signaturePath string) error {
	s.called = true
	return s.err
}

func TestApplierApplyInvokesSignatureVerifier(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()

	artifactBytes := buildTarGz(t, map[string]string{
		"bin": "#!/bin/sh\necho hi\n",
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/artifact":
			w.Write(artifactBytes)
		case "/signature":
			w.Write([]byte("sig"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	verifier := &stubVerifier{}
	applier := &Applier{
		DataDir:    dataDir,
		HTTPClient: server.Client(),
		Verifier:   verifier,
	}

	plan := Plan{
		Artifact: PlanArtifact{
			Version:      "2.0.0",
			URL:          server.URL + "/artifact",
			SHA256:       "",
			SignatureURL: server.URL + "/signature",
			ForceApply:   true,
		},
	}
	state := config.State{}

	if _, err := applier.Apply(ctx, plan, state); err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if !verifier.called {
		t.Fatalf("expected verifier to be invoked")
	}
}

func TestApplierApplySignatureFailure(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()

	artifactBytes := buildTarGz(t, map[string]string{
		"bin": "#!/bin/sh\necho hi\n",
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/artifact":
			w.Write(artifactBytes)
		case "/signature":
			w.Write([]byte("sig"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	verifier := &stubVerifier{err: errors.New("invalid signature")}
	applier := &Applier{
		DataDir:    dataDir,
		HTTPClient: server.Client(),
		Verifier:   verifier,
	}

	plan := Plan{
		Artifact: PlanArtifact{
			Version:      "2.1.0",
			URL:          server.URL + "/artifact",
			SignatureURL: server.URL + "/signature",
		},
	}

	if _, err := applier.Apply(ctx, plan, config.State{}); err == nil {
		t.Fatalf("expected verification error")
	}
	if !verifier.called {
		t.Fatalf("expected verifier to be invoked")
	}
}

func buildTarGz(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o755,
			Size: int64(len(content)),
		}); err != nil {
			t.Fatalf("write header: %v", err)
		}
		if _, err := io.Copy(tw, bytes.NewBufferString(content)); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}
