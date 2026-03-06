package services

import (
	"context"
	"testing"
	"time"

	"github.com/bankai-project/backend/internal/models"
	"github.com/redis/go-redis/v9"
)

func TestLoadActiveMarketsFromCacheFallsBackToInMemorySnapshot(t *testing.T) {
	t.Parallel()

	rdb := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  25 * time.Millisecond,
		ReadTimeout:  25 * time.Millisecond,
		WriteTimeout: 25 * time.Millisecond,
		PoolTimeout:  25 * time.Millisecond,
		MaxRetries:   0,
	})

	svc := &MarketService{Redis: rdb}
	end := time.Now().UTC().Add(10 * time.Minute)
	svc.storeActiveSnapshot([]models.Market{
		{
			ConditionID:     "cond-1",
			Active:          true,
			Closed:          false,
			AcceptingOrders: true,
			EndDate:         &end,
			QuestionID:      "q-1",
			Slug:            "btc-updown-1",
		},
	})

	markets, err := svc.loadActiveMarketsFromCache(context.Background())
	if err != nil {
		t.Fatalf("expected in-memory fallback, got error: %v", err)
	}
	if len(markets) != 1 {
		t.Fatalf("expected 1 market from in-memory fallback, got %d", len(markets))
	}
	if markets[0].ConditionID != "cond-1" {
		t.Fatalf("condition_id = %q, want cond-1", markets[0].ConditionID)
	}
}

func TestShouldRetryActiveMarketsCacheWriteSkipsOlderSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	svc := &MarketService{}

	svc.activeSnapshotMu.Lock()
	svc.activeSnapshotAt = now.Add(2 * time.Second)
	svc.activeSnapshotMu.Unlock()

	if svc.shouldRetryActiveMarketsCacheWrite(now) {
		t.Fatalf("expected retry guard to reject an older snapshot write")
	}
	if !svc.shouldRetryActiveMarketsCacheWrite(now.Add(2 * time.Second)) {
		t.Fatalf("expected retry guard to allow the current snapshot write")
	}
}
