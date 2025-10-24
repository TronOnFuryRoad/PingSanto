package certs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const defaultEnrollPath = "/api/agent/v1/enroll"

type HTTPIssuer struct {
	Client *http.Client
	Path   string
}

func NewHTTPIssuer(client *http.Client) *HTTPIssuer {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &HTTPIssuer{Client: client, Path: defaultEnrollPath}
}

func (h *HTTPIssuer) Enroll(ctx context.Context, req Request) (*Response, error) {
	if req.Server == "" {
		return nil, fmt.Errorf("server is required")
	}
	if req.Token == "" {
		return nil, fmt.Errorf("token is required")
	}

	endpoint := strings.TrimRight(req.Server, "/") + ensurePrefix(h.Path)

	body := struct {
		Token   string            `json:"token"`
		Labels  map[string]string `json:"labels,omitempty"`
		AgentID string            `json:"agent_id,omitempty"`
	}{
		Token:   req.Token,
		Labels:  req.Labels,
		AgentID: req.AgentID,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal enrollment request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build enrollment request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", "pingsanto-agent/0.0.1")

	resp, err := h.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("perform enrollment request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("enrollment failed: status %s", resp.Status)
	}

	var payloadResp struct {
		AgentID    string `json:"agent_id"`
		CertPEM    string `json:"certificate_pem"`
		KeyPEM     string `json:"private_key_pem"`
		CAPEM      string `json:"ca_pem"`
		ConfigYAML string `json:"config_yaml"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payloadResp); err != nil {
		return nil, fmt.Errorf("decode enrollment response: %w", err)
	}

	return &Response{
		AgentID:    payloadResp.AgentID,
		CertPEM:    []byte(payloadResp.CertPEM),
		KeyPEM:     []byte(payloadResp.KeyPEM),
		CAPEM:      []byte(payloadResp.CAPEM),
		ConfigYAML: []byte(payloadResp.ConfigYAML),
	}, nil
}

func ensurePrefix(p string) string {
	if p == "" {
		return defaultEnrollPath
	}
	if strings.HasPrefix(p, "/") {
		return p
	}
	return "/" + p
}
