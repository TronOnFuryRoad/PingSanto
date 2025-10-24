package store

import (
	"context"
	"testing"
)

func TestMemoryStoreChannelFallback(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	plan := PlanInput{
		Channel:        "stable",
		Version:        "1.0.1",
		ArtifactURL:    "https://example.com/pkg.tgz",
		ArtifactSHA256: "sha",
		SignatureURL:   "https://example.com/pkg.sig",
	}

	if _, _, err := store.UpsertUpgradePlan(ctx, plan); err != nil {
		t.Fatalf("UpsertUpgradePlan: %v", err)
	}

	fetched, etag, err := store.FetchUpgradePlan(ctx, "agt_123", "stable")
	if err != nil {
		t.Fatalf("FetchUpgradePlan: %v", err)
	}
	if fetched.Artifact.Version != "1.0.1" || etag == "" {
		t.Fatalf("unexpected plan: %#v etag=%q", fetched, etag)
	}
}

func TestMemoryStoreDefaultPlan(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	plan, etag, err := store.FetchUpgradePlan(ctx, "agt_abc", "")
	if err != nil {
		t.Fatalf("FetchUpgradePlan: %v", err)
	}
	if plan.AgentID != "agt_abc" || plan.Channel != "stable" || etag == "" {
		t.Fatalf("unexpected default plan: %#v etag=%q", plan, etag)
	}
}

func TestChannelPlanKey(t *testing.T) {
	if got := channelPlanKey("Stable"); got != "channel:stable" {
		t.Fatalf("unexpected key: %s", got)
	}
	if got := channelPlanKey(""); got != "channel:stable" {
		t.Fatalf("expected stable default key, got %s", got)
	}
}
