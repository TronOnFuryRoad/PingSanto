package types

import "time"

type ResultEnvelope struct {
	AgentID  string            `json:"agent_id" yaml:"agent_id"`
	SentAt   time.Time         `json:"sent_at" yaml:"sent_at"`
	BatchSeq uint64            `json:"batch_seq" yaml:"batch_seq"`
	Labels   map[string]string `json:"labels" yaml:"labels"`
	Results  []ProbeResult     `json:"results" yaml:"results"`
}

type ProbeResult struct {
	MonitorID       string    `json:"monitor_id" yaml:"monitor_id"`
	Timestamp       time.Time `json:"ts" yaml:"ts"`
	Proto           string    `json:"proto" yaml:"proto"`
	IP              string    `json:"ip" yaml:"ip"`
	RTTMilliseconds float64   `json:"rtt_ms" yaml:"rtt_ms"`
	Success         bool      `json:"success" yaml:"success"`
	Sequence        uint64    `json:"seq" yaml:"seq"`
	JitterMs        float64   `json:"jitter_ms" yaml:"jitter_ms"`
	LossWindowPct   float64   `json:"loss_window_pct" yaml:"loss_window_pct"`
	MOS             float64   `json:"mos" yaml:"mos"`
}
