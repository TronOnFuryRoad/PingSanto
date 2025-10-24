package upgrade

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pingsantohq/agent/internal/config"
)

const (
	defaultUpgradePlanPath   = "/api/agent/v1/upgrade/plan"
	defaultUpgradeReportPath = "/api/agent/v1/upgrade/report"
	userAgent                = "pingsanto-agent/0.0.1"
)

// ErrPlanNotFound indicates that no upgrade plan exists for the agent/channel.
var ErrPlanNotFound = errors.New("upgrade plan not found")

// PlanResult captures the outcome of a plan fetch call.
type PlanResult struct {
	Plan        Plan
	ETag        string
	NotModified bool
}

// Plan represents the controller upgrade plan payload.
type Plan struct {
	AgentID     string
	GeneratedAt time.Time
	Channel     string
	Artifact    PlanArtifact
	Schedule    PlanSchedule
	Paused      bool
	Notes       string
}

// PlanArtifact describes the artifact fields delivered by the controller.
type PlanArtifact struct {
	Version      string
	URL          string
	SHA256       string
	SignatureURL string
	ForceApply   bool
}

// PlanSchedule mirrors the JSON response schedule block.
type PlanSchedule struct {
	Earliest *time.Time
	Latest   *time.Time
}

// Report captures upgrade status reports sent back to the controller.
type Report struct {
	AgentID         string
	CurrentVersion  string
	PreviousVersion string
	Channel         string
	Status          string
	StartedAt       time.Time
	CompletedAt     time.Time
	Message         string
	Details         map[string]any
}

// Client performs controller upgrade plan/report requests.
type Client struct {
	baseURL    string
	httpClient *http.Client
	agentID    string
	logger     *log.Logger
}

// NewClient constructs an upgrade client with the provided HTTP transport.
func NewClient(httpClient *http.Client, baseURL, agentID string, logger *log.Logger) (*Client, error) {
	if httpClient == nil {
		return nil, errors.New("http client is required")
	}
	if strings.TrimSpace(baseURL) == "" {
		return nil, errors.New("base URL is required")
	}
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
		agentID:    agentID,
		logger:     logger,
	}, nil
}

// FetchPlan retrieves the current upgrade plan for the agent/channel with conditional requests.
func (c *Client) FetchPlan(ctx context.Context, channel, etag string) (PlanResult, error) {
	channel = strings.TrimSpace(channel)
	if channel == "" {
		channel = "stable"
	}

	reqURL, err := c.buildURL(defaultUpgradePlanPath, url.Values{"channel": []string{channel}})
	if err != nil {
		return PlanResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return PlanResult{}, fmt.Errorf("build upgrade plan request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if c.agentID != "" {
		req.Header.Set("X-Agent-ID", c.agentID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return PlanResult{}, fmt.Errorf("fetch upgrade plan: %w", err)
	}
	defer resp.Body.Close()
	defer io.Copy(io.Discard, resp.Body) // ensure body fully read

	switch resp.StatusCode {
	case http.StatusNotModified:
		return PlanResult{ETag: etag, NotModified: true}, nil
	case http.StatusOK:
		var envelope planEnvelope
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
			return PlanResult{}, fmt.Errorf("decode upgrade plan: %w", err)
		}
		result := PlanResult{
			Plan: Plan{
				AgentID:     envelope.AgentID,
				GeneratedAt: envelope.GeneratedAt,
				Channel:     envelope.Channel,
				Artifact: PlanArtifact{
					Version:      envelope.Artifact.Version,
					URL:          envelope.Artifact.URL,
					SHA256:       envelope.Artifact.SHA256,
					SignatureURL: envelope.Artifact.SignatureURL,
					ForceApply:   envelope.Artifact.ForceApply,
				},
				Schedule: PlanSchedule{
					Earliest: envelope.Schedule.Earliest,
					Latest:   envelope.Schedule.Latest,
				},
				Paused: envelope.Paused,
				Notes:  envelope.Notes,
			},
			ETag: resp.Header.Get("ETag"),
		}
		return result, nil
	case http.StatusNotFound:
		return PlanResult{}, ErrPlanNotFound
	case http.StatusForbidden, http.StatusUnauthorized:
		return PlanResult{}, fmt.Errorf("upgrade plan unauthorized: %s", resp.Status)
	default:
		return PlanResult{}, fmt.Errorf("upgrade plan fetch failed: %s", resp.Status)
	}
}

// ReportUpgrade posts upgrade progress back to the controller.
func (c *Client) ReportUpgrade(ctx context.Context, report Report) error {
	reqURL, err := c.buildURL(defaultUpgradeReportPath, nil)
	if err != nil {
		return err
	}

	payload := reportPayload{
		AgentID:         c.agentID,
		CurrentVersion:  report.CurrentVersion,
		PreviousVersion: report.PreviousVersion,
		Channel:         report.Channel,
		Status:          report.Status,
		StartedAt:       report.StartedAt,
		CompletedAt:     report.CompletedAt,
		Message:         report.Message,
		Details:         report.Details,
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(&payload); err != nil {
		return fmt.Errorf("encode upgrade report: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, &buf)
	if err != nil {
		return fmt.Errorf("build upgrade report request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if c.agentID != "" {
		req.Header.Set("X-Agent-ID", c.agentID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send upgrade report: %w", err)
	}
	defer resp.Body.Close()
	defer io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("upgrade report failed: %s", resp.Status)
	}
	return nil
}

// ToState converts a plan into persisted upgrade state metadata.
func (p Plan) ToState(now time.Time, etag string) config.UpgradePlanState {
	state := config.UpgradePlanState{
		Version:      p.Artifact.Version,
		Channel:      p.Channel,
		Source:       p.AgentID,
		Paused:       p.Paused,
		ArtifactURL:  p.Artifact.URL,
		SignatureURL: p.Artifact.SignatureURL,
		SHA256:       p.Artifact.SHA256,
		ForceApply:   p.Artifact.ForceApply,
		Notes:        p.Notes,
		Schedule: config.UpgradePlanSchedule{
			Earliest: p.Schedule.Earliest,
			Latest:   p.Schedule.Latest,
		},
		RetrievedAt: now.UTC(),
		ETag:        etag,
	}
	return state
}

func (c *Client) buildURL(path string, query url.Values) (string, error) {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("parse base URL: %w", err)
	}
	endpoint, err := url.Parse(path)
	if err != nil {
		return "", fmt.Errorf("parse endpoint: %w", err)
	}
	full := base.ResolveReference(endpoint)
	if len(query) > 0 {
		full.RawQuery = query.Encode()
	}
	return full.String(), nil
}

type planEnvelope struct {
	AgentID     string       `json:"agent_id"`
	GeneratedAt time.Time    `json:"generated_at"`
	Channel     string       `json:"channel"`
	Artifact    planArtifact `json:"artifact"`
	Schedule    planSchedule `json:"schedule"`
	Paused      bool         `json:"paused"`
	Notes       string       `json:"notes"`
}

type planArtifact struct {
	Version      string `json:"version"`
	URL          string `json:"url"`
	SHA256       string `json:"sha256"`
	SignatureURL string `json:"signature_url"`
	ForceApply   bool   `json:"force_apply"`
}

type planSchedule struct {
	Earliest *time.Time `json:"earliest"`
	Latest   *time.Time `json:"latest"`
}

type reportPayload struct {
	AgentID         string         `json:"agent_id"`
	CurrentVersion  string         `json:"current_version"`
	PreviousVersion string         `json:"previous_version"`
	Channel         string         `json:"channel"`
	Status          string         `json:"status"`
	StartedAt       time.Time      `json:"started_at"`
	CompletedAt     time.Time      `json:"completed_at"`
	Message         string         `json:"message"`
	Details         map[string]any `json:"details,omitempty"`
}
