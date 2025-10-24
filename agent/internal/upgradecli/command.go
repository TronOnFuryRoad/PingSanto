package upgradecli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/pingsantohq/agent/internal/config"
)

type Dependencies struct {
	Now func() time.Time
	Out io.Writer
}

func Run(ctx context.Context, args []string, deps Dependencies) error {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Out == nil {
		deps.Out = os.Stdout
	}

	fs := flag.NewFlagSet("upgrades", flag.ContinueOnError)
	configPath := fs.String("config", config.DefaultConfigPath, "Path to agent configuration file")
	dataDirFlag := fs.String("data-dir", "", "Override for agent data directory")
	channel := fs.String("channel", "", "Set upgrade channel (stable|canary)")
	pause := fs.Bool("pause", false, "Pause automatic upgrades")
	resume := fs.Bool("resume", false, "Resume automatic upgrades")
	status := fs.Bool("status", false, "Show current upgrade state")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *pause && *resume {
		return errors.New("cannot specify both --pause and --resume")
	}

	if *channel != "" {
		normalized := strings.ToLower(*channel)
		switch normalized {
		case "stable", "canary":
			*channel = normalized
		default:
			return fmt.Errorf("invalid channel %q (allowed: stable, canary)", *channel)
		}
	}

	cfg, err := config.Load(ctx, *configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	dataDir := strings.TrimSpace(*dataDirFlag)
	if dataDir == "" {
		dataDir = strings.TrimSpace(cfg.Agent.DataDir)
	}
	if dataDir == "" {
		return fmt.Errorf("agent data directory is required (provide via --data-dir or config)")
	}

	state, err := config.LoadState(ctx, dataDir)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	modified := false
	if *channel != "" && state.Upgrade.Channel != *channel {
		state.Upgrade.Channel = *channel
		modified = true
	}

	if *pause && !state.Upgrade.Paused {
		state.Upgrade.Paused = true
		modified = true
	}
	if *resume && state.Upgrade.Paused {
		state.Upgrade.Paused = false
		modified = true
	}

	if modified {
		if state.Upgrade.Channel == "" {
			state.Upgrade.Channel = "stable"
		}
		if err := config.UpdateState(ctx, dataDir, state); err != nil {
			return fmt.Errorf("update state: %w", err)
		}
	}

	if !modified && !*status && *channel == "" && !*pause && !*resume {
		*status = true
	}

	if *status || modified {
		fmt.Fprintf(deps.Out, "Upgrade channel: %s\n", printableChannel(state.Upgrade.Channel))
		fmt.Fprintf(deps.Out, "Auto-upgrades paused: %t\n", state.Upgrade.Paused)
		writePlanStatus(deps.Out, state.Upgrade.Plan)
		writeAppliedStatus(deps.Out, state.Upgrade.Applied)
	}
	return nil
}

func printableChannel(ch string) string {
	if ch == "" {
		return "(unset)"
	}
	return ch
}

func writePlanStatus(out io.Writer, plan config.UpgradePlanState) {
	fmt.Fprintln(out, "Latest plan:")
	if plan.Version == "" {
		fmt.Fprintln(out, "  (none)")
		return
	}
	fmt.Fprintf(out, "  Version: %s (channel=%s)\n", plan.Version, printableChannel(plan.Channel))
	if plan.Source != "" {
		fmt.Fprintf(out, "  Source: %s\n", plan.Source)
	}
	if !plan.RetrievedAt.IsZero() {
		fmt.Fprintf(out, "  Retrieved at: %s\n", formatTime(plan.RetrievedAt))
	}
	if plan.ArtifactURL != "" {
		fmt.Fprintf(out, "  Artifact URL: %s\n", plan.ArtifactURL)
	}
	if plan.SignatureURL != "" {
		fmt.Fprintf(out, "  Signature URL: %s\n", plan.SignatureURL)
	}
	if plan.SHA256 != "" {
		fmt.Fprintf(out, "  SHA256: %s\n", plan.SHA256)
	}
	fmt.Fprintf(out, "  Force apply: %t\n", plan.ForceApply)
	fmt.Fprintf(out, "  Controller paused: %t\n", plan.Paused)
	if plan.Schedule.Earliest != nil {
		fmt.Fprintf(out, "  Window earliest: %s\n", formatTime(*plan.Schedule.Earliest))
	}
	if plan.Schedule.Latest != nil {
		fmt.Fprintf(out, "  Window latest: %s\n", formatTime(*plan.Schedule.Latest))
	}
	if plan.Notes != "" {
		fmt.Fprintf(out, "  Notes: %s\n", plan.Notes)
	}
}

func writeAppliedStatus(out io.Writer, applied config.UpgradeAppliedState) {
	fmt.Fprintln(out, "Applied state:")
	if applied.Version == "" {
		fmt.Fprintln(out, "  Current version: (unknown)")
	} else {
		fmt.Fprintf(out, "  Current version: %s\n", applied.Version)
	}
	if applied.Path != "" {
		fmt.Fprintf(out, "  Install path: %s\n", applied.Path)
	}
	if !applied.AppliedAt.IsZero() {
		fmt.Fprintf(out, "  Applied at: %s\n", formatTime(applied.AppliedAt))
	}
	if !applied.LastAttempt.IsZero() {
		fmt.Fprintf(out, "  Last attempt: %s\n", formatTime(applied.LastAttempt))
	}
	if applied.LastError != "" {
		fmt.Fprintf(out, "  Last error: %s\n", applied.LastError)
	} else {
		fmt.Fprintln(out, "  Last error: (none)")
	}
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}
