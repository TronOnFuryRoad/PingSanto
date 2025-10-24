package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pingsantohq/controller/internal/artifacts"
	"github.com/pingsantohq/controller/internal/store"
)

func TestAdminUploadArtifactAndDownload(t *testing.T) {
	cfg := Config{ArtifactPath: "/artifacts", AdminBearerToken: "token"}
	deps := Dependencies{
		Logger:        log.New(io.Discard, "", 0),
		Store:         store.NewMemoryStore(),
		ArtifactStore: artifacts.NewMemoryStore(),
	}

	srv := New(cfg, deps)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("version", "1.0.0")
	filePart, err := writer.CreateFormFile("file", "agent.tar.gz")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	filePart.Write([]byte("artifact"))
	if err := writer.Close(); err != nil {
		t.Fatalf("writer close: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/artifacts", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer token")
	rr := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rr, req)
	resp := rr.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload status %d", resp.StatusCode)
	}
	defer resp.Body.Close()

	var payload struct {
		Artifact struct {
			DownloadURL  string `json:"download_url"`
			SignatureURL string `json:"signature_url"`
			SHA256       string `json:"sha256"`
		} `json:"artifact"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Artifact.DownloadURL == "" || payload.Artifact.SHA256 == "" {
		t.Fatalf("unexpected response: %+v", payload)
	}

	downloadReq := httptest.NewRequest(http.MethodGet, payload.Artifact.DownloadURL, nil)
	// simulate host for download
	downloadReq.URL.Scheme = "http"
	downloadReq.URL.Host = "example.com"
	rr2 := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rr2, downloadReq)
	if rr2.Code != http.StatusOK {
		t.Fatalf("download status %d", rr2.Code)
	}
	if rr2.Body.String() != "artifact" {
		t.Fatalf("unexpected download body: %s", rr2.Body.String())
	}
}

func TestAdminUploadArtifactRequiresVersion(t *testing.T) {
	cfg := Config{AdminBearerToken: "token"}
	deps := Dependencies{
		Logger:        log.New(io.Discard, "", 0),
		Store:         store.NewMemoryStore(),
		ArtifactStore: artifacts.NewMemoryStore(),
	}
	srv := New(cfg, deps)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	filePart, err := writer.CreateFormFile("file", "agent.tar.gz")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	filePart.Write([]byte("artifact"))
	if err := writer.Close(); err != nil {
		t.Fatalf("writer close: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/artifacts", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer token")
	rr := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rr, req)
	resp := rr.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestAgentPlanIncludesUploadedArtifact(t *testing.T) {
	tmp := t.TempDir()
	artifactStore, err := artifacts.NewFileStore(tmp)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	cfg := Config{
		ArtifactPath:     "/artifacts",
		AdminBearerToken: "token",
	}
	deps := Dependencies{
		Logger:        log.New(io.Discard, "", 0),
		Store:         store.NewMemoryStore(),
		ArtifactStore: artifactStore,
	}
	srv := New(cfg, deps)
	ts := httptest.NewServer(srv.Handler)
	defer ts.Close()

	uploadBody := &bytes.Buffer{}
	writer := multipart.NewWriter(uploadBody)
	writer.WriteField("version", "2.0.0")
	filePart, err := writer.CreateFormFile("file", "agent.tar.gz")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	filePart.Write([]byte("artifact-bytes"))
	if err := writer.Close(); err != nil {
		t.Fatalf("writer close: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/admin/v1/artifacts", uploadBody)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("artifact upload request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload status %d", resp.StatusCode)
	}

	var artifactPayload struct {
		Artifact struct {
			DownloadURL  string `json:"download_url"`
			SignatureURL string `json:"signature_url"`
			SHA256       string `json:"sha256"`
		} `json:"artifact"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&artifactPayload); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if artifactPayload.Artifact.DownloadURL == "" {
		t.Fatalf("download url missing")
	}

	plan := map[string]any{
		"agent_id": "agent-123",
		"channel":  "stable",
		"artifact": map[string]any{
			"version":       "2.0.0",
			"url":           artifactPayload.Artifact.DownloadURL,
			"sha256":        artifactPayload.Artifact.SHA256,
			"signature_url": artifactPayload.Artifact.SignatureURL,
			"force_apply":   false,
		},
		"schedule": map[string]any{},
		"paused":   false,
		"notes":    "integration test",
	}
	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(plan); err != nil {
		t.Fatalf("encode plan: %v", err)
	}

	planReq, err := http.NewRequest(http.MethodPost, ts.URL+"/api/admin/v1/upgrade/plan", buf)
	if err != nil {
		t.Fatalf("plan request: %v", err)
	}
	planReq.Header.Set("Authorization", "Bearer token")
	planReq.Header.Set("Content-Type", "application/json")
	planResp, err := http.DefaultClient.Do(planReq)
	if err != nil {
		t.Fatalf("plan response: %v", err)
	}
	planResp.Body.Close()
	if planResp.StatusCode != http.StatusOK {
		t.Fatalf("plan status %d", planResp.StatusCode)
	}

	agentReq, err := http.NewRequest(http.MethodGet, ts.URL+"/api/agent/v1/upgrade/plan", nil)
	if err != nil {
		t.Fatalf("agent request: %v", err)
	}
	agentReq.Header.Set("X-Agent-ID", "agent-123")
	agentResp, err := http.DefaultClient.Do(agentReq)
	if err != nil {
		t.Fatalf("agent response: %v", err)
	}
	defer agentResp.Body.Close()
	if agentResp.StatusCode != http.StatusOK {
		t.Fatalf("agent status %d", agentResp.StatusCode)
	}
	if agentResp.Header.Get("ETag") == "" {
		t.Fatalf("expected etag header")
	}

	var planPayload struct {
		AgentID  string `json:"agent_id"`
		Channel  string `json:"channel"`
		Artifact struct {
			Version      string `json:"version"`
			URL          string `json:"url"`
			SHA256       string `json:"sha256"`
			SignatureURL string `json:"signature_url"`
		} `json:"artifact"`
	}
	if err := json.NewDecoder(agentResp.Body).Decode(&planPayload); err != nil {
		t.Fatalf("decode plan: %v", err)
	}
	if planPayload.AgentID != "agent-123" {
		t.Fatalf("unexpected agent id: %s", planPayload.AgentID)
	}
	if planPayload.Artifact.URL != artifactPayload.Artifact.DownloadURL {
		t.Fatalf("unexpected artifact url: %s", planPayload.Artifact.URL)
	}
	if planPayload.Artifact.SHA256 != artifactPayload.Artifact.SHA256 {
		t.Fatalf("unexpected artifact sha: %s", planPayload.Artifact.SHA256)
	}
	if planPayload.Artifact.Version != "2.0.0" {
		t.Fatalf("unexpected version: %s", planPayload.Artifact.Version)
	}
}
