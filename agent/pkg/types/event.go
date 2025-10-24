package types

import "time"

type EventType string

const (
	EventQueueSpill    EventType = "QueueSpill"
	EventQueueDrop     EventType = "QueueDrop"
	EventBackfillStart EventType = "BackfillStart"
	EventBackfillEnd   EventType = "BackfillEnd"
	EventReconnect     EventType = "Reconnect"
	EventGap           EventType = "Gap"
	EventRateLimit     EventType = "RateLimit"
	EventCertExpiring  EventType = "CertExpiring"
)

type Event struct {
	Type      EventType         `json:"type"`
	Timestamp time.Time         `json:"ts"`
	MonitorID string            `json:"monitor_id,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	Details   map[string]any    `json:"details,omitempty"`
}
