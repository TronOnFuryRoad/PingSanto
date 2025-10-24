package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUploadArtifactFile(t *testing.T) {
	tmp := t.TempDir()
	artifactPath := filepath.Join(tmp, "artifact.tar.gz")
	if err := os.WriteFile(artifactPath, []byte("artifact"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/v1/artifacts" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer token" {
			t.Fatalf("unexpected auth header: %s", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"artifact":{"download_url":"https://example.com/a","signature_url":"","sha256":"abc"}}`)
	}))
	defer ts.Close()

	meta, err := uploadArtifactFile(ts.URL, "token", artifactPath, "", "1.0.0")
	if err != nil {
		t.Fatalf("uploadArtifactFile: %v", err)
	}
	if meta.DownloadURL != "https://example.com/a" || meta.SHA256 != "abc" {
		t.Fatalf("unexpected meta: %+v", meta)
	}
}

func TestUploadArtifactFileRequiresVersion(t *testing.T) {
	tmp := t.TempDir()
	artifactPath := filepath.Join(tmp, "artifact.tar.gz")
	if err := os.WriteFile(artifactPath, []byte("artifact"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if _, err := uploadArtifactFile("http://localhost", "token", artifactPath, "", ""); err == nil {
		t.Fatal("expected error when version is missing")
	}
}

func TestShowHistory(t *testing.T) {
	response := `{"agent_id":"agt","items":[{"current_version":"1.1.0","previous_version":"1.0.0","status":"success","message":"ok","channel":"stable","started_at":"2025-01-01T00:00:00Z","completed_at":"2025-01-01T00:00:10Z"}]}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/admin/v1/upgrade/history/") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, response)
	}))
	defer ts.Close()

	if err := showHistory(ts.URL, "token", "agt", 5); err != nil {
		t.Fatalf("showHistory: %v", err)
	}
}
