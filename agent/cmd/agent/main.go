package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/pingsantohq/agent/internal/backfill"
	"github.com/pingsantohq/agent/internal/certs"
	"github.com/pingsantohq/agent/internal/config"
	"github.com/pingsantohq/agent/internal/diag"
	"github.com/pingsantohq/agent/internal/enroll"
	"github.com/pingsantohq/agent/internal/health"
	"github.com/pingsantohq/agent/internal/logging"
	"github.com/pingsantohq/agent/internal/metrics"
	"github.com/pingsantohq/agent/internal/queue"
	"github.com/pingsantohq/agent/internal/queue/persist"
	"github.com/pingsantohq/agent/internal/runtime"
	"github.com/pingsantohq/agent/internal/scheduler"
	"github.com/pingsantohq/agent/internal/upgrade"
	"github.com/pingsantohq/agent/internal/upgradecli"
	"github.com/pingsantohq/agent/internal/uplink"
	"github.com/pingsantohq/agent/internal/worker"
	"github.com/pingsantohq/agent/pkg/types"
)

const (
	defaultMetricsAddr         = "127.0.0.1:9310"
	defaultDiskCapBytes        = 2 << 30
	defaultSpillThreshold      = 0.8
	defaultMonitorSyncInterval = 15 * time.Second
)

func main() {
	ctx := context.Background()

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	var err error

	switch cmd {
	case "run":
		err = run(ctx, os.Args[2:])
	case "enroll":
		err = enroll.Run(ctx, os.Args[2:], enroll.Dependencies{})
	case "diag":
		err = diag.Run(ctx, os.Args[2:], diag.Dependencies{})
	case "upgrades":
		err = upgradecli.Run(ctx, os.Args[2:], upgradecli.Dependencies{})
	case "-h", "--help", "help":
		printUsage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "command %s failed: %v\n", cmd, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	configPath := fs.String("config", config.DefaultConfigPath, "Path to agent configuration file")

	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(ctx, *configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if cfg.Agent.DataDir == "" {
		return fmt.Errorf("agent data_dir must be configured")
	}

	if err := os.MkdirAll(cfg.Agent.DataDir, 0o700); err != nil {
		return fmt.Errorf("ensure data dir: %w", err)
	}

	state, err := config.LoadState(ctx, cfg.Agent.DataDir)
	if err != nil {
		return fmt.Errorf("load agent state: %w", err)
	}

	serverURL := cfg.Agent.Server
	if serverURL == "" {
		serverURL = state.Server
	}
	if serverURL == "" {
		return fmt.Errorf("server URL missing from config and state")
	}

	logger := logging.New()
	logger.Printf("agent starting (server=%s, data_dir=%s)", serverURL, cfg.Agent.DataDir)

	metricsStore := metrics.NewStore()

	queueCapacity := cfg.Queue.MemItemsCap
	if queueCapacity <= 0 {
		queueCapacity = 1024
	}

	monitorInterval := defaultMonitorSyncInterval
	healthChecker := health.NewChecker(metricsStore, queueCapacity, monitorInterval*3)

	opts := []runtime.Option{
		runtime.WithQueueCapacity(queueCapacity),
		runtime.WithMetricsStore(metricsStore),
	}

	upgrader := upgrade.NewManager(upgrade.Config{DataDir: cfg.Agent.DataDir}, upgrade.Dependencies{Logger: logger})
	opts = append(opts, runtime.WithUpgradeManager(upgrader))

	if cfg.Run.Workers > 0 {
		opts = append(opts, runtime.WithWorkerOptions(worker.WithWorkerCount(cfg.Run.Workers)))
	}
	if cfg.Run.TickResolution > 0 {
		opts = append(opts, runtime.WithTickResolution(cfg.Run.TickResolution))
	}

	if cfg.Queue.SpillToDisk {
		spillDir := filepath.Join(cfg.Agent.DataDir, "spill")
		diskCap, err := queue.ParseSize(cfg.Queue.DiskBytesCap, defaultDiskCapBytes)
		if err != nil {
			return fmt.Errorf("parse disk_bytes_cap: %w", err)
		}
		store, err := persist.Open(spillDir, diskCap, 64<<20)
		if err != nil {
			return fmt.Errorf("open spill store: %w", err)
		}
		opts = append(opts, runtime.WithSpill(store, defaultSpillThreshold))
		backfillCtrl := backfill.New(store, backfill.WithMetrics(metricsStore.BackfillRecorder()))
		opts = append(opts, runtime.WithBackfillController(backfillCtrl))
		defer store.Close()
	}

	rt := runtime.New(opts...)

	tlsConfig, err := certs.LoadClientTLSConfig(state.CertPath, state.KeyPath, state.CAPath, serverURL)
	if err != nil {
		return fmt.Errorf("load TLS config: %w", err)
	}

	if expiry, err := certs.ClientCertExpiry(state.CertPath); err != nil {
		logger.Printf("failed to determine certificate expiry: %v", err)
	} else {
		healthChecker.SetCertExpiry(expiry.UTC())
	}

	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:     tlsConfig,
			ForceAttemptHTTP2:   true,
			Proxy:               http.ProxyFromEnvironment,
			MaxIdleConnsPerHost: 10,
		},
	}

	uplinkClient, err := uplink.NewClient(
		uplink.Config{
			ServerURL: serverURL,
			AgentID:   state.AgentID,
			Labels:    state.Labels,
		},
		uplink.Dependencies{
			HTTPClient: httpClient,
			Metrics:    metricsStore,
			Logger:     logger,
		},
	)
	if err != nil {
		return fmt.Errorf("init uplink client: %w", err)
	}

	transmitter := rt.NewTransmitter(uplinkClient)

	runCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	wait := rt.Start(runCtx)

	grp, groupCtx := errgroup.WithContext(runCtx)

	grp.Go(func() error {
		if err := transmitter.Run(groupCtx); err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	})

	heartbeatInterval := time.Duration(cfg.Agent.HeartbeatSec) * time.Second
	if heartbeatInterval <= 0 {
		heartbeatInterval = 15 * time.Second
	}
	grp.Go(func() error {
		err := uplinkClient.RunHeartbeat(groupCtx, heartbeatInterval)
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	})

	grp.Go(func() error {
		err := runMonitorSync(groupCtx, uplinkClient, rt, logger, monitorInterval, healthChecker.ObserveMonitorSync)
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	})

	grp.Go(func() error {
		<-groupCtx.Done()
		wait()
		return nil
	})

	grp.Go(func() error {
		return serveMonitoring(groupCtx, defaultMetricsAddr, metricsStore, healthChecker, logger)
	})

	if err := grp.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		stop()
		return err
	}

	logger.Printf("agent stopped")
	return nil
}

func printUsage() {
	fmt.Println("PingSanto Agent CLI")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  pingsanto-agent run [--config /etc/pingsanto/agent.yaml]")
	fmt.Println("  pingsanto-agent enroll --server URL --token TOKEN [--labels k=v,...] [--data-dir dir] [--config-path path]")
	fmt.Println("  pingsanto-agent diag [--config path] [--data-dir dir] [--logs dir] [--output file] [--include-spill]")
	fmt.Println("  pingsanto-agent upgrades [--pause|--resume|--status] [--channel stable|canary] [--config path] [--data-dir dir]")
}

func serveMonitoring(ctx context.Context, addr string, store *metrics.Store, checker *health.Checker, logger *log.Logger) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.NewHTTPHandler(store))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if checker == nil {
			w.WriteHeader(http.StatusOK)
			return
		}
		ready, reasons := checker.Ready(time.Now().UTC())
		if !ready {
			http.Error(w, strings.Join(reasons, "; "), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Printf("metrics listening on http://%s", addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case err := <-errCh:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func runMonitorSync(ctx context.Context, client *uplink.Client, rt *runtime.Runtime, logger *log.Logger, interval time.Duration, report func(time.Time, error)) error {
	if interval <= 0 {
		interval = defaultMonitorSyncInterval
	}

	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}

	var (
		etag  string
		state map[string]scheduler.MonitorSpec
	)
	syncOnce := func() error {
		result, err := client.FetchMonitors(ctx, etag)
		timestamp := time.Now().UTC()
		if err != nil {
			if report != nil {
				report(timestamp, err)
			}
			logger.Printf("monitor sync failed: %v", err)
			return err
		}
		if report != nil {
			report(timestamp, nil)
		}
		if !result.NotModified {
			var (
				upserts int
				removed int
			)
			if result.Snapshot.Incremental {
				state, upserts, removed = applyIncrementalSnapshot(state, result.Snapshot)
			} else {
				state = snapshotToSpecMap(result.Snapshot)
				upserts = len(state)
				removed = 0
			}
			specs := specsFromState(state)
			rt.UpdateMonitors(specs)
			logger.Printf("monitor sync applied revision=%s incremental=%t upserts=%d removed=%d monitors=%d", result.Snapshot.Revision, result.Snapshot.Incremental, upserts, removed, len(specs))
		}
		if result.ETag != "" {
			etag = result.ETag
		}
		return nil
	}

	if err := syncOnce(); err != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			_ = syncOnce()
		}
	}
}

func snapshotToSpecs(snapshot types.MonitorSnapshot) []scheduler.MonitorSpec {
	specs := make([]scheduler.MonitorSpec, 0, len(snapshot.Monitors))
	for _, mon := range snapshot.Monitors {
		if spec, ok := monitorAssignmentToSpec(mon); ok {
			specs = append(specs, spec)
		}
	}
	return specs
}

func snapshotToSpecMap(snapshot types.MonitorSnapshot) map[string]scheduler.MonitorSpec {
	if len(snapshot.Monitors) == 0 {
		return map[string]scheduler.MonitorSpec{}
	}
	state := make(map[string]scheduler.MonitorSpec, len(snapshot.Monitors))
	for _, mon := range snapshot.Monitors {
		spec, ok := monitorAssignmentToSpec(mon)
		if !ok {
			continue
		}
		state[spec.MonitorID] = spec
	}
	return state
}

func specsFromState(state map[string]scheduler.MonitorSpec) []scheduler.MonitorSpec {
	if len(state) == 0 {
		return nil
	}
	specs := make([]scheduler.MonitorSpec, 0, len(state))
	for _, spec := range state {
		specs = append(specs, spec)
	}
	return specs
}

func applyIncrementalSnapshot(state map[string]scheduler.MonitorSpec, snapshot types.MonitorSnapshot) (map[string]scheduler.MonitorSpec, int, int) {
	if state == nil {
		state = make(map[string]scheduler.MonitorSpec)
	}
	var upserts, removed int
	for _, mon := range snapshot.Monitors {
		if mon.MonitorID == "" {
			continue
		}
		if spec, ok := monitorAssignmentToSpec(mon); ok {
			state[spec.MonitorID] = spec
			upserts++
			continue
		}
		if _, exists := state[mon.MonitorID]; exists {
			delete(state, mon.MonitorID)
			removed++
		}
	}
	for _, id := range snapshot.Removed {
		if id == "" {
			continue
		}
		if _, exists := state[id]; exists {
			delete(state, id)
			removed++
		}
	}
	return state, upserts, removed
}

func monitorAssignmentToSpec(mon types.MonitorAssignment) (scheduler.MonitorSpec, bool) {
	if mon.Disabled {
		return scheduler.MonitorSpec{}, false
	}
	if mon.MonitorID == "" {
		return scheduler.MonitorSpec{}, false
	}
	cadence := time.Duration(mon.CadenceMillis) * time.Millisecond
	if cadence <= 0 {
		cadence = 3 * time.Second
	}
	timeout := time.Duration(mon.TimeoutMillis) * time.Millisecond
	if timeout <= 0 {
		timeout = 1 * time.Second
	}
	spec := scheduler.MonitorSpec{
		MonitorID:     mon.MonitorID,
		Protocol:      mon.Protocol,
		Targets:       append([]string{}, mon.Targets...),
		Cadence:       cadence,
		Timeout:       timeout,
		Configuration: mon.Configuration,
	}
	return spec, true
}
