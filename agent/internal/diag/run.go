package diag

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/pingsantohq/agent/internal/config"
	"gopkg.in/yaml.v3"
)

const (
	defaultLogsDir      = "/opt/pingsanto/logs/agent"
	defaultOutputPrefix = "diag_"
	infoFileName        = "diagnostics/info.json"
	configDirName       = "config"
	stateDirName        = "state"
	logsDirName         = "logs"
	spillDirName        = "spill"
	observabilityDir    = "observability"
)

const (
	redactedMarker = "REDACTED"
)

var (
	tokenPattern       = regexp.MustCompile(`(?i)(token=)([^&\s"']+)`)
	bearerPattern      = regexp.MustCompile(`(?i)(authorization:\s*bearer\s+)([A-Za-z0-9\._\-]+)`)
	apiKeyPattern      = regexp.MustCompile(`(?i)(api[_-]?key=)([^&\s"']+)`)
	secretPattern      = regexp.MustCompile(`(?i)(secret=)([^&\s"']+)`)
	passwordPattern    = regexp.MustCompile(`(?i)(password=)([^&\s"']+)`)
	accessTokenPattern = regexp.MustCompile(`(?i)(access[_-]?token=)([^&\s"']+)`)
)

type multiValue []string

func (mv *multiValue) String() string {
	return strings.Join(*mv, ",")
}

func (mv *multiValue) Set(value string) error {
	if value == "" {
		return nil
	}
	*mv = append(*mv, value)
	return nil
}

// Dependencies provides optional overrides for testing.
type Dependencies struct {
	Now        func() time.Time
	HTTPClient *http.Client
	RunCommand func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// Run executes the diagnostics workflow, producing a tar.gz bundle.
func Run(ctx context.Context, args []string, deps Dependencies) error {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.RunCommand == nil {
		deps.RunCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			return cmd.CombinedOutput()
		}
	}

	fs := flag.NewFlagSet("diag", flag.ContinueOnError)
	configPath := fs.String("config", config.DefaultConfigPath, "Path to agent configuration file")
	dataDirFlag := fs.String("data-dir", "", "Override for agent data directory")
	outputPath := fs.String("output", "", "Path for diagnostics tarball (default /opt/pingsanto/logs/agent/diag_<ts>.tar.gz)")
	logsDir := fs.String("logs", defaultLogsDir, "Directory containing agent logs to include")
	includeSpill := fs.Bool("include-spill", true, "Include spill queue data if present")
	includeMetrics := fs.Bool("include-metrics", true, "Include metrics scrape snapshot")
	metricsURL := fs.String("metrics-url", "http://127.0.0.1:9310/metrics", "Metrics endpoint URL")
	metricsTimeout := fs.Duration("metrics-timeout", 3*time.Second, "HTTP timeout when scraping metrics")
	var journalUnits multiValue
	fs.Var(&journalUnits, "journal-unit", "Systemd unit to capture via journalctl (repeatable)")
	journalSince := fs.Duration("journal-since", time.Hour, "How far back to collect journalctl logs (e.g., 1h)")
	redactLogs := fs.Bool("redact-logs", true, "Redact sensitive tokens in log files (disable for raw capture)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	now := deps.Now().UTC()
	outPath := *outputPath
	if outPath == "" {
		outDir := defaultLogsDir
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return fmt.Errorf("ensure output directory %q: %w", outDir, err)
		}
		filename := fmt.Sprintf("%s%s.tar.gz", defaultOutputPrefix, now.Format("20060102T150405Z"))
		outPath = filepath.Join(outDir, filename)
	} else {
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return fmt.Errorf("ensure output directory %q: %w", filepath.Dir(outPath), err)
		}
	}

	info := bundleInfo{
		GeneratedAt: now.Format(time.RFC3339),
		OutputPath:  outPath,
		Warnings:    make([]string, 0, 4),
		GoVersion:   runtime.Version(),
	}

	var cfg config.Config
	cfgLoaded := false
	if parsed, err := config.Load(ctx, *configPath); err != nil {
		info.Warnings = append(info.Warnings, fmt.Sprintf("config unavailable (%s): %v", *configPath, err))
	} else {
		cfg = parsed
		cfgLoaded = true
		info.ConfigPath = *configPath
	}

	dataDir := strings.TrimSpace(*dataDirFlag)
	if dataDir == "" && cfgLoaded {
		dataDir = strings.TrimSpace(cfg.Agent.DataDir)
	}
	if dataDir == "" {
		return fmt.Errorf("agent data directory is required (provide via --data-dir or config)")
	}
	info.DataDir = dataDir

	statePath := config.StatePath(dataDir)
	state, err := loadState(statePath)
	if err != nil {
		info.Warnings = append(info.Warnings, err.Error())
	} else {
		info.AgentID = state.AgentID
		info.Server = state.Server
		info.StatePath = statePath
		info.Labels = state.Labels
		if state.Upgrade.Channel != "" || state.Upgrade.Paused {
			info.Upgrade = &upgradeSummary{Channel: state.Upgrade.Channel, Paused: state.Upgrade.Paused}
		}
	}

	outFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create diagnostics file %q: %w", outPath, err)
	}
	defer outFile.Close()

	gw := gzip.NewWriter(outFile)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	// Include config file if available
	if fi, err := os.Stat(*configPath); err == nil {
		if !fi.Mode().IsRegular() {
			info.Warnings = append(info.Warnings, fmt.Sprintf("config path %q is not a regular file", *configPath))
		} else if err := addFile(tw, *configPath, filepath.ToSlash(filepath.Join(configDirName, filepath.Base(*configPath)))); err != nil {
			info.Warnings = append(info.Warnings, fmt.Sprintf("failed to include config %q: %v", *configPath, err))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		info.Warnings = append(info.Warnings, fmt.Sprintf("unable to stat config %q: %v", *configPath, err))
	}

	// Include state file if available
	if _, err := os.Stat(statePath); err == nil {
		if err := addFile(tw, statePath, filepath.ToSlash(filepath.Join(stateDirName, filepath.Base(statePath)))); err != nil {
			info.Warnings = append(info.Warnings, fmt.Sprintf("failed to include state %q: %v", statePath, err))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		info.Warnings = append(info.Warnings, fmt.Sprintf("unable to stat state %q: %v", statePath, err))
	}

	// Include logs directory if requested
	if *logsDir != "" {
		if _, err := os.Stat(*logsDir); err == nil {
			if err := addLogsDir(tw, *logsDir, logsDirName, *redactLogs); err != nil {
				info.Warnings = append(info.Warnings, fmt.Sprintf("failed to include logs dir %q: %v", *logsDir, err))
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			info.Warnings = append(info.Warnings, fmt.Sprintf("unable to stat logs dir %q: %v", *logsDir, err))
		}
	}
	info.LogsRedacted = *redactLogs

	// Include spill information and optionally contents
	if *includeSpill {
		spillPath := filepath.Join(dataDir, "spill")
		if _, err := os.Stat(spillPath); err == nil {
			if err := summarizeSpill(spillPath, &info); err != nil {
				info.Warnings = append(info.Warnings, fmt.Sprintf("failed to summarize spill dir %q: %v", spillPath, err))
			} else if err := addDir(tw, spillPath, filepath.ToSlash(spillDirName)); err != nil {
				info.Warnings = append(info.Warnings, fmt.Sprintf("failed to include spill dir %q: %v", spillPath, err))
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			info.Warnings = append(info.Warnings, fmt.Sprintf("unable to stat spill dir %q: %v", spillPath, err))
		}
	}

	if *includeMetrics && *metricsURL != "" {
		client := deps.HTTPClient
		if client == nil {
			client = &http.Client{Timeout: *metricsTimeout}
		} else if client.Timeout == 0 && *metricsTimeout > 0 {
			client.Timeout = *metricsTimeout
		}
		scrapeCtx := ctx
		var cancel context.CancelFunc
		if *metricsTimeout > 0 {
			scrapeCtx, cancel = context.WithTimeout(ctx, *metricsTimeout)
		}
		if cancel != nil {
			defer cancel()
		}
		metricsData, err := scrapeMetrics(scrapeCtx, client, *metricsURL)
		if err != nil {
			info.Warnings = append(info.Warnings, fmt.Sprintf("metrics scrape failed: %v", err))
		} else {
			if err := addBytes(tw, metricsData, filepath.ToSlash(filepath.Join(observabilityDir, "metrics.prom"))); err != nil {
				info.Warnings = append(info.Warnings, fmt.Sprintf("failed to include metrics snapshot: %v", err))
			}
			summary, warns := summarizeMetrics(metricsData, *metricsURL)
			info.Metrics = summary
			info.Warnings = append(info.Warnings, warns...)
		}
	}

	if len(journalUnits) > 0 {
		since := deps.Now().Add(-*journalSince)
		sinceArg := since.Format(time.RFC3339)
		info.Journal = &journalSummary{
			Units: append([]string(nil), ([]string)(journalUnits)...),
			Since: sinceArg,
		}
		for _, unit := range journalUnits {
			args := []string{"--unit", unit, "--since", sinceArg, "--no-pager"}
			data, err := deps.RunCommand(ctx, "journalctl", args...)
			if err != nil {
				info.Warnings = append(info.Warnings, fmt.Sprintf("journalctl for unit %s failed: %v", unit, err))
				continue
			}
			name := filepath.ToSlash(filepath.Join(logsDirName, "journalctl", sanitizeFilename(unit)+".log"))
			if err := addBytes(tw, data, name); err != nil {
				info.Warnings = append(info.Warnings, fmt.Sprintf("failed to include journal for unit %s: %v", unit, err))
			}
		}
	}

	if err := writeInfo(tw, info); err != nil {
		return err
	}

	return nil
}

func loadState(path string) (config.State, error) {
	var zero config.State
	data, err := os.ReadFile(path)
	if err != nil {
		return zero, fmt.Errorf("unable to read state %q: %w", path, err)
	}
	var state config.State
	if err := yaml.Unmarshal(data, &state); err != nil {
		return zero, fmt.Errorf("unable to parse state %q: %w", path, err)
	}
	return state, nil
}

func writeInfo(tw *tar.Writer, info bundleInfo) error {
	payload, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal diagnostics info: %w", err)
	}
	return addBytes(tw, payload, infoFileName)
}

func addBytes(tw *tar.Writer, data []byte, name string) error {
	header := &tar.Header{
		Name:    name,
		Mode:    0o600,
		Size:    int64(len(data)),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("write tar header for %q: %w", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("write tar content for %q: %w", name, err)
	}
	return nil
}

func addFile(tw *tar.Writer, src, name string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat %q: %w", src, err)
	}
	file, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %q: %w", src, err)
	}
	defer file.Close()

	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("header for %q: %w", src, err)
	}
	header.Name = name
	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("write header for %q: %w", src, err)
	}
	if _, err := io.Copy(tw, file); err != nil {
		return fmt.Errorf("copy %q: %w", src, err)
	}
	return nil
}

func addDir(tw *tar.Writer, dir, base string) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		name := base
		if rel != "." {
			name = filepath.ToSlash(filepath.Join(base, rel))
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		if d.IsDir() {
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			if !strings.HasSuffix(name, "/") {
				name += "/"
			}
			header.Name = name
			if err := tw.WriteHeader(header); err != nil {
				return err
			}
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = name
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if _, err := io.Copy(tw, file); err != nil {
			return err
		}
		return nil
	})
}

func addLogsDir(tw *tar.Writer, dir, base string, redact bool) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		name := base
		if rel != "." {
			name = filepath.ToSlash(filepath.Join(base, rel))
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		if d.IsDir() {
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			if !strings.HasSuffix(name, "/") {
				name += "/"
			}
			header.Name = name
			if err := tw.WriteHeader(header); err != nil {
				return err
			}
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if redact && shouldRedactFile(path) {
			data = redactSensitive(path, data)
		}

		header := &tar.Header{
			Name:    name,
			Mode:    int64(info.Mode().Perm()),
			Size:    int64(len(data)),
			ModTime: info.ModTime(),
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
		return nil
	})
}

func summarizeSpill(dir string, info *bundleInfo) error {
	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var total int64
	for _, f := range files {
		fp := filepath.Join(dir, f.Name())
		stat, err := os.Stat(fp)
		if err != nil {
			return err
		}
		total += stat.Size()
	}
	info.Spill = &spillSummary{
		Path:      dir,
		FileCount: len(files),
		TotalSize: total,
	}
	return nil
}

func shouldRedactFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".log", ".txt", ".json", ".ndjson", ".yaml", ".yml", ".csv":
		return true
	default:
		return false
	}
}

func redactSensitive(path string, data []byte) []byte {
	text := string(data)
	patterns := []*regexp.Regexp{
		tokenPattern,
		bearerPattern,
		apiKeyPattern,
		secretPattern,
		passwordPattern,
		accessTokenPattern,
	}
	for _, pattern := range patterns {
		text = applyRedaction(pattern, text)
	}
	return []byte(text)
}

func applyRedaction(pattern *regexp.Regexp, text string) string {
	return pattern.ReplaceAllStringFunc(text, func(match string) string {
		sub := pattern.FindStringSubmatch(match)
		if len(sub) >= 2 {
			return sub[1] + redactedMarker
		}
		return redactedMarker
	})
}

func scrapeMetrics(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/plain")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func summarizeMetrics(data []byte, url string) (*metricsSummary, []string) {
	lines := strings.Split(string(data), "\n")
	summary := &metricsSummary{
		URL: url,
	}
	var warnings []string
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "pingsanto_agent_queue_depth_number"):
			val, err := parseMetricValue(line, "pingsanto_agent_queue_depth_number")
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("parse queue depth: %v", err))
				continue
			}
			depth := int64(val)
			summary.QueueDepth = ptrInt64(depth)
		case strings.HasPrefix(line, "pingsanto_agent_queue_dropped_total"):
			val, err := parseMetricValue(line, "pingsanto_agent_queue_dropped_total")
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("parse queue dropped: %v", err))
				continue
			}
			dropped := uint64(val)
			summary.QueueDropped = ptrUint64(dropped)
		case strings.HasPrefix(line, "pingsanto_agent_queue_spilled_total"):
			val, err := parseMetricValue(line, "pingsanto_agent_queue_spilled_total")
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("parse queue spilled: %v", err))
				continue
			}
			spilled := uint64(val)
			summary.QueueSpilled = ptrUint64(spilled)
		}
	}
	return summary, warnings
}

func parseMetricValue(line, name string) (float64, error) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0, fmt.Errorf("invalid metric line %q", line)
	}
	if fields[0] != name {
		return 0, fmt.Errorf("expected metric %s, got %s", name, fields[0])
	}
	return strconv.ParseFloat(fields[1], 64)
}

func ptrInt64(v int64) *int64 {
	return &v
}

func ptrUint64(v uint64) *uint64 {
	return &v
}

func sanitizeFilename(input string) string {
	safe := strings.ReplaceAll(input, "/", "_")
	safe = strings.ReplaceAll(safe, "..", "_")
	if safe == "" {
		return "unknown"
	}
	return safe
}

type bundleInfo struct {
	GeneratedAt  string            `json:"generated_at"`
	OutputPath   string            `json:"output_path"`
	ConfigPath   string            `json:"config_path,omitempty"`
	DataDir      string            `json:"data_dir,omitempty"`
	StatePath    string            `json:"state_path,omitempty"`
	AgentID      string            `json:"agent_id,omitempty"`
	Server       string            `json:"server,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	Spill        *spillSummary     `json:"spill,omitempty"`
	Metrics      *metricsSummary   `json:"metrics,omitempty"`
	Journal      *journalSummary   `json:"journal,omitempty"`
	LogsRedacted bool              `json:"logs_redacted"`
	Upgrade      *upgradeSummary   `json:"upgrade,omitempty"`
	Warnings     []string          `json:"warnings,omitempty"`
	GoVersion    string            `json:"go_version"`
}

type spillSummary struct {
	Path      string `json:"path"`
	FileCount int    `json:"file_count"`
	TotalSize int64  `json:"total_size_bytes"`
}

type metricsSummary struct {
	URL          string  `json:"url"`
	QueueDepth   *int64  `json:"queue_depth,omitempty"`
	QueueDropped *uint64 `json:"queue_dropped_total,omitempty"`
	QueueSpilled *uint64 `json:"queue_spilled_total,omitempty"`
}

type journalSummary struct {
	Units []string `json:"units"`
	Since string   `json:"since"`
}

type upgradeSummary struct {
	Channel string `json:"channel,omitempty"`
	Paused  bool   `json:"paused"`
}
