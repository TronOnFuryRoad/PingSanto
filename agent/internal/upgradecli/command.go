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
	}
	return nil
}

func printableChannel(ch string) string {
	if ch == "" {
		return "(unset)"
	}
	return ch
}
