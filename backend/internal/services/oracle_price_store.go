package services

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	ChainlinkPriceUpdateChannel = "oracle:chainlink:price_updates"

	chainlinkLatestTTL             = 24 * time.Hour
	chainlinkSnapshotTTL           = 72 * time.Hour
	chainlinkBoundaryCaptureWindow = 2 * time.Minute
)

type OraclePricePoint struct {
	Asset     string    `json:"asset"`
	Price     float64   `json:"price"`
	UpdatedAt time.Time `json:"updated_at"`
}

func CanonicalOracleAsset(raw string) string {
	trimmed := strings.TrimSpace(strings.ToUpper(raw))
	switch {
	case trimmed == "":
		return ""
	case strings.Contains(trimmed, "/"):
		parts := strings.SplitN(trimmed, "/", 2)
		return strings.TrimSpace(parts[0])
	case strings.HasSuffix(trimmed, "USDT"):
		return strings.TrimSuffix(trimmed, "USDT")
	case strings.HasSuffix(trimmed, "USD"):
		return strings.TrimSuffix(trimmed, "USD")
	default:
		return trimmed
	}
}

func chainlinkLatestKey(asset string) string {
	return "oracle:chainlink:latest:" + CanonicalOracleAsset(asset)
}

func chainlinkStartKey(asset string, start time.Time) string {
	return "oracle:chainlink:start:" + CanonicalOracleAsset(asset) + ":" + strconv.FormatInt(start.UTC().UnixMilli(), 10)
}

func chainlinkEndKey(asset string, end time.Time) string {
	return "oracle:chainlink:end:" + CanonicalOracleAsset(asset) + ":" + strconv.FormatInt(end.UTC().UnixMilli(), 10)
}

func StoreChainlinkLatest(ctx context.Context, rdb *redis.Client, asset string, price float64, updatedAt time.Time) error {
	if rdb == nil || price <= 0 {
		return nil
	}
	asset = CanonicalOracleAsset(asset)
	if asset == "" {
		return nil
	}

	key := chainlinkLatestKey(asset)
	pipe := rdb.TxPipeline()
	pipe.HSet(ctx, key, map[string]any{
		"asset":   asset,
		"price":   strconv.FormatFloat(price, 'f', -1, 64),
		"updated": updatedAt.UTC().Format(time.RFC3339Nano),
	})
	pipe.Expire(ctx, key, chainlinkLatestTTL)
	_, err := pipe.Exec(ctx)
	return err
}

func GetChainlinkLatest(ctx context.Context, rdb *redis.Client, asset string) *OraclePricePoint {
	if rdb == nil {
		return nil
	}
	key := chainlinkLatestKey(asset)
	values, err := rdb.HGetAll(ctx, key).Result()
	if err != nil || len(values) == 0 {
		return nil
	}
	price, err := strconv.ParseFloat(strings.TrimSpace(values["price"]), 64)
	if err != nil || price <= 0 {
		return nil
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(values["updated"]))
	if err != nil {
		return nil
	}
	return &OraclePricePoint{
		Asset:     CanonicalOracleAsset(values["asset"]),
		Price:     price,
		UpdatedAt: updatedAt.UTC(),
	}
}

func CaptureChainlinkStart(ctx context.Context, rdb *redis.Client, asset string, start time.Time, point OraclePricePoint) error {
	return captureChainlinkBoundarySnapshot(ctx, rdb, chainlinkStartKey(asset, start), start, point)
}

func CaptureChainlinkEnd(ctx context.Context, rdb *redis.Client, asset string, end time.Time, point OraclePricePoint) error {
	return captureChainlinkBoundarySnapshot(ctx, rdb, chainlinkEndKey(asset, end), end, point)
}

func GetChainlinkStart(ctx context.Context, rdb *redis.Client, asset string, start time.Time) *OraclePricePoint {
	return getChainlinkSnapshot(ctx, rdb, chainlinkStartKey(asset, start))
}

func GetChainlinkEnd(ctx context.Context, rdb *redis.Client, asset string, end time.Time) *OraclePricePoint {
	return getChainlinkSnapshot(ctx, rdb, chainlinkEndKey(asset, end))
}

func captureChainlinkSnapshot(ctx context.Context, rdb *redis.Client, key string, point OraclePricePoint) error {
	if rdb == nil || point.Price <= 0 || point.UpdatedAt.IsZero() {
		return nil
	}
	data, err := json.Marshal(point)
	if err != nil {
		return err
	}
	return rdb.SetNX(ctx, key, data, chainlinkSnapshotTTL).Err()
}

func captureChainlinkBoundarySnapshot(ctx context.Context, rdb *redis.Client, key string, boundary time.Time, point OraclePricePoint) error {
	if !shouldCaptureChainlinkBoundary(boundary, point.UpdatedAt) {
		return nil
	}
	if existing := getChainlinkSnapshot(ctx, rdb, key); existing != nil && !isCloserToBoundary(boundary, point.UpdatedAt, existing.UpdatedAt) {
		return nil
	}

	return captureChainlinkSnapshotForce(ctx, rdb, key, point)
}

func captureChainlinkSnapshotForce(ctx context.Context, rdb *redis.Client, key string, point OraclePricePoint) error {
	if rdb == nil || point.Price <= 0 || point.UpdatedAt.IsZero() {
		return nil
	}
	data, err := json.Marshal(point)
	if err != nil {
		return err
	}
	return rdb.Set(ctx, key, data, chainlinkSnapshotTTL).Err()
}

func shouldCaptureChainlinkBoundary(boundary, updatedAt time.Time) bool {
	if boundary.IsZero() || updatedAt.IsZero() {
		return false
	}
	updatedAt = updatedAt.UTC()
	boundary = boundary.UTC()
	delta := updatedAt.Sub(boundary)
	return delta >= 0 && delta <= chainlinkBoundaryCaptureWindow
}

func isCloserToBoundary(boundary, candidate, existing time.Time) bool {
	return candidate.UTC().Sub(boundary.UTC()) < existing.UTC().Sub(boundary.UTC())
}

func getChainlinkSnapshot(ctx context.Context, rdb *redis.Client, key string) *OraclePricePoint {
	if rdb == nil {
		return nil
	}
	data, err := rdb.Get(ctx, key).Bytes()
	if err != nil || len(data) == 0 {
		return nil
	}
	var point OraclePricePoint
	if err := json.Unmarshal(data, &point); err != nil {
		return nil
	}
	if point.Price <= 0 || point.UpdatedAt.IsZero() {
		return nil
	}
	point.Asset = CanonicalOracleAsset(point.Asset)
	point.UpdatedAt = point.UpdatedAt.UTC()
	return &point
}
