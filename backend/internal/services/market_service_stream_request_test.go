package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/bankai-project/backend/internal/models"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func TestRequestMarketStreamUsesMarketAssetsCacheWhenActiveCacheMiss(t *testing.T) {
	mini := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	svc := &MarketService{
		Redis: rdb,
	}

	ctx := context.Background()
	target := models.Market{
		ConditionID: "cond-stream-cache-hit",
		TokenIDYes:  "yes-stream-cache-hit",
		TokenIDNo:   "no-stream-cache-hit",
	}
	svc.cacheMarketAssets(ctx, []models.Market{
		{
			ConditionID: "cond-other",
			TokenIDYes:  "yes-other",
			TokenIDNo:   "no-other",
		},
		target,
	})

	if err := svc.RequestMarketStream(ctx, target.ConditionID); err != nil {
		t.Fatalf("request market stream: %v", err)
	}

	if ok, err := rdb.SIsMember(ctx, streamRequestTokenKey, target.TokenIDYes).Result(); err != nil {
		t.Fatalf("check yes token queued: %v", err)
	} else if !ok {
		t.Fatalf("expected yes token to be queued")
	}
	if ok, err := rdb.SIsMember(ctx, streamRequestTokenKey, target.TokenIDNo).Result(); err != nil {
		t.Fatalf("check no token queued: %v", err)
	} else if !ok {
		t.Fatalf("expected no token to be queued")
	}
}

func TestRequestMarketStreamReturnsErrMarketHasNoTokensFromAssetsCache(t *testing.T) {
	mini := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	svc := &MarketService{
		Redis: rdb,
	}

	ctx := context.Background()
	assets := []MarketAsset{
		{
			ConditionID: "cond-stream-no-tokens",
			TokenIDYes:  "",
			TokenIDNo:   "",
		},
	}
	data, err := json.Marshal(assets)
	if err != nil {
		t.Fatalf("marshal assets: %v", err)
	}
	if err := rdb.Set(ctx, CacheKeyMarketAssets, data, CacheTTL).Err(); err != nil {
		t.Fatalf("seed market assets cache: %v", err)
	}
	// Clear in-memory snapshot so this test exercises Redis cache loading path.
	svc.assetSnapshotMu.Lock()
	svc.assetSnapshot = nil
	svc.assetSnapshotAt = time.Time{}
	svc.assetSnapshotMu.Unlock()

	err = svc.RequestMarketStream(ctx, "cond-stream-no-tokens")
	if !errors.Is(err, ErrMarketHasNoTokens) {
		t.Fatalf("expected ErrMarketHasNoTokens, got %v", err)
	}
}

func TestRequestMarketStreamReturnsNotFoundWhenCachesMiss(t *testing.T) {
	mini := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	svc := &MarketService{
		Redis: rdb,
	}

	err := svc.RequestMarketStream(context.Background(), "cond-stream-missing")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected gorm.ErrRecordNotFound, got %v", err)
	}
}

func TestCacheMarketAssetsPartialSnapshotDoesNotShrinkAuthoritativeCache(t *testing.T) {
	mini := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	svc := &MarketService{
		Redis: rdb,
	}

	ctx := context.Background()
	svc.cacheFullMarketAssets(ctx, []models.Market{
		{
			ConditionID: "cond-a",
			TokenIDYes:  "yes-a",
			TokenIDNo:   "no-a",
		},
		{
			ConditionID: "cond-b",
			TokenIDYes:  "yes-b",
			TokenIDNo:   "no-b",
		},
	})

	svc.cacheMarketAssets(ctx, []models.Market{
		{
			ConditionID: "cond-a",
			TokenIDYes:  "yes-a",
			TokenIDNo:   "no-a",
		},
	})

	assets, err := svc.loadMarketAssetsFromCache(ctx)
	if err != nil {
		t.Fatalf("load market assets: %v", err)
	}
	if len(assets) != 2 {
		t.Fatalf("expected partial snapshot write to be ignored, got %d assets", len(assets))
	}
	if asset := svc.getCachedMarketAssetByConditionID(ctx, "cond-b"); asset == nil {
		t.Fatalf("expected cond-b to remain available after partial snapshot write")
	}
}

func TestCacheFullMarketAssetsAllowsShrinkOnAuthoritativeRefresh(t *testing.T) {
	mini := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	svc := &MarketService{
		Redis: rdb,
	}

	ctx := context.Background()
	svc.cacheFullMarketAssets(ctx, []models.Market{
		{
			ConditionID: "cond-a",
			TokenIDYes:  "yes-a",
			TokenIDNo:   "no-a",
		},
		{
			ConditionID: "cond-b",
			TokenIDYes:  "yes-b",
			TokenIDNo:   "no-b",
		},
	})

	svc.cacheFullMarketAssets(ctx, []models.Market{
		{
			ConditionID: "cond-a",
			TokenIDYes:  "yes-a",
			TokenIDNo:   "no-a",
		},
	})

	assets, err := svc.loadMarketAssetsFromCache(ctx)
	if err != nil {
		t.Fatalf("load market assets: %v", err)
	}
	if len(assets) != 1 {
		t.Fatalf("expected authoritative refresh to shrink cache to 1 asset, got %d", len(assets))
	}
	if strings.TrimSpace(assets[0].ConditionID) != "cond-a" {
		t.Fatalf("expected cond-a to be the remaining asset, got %q", assets[0].ConditionID)
	}
	if asset := svc.getCachedMarketAssetByConditionID(ctx, "cond-b"); asset != nil {
		t.Fatalf("expected cond-b to be removed by authoritative refresh")
	}
}
