package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStoreQueueRecorder(t *testing.T) {
	store := NewStore()
	rec := store.QueueRecorder()

	rec.ObserveQueueDepth(5)
	rec.IncQueueDrops()
	rec.IncQueueDrops()
	rec.IncQueueSpills()

	snap := store.Snapshot()
	if snap.QueueDepth != 5 {
		t.Fatalf("expected depth 5 got %d", snap.QueueDepth)
	}
	if snap.QueueDroppedTotal != 2 {
		t.Fatalf("expected drops 2 got %d", snap.QueueDroppedTotal)
	}
	if snap.QueueSpilledTotal != 1 {
		t.Fatalf("expected spills 1 got %d", snap.QueueSpilledTotal)
	}
}

func TestStoreBackfillRecorder(t *testing.T) {
	store := NewStore()
	rec := store.BackfillRecorder()

	rec.ObservePendingBytes(1024)
	if got := store.Snapshot().BackfillPendingBytes; got != 1024 {
		t.Fatalf("expected 1024 got %d", got)
	}

	rec.ObservePendingBytes(-10)
	if got := store.Snapshot().BackfillPendingBytes; got != 0 {
		t.Fatalf("expected clamp to 0 got %d", got)
	}
}

func TestStoreWritePrometheus(t *testing.T) {
	store := NewStore()
	store.QueueRecorder().ObserveQueueDepth(7)
	store.QueueRecorder().IncQueueDrops()
	store.BackfillRecorder().ObservePendingBytes(2048)
	store.ObserveReadiness(true, "", nil)

	var sb strings.Builder
	if err := store.WritePrometheus(&sb); err != nil {
		t.Fatalf("WritePrometheus: %v", err)
	}
	output := sb.String()
	expect := []string{
		"pingsanto_agent_queue_depth_number 7",
		"pingsanto_agent_queue_dropped_total 1",
		"pingsanto_agent_queue_spilled_total 0",
		"pingsanto_agent_backfill_pending_bytes 2048",
		"pingsanto_agent_ready 1",
		"pingsanto_agent_ready_info{reason=\"ready\"} 1",
		"pingsanto_agent_ready_transitions_total{state=\"ready\"} 1",
		"pingsanto_agent_ready_transitions_total{state=\"not_ready\"} 0",
		"pingsanto_agent_ready_alerts_total 0",
		"pingsanto_agent_ready_categories_info{category=\"none\",severity=\"none\"} 1",
		"pingsanto_agent_ready_category_transitions_total{category=\"none\",severity=\"none\"} 0",
	}
	for _, fragment := range expect {
		if !strings.Contains(output, fragment) {
			t.Fatalf("expected output to contain %q, got:\n%s", fragment, output)
		}
	}
}

func TestHTTPHandler(t *testing.T) {
	store := NewStore()
	h := NewHTTPHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Fatalf("expected text/plain content-type got %s", ct)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(body) == 0 {
		t.Fatalf("expected body content")
	}

	headReq := httptest.NewRequest(http.MethodHead, "/metrics", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, headReq)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for HEAD got %d", w.Result().StatusCode)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/metrics", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, postReq)
	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 got %d", w.Result().StatusCode)
	}
}

func TestStoreObserveReadiness(t *testing.T) {
	store := NewStore()

	// Initial failure should not count as an alert transition because the agent has not been ready yet.
	store.ObserveReadiness(false, "monitors not yet synced", []ReadinessCategory{
		{Name: "MONITOR_PENDING", Severity: "info"},
	})
	snap := store.Snapshot()
	if snap.Ready {
		t.Fatalf("expected readiness false")
	}
	if snap.ReadyReason != "monitors not yet synced" {
		t.Fatalf("unexpected reason: %q", snap.ReadyReason)
	}
	if snap.ReadyTransitions != 0 || snap.NotReadyTransitions != 0 || snap.ReadyAlerts != 0 {
		t.Fatalf("unexpected counters after initial failure: %+v", snap)
	}
	if len(snap.ReadyCategories) != 1 {
		t.Fatalf("expected one category, got %+v", snap.ReadyCategories)
	}
	if snap.ReadyCategories[0].Name != "MONITOR_PENDING" || snap.ReadyCategories[0].Severity != "info" {
		t.Fatalf("unexpected category snapshot: %+v", snap.ReadyCategories[0])
	}
	if count := getTransitionCount(snap.CategoryTransitions, "MONITOR_PENDING", "info"); count != 0 {
		t.Fatalf("expected zero MONITOR_PENDING transitions, got %d", count)
	}

	// Transition to ready should bump ready transitions without creating alerts.
	store.ObserveReadiness(true, "", nil)
	snap = store.Snapshot()
	if !snap.Ready {
		t.Fatalf("expected readiness true")
	}
	if snap.ReadyReason != "" {
		t.Fatalf("expected empty reason when ready, got %q", snap.ReadyReason)
	}
	if snap.ReadyTransitions != 1 || snap.NotReadyTransitions != 0 || snap.ReadyAlerts != 0 {
		t.Fatalf("unexpected counters after transition to ready: %+v", snap)
	}
	if len(snap.ReadyCategories) != 0 {
		t.Fatalf("expected no categories when ready, got %+v", snap.ReadyCategories)
	}

	// Transitioning back to not ready should increment alert counters.
	store.ObserveReadiness(false, "queue capacity exceeded", []ReadinessCategory{
		{Name: "QUEUE_PRESSURE", Severity: "warning"},
	})
	snap = store.Snapshot()
	if snap.Ready {
		t.Fatalf("expected readiness false after degradation")
	}
	if snap.ReadyReason != "queue capacity exceeded" {
		t.Fatalf("unexpected reason after degradation: %q", snap.ReadyReason)
	}
	if snap.ReadyTransitions != 1 || snap.NotReadyTransitions != 1 || snap.ReadyAlerts != 1 {
		t.Fatalf("unexpected counters after degradation: %+v", snap)
	}
	if len(snap.ReadyCategories) != 1 {
		t.Fatalf("expected one category after degradation, got %+v", snap.ReadyCategories)
	}
	if snap.ReadyCategories[0].Name != "QUEUE_PRESSURE" || snap.ReadyCategories[0].Severity != "warning" {
		t.Fatalf("unexpected category after degradation: %+v", snap.ReadyCategories[0])
	}
	if count := getTransitionCount(snap.CategoryTransitions, "QUEUE_PRESSURE", "warning"); count != 1 {
		t.Fatalf("expected one QUEUE_PRESSURE transition, got %d", count)
	}

	// Recovering to ready again increments ready transitions while keeping alert count stable.
	store.ObserveReadiness(true, "", nil)
	snap = store.Snapshot()
	if !snap.Ready {
		t.Fatalf("expected readiness true after recovery")
	}
	if snap.ReadyReason != "" {
		t.Fatalf("expected empty reason on recovery, got %q", snap.ReadyReason)
	}
	if snap.ReadyTransitions != 2 || snap.NotReadyTransitions != 1 || snap.ReadyAlerts != 1 {
		t.Fatalf("unexpected counters after recovery: %+v", snap)
	}
	if len(snap.ReadyCategories) != 0 {
		t.Fatalf("expected no categories after recovery, got %+v", snap.ReadyCategories)
	}
}

func TestStoreDedupesCategories(t *testing.T) {
	store := NewStore()

	cats := []ReadinessCategory{
		{Name: "QUEUE_PRESSURE", Severity: "warning"},
		{Name: "MONITOR_STALE", Severity: "warning"},
		{Name: "QUEUE_PRESSURE", Severity: "warning"},
		{Name: "", Severity: "info"},
		{Name: "  MONITOR_STALE  ", Severity: "Warning"},
	}
	store.ObserveReadiness(false, "multiple issues", cats)

	snap := store.Snapshot()
	if len(snap.ReadyCategories) != 2 {
		t.Fatalf("expected 2 categories, got %+v", snap.ReadyCategories)
	}
	expected := map[string]string{
		"QUEUE_PRESSURE": "warning",
		"MONITOR_STALE":  "warning",
	}
	for _, c := range snap.ReadyCategories {
		sev, ok := expected[c.Name]
		if !ok {
			t.Fatalf("unexpected category %+v", c)
		}
		if c.Severity != sev {
			t.Fatalf("unexpected severity for %s: %s", c.Name, c.Severity)
		}
		delete(expected, c.Name)
	}
	// No transitions yet since we never flipped from ready.
	if len(snap.CategoryTransitions) != 0 {
		t.Fatalf("expected zero transition counters, got %+v", snap.CategoryTransitions)
	}
}

func getTransitionCount(counts []CategoryCount, category, severity string) uint64 {
	for _, cc := range counts {
		if cc.Category == category && cc.Severity == severity {
			return cc.Count
		}
	}
	return 0
}
