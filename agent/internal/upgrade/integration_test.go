package upgrade

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pingsantohq/agent/internal/config"
)

type captureInstaller struct {
	target string
	calls  int
}

func (c *captureInstaller) Install(ctx context.Context, sourcePath string) (InstallResult, error) {
	c.calls++
	if c.target == "" {
		return InstallResult{}, nil
	}
	if err := copyFile(sourcePath, c.target, 0o755); err != nil {
		return InstallResult{}, err
	}
	return InstallResult{TargetPath: c.target}, nil
}

func (c *captureInstaller) Rollback(ctx context.Context, res InstallResult) error {
	c.calls++
	return nil
}

type captureRestarter struct {
	calls int
	path  string
	args  []string
}

func (c *captureRestarter) Restart(ctx context.Context, binaryPath string, args []string, env []string) error {
	c.calls++
	c.path = binaryPath
	c.args = append([]string(nil), args...)
	return nil
}

type captureReporter struct {
	reports []Report
}

func (c *captureReporter) ReportUpgrade(ctx context.Context, report Report) error {
	c.reports = append(c.reports, report)
	return nil
}

func TestUpgradeManagerPlanToRestartIntegration(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()

	agentBinary := filepath.Join(dataDir, "agent.bin")
	if err := os.WriteFile(agentBinary, []byte("old"), 0o755); err != nil {
		t.Fatalf("write agent binary: %v", err)
	}

	state := config.State{
		AgentID: "agt-1",
		Upgrade: config.UpgradeState{
			Channel: "stable",
		},
	}
	if err := config.SaveState(ctx, dataDir, state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	artifactBytes := buildExecutableTar(t)
	var capturedReport Report

	mux := http.NewServeMux()
	var serverURL string
	mux.HandleFunc("/api/agent/v1/upgrade/plan", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"agent_id":     "channel:stable",
			"generated_at": time.Now().UTC(),
			"channel":      "stable",
			"artifact": map[string]any{
				"version":       "1.1.0",
				"url":           serverURL + "/artifacts/agent.tar.gz",
				"sha256":        "",
				"signature_url": "",
				"force_apply":   true,
			},
			"paused": false,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/api/agent/v1/upgrade/report", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		json.NewDecoder(r.Body).Decode(&capturedReport)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/artifacts/agent.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Write(artifactBytes)
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host == "" {
			r.URL.Host = r.Host
		}
		if r.URL.Scheme == "" {
			r.URL.Scheme = "http"
		}
		mux.ServeHTTP(w, r)
	}))
	defer server.Close()
	serverURL = server.URL

	httpClient := server.Client()
	logger := log.New(io.Discard, "", 0)

	upgradeClient, err := NewClient(httpClient, server.URL, "agt-1", logger)
	if err != nil {
		t.Fatalf("new upgrade client: %v", err)
	}

	applier := &Applier{
		DataDir:    dataDir,
		HTTPClient: httpClient,
		Logger:     logger,
		Now:        time.Now,
	}

	installer := &captureInstaller{target: agentBinary}
	restarter := &captureRestarter{}
	reporter := &captureReporter{}

	mgr := NewManager(
		Config{DataDir: dataDir, PollInterval: 10 * time.Millisecond},
		Dependencies{
			Logger:      logger,
			LoadState:   config.LoadState,
			UpdateState: config.UpdateState,
			PlanFetcher: upgradeClient,
			Reporter:    reporter,
			Applier:     applier,
			Installer:   installer,
			Restarter:   restarter,
			Args:        []string{"pingsanto-agent"},
			Env:         []string{"TEST=1"},
			Now:         time.Now,
		},
	)

	mgr.reload(ctx)
	if err := mgr.poll(ctx); err != nil {
		t.Fatalf("poll returned error: %v", err)
	}

	newBytes, err := os.ReadFile(agentBinary)
	if err != nil {
		t.Fatalf("read agent binary: %v", err)
	}
	if string(newBytes) != "hello-agent" {
		t.Fatalf("expected installed binary content, got %s", newBytes)
	}
	if restarter.calls != 1 {
		t.Fatalf("expected restarter invoked once")
	}
	if len(reporter.reports) == 0 {
		t.Fatalf("expected report generated")
	}
	if reporter.reports[len(reporter.reports)-1].Status != "success" {
		t.Fatalf("expected success report, got %+v", reporter.reports[len(reporter.reports)-1])
	}
}

func buildExecutableTar(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	content := []byte("hello-agent")
	header := &tar.Header{
		Name: "pingsanto-agent",
		Mode: 0o755,
		Size: int64(len(content)),
	}
	if err := tw.WriteHeader(header); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("write content: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}
