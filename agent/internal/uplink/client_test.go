package uplink

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/pingsantohq/agent/internal/metrics"
	"github.com/pingsantohq/agent/pkg/types"
)

func TestClientSendPostsEnvelope(t *testing.T) {
	var mu sync.Mutex
	var requests []types.ResultEnvelope

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != defaultResultsPath {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", r.Method)
		}
		var env types.ResultEnvelope
		if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		mu.Lock()
		requests = append(requests, env)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	now := func() time.Time { return time.Unix(123, 0) }

	client, err := NewClient(
		Config{
			ServerURL: server.URL,
			AgentID:   "agt_test",
			Labels:    map[string]string{"site": "DAL"},
		},
		Dependencies{
			HTTPClient: server.Client(),
			Now:        now,
		},
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	results := []types.ProbeResult{{MonitorID: "mon-1"}, {MonitorID: "mon-2"}}
	if err := client.Send(context.Background(), results); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := client.Send(context.Background(), results[:1]); err != nil {
		t.Fatalf("Send second: %v", err)
	}

	mu.Lock()
	if len(requests) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(requests))
	}
	first := requests[0]
	second := requests[1]
	mu.Unlock()

	if first.AgentID != "agt_test" || first.BatchSeq != 1 {
		t.Fatalf("unexpected first envelope: %+v", first)
	}
	if second.BatchSeq != 2 {
		t.Fatalf("expected sequential batch seq, got %+v", second)
	}
	if first.Labels["site"] != "DAL" {
		t.Fatalf("expected label preserved")
	}
}

func TestClientSendHandlesFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	client, err := NewClient(
		Config{
			ServerURL: server.URL,
			AgentID:   "agt_test",
		},
		Dependencies{
			HTTPClient: server.Client(),
		},
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	err = client.Send(context.Background(), []types.ProbeResult{{MonitorID: "mon"}})
	if err == nil {
		t.Fatalf("expected error on failure status")
	}
}

func TestHeartbeatIncludesMetrics(t *testing.T) {
	store := metrics.NewStore()
	store.QueueRecorder().ObserveQueueDepth(7)
	store.QueueRecorder().IncQueueDrops()
	store.BackfillRecorder().ObservePendingBytes(1024)

	hbCh := make(chan heartbeatPayload, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == defaultHeartbeatPath {
			var payload heartbeatPayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode heartbeat: %v", err)
			}
			select {
			case hbCh <- payload:
			default:
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client, err := NewClient(
		Config{
			ServerURL: server.URL,
			AgentID:   "agt_test",
		},
		Dependencies{
			HTTPClient: server.Client(),
			Metrics:    store,
			Now:        func() time.Time { return time.Unix(123, 0) },
		},
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.RunHeartbeat(ctx, 10*time.Millisecond)
	}()

	select {
	case hb := <-hbCh:
		if hb.AgentID != "agt_test" {
			t.Fatalf("unexpected agent id %s", hb.AgentID)
		}
		if hb.QueueDepth != 7 || hb.QueueDroppedTotal != 1 || hb.BackfillPendingBytes != 1024 {
			t.Fatalf("unexpected heartbeat payload: %+v", hb)
		}
		cancel()
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for heartbeat")
	}

	if err := <-errCh; err != context.Canceled {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestFetchMonitorsReturnsSnapshot(t *testing.T) {
	snapshot := types.MonitorSnapshot{
		Revision:    "rev-1",
		GeneratedAt: time.Unix(123, 0).UTC(),
		Monitors: []types.MonitorAssignment{
			{MonitorID: "mon-test", Protocol: "icmp"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != defaultMonitorPath {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method %s", r.Method)
		}
		if match := r.Header.Get("If-None-Match"); match != "" {
			t.Fatalf("expected empty if-none-match header, got %s", match)
		}
		w.Header().Set("ETag", "rev-1")
		if err := json.NewEncoder(w).Encode(snapshot); err != nil {
			t.Fatalf("encode snapshot: %v", err)
		}
	}))
	defer server.Close()

	client, err := NewClient(
		Config{ServerURL: server.URL, AgentID: "agt-test"},
		Dependencies{HTTPClient: server.Client()},
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	result, err := client.FetchMonitors(context.Background(), "")
	if err != nil {
		t.Fatalf("FetchMonitors: %v", err)
	}
	if result.NotModified {
		t.Fatalf("expected snapshot to be modified")
	}
	if result.ETag != "rev-1" {
		t.Fatalf("expected etag rev-1, got %s", result.ETag)
	}
	if result.Snapshot.Revision != "rev-1" || len(result.Snapshot.Monitors) != 1 {
		t.Fatalf("unexpected snapshot: %+v", result.Snapshot)
	}
}

func TestFetchMonitorsHandlesNotModified(t *testing.T) {
	snapshot := types.MonitorSnapshot{
		Revision:    "rev-1",
		GeneratedAt: time.Unix(123, 0).UTC(),
	}

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != defaultMonitorPath {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method %s", r.Method)
		}
		if r.Header.Get("If-None-Match") == "rev-1" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", "rev-1")
		if err := json.NewEncoder(w).Encode(snapshot); err != nil {
			t.Fatalf("encode snapshot: %v", err)
		}
	}))
	defer server.Close()

	client, err := NewClient(
		Config{ServerURL: server.URL, AgentID: "agt-test"},
		Dependencies{HTTPClient: server.Client()},
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	first, err := client.FetchMonitors(context.Background(), "")
	if err != nil {
		t.Fatalf("FetchMonitors first: %v", err)
	}
	if first.NotModified {
		t.Fatalf("expected initial fetch to return snapshot")
	}

	second, err := client.FetchMonitors(context.Background(), first.ETag)
	if err != nil {
		t.Fatalf("FetchMonitors second: %v", err)
	}
	if !second.NotModified {
		t.Fatalf("expected not modified on second fetch")
	}
	if calls != 2 {
		t.Fatalf("expected two fetch calls, got %d", calls)
	}
}
