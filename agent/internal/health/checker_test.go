package health

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pingsantohq/agent/internal/metrics"
)

func TestCheckerReadyConditions(t *testing.T) {
	store := metrics.NewStore()
	checker := NewChecker(store, 10, 30*time.Second)

	now := time.Unix(1000, 0).UTC()
	ready, reasons := checker.Ready(now)
	if ready {
		t.Fatalf("expected not ready without monitor sync")
	}
	if len(reasons) == 0 || reasons[0] != "monitors not yet synced" {
		t.Fatalf("unexpected reasons: %v", reasons)
	}
	snap := store.Snapshot()
	if snap.Ready {
		t.Fatalf("expected readiness gauge to be false")
	}
	if !strings.Contains(snap.ReadyReason, "monitors not yet synced") {
		t.Fatalf("expected readiness reason to mention monitors, got %q", snap.ReadyReason)
	}
	if snap.ReadyTransitions != 0 || snap.NotReadyTransitions != 0 || snap.ReadyAlerts != 0 {
		t.Fatalf("expected readiness counters to remain zero initially, got %+v", snap)
	}
	if !containsCategoryWithSeverity(snap.ReadyCategories, categoryMonitorPending, severityInfo) {
		t.Fatalf("expected MONITOR_PENDING category, got %+v", snap.ReadyCategories)
	}

	checker.ObserveMonitorSync(now, nil)
	ready, _ = checker.Ready(now)
	if !ready {
		t.Fatalf("expected ready after successful sync")
	}
	snap = store.Snapshot()
	if !snap.Ready {
		t.Fatalf("expected readiness gauge true after recovery")
	}
	if snap.ReadyReason != "" {
		t.Fatalf("expected empty readiness reason when healthy, got %q", snap.ReadyReason)
	}
	if snap.ReadyTransitions != 1 || snap.NotReadyTransitions != 0 || snap.ReadyAlerts != 0 {
		t.Fatalf("expected counters after first recovery to be (1,0,0), got %+v", snap)
	}
	if len(snap.ReadyCategories) != 0 {
		t.Fatalf("expected no categories when healthy, got %+v", snap.ReadyCategories)
	}

	// Queue at capacity should flip readiness.
	store.QueueRecorder().ObserveQueueDepth(10)
	ready, reasons = checker.Ready(now)
	if ready {
		t.Fatalf("expected not ready when queue at capacity")
	}
	if reasons[0] != "queue capacity exceeded" {
		t.Fatalf("unexpected reasons: %v", reasons)
	}
	snap = store.Snapshot()
	if snap.Ready {
		t.Fatalf("expected readiness gauge false due to queue pressure")
	}
	if !strings.Contains(snap.ReadyReason, "queue capacity exceeded") {
		t.Fatalf("expected readiness reason to capture queue pressure, got %q", snap.ReadyReason)
	}
	if snap.ReadyTransitions != 1 || snap.NotReadyTransitions != 1 || snap.ReadyAlerts != 1 {
		t.Fatalf("expected counters after queue alert to be (1,1,1), got %+v", snap)
	}
	if !containsCategoryWithSeverity(snap.ReadyCategories, categoryQueuePressure, severityWarning) {
		t.Fatalf("expected QUEUE_PRESSURE category, got %+v", snap.ReadyCategories)
	}

	// Clear queue pressure and advance until stale.
	store.QueueRecorder().ObserveQueueDepth(0)
	staleNow := now.Add(time.Minute)
	ready, reasons = checker.Ready(staleNow)
	if ready {
		t.Fatalf("expected not ready when sync stale")
	}
	snap = store.Snapshot()
	if snap.Ready {
		t.Fatalf("expected readiness gauge false when stale")
	}
	if !strings.Contains(snap.ReadyReason, "monitor sync stale") {
		t.Fatalf("expected stale reason, got %q", snap.ReadyReason)
	}
	if snap.ReadyTransitions != 1 || snap.NotReadyTransitions != 1 || snap.ReadyAlerts != 1 {
		t.Fatalf("expected counters unchanged during stale period, got %+v", snap)
	}
	if !containsCategoryWithSeverity(snap.ReadyCategories, categoryMonitorStale, severityWarning) {
		t.Fatalf("expected MONITOR_STALE category, got %+v", snap.ReadyCategories)
	}

	// Recent failure keeps readiness false without additional alerts.
	checker.ObserveMonitorSync(staleNow, errors.New("remote 500"))
	ready, reasons = checker.Ready(staleNow)
	if ready {
		t.Fatalf("expected not ready after sync failure")
	}
	if reasons[len(reasons)-1] != "monitor sync failing: remote 500" {
		t.Fatalf("expected failure reason, got %v", reasons)
	}
	snap = store.Snapshot()
	if snap.Ready {
		t.Fatalf("expected readiness gauge false after failure")
	}
	if !strings.Contains(snap.ReadyReason, "monitor sync failing") {
		t.Fatalf("expected failure reason in metrics, got %q", snap.ReadyReason)
	}
	if snap.ReadyTransitions != 1 || snap.NotReadyTransitions != 1 || snap.ReadyAlerts != 1 {
		t.Fatalf("expected counters unchanged during repeated failure, got %+v", snap)
	}
	if !containsCategoryWithSeverity(snap.ReadyCategories, categoryMonitorError, severityCritical) {
		t.Fatalf("expected MONITOR_ERROR category, got %+v", snap.ReadyCategories)
	}
	if !containsCategoryWithSeverity(snap.ReadyCategories, categoryMonitorStale, severityWarning) {
		t.Fatalf("expected stale category persisted, got %+v", snap.ReadyCategories)
	}

	// Success clears failure state.
	recovery := staleNow.Add(2 * time.Second)
	checker.ObserveMonitorSync(recovery, nil)
	ready, _ = checker.Ready(recovery)
	if !ready {
		t.Fatalf("expected ready after recovery")
	}
	snap = store.Snapshot()
	if !snap.Ready {
		t.Fatalf("expected readiness gauge true after recovery")
	}
	if snap.ReadyReason != "" {
		t.Fatalf("expected empty readiness reason after recovery, got %q", snap.ReadyReason)
	}
	if snap.ReadyTransitions != 2 || snap.NotReadyTransitions != 1 || snap.ReadyAlerts != 1 {
		t.Fatalf("expected counters after recovery to be (2,1,1), got %+v", snap)
	}
	if len(snap.ReadyCategories) != 0 {
		t.Fatalf("expected no categories after recovery, got %+v", snap.ReadyCategories)
	}

	// Certificate expiry warnings should trigger another alert transition.
	checker.SetCertExpiry(recovery.Add(30 * time.Minute))
	ready, reasons = checker.Ready(recovery)
	if ready {
		t.Fatalf("expected not ready when cert expiring soon")
	}
	if reasons[len(reasons)-1] != "client certificate expiring soon" {
		t.Fatalf("unexpected reasons: %v", reasons)
	}
	snap = store.Snapshot()
	if snap.Ready {
		t.Fatalf("expected readiness gauge false when cert expiring")
	}
	if !strings.Contains(snap.ReadyReason, "client certificate expiring soon") {
		t.Fatalf("expected cert warning in metrics, got %q", snap.ReadyReason)
	}
	if snap.ReadyTransitions != 2 || snap.NotReadyTransitions != 2 || snap.ReadyAlerts != 2 {
		t.Fatalf("expected counters after cert warning to be (2,2,2), got %+v", snap)
	}
	if !containsCategoryWithSeverity(snap.ReadyCategories, categoryCertExpiring, severityWarning) {
		t.Fatalf("expected CERT_EXPIRING category, got %+v", snap.ReadyCategories)
	}

	checker.SetCertExpiry(recovery.Add(2 * time.Hour))
	ready, _ = checker.Ready(recovery)
	if !ready {
		t.Fatalf("expected ready when cert expiry is far enough away")
	}
	snap = store.Snapshot()
	if !snap.Ready {
		t.Fatalf("expected readiness gauge recovered when cert ok")
	}
	if snap.ReadyReason != "" {
		t.Fatalf("expected empty readiness reason when healthy, got %q", snap.ReadyReason)
	}
	if snap.ReadyTransitions != 3 || snap.NotReadyTransitions != 2 || snap.ReadyAlerts != 2 {
		t.Fatalf("expected final counters (3,2,2), got %+v", snap)
	}
	if len(snap.ReadyCategories) != 0 {
		t.Fatalf("expected no categories when healthy, got %+v", snap.ReadyCategories)
	}
}
func TestCheckerExpiredCertificate(t *testing.T) {
	store := metrics.NewStore()
	checker := NewChecker(store, 0, 0)
	ref := time.Unix(2000, 0).UTC()
	checker.ObserveMonitorSync(ref, nil)

	checker.SetCertExpiry(ref.Add(-time.Minute))
	ready, reasons := checker.Ready(ref)
	if ready {
		t.Fatalf("expected not ready with expired certificate")
	}
	if reasons[len(reasons)-1] != "client certificate expired" {
		t.Fatalf("unexpected reasons: %v", reasons)
	}
	snap := store.Snapshot()
	if snap.Ready {
		t.Fatalf("expected readiness gauge false for expired certificate")
	}
	if !strings.Contains(snap.ReadyReason, "client certificate expired") {
		t.Fatalf("expected readiness reason to mention expiry, got %q", snap.ReadyReason)
	}
	if !containsCategoryWithSeverity(snap.ReadyCategories, categoryCertExpired, severityCritical) {
		t.Fatalf("expected CERT_EXPIRED category, got %+v", snap.ReadyCategories)
	}
}

func containsCategoryWithSeverity(categories []metrics.ReadinessCategory, name, severity string) bool {
	for _, c := range categories {
		if c.Name == name && c.Severity == severity {
			return true
		}
	}
	return false
}
