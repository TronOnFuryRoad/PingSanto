package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/pingsantohq/controller/internal/artifacts"
	"github.com/pingsantohq/controller/internal/store"
)

// Config controls HTTP server settings.
type Config struct {
	Addr             string
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration
	IdleTimeout      time.Duration
	AgentAuthMode    string
	AdminBearerToken string
	PublicBaseURL    string
	ArtifactPath     string
}

// Dependencies holds external collaborators required by the server.
type Dependencies struct {
	Logger        *log.Logger
	Store         store.Store
	ArtifactStore artifacts.Store
}

// Server wraps http.Server for convenience.
type Server struct {
	*http.Server
	cfg  Config
	deps Dependencies
}

// New constructs an HTTP server with upgrade endpoints.
func New(cfg Config, deps Dependencies) *Server {
	if cfg.Addr == "" {
		cfg.Addr = ":8080"
	}
	if deps.Logger == nil {
		deps.Logger = log.New(io.Discard, "", 0)
	}
	if deps.Store == nil {
		deps.Store = store.NewMemoryStore()
	}
	if cfg.ArtifactPath == "" {
		cfg.ArtifactPath = "/artifacts"
	}
	if cfg.AgentAuthMode == "" {
		cfg.AgentAuthMode = "header"
	}
	if deps.ArtifactStore == nil {
		deps.ArtifactStore = artifacts.NewMemoryStore()
	}

	r := mux.NewRouter()
	r.HandleFunc("/api/agent/v1/upgrade/plan", planHandler(cfg, deps)).Methods(http.MethodGet)
	r.HandleFunc("/api/agent/v1/upgrade/report", reportHandler(cfg, deps)).Methods(http.MethodPost)
	r.HandleFunc("/api/admin/v1/upgrade/plan", adminUpsertPlanHandler(cfg, deps)).Methods(http.MethodPost)
	r.HandleFunc("/api/admin/v1/upgrade/history/{agent_id}", adminHistoryHandler(cfg, deps)).Methods(http.MethodGet)
	r.HandleFunc("/api/admin/v1/settings/notifications", adminGetNotificationSettingsHandler(cfg, deps)).Methods(http.MethodGet)
	r.HandleFunc("/api/admin/v1/settings/notifications", adminUpdateNotificationSettingsHandler(cfg, deps)).Methods(http.MethodPost)
	r.HandleFunc("/api/admin/v1/artifacts", adminUploadArtifactHandler(cfg, deps)).Methods(http.MethodPost)
	artifactRoute := strings.TrimRight(cfg.ArtifactPath, "/")
	if artifactRoute == "" {
		artifactRoute = "/artifacts"
	}
	r.HandleFunc(fmt.Sprintf("%s/{name}", artifactRoute), artifactDownloadHandler(cfg, deps)).Methods(http.MethodGet)
	r.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	s := &http.Server{
		Addr:         cfg.Addr,
		Handler:      r,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}
	return &Server{Server: s, cfg: cfg, deps: deps}
}

func planHandler(cfg Config, deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agentID, err := extractAgentID(r, cfg.AgentAuthMode)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		channel := r.URL.Query().Get("channel")
		plan, etag, err := deps.Store.FetchUpgradePlan(r.Context(), agentID, channel)
		if err != nil {
			if errors.Is(err, store.ErrPlanNotFound) {
				http.Error(w, "plan not found", http.StatusNotFound)
			} else {
				deps.Logger.Printf("fetch plan failed for agent %s: %v", agentID, err)
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
			return
		}

		if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", etag)
		if err := json.NewEncoder(w).Encode(plan); err != nil {
			deps.Logger.Printf("encode plan failed: %v", err)
		}
	}
}

func reportHandler(cfg Config, deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		agentID, err := extractAgentID(r, cfg.AgentAuthMode)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		var req store.UpgradeReport
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		req.AgentID = agentID

		if err := deps.Store.RecordUpgradeReport(r.Context(), req); err != nil {
			deps.Logger.Printf("record report failed for agent %s: %v", agentID, err)
			http.Error(w, "unable to record report", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func adminUpsertPlanHandler(cfg Config, deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorizeAdmin(r, cfg.AdminBearerToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req struct {
			AgentID  string         `json:"agent_id"`
			Channel  string         `json:"channel"`
			Artifact store.Artifact `json:"artifact"`
			Schedule store.Schedule `json:"schedule"`
			Paused   bool           `json:"paused"`
			Notes    string         `json:"notes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		input := store.PlanInput{
			AgentID:          req.AgentID,
			Channel:          req.Channel,
			Version:          req.Artifact.Version,
			ArtifactURL:      req.Artifact.URL,
			ArtifactSHA256:   req.Artifact.SHA256,
			SignatureURL:     req.Artifact.SignatureURL,
			ForceApply:       req.Artifact.ForceApply,
			ScheduleEarliest: req.Schedule.Earliest,
			ScheduleLatest:   req.Schedule.Latest,
			Paused:           req.Paused,
			Notes:            req.Notes,
		}

		plan, etag, err := deps.Store.UpsertUpgradePlan(r.Context(), input)
		if err != nil {
			deps.Logger.Printf("upsert plan failed: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(plan)
	}
}

func adminHistoryHandler(cfg Config, deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorizeAdmin(r, cfg.AdminBearerToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		vars := mux.Vars(r)
		agentID := vars["agent_id"]
		if agentID == "" {
			http.Error(w, "agent_id required", http.StatusBadRequest)
			return
		}
		limit := 50
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil && v > 0 {
				limit = v
			}
		}

		reports, err := deps.Store.ListUpgradeHistory(r.Context(), agentID, limit)
		if err != nil {
			deps.Logger.Printf("list history failed: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			AgentID string                `json:"agent_id"`
			Items   []store.UpgradeReport `json:"items"`
		}{AgentID: agentID, Items: reports})
	}
}

func adminGetNotificationSettingsHandler(cfg Config, deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorizeAdmin(r, cfg.AdminBearerToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		settings, err := deps.Store.GetNotificationSettings(r.Context())
		if err != nil {
			deps.Logger.Printf("get notification settings failed: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(settings)
	}
}

func adminUpdateNotificationSettingsHandler(cfg Config, deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorizeAdmin(r, cfg.AdminBearerToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req struct {
			NotifyOnPublish *bool `json:"notify_on_publish"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if req.NotifyOnPublish == nil {
			http.Error(w, "notify_on_publish is required", http.StatusBadRequest)
			return
		}
		settings, err := deps.Store.UpdateNotificationSettings(r.Context(), *req.NotifyOnPublish)
		if err != nil {
			deps.Logger.Printf("update notification settings failed: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(settings)
	}
}

func adminUploadArtifactHandler(cfg Config, deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authorizeAdmin(r, cfg.AdminBearerToken) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if deps.ArtifactStore == nil {
			http.Error(w, "artifact store not configured", http.StatusServiceUnavailable)
			return
		}
		if err := r.ParseMultipartForm(200 << 20); err != nil {
			http.Error(w, "invalid multipart form", http.StatusBadRequest)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "file field is required", http.StatusBadRequest)
			return
		}
		defer file.Close()

		req := artifacts.SaveRequest{
			Version:      r.FormValue("version"),
			Artifact:     file,
			ArtifactName: header.Filename,
		}
		req.Version = strings.TrimSpace(req.Version)
		if req.Version == "" {
			http.Error(w, "version is required", http.StatusBadRequest)
			return
		}
		if sigFile, sigHeader, err := r.FormFile("signature"); err == nil {
			req.Signature = sigFile
			req.SignatureName = sigHeader.Filename
			defer sigFile.Close()
		} else if err != nil && err != http.ErrMissingFile {
			http.Error(w, "invalid signature field", http.StatusBadRequest)
			return
		}

		start := time.Now()
		meta, err := deps.ArtifactStore.Save(r.Context(), req)
		if err != nil {
			if errors.Is(err, artifacts.ErrArtifactRequired) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			deps.Logger.Printf("save artifact failed: %v", err)
			http.Error(w, "unable to save artifact", http.StatusInternalServerError)
			return
		}
		duration := time.Since(start)
		if deps.Logger != nil {
			throughput := float64(meta.Size) / duration.Seconds() / (1024 * 1024)
			deps.Logger.Printf("admin upload: artifact=%s size=%dB duration=%s throughput=%.2fMiB/s", meta.ArtifactName, meta.Size, duration.Round(time.Millisecond), throughput)
		}

		downloadURL := buildArtifactURL(cfg, r, meta.ArtifactName)
		response := map[string]any{
			"artifact": map[string]any{
				"name":         meta.ArtifactName,
				"download_url": downloadURL,
				"sha256":       meta.SHA256,
				"size":         meta.Size,
			},
		}
		if meta.SignatureName != "" {
			response["artifact"].(map[string]any)["signature_url"] = buildArtifactURL(cfg, r, meta.SignatureName)
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			deps.Logger.Printf("encode artifact response failed: %v", err)
		}
	}
}

func artifactDownloadHandler(cfg Config, deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.ArtifactStore == nil {
			http.Error(w, "artifact store not configured", http.StatusServiceUnavailable)
			return
		}
		name := mux.Vars(r)["name"]
		reader, meta, err := deps.ArtifactStore.Open(r.Context(), name)
		if err != nil {
			if os.IsNotExist(err) {
				http.NotFound(w, r)
			} else {
				deps.Logger.Printf("artifact open failed: %v", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
			return
		}
		defer reader.Close()
		http.ServeContent(w, r, meta.ArtifactName, meta.CreatedAt, reader)
	}
}

func buildArtifactURL(cfg Config, r *http.Request, artifactName string) string {
	base := strings.TrimSpace(cfg.PublicBaseURL)
	if base == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		base = fmt.Sprintf("%s://%s", scheme, r.Host)
	}
	pathPrefix := strings.TrimRight(cfg.ArtifactPath, "/")
	if pathPrefix == "" {
		pathPrefix = "/artifacts"
	}
	return fmt.Sprintf("%s%s/%s", strings.TrimRight(base, "/"), pathPrefix, artifactName)
}

func extractAgentID(r *http.Request, mode string) (string, error) {
	switch strings.ToLower(mode) {
	case "mtls":
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			return "", errors.New("client certificate required")
		}
		return r.TLS.PeerCertificates[0].Subject.CommonName, nil
	default:
		id := r.Header.Get("X-Agent-ID")
		if strings.TrimSpace(id) == "" {
			return "", errors.New("missing X-Agent-ID header")
		}
		return id, nil
	}
}

func authorizeAdmin(r *http.Request, token string) bool {
	if strings.TrimSpace(token) == "" {
		return false
	}
	const prefix = "Bearer "
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	return strings.TrimSpace(strings.TrimPrefix(value, prefix)) == token
}
