package metrics

type QueueRecorder interface {
	ObserveQueueDepth(depth int)
	IncQueueDrops()
	IncQueueSpills()
}

type NoopQueueRecorder struct{}

func (NoopQueueRecorder) ObserveQueueDepth(depth int) {}
func (NoopQueueRecorder) IncQueueDrops()              {}
func (NoopQueueRecorder) IncQueueSpills()             {}

type BackfillRecorder interface {
	ObservePendingBytes(bytes int64)
}

type NoopBackfillRecorder struct{}

func (NoopBackfillRecorder) ObservePendingBytes(bytes int64) {}
