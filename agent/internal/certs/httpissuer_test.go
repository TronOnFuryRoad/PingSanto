package certs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPIssuerEnroll(t *testing.T) {
	var received struct {
		Token  string            `json:"token"`
		Labels map[string]string `json:"labels"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/v1/enroll" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		resp := map[string]string{
			"agent_id":        "agt_http",
			"certificate_pem": "CERTDATA",
			"private_key_pem": "KEYDATA",
			"ca_pem":          "CADATA",
			"config_yaml":     "agent: {}\n",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	issuer := NewHTTPIssuer(server.Client())
	issuer.Path = defaultEnrollPath

	req := Request{
		Server: server.URL,
		Token:  "TOKEN",
		Labels: map[string]string{"site": "ATL-1"},
	}

	resp, err := issuer.Enroll(context.Background(), req)
	if err != nil {
		t.Fatalf("Enroll returned error: %v", err)
	}
	if resp.AgentID != "agt_http" {
		t.Fatalf("unexpected agent id %s", resp.AgentID)
	}
	if string(resp.ConfigYAML) != "agent: {}\n" {
		t.Fatalf("unexpected config yaml %q", string(resp.ConfigYAML))
	}
	if received.Token != "TOKEN" {
		t.Fatalf("expected token TOKEN got %s", received.Token)
	}
}

func TestHTTPIssuerEnrollError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	issuer := NewHTTPIssuer(server.Client())
	_, err := issuer.Enroll(context.Background(), Request{Server: server.URL, Token: "bad"})
	if err == nil {
		t.Fatalf("expected error from non-200 response")
	}
}
