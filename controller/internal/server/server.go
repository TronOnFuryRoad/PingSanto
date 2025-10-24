package server

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
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
}

// Dependencies holds external collaborators required by the server.
type Dependencies struct {
	Logger *log.Logger
	Store  store.Store
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
	if cfg.AgentAuthMode == "" {
		cfg.AgentAuthMode = "header"
	}

	r := mux.NewRouter()
	r.HandleFunc("/api/agent/v1/upgrade/plan", planHandler(cfg, deps)).Methods(http.MethodGet)
	r.HandleFunc("/api/agent/v1/upgrade/report", reportHandler(cfg, deps)).Methods(http.MethodPost)
	r.HandleFunc("/api/admin/v1/upgrade/plan", adminUpsertPlanHandler(cfg, deps)).Methods(http.MethodPost)
	r.HandleFunc("/api/admin/v1/upgrade/history/{agent_id}", adminHistoryHandler(cfg, deps)).Methods(http.MethodGet)
	r.HandleFunc("/api/admin/v1/settings/notifications", adminGetNotificationSettingsHandler(cfg, deps)).Methods(http.MethodGet)
	r.HandleFunc("/api/admin/v1/settings/notifications", adminUpdateNotificationSettingsHandler(cfg, deps)).Methods(http.MethodPost)
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

		plan, etag, err := deps.Store.FetchUpgradePlan(r.Context(), agentID)
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
