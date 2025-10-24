package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	baseURL := flag.String("base-url", os.Getenv("CONTROLLER_BASE_URL"), "Controller base URL")
	token := flag.String("token", os.Getenv("CONTROLLER_ADMIN_TOKEN"), "Admin bearer token")
	agentID := flag.String("agent", "", "Agent ID (optional; empty means default plan)")
	channel := flag.String("channel", "stable", "Upgrade channel")
	version := flag.String("version", "", "Artifact version (required)")
	artifactURL := flag.String("artifact-url", "", "Artifact download URL (required)")
	checksum := flag.String("sha256", "", "Artifact SHA-256 checksum (required)")
	signatureURL := flag.String("signature-url", "", "Signature URL (required)")
	notes := flag.String("notes", "", "Notes for plan")
	force := flag.Bool("force", false, "Force apply even if agent paused")
	paused := flag.Bool("paused", false, "Pause auto-upgrades at controller")
	scheduleEarliest := flag.String("schedule-earliest", "", "Rollout window start (RFC3339 UTC)")
	scheduleLatest := flag.String("schedule-latest", "", "Rollout window end (RFC3339 UTC)")
	flag.Parse()

	if *baseURL == "" || *token == "" {
		fmt.Fprintln(os.Stderr, "base-url and token are required (set flags or CONTROLLER_BASE_URL/CONTROLLER_ADMIN_TOKEN)")
		os.Exit(1)
	}
	if *version == "" || *artifactURL == "" || *checksum == "" || *signatureURL == "" {
		fmt.Fprintln(os.Stderr, "version, artifact-url, sha256, and signature-url are required")
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
