package rtds

import "testing"

func TestShouldEnqueueMessageWithoutAllowlist(t *testing.T) {
	h := NewMessageHandler(nil, nil)
	msg := []byte(`{"event_type":"price_change","market":"m1","price_changes":[{"asset_id":"a1"}]}`)
	if !h.ShouldEnqueueMessage(msg) {
		t.Fatalf("expected enqueue=true when no allowlist is set")
	}
}

func TestShouldEnqueueMessageWithAllowlist(t *testing.T) {
	h := NewMessageHandler(nil, nil)
	allow := NewCacheAllowlist(0)
	allow.Allow([]string{"a-allow"})
	h.SetCacheAllowlist(allow)

	disallowedPrice := []byte(`{"event_type":"price_change","market":"m1","price_changes":[{"asset_id":"a-block"}]}`)
	if h.ShouldEnqueueMessage(disallowedPrice) {
		t.Fatalf("expected disallowed price_change to be dropped")
	}

	allowedPrice := []byte(`{"event_type":"price_change","market":"m1","price_changes":[{"asset_id":"a-block"},{"asset_id":"a-allow"}]}`)
	if !h.ShouldEnqueueMessage(allowedPrice) {
		t.Fatalf("expected allowed price_change to be enqueued")
	}

	disallowedTrade := []byte(`{"event_type":"last_trade","asset_id":"a-block"}`)
	if !h.ShouldEnqueueMessage(disallowedTrade) {
		t.Fatalf("expected last_trade to remain enqueued for analytics paths")
	}

	allowedTrade := []byte(`{"event_type":"last_trade","asset_id":"a-allow"}`)
	if !h.ShouldEnqueueMessage(allowedTrade) {
		t.Fatalf("expected allowed last_trade to be enqueued")
	}
}

func TestShouldEnqueueMessageBatch(t *testing.T) {
	h := NewMessageHandler(nil, nil)
	allow := NewCacheAllowlist(0)
	allow.Allow([]string{"a-allow"})
	h.SetCacheAllowlist(allow)

	allowedBatch := []byte(`[
		{"event_type":"price_change","market":"m1","price_changes":[{"asset_id":"a-block"}]},
		{"event_type":"last_trade","asset_id":"a-allow"}
	]`)
	if !h.ShouldEnqueueMessage(allowedBatch) {
		t.Fatalf("expected batch with at least one allowlisted item to be enqueued")
	}

	disallowedBatch := []byte(`[
		{"event_type":"price_change","market":"m1","price_changes":[{"asset_id":"a-block"}]},
		{"event_type":"book","asset_id":"a-block"}
	]`)
	if h.ShouldEnqueueMessage(disallowedBatch) {
		t.Fatalf("expected batch without allowlisted items to be dropped")
	}
}
