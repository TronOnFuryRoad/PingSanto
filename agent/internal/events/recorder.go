package events

import "github.com/pingsantohq/agent/pkg/types"

type Recorder interface {
	Record(event types.Event)
}

type NoopRecorder struct{}

func (NoopRecorder) Record(event types.Event) {}

type Multi struct {
	recorders []Recorder
}

func NewMulti(recorders ...Recorder) Multi {
	return Multi{recorders: recorders}
}

func (m Multi) Record(event types.Event) {
	for _, rec := range m.recorders {
		if rec != nil {
			rec.Record(event)
		}
	}
}
