package polymarket

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/bankai-project/backend/internal/config"
)

// Integration-style smoke test. Skipped unless explicitly enabled to avoid network calls in CI/local dev.
func TestFetchOrderFilledEventsSmoke(t *testing.T) {
	if os.Getenv("RUN_POLYMARKET_SUBGRAPH_TESTS") == "" {
		t.Skip("set RUN_POLYMARKET_SUBGRAPH_TESTS=1 to exercise the Goldsky subgraph")
	}

	cfg := &config.Config{
		Polymarket: config.PolymarketConfig{
			OrderbookSubgraphURL: os.Getenv("POLYMARKET_ORDERBOOK_SUBGRAPH_URL"),
			CollateralAssetID:    os.Getenv("POLYMARKET_COLLATERAL_ASSET_ID"),
		},
	}

	client := NewSubgraphClient(cfg)
	since := time.Now().Add(-1 * time.Hour)

	events, err := client.FetchOrderFilledEvents(context.Background(), since, 2)
	if err != nil {
		t.Fatalf("FetchOrderFilledEvents failed: %v", err)
	}

	t.Logf("fetched %d orderFilledEvents since %s", len(events), since.UTC().Format(time.RFC3339))
	for i := 0; i < len(events) && i < 3; i++ {
		t.Logf("sample[%d]: id=%s ts=%d makerAsset=%s takerAsset=%s", i, events[i].ID, events[i].Timestamp, events[i].MakerAssetID, events[i].TakerAssetID)
	}
}
