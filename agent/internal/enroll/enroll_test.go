package enroll

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pingsantohq/agent/internal/certs"
	"github.com/pingsantohq/agent/internal/config"
)

type stubIssuer struct {
	request certs.Request
	resp    *certs.Response
	err     error
}

func (s *stubIssuer) Enroll(ctx context.Context, req certs.Request) (*certs.Response, error) {
	s.request = req
	return s.resp, s.err
}

func TestParseLabels(t *testing.T) {
	labels, err := parseLabels("site=ATL-1, isp=Comcast")
	if err != nil {
		t.Fatalf("parseLabels returned error: %v", err)
	}
	if labels["site"] != "ATL-1" {
		t.Fatalf("expected site label ATL-1 got %q", labels["site"])
	}
	if labels["isp"] != "Comcast" {
		t.Fatalf("expected isp label Comcast got %q", labels["isp"])
	}
}

func TestParseLabelsInvalid(t *testing.T) {
	if _, err := parseLabels("badlabel"); err == nil {
		t.Fatalf("expected error for invalid label")
	}
}

func TestRunCreatesStateFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	stub := &stubIssuer{
		resp: &certs.Response{
			AgentID:    "agt_test",
			CertPEM:    []byte("CERT"),
			KeyPEM:     []byte("KEY"),
			CAPEM:      []byte("CA"),
			ConfigYAML: []byte("agent:\n  server: https://central.example.com\n"),
		},
	}

	args := []string{
		"--server", "https://central.example.com",
		"--token", "ABC123",
		"--labels", "site=ATL-1,env=prod",
		"--data-dir", dir,
		"--config-path", filepath.Join(dir, "agent.yaml"),
	}

	now := func() time.Time {
		return time.Unix(1730000000, 0).UTC()
	}

	deps := Dependencies{
		Issuer: stub,
		Now:    now,
		Verify: func(ctx context.Context, server string, resp *certs.Response) error { return nil },
	}

	if err := Run(ctx, args, deps); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	state, err := config.LoadState(ctx, dir)
	if err != nil {
		t.Fatalf("LoadState returned error: %v", err)
	}

	if state.AgentID != "agt_test" {
		t.Fatalf("expected agent_id agt_test got %s", state.AgentID)
	}
	if state.Labels["env"] != "prod" {
		t.Fatalf("expected env label prod got %q", state.Labels["env"])
	}
	if state.EnrolledAt != now() {
		t.Fatalf("expected enrolled_at %v got %v", now(), state.EnrolledAt)
	}
	if state.Credentials.TokenHash == "" {
		t.Fatalf("expected token hash to be populated")
	}
	if state.Credentials.TokenHash != func() string {
		sum := sha256.Sum256([]byte("ABC123"))
		return hex.EncodeToString(sum[:])
	}() {
		t.Fatalf("unexpected token hash %s", state.Credentials.TokenHash)
	}

	configData, err := os.ReadFile(filepath.Join(dir, "agent.yaml"))
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	if string(configData) != "agent:\n  server: https://central.example.com\n" {
		t.Fatalf("unexpected config content %q", string(configData))
	}

	certData, err := os.ReadFile(filepath.Join(dir, "client.crt"))
	if err != nil {
		t.Fatalf("expected cert file: %v", err)
	}
	if string(certData) != "CERT" {
		t.Fatalf("unexpected cert data %q", string(certData))
	}

	content, err := os.ReadFile(config.StatePath(dir))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	if len(content) == 0 {
		t.Fatalf("state file is empty")
	}

	if stub.request.Server != "https://central.example.com" {
		t.Fatalf("issuer request server mismatch")
	}
	if stub.request.Labels["site"] != "ATL-1" {
		t.Fatalf("issuer request label mismatch")
	}
	if stub.request.Token != "ABC123" {
		t.Fatalf("issuer request token mismatch")
	}
}
