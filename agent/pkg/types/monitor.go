package types

import "time"

// MonitorAssignment represents a single monitor configuration assigned to an agent.
type MonitorAssignment struct {
	MonitorID     string   `json:"monitor_id" yaml:"monitor_id"`
	Protocol      string   `json:"protocol" yaml:"protocol"`
	Targets       []string `json:"targets" yaml:"targets"`
	CadenceMillis int      `json:"cadence_ms" yaml:"cadence_ms"`
	TimeoutMillis int      `json:"timeout_ms" yaml:"timeout_ms"`
	Configuration string   `json:"configuration" yaml:"configuration"`
	Disabled      bool     `json:"disabled" yaml:"disabled"`
}

// MonitorSnapshot captures the full assignment state returned by the central service.
type MonitorSnapshot struct {
	Revision    string              `json:"revision" yaml:"revision"`
	GeneratedAt time.Time           `json:"generated_at" yaml:"generated_at"`
	Monitors    []MonitorAssignment `json:"monitors" yaml:"monitors"`
	Incremental bool                `json:"incremental,omitempty" yaml:"incremental,omitempty"`
	Removed     []string            `json:"removed,omitempty" yaml:"removed,omitempty"`
}
