package worker

import "time"

type Job struct {
	MonitorID     string
	Protocol      string
	Targets       []string
	Cadence       time.Duration
	Timeout       time.Duration
	ScheduledFor  time.Time
	Configuration string
}
