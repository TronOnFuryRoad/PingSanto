package queue

import (
	"sync"
	"time"

	"github.com/pingsantohq/agent/internal/events"
	"github.com/pingsantohq/agent/internal/metrics"
	"github.com/pingsantohq/agent/internal/queue/persist"
	"github.com/pingsantohq/agent/pkg/types"
)

type ResultQueue struct {
	mu        sync.Mutex
	capacity  int
	items     []types.ProbeResult
	spill     *persist.Store
	threshold int
	spilled   uint64
	dropped   uint64
	events    events.Recorder
	metrics   metrics.QueueRecorder
}

func NewResultQueue(capacity int) *ResultQueue {
	if capacity <= 0 {
		capacity = 1
	}
	return &ResultQueue{
		capacity: capacity,
		items:    make([]types.ProbeResult, 0, capacity),
	}
}

func (q *ResultQueue) AttachSpill(store *persist.Store, thresholdRatio float64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.spill = store
	if thresholdRatio <= 0 || thresholdRatio > 1 {
		thresholdRatio = 0.8
	}
	threshold := int(float64(q.capacity) * thresholdRatio)
	if threshold < 1 {
		threshold = q.capacity
	}
	q.threshold = threshold
}

func (q *ResultQueue) SetEventRecorder(rec events.Recorder) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.events = rec
}

func (q *ResultQueue) SetMetricsRecorder(rec metrics.QueueRecorder) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.metrics = rec
}

func (q *ResultQueue) Enqueue(result types.ProbeResult) (dropped bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.spill != nil && q.threshold > 0 {
		for len(q.items) >= q.threshold {
			if !q.spillOldestLocked() {
				break
			}
		}
	}

	if len(q.items) >= q.capacity {
		if q.spill != nil {
			if q.spillOldestLocked() && len(q.items) < q.capacity {
				goto appendResult
			}
		}
		if len(q.items) > 0 {
			removed := q.items[0]
			q.items = q.items[1:]
			dropped = true
			q.dropped++
			q.recordEvent(types.EventQueueDrop, removed.MonitorID)
			q.incrementDrop()
			q.observeDepthLocked()
		}
	}

appendResult:
	q.items = append(q.items, result)
	q.observeDepthLocked()
	return dropped
}

func (q *ResultQueue) Drain(max int) []types.ProbeResult {
	q.mu.Lock()
	defer q.mu.Unlock()

	n := len(q.items)
	if max > 0 && max < n {
		n = max
	}
	drained := make([]types.ProbeResult, n)
	copy(drained, q.items[:n])
	q.items = q.items[n:]
	q.observeDepthLocked()
	return drained
}

func (q *ResultQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

func (q *ResultQueue) Stats() Stats {
	q.mu.Lock()
	defer q.mu.Unlock()
	return Stats{
		Len:     len(q.items),
		Dropped: q.dropped,
		Spilled: q.spilled,
	}
}

func (q *ResultQueue) spillOldestLocked() bool {
	if q.spill == nil || len(q.items) == 0 {
		return false
	}
	result := q.items[0]
	if err := q.spill.Append(result); err != nil {
		q.items = q.items[1:]
		q.dropped++
		q.recordEvent(types.EventQueueDrop, result.MonitorID)
		q.incrementDrop()
		q.observeDepthLocked()
		return false
	}
	q.items = q.items[1:]
	q.spilled++
	q.recordEvent(types.EventQueueSpill, result.MonitorID)
	q.incrementSpill()
	q.observeDepthLocked()
	return true
}

type Stats struct {
	Len     int
	Dropped uint64
	Spilled uint64
}

func (q *ResultQueue) recordEvent(eventType types.EventType, monitorID string) {
	if q.events == nil {
		return
	}
	q.events.Record(types.Event{
		Type:      eventType,
		Timestamp: time.Now().UTC(),
		MonitorID: monitorID,
	})
}

func (q *ResultQueue) observeDepthLocked() {
	if q.metrics == nil {
		return
	}
	q.metrics.ObserveQueueDepth(len(q.items))
}

func (q *ResultQueue) incrementDrop() {
	if q.metrics == nil {
		return
	}
	q.metrics.IncQueueDrops()
}

func (q *ResultQueue) incrementSpill() {
	if q.metrics == nil {
		return
	}
	q.metrics.IncQueueSpills()
}
