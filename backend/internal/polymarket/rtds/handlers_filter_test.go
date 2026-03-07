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

func TestPriceCoalesceKey(t *testing.T) {
	h := NewMessageHandler(nil, nil)
	allow := NewCacheAllowlist(0)
	allow.Allow([]string{"a-allow"})
	h.SetCacheAllowlist(allow)

	key, isPrice := h.PriceCoalesceKey([]byte(`{"event_type":"price_change","market":"m-1","price_changes":[{"asset_id":"a-allow"}]}`))
	if !isPrice {
		t.Fatalf("expected price_change probe to be recognized")
	}
	if key != "m-1" {
		t.Fatalf("expected market key m-1, got %q", key)
	}

	key, isPrice = h.PriceCoalesceKey([]byte(`{"event_type":"price_change","market":"m-2","price_changes":[{"asset_id":"a-block"}]}`))
	if !isPrice {
		t.Fatalf("expected price_change probe to be recognized")
	}
	if key != "" {
		t.Fatalf("expected disallowed price_change to return empty key, got %q", key)
	}

	key, isPrice = h.PriceCoalesceKey([]byte(`{"event_type":"last_trade","asset_id":"a-allow"}`))
	if isPrice {
		t.Fatalf("expected non-price event to return isPrice=false, key=%q", key)
	}
}
