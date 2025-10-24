package uplink

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pingsantohq/agent/internal/metrics"
	"github.com/pingsantohq/agent/internal/transmit"
	"github.com/pingsantohq/agent/pkg/types"
)

const (
	defaultResultsPath   = "/api/agent/v1/results"
	defaultHeartbeatPath = "/api/agent/v1/heartbeat"
	defaultMonitorPath   = "/api/agent/v1/monitors"
)

// Config holds the static configuration for an Uplink client.
type Config struct {
	ServerURL string
	AgentID   string
	Labels    map[string]string
}

// Dependencies allow test overrides for HTTP client, clock, and logging.
type Dependencies struct {
	HTTPClient    *http.Client
	Metrics       *metrics.Store
	Now           func() time.Time
	Logger        *log.Logger
	ResultsPath   string
	HeartbeatPath string
	MonitorPath   string
}

// Client provides result publishing and heartbeat signalling to the central service.
type Client struct {
	httpClient   *http.Client
	resultsURL   string
	heartbeatURL string
	monitorURL   string
	agentID      string
	labels       map[string]string
	metrics      *metrics.Store
	now          func() time.Time
	logger       *log.Logger
	seq          atomic.Uint64
}

// NewClient builds an Uplink client from configuration and dependencies.
func NewClient(cfg Config, deps Dependencies) (*Client, error) {
	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("server URL is required")
	}
	if cfg.AgentID == "" {
		return nil, fmt.Errorf("agent ID is required")
	}
	httpClient := deps.HTTPClient
	if httpClient == nil {
		return nil, fmt.Errorf("HTTP client is required")
	}
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	logger := deps.Logger
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	resultsPath := deps.ResultsPath
	if resultsPath == "" {
		resultsPath = defaultResultsPath
	}
	heartbeatPath := deps.HeartbeatPath
	if heartbeatPath == "" {
		heartbeatPath = defaultHeartbeatPath
	}
	monitorPath := deps.MonitorPath
	if monitorPath == "" {
		monitorPath = defaultMonitorPath
	}

	client := &Client{
		httpClient:   httpClient,
		resultsURL:   joinURL(cfg.ServerURL, resultsPath),
		heartbeatURL: joinURL(cfg.ServerURL, heartbeatPath),
		monitorURL:   joinURL(cfg.ServerURL, monitorPath),
		agentID:      cfg.AgentID,
		labels:       cloneLabels(cfg.Labels),
		metrics:      deps.Metrics,
		now:          now,
		logger:       logger,
	}
	return client, nil
}

// Send implements transmit.Sink, encoding results into a result envelope.
func (c *Client) Send(ctx context.Context, results []types.ProbeResult) error {
	if len(results) == 0 {
		return nil
	}

	envelope := types.ResultEnvelope{
		AgentID:  c.agentID,
		SentAt:   c.now().UTC(),
		BatchSeq: c.seq.Add(1),
		Labels:   cloneLabels(c.labels),
		Results:  cloneResults(results),
	}

	payload, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal result envelope: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.resultsURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build results request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "pingsanto-agent/0.0.1")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send results: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("results upload failed: status %s", resp.Status)
	}

	return nil
}

// RunHeartbeat emits heartbeat payloads on the configured interval until the context is cancelled.
func (c *Client) RunHeartbeat(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = 15 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	c.sendHeartbeat(ctx)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			c.sendHeartbeat(ctx)
		}
	}
}

func (c *Client) sendHeartbeat(ctx context.Context) {
	payload := c.heartbeatPayload()
	data, err := json.Marshal(payload)
	if err != nil {
		c.logger.Printf("heartbeat marshal failed: %v", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.heartbeatURL, bytes.NewReader(data))
	if err != nil {
		c.logger.Printf("heartbeat request build failed: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "pingsanto-agent/0.0.1")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Printf("heartbeat send failed: %v", err)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.logger.Printf("heartbeat failed: %s", resp.Status)
	}
}

func (c *Client) heartbeatPayload() heartbeatPayload {
	snap := metrics.Snapshot{}
	if c.metrics != nil {
		snap = c.metrics.Snapshot()
	}
	return heartbeatPayload{
		AgentID:              c.agentID,
		SentAt:               c.now().UTC(),
		QueueDepth:           snap.QueueDepth,
		QueueDroppedTotal:    snap.QueueDroppedTotal,
		QueueSpilledTotal:    snap.QueueSpilledTotal,
		BackfillPendingBytes: snap.BackfillPendingBytes,
	}
}

// MonitorSnapshotResult captures the outcome of a monitor snapshot fetch operation.
type MonitorSnapshotResult struct {
	Snapshot    types.MonitorSnapshot
	ETag        string
	NotModified bool
}

// FetchMonitors retrieves the current monitor assignment snapshot from the central service.
// The caller may pass the previously observed ETag to leverage conditional requests.
func (c *Client) FetchMonitors(ctx context.Context, etag string) (MonitorSnapshotResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.monitorURL, nil)
	if err != nil {
		return MonitorSnapshotResult{}, fmt.Errorf("build monitor request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return MonitorSnapshotResult{}, fmt.Errorf("fetch monitors: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return MonitorSnapshotResult{}, fmt.Errorf("read monitor response: %w", err)
	}

	if resp.StatusCode == http.StatusNotModified {
		return MonitorSnapshotResult{
			ETag:        etag,
			NotModified: true,
		}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return MonitorSnapshotResult{}, fmt.Errorf("monitor fetch failed: status %s", resp.Status)
	}

	var snapshot types.MonitorSnapshot
	if err := json.Unmarshal(body, &snapshot); err != nil {
		return MonitorSnapshotResult{}, fmt.Errorf("decode monitor snapshot: %w", err)
	}

	return MonitorSnapshotResult{
		Snapshot:    snapshot,
		ETag:        resp.Header.Get("ETag"),
		NotModified: false,
	}, nil
}

type heartbeatPayload struct {
	AgentID              string    `json:"agent_id"`
	SentAt               time.Time `json:"sent_at"`
	QueueDepth           int64     `json:"queue_depth"`
	QueueDroppedTotal    uint64    `json:"queue_dropped_total"`
	QueueSpilledTotal    uint64    `json:"queue_spilled_total"`
	BackfillPendingBytes int64     `json:"backfill_pending_bytes"`
}

func cloneResults(in []types.ProbeResult) []types.ProbeResult {
	out := make([]types.ProbeResult, len(in))
	copy(out, in)
	return out
}

func cloneLabels(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func joinURL(base, path string) string {
	if base == "" {
		return path
	}
	base = strings.TrimRight(base, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

var _ transmit.Sink = (*Client)(nil)
