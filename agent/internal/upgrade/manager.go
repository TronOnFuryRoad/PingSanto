package upgrade

import (
	"context"
	"errors"
	"fmt"
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

// PlanFetcher fetches upgrade plans from the controller.
type PlanFetcher interface {
	FetchPlan(ctx context.Context, channel string, etag string) (PlanResult, error)
}

// Reporter reports upgrade progress back to the controller.
type Reporter interface {
	ReportUpgrade(ctx context.Context, report Report) error
}

// Dependencies allow tests to stub collaborators.
type Dependencies struct {
	Logger      *log.Logger
	LoadState   func(context.Context, string) (config.State, error)
	UpdateState func(context.Context, string, config.State) error
	PlanFetcher PlanFetcher
	Reporter    Reporter
	Applier     PlanApplier
	Installer   Installer
	Restarter   Restarter
	Args        []string
	Env         []string
	Now         func() time.Time
}

// Manager periodically refreshes upgrade directives and will invoke upgrade flows once wired to central.
type Manager struct {
	cfg  Config
	deps Dependencies

	mu        sync.RWMutex
	channel   string
	paused    bool
	planETag  string
	installer Installer
	restarter Restarter
	args      []string
	env       []string
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
	if deps.UpdateState == nil {
		deps.UpdateState = config.UpdateState
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	mgr := &Manager{cfg: cfg, deps: deps}
	mgr.installer = deps.Installer
	mgr.restarter = deps.Restarter
	mgr.args = append([]string(nil), deps.Args...)
	mgr.env = append([]string(nil), deps.Env...)
	return mgr
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
	m.reload(ctx)
	if err := m.poll(ctx); err != nil {
		m.deps.Logger.Printf("upgrade manager: poll failed: %v", err)
	}

	ticker := time.NewTicker(m.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			m.reload(ctx)
			if err := m.poll(ctx); err != nil {
				m.deps.Logger.Printf("upgrade manager: poll failed: %v", err)
			}
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
	m.planETag = state.Upgrade.Plan.ETag
	m.mu.Unlock()
}

func (m *Manager) poll(ctx context.Context) error {
	channel, paused, etag := m.snapshot()
	if m.deps.PlanFetcher == nil {
		return nil
	}

	result, err := m.deps.PlanFetcher.FetchPlan(ctx, channel, etag)
	if err != nil {
		if errors.Is(err, ErrPlanNotFound) {
			m.deps.Logger.Printf("upgrade manager: no upgrade plan for channel=%s", channel)
			return nil
		}
		return err
	}
	if result.NotModified {
		return nil
	}

	now := m.deps.Now().UTC()
	statePlan := result.Plan.ToState(now, result.ETag)
	state, err := m.persistPlan(ctx, statePlan)
	if err != nil {
		return err
	}

	m.deps.Logger.Printf("upgrade manager: fetched plan version=%s channel=%s paused=%t", result.Plan.Artifact.Version, result.Plan.Channel, result.Plan.Paused)
	return m.applyPlan(ctx, result.Plan, state, paused)
}

func (m *Manager) persistPlan(ctx context.Context, plan config.UpgradePlanState) (config.State, error) {
	var empty config.State
	if m.deps.LoadState == nil || m.deps.UpdateState == nil || m.cfg.DataDir == "" {
		return empty, nil
	}
	state, err := m.deps.LoadState(ctx, m.cfg.DataDir)
	if err != nil {
		return empty, err
	}
	state.Upgrade.Plan = plan
	if err := m.deps.UpdateState(ctx, m.cfg.DataDir, state); err != nil {
		return empty, err
	}
	m.mu.Lock()
	m.planETag = plan.ETag
	m.mu.Unlock()
	return state, nil
}

func (m *Manager) snapshot() (channel string, paused bool, etag string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.channel, m.paused, m.planETag
}

func (m *Manager) applyPlan(ctx context.Context, plan Plan, state config.State, locallyPaused bool) error {
	if plan.Artifact.Version == "" {
		return nil
	}
	if state.AgentID == "" {
		state.AgentID = plan.AgentID
	}
	if locallyPaused && !plan.Artifact.ForceApply {
		m.deps.Logger.Printf("upgrade manager: locally paused; skipping plan version=%s", plan.Artifact.Version)
		return nil
	}
	if plan.Paused && !plan.Artifact.ForceApply {
		m.deps.Logger.Printf("upgrade manager: controller paused plan version=%s", plan.Artifact.Version)
		return nil
	}
	now := m.deps.Now().UTC()
	if plan.Schedule.Earliest != nil && now.Before(*plan.Schedule.Earliest) {
		m.deps.Logger.Printf("upgrade manager: plan version=%s not within rollout window yet", plan.Artifact.Version)
		return nil
	}
	if plan.Artifact.Version == state.Upgrade.Applied.Version && !plan.Artifact.ForceApply {
		return nil
	}
	if m.deps.Applier == nil {
		m.deps.Logger.Printf("upgrade manager: applier not configured; cannot apply plan version=%s", plan.Artifact.Version)
		return nil
	}

	applyResult, err := m.deps.Applier.Apply(ctx, plan, state)
	previousVersion := state.Upgrade.Applied.Version
	state.Upgrade.Applied.LastAttempt = now

	var installResult InstallResult
	if err == nil {
		if m.installer != nil {
			installResult, err = m.installer.Install(ctx, applyResult.BinaryPath)
		} else {
			installResult.TargetPath = applyResult.BinaryPath
		}
		if installResult.TargetPath == "" {
			installResult.TargetPath = applyResult.BinaryPath
		}
	}

	if err != nil {
		state.Upgrade.Applied.LastError = err.Error()
		if m.deps.UpdateState != nil && m.cfg.DataDir != "" {
			_ = m.deps.UpdateState(ctx, m.cfg.DataDir, state)
		}
		m.report(ctx, plan, state.AgentID, previousVersion, "failed", err.Error(), map[string]any{"stage": "apply"})
		return err
	}

	state.Upgrade.Applied.Version = plan.Artifact.Version
	state.Upgrade.Applied.Path = installResult.TargetPath
	state.Upgrade.Applied.AppliedAt = applyResult.AppliedAt
	state.Upgrade.Applied.LastError = ""

	if m.deps.UpdateState != nil && m.cfg.DataDir != "" {
		if updateErr := m.deps.UpdateState(ctx, m.cfg.DataDir, state); updateErr != nil && m.deps.Logger != nil {
			m.deps.Logger.Printf("upgrade manager: failed to record apply results: %v", updateErr)
		}
	}

	details := map[string]any{
		"bundle_path":    applyResult.BundlePath,
		"binary_path":    applyResult.BinaryPath,
		"installed_path": installResult.TargetPath,
	}
	m.report(ctx, plan, state.AgentID, previousVersion, "success", fmt.Sprintf("applied %s", plan.Artifact.Version), details)

	if m.restarter != nil && installResult.TargetPath != "" {
		restartErr := m.restarter.Restart(ctx, installResult.TargetPath, m.args, m.env)
		if restartErr != nil {
			state.Upgrade.Applied.LastError = restartErr.Error()
			state.Upgrade.Applied.Version = previousVersion
			if m.installer != nil {
				if rbErr := m.installer.Rollback(ctx, installResult); rbErr != nil && m.deps.Logger != nil {
					m.deps.Logger.Printf("upgrade manager: rollback failed: %v", rbErr)
				}
			}
			if m.deps.UpdateState != nil && m.cfg.DataDir != "" {
				_ = m.deps.UpdateState(ctx, m.cfg.DataDir, state)
			}
			m.report(ctx, plan, state.AgentID, previousVersion, "failed", restartErr.Error(), map[string]any{"stage": "restart"})
			return restartErr
		}
	}

	return nil
}

func (m *Manager) report(ctx context.Context, plan Plan, agentID, previousVersion, status, message string, details map[string]any) {
	if m.deps.Reporter == nil {
		return
	}
	report := Report{
		AgentID:         agentID,
		CurrentVersion:  plan.Artifact.Version,
		PreviousVersion: previousVersion,
		Channel:         plan.Channel,
		Status:          status,
		StartedAt:       m.deps.Now().UTC(),
		CompletedAt:     m.deps.Now().UTC(),
		Message:         message,
		Details:         details,
	}
	if err := m.deps.Reporter.ReportUpgrade(ctx, report); err != nil && m.deps.Logger != nil {
		m.deps.Logger.Printf("upgrade manager: failed to report upgrade status: %v", err)
	}
}
