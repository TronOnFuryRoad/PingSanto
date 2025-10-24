package diag

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pingsantohq/agent/internal/config"
	"gopkg.in/yaml.v3"
)

func TestRunCreatesDiagnosticsBundle(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()

	dataDir := filepath.Join(tmp, "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	logsDir := filepath.Join(tmp, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatalf("mkdir logs dir: %v", err)
	}

	configPath := filepath.Join(tmp, "agent.yaml")
	cfg := map[string]any{
		"agent": map[string]any{
			"data_dir": dataDir,
		},
	}
	cfgBytes, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(configPath, cfgBytes, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	state := config.State{
		AgentID:    "agt-test",
		Server:     "https://central.example.com",
		ConfigPath: configPath,
		Labels:     map[string]string{"site": "ATL-1"},
		Upgrade:    config.UpgradeState{Channel: "stable", Paused: false},
	}
	if err := config.SaveState(ctx, dataDir, state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	logFile := filepath.Join(logsDir, "agent.log")
	logContentOriginal := "log-line token=mysecret Authorization: Bearer secretvalue\n"
	if err := os.WriteFile(logFile, []byte(logContentOriginal), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	spillDir := filepath.Join(dataDir, "spill")
	if err := os.MkdirAll(spillDir, 0o700); err != nil {
		t.Fatalf("mkdir spill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(spillDir, "segment-000001.log"), []byte("spill-data"), 0o600); err != nil {
		t.Fatalf("write spill: %v", err)
	}

	metricsBody := "" +
		"# HELP pingsanto_agent_queue_depth_number Number of probe results currently buffered in memory.\n" +
		"# TYPE pingsanto_agent_queue_depth_number gauge\n" +
		"pingsanto_agent_queue_depth_number 42\n" +
		"pingsanto_agent_queue_dropped_total 3\n" +
		"pingsanto_agent_queue_spilled_total 7\n"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(metricsBody))
	}))
	defer ts.Close()

	output := filepath.Join(tmp, "diag.tar.gz")
	var journalCalls [][]string
	deps := Dependencies{
		Now: func() time.Time {
			return time.Date(2025, 10, 23, 15, 4, 5, 0, time.UTC)
		},
		HTTPClient: ts.Client(),
		RunCommand: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name != "journalctl" {
				t.Fatalf("unexpected command: %s", name)
			}
			journalCalls = append(journalCalls, args)
			return []byte("journal log line"), nil
		},
	}

	if err := Run(ctx, []string{
		"--config", configPath,
		"--data-dir", dataDir,
		"--logs", logsDir,
		"--output", output,
		"--metrics-url", ts.URL,
		"--journal-unit", "pingsanto-agent.service",
		"--journal-since", "30m",
	}, deps); err != nil {
		t.Fatalf("Run: %v", err)
	}

	f, err := os.Open(output)
	if err != nil {
		t.Fatalf("open bundle: %v", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	entries := make(map[string]bool)
	var info bundleInfo
	var metricsContent string
	var journalContent string
	var sanitizedLog string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar: %v", err)
		}
		entries[hdr.Name] = true
		if hdr.Name == infoFileName {
			payload, err := io.ReadAll(tr)
			if err != nil {
				t.Fatalf("read info: %v", err)
			}
			if err := json.Unmarshal(payload, &info); err != nil {
				t.Fatalf("decode info: %v", err)
			}
		} else if hdr.Name == "observability/metrics.prom" {
			data, err := io.ReadAll(tr)
			if err != nil {
				t.Fatalf("read metrics: %v", err)
			}
			metricsContent = string(data)
		} else if hdr.Name == "logs/journalctl/pingsanto-agent.service.log" {
			data, err := io.ReadAll(tr)
			if err != nil {
				t.Fatalf("read journal: %v", err)
			}
			journalContent = string(data)
		} else if hdr.Name == "logs/"+filepath.Base(logFile) {
			data, err := io.ReadAll(tr)
			if err != nil {
				t.Fatalf("read log: %v", err)
			}
			sanitizedLog = string(data)
		}
	}

	if !entries["diagnostics/info.json"] {
		t.Fatalf("missing diagnostics info")
	}
	if !entries["config/"+filepath.Base(configPath)] {
		t.Fatalf("missing config file entry")
	}
	if !entries["state/"+filepath.Base(config.StatePath(dataDir))] {
		t.Fatalf("missing state file entry")
	}
	if !entries["logs/"] || !entries["logs/"+filepath.Base(logFile)] {
		t.Fatalf("missing logs directory entries")
	}
	if !entries["spill/"] || !entries["spill/segment-000001.log"] {
		t.Fatalf("missing spill directory entries")
	}
	if !entries["observability/metrics.prom"] {
		t.Fatalf("missing metrics snapshot")
	}
	if !entries["logs/journalctl/pingsanto-agent.service.log"] {
		t.Fatalf("missing journalctl log")
	}
	if sanitizedLog == "" {
		t.Fatalf("missing sanitized log content")
	}

	if !strings.Contains(metricsContent, "pingsanto_agent_queue_depth_number 42") {
		t.Fatalf("metrics content missing queue depth")
	}
	if strings.TrimSpace(journalContent) != "journal log line" {
		t.Fatalf("unexpected journal content: %q", journalContent)
	}
	if strings.Contains(sanitizedLog, "mysecret") || strings.Contains(sanitizedLog, "secretvalue") {
		t.Fatalf("log content not redacted: %q", sanitizedLog)
	}
	if !strings.Contains(sanitizedLog, redactedMarker) {
		t.Fatalf("expected redaction marker in log content: %q", sanitizedLog)
	}

	if info.AgentID != state.AgentID {
		t.Fatalf("info AgentID = %s, want %s", info.AgentID, state.AgentID)
	}
	if info.Server != state.Server {
		t.Fatalf("info Server = %s, want %s", info.Server, state.Server)
	}
	if info.Spill == nil {
		t.Fatalf("expected spill summary")
	}
	if info.Spill.FileCount == 0 {
		t.Fatalf("expected spill file count > 0")
	}
	if info.Upgrade == nil || info.Upgrade.Channel != "stable" || info.Upgrade.Paused {
		t.Fatalf("unexpected upgrade summary: %+v", info.Upgrade)
	}
	if info.Metrics == nil {
		t.Fatalf("expected metrics summary")
	}
	if info.Metrics.URL != ts.URL {
		t.Fatalf("metrics URL = %s, want %s", info.Metrics.URL, ts.URL)
	}
	if info.Metrics.QueueDepth == nil || *info.Metrics.QueueDepth != 42 {
		t.Fatalf("unexpected queue depth: %v", info.Metrics.QueueDepth)
	}
	if info.Metrics.QueueDropped == nil || *info.Metrics.QueueDropped != 3 {
		t.Fatalf("unexpected queue dropped: %v", info.Metrics.QueueDropped)
	}
	if info.Metrics.QueueSpilled == nil || *info.Metrics.QueueSpilled != 7 {
		t.Fatalf("unexpected queue spilled: %v", info.Metrics.QueueSpilled)
	}
	if info.Journal == nil || len(info.Journal.Units) != 1 || info.Journal.Units[0] != "pingsanto-agent.service" {
		t.Fatalf("unexpected journal summary: %+v", info.Journal)
	}
	if info.Journal.Since == "" {
		t.Fatalf("expected journal since timestamp")
	}
	if !info.LogsRedacted {
		t.Fatalf("expected logs redacted flag true")
	}
	if len(info.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", info.Warnings)
	}
	if len(journalCalls) != 1 {
		t.Fatalf("expected one journalctl call, got %d", len(journalCalls))
	}
	args := journalCalls[0]
	if len(args) < 4 || args[0] != "--unit" || args[1] != "pingsanto-agent.service" || args[2] != "--since" {
		t.Fatalf("unexpected journalctl args: %v", args)
	}
}

func TestRunRequiresDataDir(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	configPath := filepath.Join(tmp, "missing.yaml")
	output := filepath.Join(tmp, "out.tar.gz")

	err := Run(ctx, []string{
		"--config", configPath,
		"--output", output,
	}, Dependencies{Now: time.Now})
	if err == nil {
		t.Fatalf("expected error due to missing data dir")
	}
}

func TestRedactSensitive(t *testing.T) {
	input := []byte("token=abc Authorization: Bearer def api_key=xyz secret=s123 password=p456 access_token=q789")
	redacted := redactSensitive("test.log", input)
	out := string(redacted)
	checks := []string{
		"token=" + redactedMarker,
		"Authorization: Bearer " + redactedMarker,
		"api_key=" + redactedMarker,
		"secret=" + redactedMarker,
		"password=" + redactedMarker,
		"access_token=" + redactedMarker,
	}
	for _, c := range checks {
		if !strings.Contains(out, c) {
			t.Fatalf("expected %q in output: %s", c, out)
		}
	}
	if strings.Contains(out, "abc") || strings.Contains(out, "def") || strings.Contains(out, "xyz") {
		t.Fatalf("redaction incomplete: %s", out)
	}
}
