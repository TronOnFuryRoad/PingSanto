package probe

import "time"

type Request struct {
	MonitorID string
	Protocol  string
	Targets   []string
	Timeout   time.Duration
}
