package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientFetchPlanSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("channel") != "stable" {
			t.Fatalf("expected channel query parameter")
		}
		if r.Header.Get("If-None-Match") != `"etag-old"` {
			w.Header().Set("ETag", `"etag-new"`)
			_ = json.NewEncoder(w).Encode(planEnvelope{
				AgentID:     "channel:stable",
				GeneratedAt: time.Unix(1730000000, 0).UTC(),
				Channel:     "stable",
				Artifact: planArtifact{
					Version:      "1.2.3",
					URL:          "https://example.com/pkg.tgz",
					SHA256:       "deadbeef",
					SignatureURL: "https://example.com/pkg.sig",
					ForceApply:   true,
				},
				Schedule: planSchedule{},
				Paused:   false,
				Notes:    "rollout",
			})
			return
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer ts.Close()

	client, err := NewClient(ts.Client(), ts.URL, "agt_1", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	result, err := client.FetchPlan(context.Background(), "stable", "")
	if err != nil {
		t.Fatalf("FetchPlan: %v", err)
	}
	if result.NotModified {
		t.Fatalf("expected plan to be returned")
	}
	if result.Plan.Artifact.Version != "1.2.3" || result.ETag != `"etag-new"` {
		t.Fatalf("unexpected plan result: %#v %q", result.Plan, result.ETag)
	}

	state := result.Plan.ToState(time.Unix(1730003600, 0), result.ETag)
	if state.Version != "1.2.3" || state.Source != "channel:stable" || state.ETag != `"etag-new"` {
		t.Fatalf("unexpected state conversion: %#v", state)
	}
}

func TestClientFetchPlanNotModified(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	defer ts.Close()

	client, err := NewClient(ts.Client(), ts.URL, "agt_1", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	result, err := client.FetchPlan(context.Background(), "stable", `"etag-old"`)
	if err != nil {
		t.Fatalf("FetchPlan: %v", err)
	}
	if !result.NotModified {
		t.Fatalf("expected NotModified")
	}
	if result.ETag != `"etag-old"` {
		t.Fatalf("expected original etag preserved")
	}
}

func TestClientFetchPlanNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	client, err := NewClient(ts.Client(), ts.URL, "agt_1", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.FetchPlan(context.Background(), "stable", "")
	if !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("expected ErrPlanNotFound, got %v", err)
	}
}

func TestClientReportUpgrade(t *testing.T) {
	var received reportPayload
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	client, err := NewClient(ts.Client(), ts.URL, "agt_1", nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	start := time.Unix(1730000000, 0).UTC()
	end := start.Add(time.Minute)
	report := Report{
		AgentID:        "agt_1",
		CurrentVersion: "1.2.3",
		Channel:        "stable",
		Status:         "success",
		StartedAt:      start,
		CompletedAt:    end,
		Message:        "done",
		Details:        map[string]any{"duration": "1m"},
	}
	if err := client.ReportUpgrade(context.Background(), report); err != nil {
		t.Fatalf("ReportUpgrade: %v", err)
	}

	if received.AgentID != "agt_1" || received.Status != "success" {
		t.Fatalf("unexpected payload: %#v", received)
	}
}
