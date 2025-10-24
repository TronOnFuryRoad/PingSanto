package health

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pingsantohq/agent/internal/metrics"
)

const (
	defaultMonitorStale    = time.Minute
	certExpiryWarningAhead = time.Hour
)

const (
	categoryQueuePressure  = "QUEUE_PRESSURE"
	categoryMonitorPending = "MONITOR_PENDING"
	categoryMonitorStale   = "MONITOR_STALE"
	categoryMonitorError   = "MONITOR_ERROR"
	categoryCertExpiring   = "CERT_EXPIRING"
	categoryCertExpired    = "CERT_EXPIRED"
)

const (
	severityInfo     = "info"
	severityWarning  = "warning"
	severityCritical = "critical"
)

// Checker evaluates readiness conditions for the agent.
type Checker struct {
	metrics       *metrics.Store
	queueCapacity int
	staleAfter    time.Duration

	mu                 sync.RWMutex
	lastMonitorSuccess time.Time
	monitorErr         string
	lastMonitorError   time.Time
	certExpiry         time.Time
}

// NewChecker constructs a readiness checker bound to the provided metrics store.
func NewChecker(store *metrics.Store, queueCapacity int, staleAfter time.Duration) *Checker {
	if staleAfter <= 0 {
		staleAfter = defaultMonitorStale
	}
	return &Checker{
		metrics:       store,
		queueCapacity: queueCapacity,
		staleAfter:    staleAfter,
	}
}

// ObserveMonitorSync records the outcome of a monitor sync attempt.
func (c *Checker) ObserveMonitorSync(ts time.Time, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.monitorErr = err.Error()
		c.lastMonitorError = ts
		return
	}
	c.lastMonitorSuccess = ts
	c.monitorErr = ""
	c.lastMonitorError = time.Time{}
}

// SetCertExpiry records the expiry timestamp of the current client certificate.
func (c *Checker) SetCertExpiry(expiry time.Time) {
	c.mu.Lock()
	c.certExpiry = expiry
	c.mu.Unlock()
}

// Ready evaluates all readiness conditions and returns the overall status and reasons for failure.
func (c *Checker) Ready(now time.Time) (bool, []string) {
	reasons := make([]string, 0, 4)
	categories := make([]metrics.ReadinessCategory, 0, 4)
	appendCategory := func(name, severity string) {
		categories = append(categories, metrics.ReadinessCategory{
			Name:     name,
			Severity: severity,
		})
	}

	if c.metrics != nil && c.queueCapacity > 0 {
		snap := c.metrics.Snapshot()
		if snap.QueueDepth >= int64(c.queueCapacity) {
			reasons = append(reasons, "queue capacity exceeded")
			appendCategory(categoryQueuePressure, severityWarning)
		}
	}

	c.mu.RLock()
	lastSuccess := c.lastMonitorSuccess
	monitorErr := c.monitorErr
	lastErr := c.lastMonitorError
	certExpiry := c.certExpiry
	staleAfter := c.staleAfter
	c.mu.RUnlock()

	if lastSuccess.IsZero() {
		reasons = append(reasons, "monitors not yet synced")
		appendCategory(categoryMonitorPending, severityInfo)
	} else if staleAfter > 0 && now.Sub(lastSuccess) > staleAfter {
		reasons = append(reasons, fmt.Sprintf("monitor sync stale (%s)", now.Sub(lastSuccess).Round(time.Second)))
		appendCategory(categoryMonitorStale, severityWarning)
	}

	if monitorErr != "" {
		if staleAfter <= 0 || now.Sub(lastErr) <= staleAfter {
			reasons = append(reasons, fmt.Sprintf("monitor sync failing: %s", monitorErr))
			appendCategory(categoryMonitorError, severityCritical)
		}
	}

	if !certExpiry.IsZero() {
		if !certExpiry.After(now) {
			reasons = append(reasons, "client certificate expired")
			appendCategory(categoryCertExpired, severityCritical)
		} else if certExpiry.Sub(now) < certExpiryWarningAhead {
			reasons = append(reasons, "client certificate expiring soon")
			appendCategory(categoryCertExpiring, severityWarning)
		}
	}

	ready := len(reasons) == 0
	if c.metrics != nil {
		reasonText := strings.Join(reasons, "; ")
		if ready {
			c.metrics.ObserveReadiness(true, "", nil)
		} else {
			c.metrics.ObserveReadiness(false, reasonText, categories)
		}
	}
	if !ready {
		return false, reasons
	}
	return true, nil
}
