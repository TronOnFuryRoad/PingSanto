package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Store maintains in-memory gauges and counters for agent telemetry.
type Store struct {
	queueDepth           atomic.Int64
	queueDrops           atomic.Uint64
	queueSpills          atomic.Uint64
	backfillPendingBytes atomic.Int64
	readinessState       atomic.Int64
	readinessReason      atomic.Value
	readinessCategories  atomic.Value
	readyTransitions     atomic.Uint64
	notReadyTransitions  atomic.Uint64
	readyAlerts          atomic.Uint64
	categoryTotals       sync.Map // categoryKey -> *atomic.Uint64
}

// ReadinessCategory captures a categorized readiness reason with severity.
type ReadinessCategory struct {
	Name     string
	Severity string
}

type categoryKey struct {
	Name     string
	Severity string
}

// NewStore constructs a Store with zeroed metrics.
func NewStore() *Store {
	store := &Store{}
	store.readinessReason.Store("")
	store.readinessCategories.Store([]ReadinessCategory(nil))
	return store
}

// Snapshot captures the current metric values in a plain struct.
type Snapshot struct {
	QueueDepth           int64
	QueueDroppedTotal    uint64
	QueueSpilledTotal    uint64
	BackfillPendingBytes int64
	Ready                bool
	ReadyReason          string
	ReadyTransitions     uint64
	NotReadyTransitions  uint64
	ReadyAlerts          uint64
	ReadyCategories      []ReadinessCategory
	CategoryTransitions  []CategoryCount
}

// CategoryCount captures accumulated transition counts per category/severity.
type CategoryCount struct {
	Category string
	Severity string
	Count    uint64
}

// Snapshot returns a point-in-time copy of the metrics.
func (s *Store) Snapshot() Snapshot {
	readyReason, _ := s.readinessReason.Load().(string)
	rawCategories, _ := s.readinessCategories.Load().([]ReadinessCategory)
	categories := make([]ReadinessCategory, len(rawCategories))
	copy(categories, rawCategories)
	categoryCounts := make([]CategoryCount, 0)
	s.categoryTotals.Range(func(key, value any) bool {
		ckey, ok := key.(categoryKey)
		if !ok {
			return true
		}
		counter, ok := value.(*atomic.Uint64)
		if !ok || counter == nil {
			return true
		}
		categoryCounts = append(categoryCounts, CategoryCount{
			Category: ckey.Name,
			Severity: ckey.Severity,
			Count:    counter.Load(),
		})
		return true
	})
	return Snapshot{
		QueueDepth:           s.queueDepth.Load(),
		QueueDroppedTotal:    s.queueDrops.Load(),
		QueueSpilledTotal:    s.queueSpills.Load(),
		BackfillPendingBytes: s.backfillPendingBytes.Load(),
		Ready:                s.readinessState.Load() == 1,
		ReadyReason:          readyReason,
		ReadyTransitions:     s.readyTransitions.Load(),
		NotReadyTransitions:  s.notReadyTransitions.Load(),
		ReadyAlerts:          s.readyAlerts.Load(),
		ReadyCategories:      categories,
		CategoryTransitions:  categoryCounts,
	}
}

// QueueRecorder returns an implementation of QueueRecorder backed by the store.
func (s *Store) QueueRecorder() QueueRecorder {
	return queueRecorder{store: s}
}

// BackfillRecorder returns an implementation of BackfillRecorder backed by the store.
func (s *Store) BackfillRecorder() BackfillRecorder {
	return backfillRecorder{store: s}
}

type queueRecorder struct {
	store *Store
}

func (r queueRecorder) ObserveQueueDepth(depth int) {
	r.store.queueDepth.Store(int64(depth))
}

func (r queueRecorder) IncQueueDrops() {
	r.store.queueDrops.Add(1)
}

func (r queueRecorder) IncQueueSpills() {
	r.store.queueSpills.Add(1)
}

type backfillRecorder struct {
	store *Store
}

func (r backfillRecorder) ObservePendingBytes(bytes int64) {
	if bytes < 0 {
		bytes = 0
	}
	r.store.backfillPendingBytes.Store(bytes)
}

func (s *Store) ObserveReadiness(ready bool, reason string, categories []ReadinessCategory) {
	prev := s.readinessState.Load()
	if ready {
		if prev == 0 {
			s.readyTransitions.Add(1)
		}
		s.readinessState.Store(1)
		s.readinessReason.Store("")
		s.readinessCategories.Store([]ReadinessCategory(nil))
		return
	}
	if prev == 1 {
		s.notReadyTransitions.Add(1)
		s.readyAlerts.Add(1)
	}
	s.readinessState.Store(0)
	s.readinessReason.Store(reason)
	deduped := dedupeCategories(categories)
	s.readinessCategories.Store(deduped)
	if prev == 1 && len(deduped) > 0 {
		for _, cat := range deduped {
			counter := s.getCategoryCounter(cat)
			counter.Add(1)
		}
	}
}

func (s *Store) getCategoryCounter(category ReadinessCategory) *atomic.Uint64 {
	key := categoryKey{
		Name:     normalizeCategoryName(category.Name),
		Severity: normalizeSeverity(category.Severity),
	}
	if value, ok := s.categoryTotals.Load(key); ok {
		if counter, ok := value.(*atomic.Uint64); ok && counter != nil {
			return counter
		}
	}
	counter := &atomic.Uint64{}
	actual, _ := s.categoryTotals.LoadOrStore(key, counter)
	if existing, ok := actual.(*atomic.Uint64); ok && existing != nil {
		return existing
	}
	return counter
}

func dedupeCategories(categories []ReadinessCategory) []ReadinessCategory {
	if len(categories) == 0 {
		return nil
	}
	seen := make(map[categoryKey]struct{}, len(categories))
	result := make([]ReadinessCategory, 0, len(categories))
	for _, c := range categories {
		rawName := strings.TrimSpace(c.Name)
		if rawName == "" {
			continue
		}
		name := normalizeCategoryName(c.Name)
		severity := normalizeSeverity(c.Severity)
		key := categoryKey{Name: name, Severity: severity}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, ReadinessCategory{
			Name:     name,
			Severity: severity,
		})
	}
	return result
}

func normalizeCategoryName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	return name
}

func normalizeSeverity(severity string) string {
	severity = strings.TrimSpace(strings.ToLower(severity))
	if severity == "" {
		return "unknown"
	}
	switch severity {
	case "info", "informational":
		return "info"
	case "warn", "warning":
		return "warning"
	case "critical", "crit":
		return "critical"
	default:
		return severity
	}
}

// WritePrometheus renders the current metrics using the Prometheus text format.
func (s *Store) WritePrometheus(w io.Writer) error {
	snap := s.Snapshot()
	readyValue := 0
	if snap.Ready {
		readyValue = 1
	}
	reason := snap.ReadyReason
	if !snap.Ready && reason == "" {
		reason = "unknown"
	}
	if snap.Ready && reason == "" {
		reason = "ready"
	}
	lines := []string{
		"# HELP pingsanto_agent_queue_depth_number Number of probe results currently buffered in memory.",
		"# TYPE pingsanto_agent_queue_depth_number gauge",
		fmt.Sprintf("pingsanto_agent_queue_depth_number %d", snap.QueueDepth),
		"# HELP pingsanto_agent_queue_dropped_total Total probe results dropped due to queue pressure.",
		"# TYPE pingsanto_agent_queue_dropped_total counter",
		fmt.Sprintf("pingsanto_agent_queue_dropped_total %d", snap.QueueDroppedTotal),
		"# HELP pingsanto_agent_queue_spilled_total Total probe results spilled to disk.",
		"# TYPE pingsanto_agent_queue_spilled_total counter",
		fmt.Sprintf("pingsanto_agent_queue_spilled_total %d", snap.QueueSpilledTotal),
		"# HELP pingsanto_agent_backfill_pending_bytes Bytes currently pending in backfill spill storage.",
		"# TYPE pingsanto_agent_backfill_pending_bytes gauge",
		fmt.Sprintf("pingsanto_agent_backfill_pending_bytes %d", snap.BackfillPendingBytes),
		"# HELP pingsanto_agent_ready Whether the agent considers itself ready (1=ready).",
		"# TYPE pingsanto_agent_ready gauge",
		fmt.Sprintf("pingsanto_agent_ready %d", readyValue),
		"# HELP pingsanto_agent_ready_info Reason associated with the most recent readiness evaluation.",
		"# TYPE pingsanto_agent_ready_info gauge",
		fmt.Sprintf("pingsanto_agent_ready_info{reason=%q} 1", reason),
		"# HELP pingsanto_agent_ready_transitions_total Count of readiness state transitions by resulting state.",
		"# TYPE pingsanto_agent_ready_transitions_total counter",
		fmt.Sprintf("pingsanto_agent_ready_transitions_total{state=%q} %d", "ready", snap.ReadyTransitions),
		fmt.Sprintf("pingsanto_agent_ready_transitions_total{state=%q} %d", "not_ready", snap.NotReadyTransitions),
		"# HELP pingsanto_agent_ready_alerts_total Total number of readiness alert transitions.",
		"# TYPE pingsanto_agent_ready_alerts_total counter",
		fmt.Sprintf("pingsanto_agent_ready_alerts_total %d", snap.ReadyAlerts),
		"# HELP pingsanto_agent_ready_categories_info Categories associated with the most recent readiness evaluation.",
		"# TYPE pingsanto_agent_ready_categories_info gauge",
	}
	if len(snap.ReadyCategories) == 0 {
		lines = append(lines, fmt.Sprintf("pingsanto_agent_ready_categories_info{category=%q,severity=%q} 1", "none", "none"))
	} else {
		cats := append([]ReadinessCategory(nil), snap.ReadyCategories...)
		sort.Slice(cats, func(i, j int) bool {
			if cats[i].Name == cats[j].Name {
				return cats[i].Severity < cats[j].Severity
			}
			return cats[i].Name < cats[j].Name
		})
		for _, cat := range cats {
			lines = append(lines, fmt.Sprintf("pingsanto_agent_ready_categories_info{category=%q,severity=%q} 1", cat.Name, cat.Severity))
		}
	}
	lines = append(lines,
		"# HELP pingsanto_agent_ready_category_transitions_total Count of readiness degradations annotated by category.",
		"# TYPE pingsanto_agent_ready_category_transitions_total counter",
	)
	if len(snap.CategoryTransitions) == 0 {
		lines = append(lines, fmt.Sprintf("pingsanto_agent_ready_category_transitions_total{category=%q,severity=%q} %d", "none", "none", 0))
	} else {
		counts := append([]CategoryCount(nil), snap.CategoryTransitions...)
		sort.Slice(counts, func(i, j int) bool {
			if counts[i].Category == counts[j].Category {
				return counts[i].Severity < counts[j].Severity
			}
			return counts[i].Category < counts[j].Category
		})
		for _, cc := range counts {
			lines = append(lines, fmt.Sprintf("pingsanto_agent_ready_category_transitions_total{category=%q,severity=%q} %d", cc.Category, cc.Severity, cc.Count))
		}
	}
	lines = append(lines, "")
	for _, line := range lines {
		if _, err := io.WriteString(w, line+"\n"); err != nil {
			return err
		}
	}
	return nil
}

// NewHTTPHandler returns an http.Handler that serves Prometheus formatted metrics.
func NewHTTPHandler(store *Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		if r.Method == http.MethodHead {
			return
		}
		if err := store.WritePrometheus(w); err != nil {
			http.Error(w, "metrics unavailable", http.StatusInternalServerError)
		}
	})
}
