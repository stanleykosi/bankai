package services

import (
	"context"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestCaptureChainlinkStartIgnoresDelayedBoundaryTick(t *testing.T) {
	t.Parallel()

	mini := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	ctx := context.Background()
	start := time.Date(2026, 3, 6, 10, 0, 0, 0, time.UTC)

	if err := CaptureChainlinkStart(ctx, rdb, "BTC", start, OraclePricePoint{
		Asset:     "BTC",
		Price:     67234.5,
		UpdatedAt: start.Add(5 * time.Minute),
	}); err != nil {
		t.Fatalf("capture start snapshot: %v", err)
	}

	if point := GetChainlinkStart(ctx, rdb, "BTC", start); point != nil {
		t.Fatalf("expected delayed boundary tick to be ignored, got %+v", point)
	}
}

func TestCaptureChainlinkStartPrefersCloserBoundaryTick(t *testing.T) {
	t.Parallel()

	mini := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	ctx := context.Background()
	start := time.Date(2026, 3, 6, 10, 0, 0, 0, time.UTC)

	if err := CaptureChainlinkStart(ctx, rdb, "BTC", start, OraclePricePoint{
		Asset:     "BTC",
		Price:     67240,
		UpdatedAt: start.Add(90 * time.Second),
	}); err != nil {
		t.Fatalf("capture initial start snapshot: %v", err)
	}
	if err := CaptureChainlinkStart(ctx, rdb, "BTC", start, OraclePricePoint{
		Asset:     "BTC",
		Price:     67234.5,
		UpdatedAt: start.Add(10 * time.Second),
	}); err != nil {
		t.Fatalf("capture closer start snapshot: %v", err)
	}

	point := GetChainlinkStart(ctx, rdb, "BTC", start)
	if point == nil {
		t.Fatal("expected stored start snapshot")
	}
	if point.Price != 67234.5 {
		t.Fatalf("expected closer boundary tick to replace earlier snapshot, got %v", point.Price)
	}
}
