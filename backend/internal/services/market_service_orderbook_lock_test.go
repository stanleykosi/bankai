package services

import (
	"context"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/bankai-project/backend/internal/polymarket/clob"
	"github.com/redis/go-redis/v9"
)

func TestFetchAndCacheOrderBookWaitsForInFlightCacheWriter(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	svc := &MarketService{
		Redis:      rdb,
		ClobClient: &clob.Client{},
	}

	ctx := context.Background()
	marketID := "market-1"
	tokenID := "token-1"
	cacheKey := "book:" + marketID + ":" + tokenID
	lockKey := "lock:book:" + marketID + ":" + tokenID
	cachedPayload := `{"asset_id":"token-1","market":"market-1","bids":[{"price":"0.49","size":"10"}],"asks":[{"price":"0.51","size":"8"}]}`

	// Simulate another worker currently holding the fetch lock.
	if err := rdb.Set(ctx, lockKey, "1", 3*time.Second).Err(); err != nil {
		t.Fatalf("failed to seed lock key: %v", err)
	}

	go func() {
		time.Sleep(75 * time.Millisecond)
		_ = rdb.Set(ctx, cacheKey, cachedPayload, time.Minute).Err()
	}()

	got, err := svc.fetchAndCacheOrderBook(ctx, marketID, tokenID)
	if err != nil {
		t.Fatalf("expected to read freshly cached order book while lock is held, got error: %v", err)
	}
	if got != cachedPayload {
		t.Fatalf("expected cached payload to be returned, got %q", got)
	}
}
