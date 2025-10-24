package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	baseURL := flag.String("base-url", os.Getenv("CONTROLLER_BASE_URL"), "Controller base URL")
	token := flag.String("token", os.Getenv("CONTROLLER_ADMIN_TOKEN"), "Admin bearer token")
	agentID := flag.String("agent", "", "Agent ID (optional; empty means default plan)")
	channel := flag.String("channel", "stable", "Upgrade channel")
	version := flag.String("version", "", "Artifact version (required)")
	artifactURL := flag.String("artifact-url", "", "Artifact download URL (required unless --upload-artifact used)")
	checksum := flag.String("sha256", "", "Artifact SHA-256 checksum (required unless upload sets it)")
	signatureURL := flag.String("signature-url", "", "Signature URL (optional)")
	notes := flag.String("notes", "", "Notes for plan")
	force := flag.Bool("force", false, "Force apply even if agent paused")
	paused := flag.Bool("paused", false, "Pause auto-upgrades at controller")
	scheduleEarliest := flag.String("schedule-earliest", "", "Rollout window start (RFC3339 UTC)")
	scheduleLatest := flag.String("schedule-latest", "", "Rollout window end (RFC3339 UTC)")
	historyAgent := flag.String("history", "", "Show upgrade history for the specified agent and exit")
	historyLimit := flag.Int("history-limit", 20, "Number of history entries to fetch with --history")
	uploadArtifact := flag.String("upload-artifact", "", "Path to artifact file to upload before plan update")
	uploadSignature := flag.String("upload-signature", "", "Optional path to signature file when uploading artifact")
	flag.Parse()

	if *baseURL == "" || *token == "" {
		fmt.Fprintln(os.Stderr, "base-url and token are required (set flags or CONTROLLER_BASE_URL/CONTROLLER_ADMIN_TOKEN)")
		os.Exit(1)
	}

	if *historyAgent != "" {
		if err := showHistory(*baseURL, *token, *historyAgent, *historyLimit); err != nil {
			fmt.Fprintf(os.Stderr, "history fetch failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if *uploadArtifact != "" {
		if strings.TrimSpace(*version) == "" {
			fmt.Fprintln(os.Stderr, "version is required when uploading an artifact")
			os.Exit(1)
		}
		meta, err := uploadArtifactFile(*baseURL, *token, *uploadArtifact, *uploadSignature, *version)
		if err != nil {
			fmt.Fprintf(os.Stderr, "artifact upload failed: %v\n", err)
			os.Exit(1)
		}
		if *artifactURL == "" {
			*artifactURL = meta.DownloadURL
		}
		if *checksum == "" {
			*checksum = meta.SHA256
		}
		if *signatureURL == "" {
			*signatureURL = meta.SignatureURL
		}
	}

	if *version == "" || *artifactURL == "" || *checksum == "" {
		fmt.Fprintln(os.Stderr, "version, artifact-url, and sha256 are required")
		os.Exit(1)
	}

	payload := map[string]any{
		"agent_id": *agentID,
		"channel":  *channel,
		"artifact": map[string]any{
			"version":       *version,
			"url":           *artifactURL,
			"sha256":        *checksum,
			"signature_url": *signatureURL,
			"force_apply":   *force,
		},
		"schedule": map[string]any{},
		"paused":   *paused,
		"notes":    *notes,
	}

	if *scheduleEarliest != "" {
		payload["schedule"].(map[string]any)["earliest"] = *scheduleEarliest
	}
	if *scheduleLatest != "" {
		payload["schedule"].(map[string]any)["latest"] = *scheduleLatest
	}

	body, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal payload: %v\n", err)
		os.Exit(1)
	}

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/admin/v1/upgrade/plan", *baseURL), bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create request: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+*token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "request failed: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		fmt.Fprintf(os.Stderr, "controller responded with %s\n", resp.Status)
		os.Exit(1)
	}

	fmt.Println("upgrade plan updated successfully")
}

func showHistory(baseURL, token, agentID string, limit int) error {
	url := fmt.Sprintf("%s/api/admin/v1/upgrade/history/%s?limit=%d", baseURL, agentID, limit)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("controller responded with %s", resp.Status)
	}

	var payload struct {
		AgentID string `json:"agent_id"`
		Items   []struct {
			Channel         string    `json:"channel"`
			CurrentVersion  string    `json:"current_version"`
			PreviousVersion string    `json:"previous_version"`
			Status          string    `json:"status"`
			Message         string    `json:"message"`
			StartedAt       time.Time `json:"started_at"`
			CompletedAt     time.Time `json:"completed_at"`
		} `json:"items"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return err
	}

	fmt.Printf("History for agent %s\n", payload.AgentID)
	for _, item := range payload.Items {
		fmt.Printf("[%s] %s -> %s (%s) %s\n", item.CompletedAt.UTC().Format(time.RFC3339), item.PreviousVersion, item.CurrentVersion, item.Status, item.Message)
	}
	return nil
}

type uploadResponse struct {
	DownloadURL  string
	SignatureURL string
	SHA256       string
}

func uploadArtifactFile(baseURL, token, artifactPath, signaturePath, version string) (uploadResponse, error) {
	var result uploadResponse
	if strings.TrimSpace(version) == "" {
		return result, fmt.Errorf("version is required")
	}
	file, err := os.Open(artifactPath)
	if err != nil {
		return result, err
	}
	defer file.Close()

	var sig io.ReadCloser
	if signaturePath != "" {
		sig, err = os.Open(signaturePath)
		if err != nil {
			return result, err
		}
		defer sig.Close()
	}

	buf := &bytes.Buffer{}
	writer := multipart.NewWriter(buf)

	writer.WriteField("version", version)

	part, err := writer.CreateFormFile("file", filepath.Base(artifactPath))
	if err != nil {
		return result, err
	}
	if _, err := io.Copy(part, file); err != nil {
		return result, err
	}

	if sig != nil {
		sigPart, err := writer.CreateFormFile("signature", filepath.Base(signaturePath))
		if err != nil {
			return result, err
		}
		if _, err := io.Copy(sigPart, sig); err != nil {
			return result, err
		}
	}

	if err := writer.Close(); err != nil {
		return result, err
	}

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/admin/v1/artifacts", baseURL), buf)
	if err != nil {
		return result, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return result, fmt.Errorf("artifact upload failed: %s", resp.Status)
	}

	var payload struct {
		Artifact struct {
			DownloadURL  string `json:"download_url"`
			SignatureURL string `json:"signature_url"`
			SHA256       string `json:"sha256"`
		} `json:"artifact"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return result, err
	}

	result.DownloadURL = payload.Artifact.DownloadURL
	result.SignatureURL = payload.Artifact.SignatureURL
	result.SHA256 = payload.Artifact.SHA256
	return result, nil
}
