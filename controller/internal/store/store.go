package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// UpgradePlanResponse mirrors the API payload described in docs/agent_upgrade_api.md.
type UpgradePlanResponse struct {
	AgentID     string    `json:"agent_id"`
	GeneratedAt time.Time `json:"generated_at"`
	Channel     string    `json:"channel"`
	Artifact    Artifact  `json:"artifact"`
	Schedule    Schedule  `json:"schedule"`
	Paused      bool      `json:"paused"`
	Notes       string    `json:"notes,omitempty"`
}

type PlanInput struct {
	AgentID          string
	Channel          string
	Version          string
	ArtifactURL      string
	ArtifactSHA256   string
	SignatureURL     string
	ForceApply       bool
	ScheduleEarliest *time.Time
	ScheduleLatest   *time.Time
	Paused           bool
	Notes            string
}

type Artifact struct {
	Version      string `json:"version"`
	URL          string `json:"url"`
	SHA256       string `json:"sha256"`
	SignatureURL string `json:"signature_url"`
	ForceApply   bool   `json:"force_apply"`
}

type Schedule struct {
	Earliest *time.Time `json:"earliest,omitempty"`
	Latest   *time.Time `json:"latest,omitempty"`
}

// UpgradeReport is the shape persisted by the controller after agent submission.
type UpgradeReport struct {
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

// NotificationSettings describe controller behaviour for CI notifications.
type NotificationSettings struct {
	NotifyOnPublish bool      `json:"notify_on_publish"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// ErrPlanNotFound signals the absence of an upgrade plan for the requested agent.
var ErrPlanNotFound = errors.New("upgrade plan not found")

// Store exposes persistence operations required by the upgrade API.
type Store interface {
	FetchUpgradePlan(ctx context.Context, agentID string) (UpgradePlanResponse, string, error)
	RecordUpgradeReport(ctx context.Context, report UpgradeReport) error
	UpsertUpgradePlan(ctx context.Context, input PlanInput) (UpgradePlanResponse, string, error)
	ListUpgradeHistory(ctx context.Context, agentID string, limit int) ([]UpgradeReport, error)
	GetNotificationSettings(ctx context.Context) (NotificationSettings, error)
	UpdateNotificationSettings(ctx context.Context, notify bool) (NotificationSettings, error)
}

// NewMemoryStore returns an in-memory implementation useful for scaffolding/testing.
func NewMemoryStore() Store {
	return &memoryStore{
		plans:   map[string]UpgradePlanResponse{},
		reports: []UpgradeReport{},
		notifyOnPublish: true,
		notifyUpdatedAt: time.Now().UTC(),
	}
}

type memoryStore struct {
	mu      sync.RWMutex
	plans   map[string]UpgradePlanResponse
	reports []UpgradeReport
	notifyOnPublish bool
	notifyUpdatedAt time.Time
}

func (m *memoryStore) FetchUpgradePlan(ctx context.Context, agentID string) (UpgradePlanResponse, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	plan, ok := m.plans[agentID]
	if !ok {
		// Fallback to default plan for scaffolding
		plan = UpgradePlanResponse{
			AgentID:     agentID,
			GeneratedAt: time.Now().UTC(),
			Channel:     "stable",
			Artifact: Artifact{
				Version:      "1.0.0",
				URL:          "https://artifacts.example.com/pingsanto/agent/1.0.0/pingsanto-agent.tgz",
				SHA256:       "deadbeef",
				SignatureURL: "https://artifacts.example.com/pingsanto/agent/1.0.0/pingsanto-agent.sig",
			},
			Paused: false,
			Notes:  "scaffolding plan",
		}
	}

	etag := computeETag(plan)
	return plan, etag, nil
}

func (m *memoryStore) RecordUpgradeReport(ctx context.Context, report UpgradeReport) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reports = append(m.reports, report)
	return nil
}

func (m *memoryStore) UpsertUpgradePlan(ctx context.Context, input PlanInput) (UpgradePlanResponse, string, error) {
	if strings.TrimSpace(input.AgentID) == "" {
		return UpgradePlanResponse{}, "", errors.New("agent_id required")
	}
	if strings.TrimSpace(input.Version) == "" {
		return UpgradePlanResponse{}, "", errors.New("version required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	plan := UpgradePlanResponse{
		AgentID:     input.AgentID,
		GeneratedAt: time.Now().UTC(),
		Channel:     defaultString(input.Channel, "stable"),
		Artifact: Artifact{
			Version:      input.Version,
			URL:          input.ArtifactURL,
			SHA256:       input.ArtifactSHA256,
			SignatureURL: input.SignatureURL,
			ForceApply:   input.ForceApply,
		},
		Schedule: Schedule{
			Earliest: input.ScheduleEarliest,
			Latest:   input.ScheduleLatest,
		},
		Paused: input.Paused,
		Notes:  input.Notes,
	}
	m.plans[input.AgentID] = plan
	etag := computeETag(plan)
	return plan, etag, nil
}

func (m *memoryStore) ListUpgradeHistory(ctx context.Context, agentID string, limit int) ([]UpgradeReport, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var results []UpgradeReport
	for _, r := range m.reports {
		if r.AgentID == agentID {
			results = append(results, r)
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].CompletedAt.After(results[j].CompletedAt)
	})
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (m *memoryStore) GetNotificationSettings(ctx context.Context) (NotificationSettings, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return NotificationSettings{
		NotifyOnPublish: m.notifyOnPublish,
		UpdatedAt:       m.notifyUpdatedAt,
	}, nil
}

func (m *memoryStore) UpdateNotificationSettings(ctx context.Context, notify bool) (NotificationSettings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notifyOnPublish = notify
	m.notifyUpdatedAt = time.Now().UTC()
	return NotificationSettings{
		NotifyOnPublish: m.notifyOnPublish,
		UpdatedAt:       m.notifyUpdatedAt,
	}, nil
}

func computeETag(plan UpgradePlanResponse) string {
	payload, _ := json.Marshal(plan)
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("\"%s\"", hex.EncodeToString(sum[:]))
}

func defaultString(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
