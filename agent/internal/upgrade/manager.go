package upgrade

import (
	"context"
	"io"
	"log"
	"sync"
	"time"

	"github.com/pingsantohq/agent/internal/config"
)

const defaultPollInterval = time.Minute

// Config configures the upgrade manager.
type Config struct {
	DataDir      string
	PollInterval time.Duration
}

// Dependencies allow tests to stub collaborators.
type Dependencies struct {
	Logger    *log.Logger
	LoadState func(context.Context, string) (config.State, error)
}

// Manager periodically refreshes upgrade directives and will invoke upgrade flows once wired to central.
type Manager struct {
	cfg  Config
	deps Dependencies

	mu      sync.RWMutex
	channel string
	paused  bool
}

// NewManager constructs an Upgrade manager.
func NewManager(cfg Config, deps Dependencies) *Manager {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaultPollInterval
	}
	if deps.Logger == nil {
		deps.Logger = log.New(io.Discard, "", 0)
	}
	if deps.LoadState == nil {
		deps.LoadState = config.LoadState
	}
	return &Manager{cfg: cfg, deps: deps}
}

// Channel returns the latest upgrade channel derived from state.
func (m *Manager) Channel() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.channel
}

// Paused reports whether auto-upgrades are paused.
func (m *Manager) Paused() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.paused
}

// Run starts the polling loop until the context is cancelled.
func (m *Manager) Run(ctx context.Context) error {
	if m.cfg.DataDir == "" {
		return nil
	}
	// Initial load.
	m.reload(ctx)

	ticker := time.NewTicker(m.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			m.reload(ctx)
			if m.Paused() {
				m.deps.Logger.Printf("upgrade manager: auto-upgrades paused (channel=%s)", m.Channel())
				continue
			}
			// Placeholder for future central integration.
			m.deps.Logger.Printf("upgrade manager: channel=%s (awaiting central upgrade API)", m.Channel())
		}
	}
}

func (m *Manager) reload(ctx context.Context) {
	if m.deps.LoadState == nil || m.cfg.DataDir == "" {
		return
	}
	state, err := m.deps.LoadState(ctx, m.cfg.DataDir)
	if err != nil {
		m.deps.Logger.Printf("upgrade manager: failed to load state: %v", err)
		return
	}
	channel := state.Upgrade.Channel
	if channel == "" {
		channel = "stable"
	}
	m.mu.Lock()
	m.channel = channel
	m.paused = state.Upgrade.Paused
	m.mu.Unlock()
}
