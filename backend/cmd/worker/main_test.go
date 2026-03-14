package main

import (
	"testing"
	"time"

	"github.com/bankai-project/backend/internal/polymarket/rtds"
)

func TestReplaceTrackedSubscriptionsRefreshesAllowlist(t *testing.T) {
	allowlist := rtds.NewCacheAllowlist(2 * time.Minute)

	err := replaceTrackedSubscriptions(nil, allowlist, []string{"yes-token", "no-token"})
	if err == nil {
		t.Fatalf("expected error when RTDS client is missing")
	}

	if !allowlist.IsAllowed("yes-token") {
		t.Fatalf("expected yes-token to be allowlisted")
	}
	if !allowlist.IsAllowed("no-token") {
		t.Fatalf("expected no-token to be allowlisted")
	}
}
