/**
 * @description
 * Service layer for Market data.
 * Orchestrates fetching data from Gamma API, caching in Redis, and persisting to Postgres.
 *
 * @dependencies
 * - backend/internal/polymarket/gamma
 * - backend/internal/db
 * - backend/internal/models
 * - gorm.io/gorm
 * - github.com/redis/go-redis/v9
 */

package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"strconv"
	"time"

	"github.com/bankai-project/backend/internal/models"
	"github.com/bankai-project/backend/internal/polymarket/gamma"
	"github.com/jackc/pgconn"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	CacheKeyActiveMarkets = "markets:active"
	CacheKeyFreshDrops    = "markets:fresh"
	CacheTTL              = 5 * time.Minute

	PriceUpdateChannel = "market:price_updates"

	marketSyncLockKey = 42
)

type MarketService struct {
	DB          *gorm.DB
	Redis       *redis.Client
	GammaClient *gamma.Client
}

func NewMarketService(db *gorm.DB, redis *redis.Client, gammaClient *gamma.Client) *MarketService {
	return &MarketService{
		DB:          db,
		Redis:       redis,
		GammaClient: gammaClient,
	}
}

// SyncActiveMarkets fetches top active markets from Gamma and updates DB + Cache
func (s *MarketService) SyncActiveMarkets(ctx context.Context) error {
	active := true
	closed := false
	desc := false
	limit := 100
	offset := 0

	var allMarkets []models.Market
	dedup := make(map[string]models.Market)

	for {
		events, err := s.GammaClient.GetEvents(ctx, gamma.GetEventsParams{
			Limit:     limit,
			Offset:    offset,
			Active:    &active,
			Closed:    &closed,
			Order:     "id",
			Ascending: &desc,
		})
		if err != nil {
			return fmt.Errorf("failed to fetch events from gamma: %w", err)
		}

		if len(events) == 0 {
			break
		}

		for _, event := range events {
			for _, gm := range event.Markets {
				market := gm.ToDBModel()

				var tags []string
				for _, t := range event.Tags {
					tags = append(tags, t.Slug)
				}
				market.Tags = tags
				market.Category = "general"
				market.Archived = event.Archived

				yes, no := gamma.ParseTokenIDs(gm.ClobTokenIds)
				market.TokenIDYes = yes
				market.TokenIDNo = no

				if market.ConditionID == "" {
					continue
				}

				// Keep latest version if Gamma re-sends the same condition_id within a page
				dedup[market.ConditionID] = *market
			}
		}

		if len(events) < limit {
			break
		}
		offset += limit
	}

	if len(dedup) == 0 {
		return nil
	}

	allMarkets = allMarkets[:0]
	for _, market := range dedup {
		allMarkets = append(allMarkets, market)
	}

	unlock, lockErr := s.acquireMarketSyncLock(ctx)
	if lockErr != nil {
		return fmt.Errorf("failed to acquire market sync lock: %w", lockErr)
	}
	defer unlock()

	const maxRetries = 5
	var err error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err = s.DB.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "condition_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"volume_24h",
				"liquidity",
				"active",
				"closed",
				"archived",
				"title",
				"description",
				"resolution_rules",
				"category",
				"tags",
				"token_id_yes",
				"token_id_no",
				"end_date",
			}),
		}).CreateInBatches(allMarkets, 100).Error
		if err == nil {
			break
		}

		if pgErr, ok := err.(*pgconn.PgError); ok && (pgErr.Code == "40P01" || pgErr.Code == "40001") {
			backoff := time.Duration(attempt*100+rand.Intn(100)) * time.Millisecond
			time.Sleep(backoff)
			continue
		}
		break
	}
	if err != nil {
		return fmt.Errorf("failed to upsert markets to db: %w", err)
	}

	data, err := json.Marshal(allMarkets)
	if err != nil {
		log.Printf("Failed to marshal markets for cache: %v", err)
	} else {
		if err := s.Redis.Set(ctx, CacheKeyActiveMarkets, data, CacheTTL).Err(); err != nil {
			log.Printf("Failed to set active markets cache: %v", err)
		}
	}

	return nil
}

// GetActiveMarkets returns active markets, preferring Cache -> DB
func (s *MarketService) GetActiveMarkets(ctx context.Context) ([]models.Market, error) {
	// 1. Try Redis
	val, err := s.Redis.Get(ctx, CacheKeyActiveMarkets).Result()
	if err == nil {
		var markets []models.Market
		if err := json.Unmarshal([]byte(val), &markets); err == nil {
			s.attachRealtimePrices(ctx, markets)
			return markets, nil
		}
		// If unmarshal fails, fall through to DB
	}

	// 2. Fallback to DB
	var markets []models.Market
	if err := s.DB.Where("active = ?", true).Order("volume_24h DESC").Limit(50).Find(&markets).Error; err != nil {
		return nil, err
	}

	s.attachRealtimePrices(ctx, markets)

	return markets, nil
}

// SyncFreshDrops fetches newest markets
func (s *MarketService) SyncFreshDrops(ctx context.Context) error {
	// Fetch sorted by creation date (newest first)
	active := true
	ascending := false // Descending order (newest first)
	events, err := s.GammaClient.GetEvents(ctx, gamma.GetEventsParams{
		Limit:     20,
		Active:    &active,
		Order:     "createdAt",
		Ascending: &ascending,
	})
	if err != nil {
		return fmt.Errorf("failed to fetch fresh drops from gamma: %w", err)
	}

	var dbMarkets []models.Market
	for _, event := range events {
		for _, gm := range event.Markets {
			m := gm.ToDBModel()

			// Extract tags from event
			var tags []string
			for _, t := range event.Tags {
				tags = append(tags, t.Slug)
			}
			m.Tags = tags
			m.Category = "general"

			// Set archived status from event
			m.Archived = event.Archived

			// Parse Token IDs
			yes, no := gamma.ParseTokenIDs(gm.ClobTokenIds)
			m.TokenIDYes = yes
			m.TokenIDNo = no

			dbMarkets = append(dbMarkets, *m)
		}
	}

	if len(dbMarkets) > 0 {
		// Upsert to DB
		err = s.DB.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "condition_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"volume_24h",
				"liquidity",
				"active",
				"closed",
				"archived",
				"title",
				"description",
				"resolution_rules",
				"category",
				"tags",
				"token_id_yes",
				"token_id_no",
				"end_date",
			}),
		}).CreateInBatches(dbMarkets, 100).Error

		if err != nil {
			return fmt.Errorf("failed to upsert fresh drops to db: %w", err)
		}

		// Update Redis Cache
		data, err := json.Marshal(dbMarkets)
		if err != nil {
			log.Printf("Failed to marshal fresh drops for cache: %v", err)
		} else {
			if err := s.Redis.Set(ctx, CacheKeyFreshDrops, data, CacheTTL).Err(); err != nil {
				log.Printf("Failed to set fresh drops cache: %v", err)
			}
		}
	}

	return nil
}

// GetFreshDrops retrieves cached fresh markets
func (s *MarketService) GetFreshDrops(ctx context.Context) ([]models.Market, error) {
	// 1. Try Redis
	val, err := s.Redis.Get(ctx, CacheKeyFreshDrops).Result()
	if err == nil {
		var markets []models.Market
		if err := json.Unmarshal([]byte(val), &markets); err == nil {
			s.attachRealtimePrices(ctx, markets)
			return markets, nil
		}
		// If unmarshal fails, fall through to DB
	}

	// 2. Fallback to DB (get most recently created markets)
	var markets []models.Market
	if err := s.DB.Where("active = ?", true).Order("created_at DESC").Limit(20).Find(&markets).Error; err != nil {
		return nil, err
	}

	s.attachRealtimePrices(ctx, markets)

	return markets, nil
}

func (s *MarketService) attachRealtimePrices(ctx context.Context, markets []models.Market) {
	if len(markets) == 0 {
		return
	}

	type keyMeta struct {
		index int
		side  string
	}

	pipe := s.Redis.Pipeline()
	cmdMeta := make(map[*redis.MapStringStringCmd]keyMeta)

	for idx, market := range markets {
		if market.TokenIDYes != "" {
			cmd := pipe.HGetAll(ctx, priceRedisKey(market.ConditionID, market.TokenIDYes))
			cmdMeta[cmd] = keyMeta{index: idx, side: "yes"}
		}
		if market.TokenIDNo != "" {
			cmd := pipe.HGetAll(ctx, priceRedisKey(market.ConditionID, market.TokenIDNo))
			cmdMeta[cmd] = keyMeta{index: idx, side: "no"}
		}
	}

	if len(cmdMeta) == 0 {
		return
	}

	if _, err := pipe.Exec(ctx); err != nil {
		log.Printf("attachRealtimePrices pipeline error: %v", err)
	}

	for cmd, meta := range cmdMeta {
		result, err := cmd.Result()
		if err != nil || len(result) == 0 {
			continue
		}

		price := parseStringFloat(result["price"])
		bestBid := parseStringFloat(result["best_bid"])
		bestAsk := parseStringFloat(result["best_ask"])
		ts := parseUnixTimestamp(result["updated"])

		if meta.side == "yes" {
			markets[meta.index].YesPrice = price
			markets[meta.index].YesBestBid = bestBid
			markets[meta.index].YesBestAsk = bestAsk
			markets[meta.index].YesPriceUpdated = ts
		} else {
			markets[meta.index].NoPrice = price
			markets[meta.index].NoBestBid = bestBid
			markets[meta.index].NoBestAsk = bestAsk
			markets[meta.index].NoPriceUpdated = ts
		}
	}
}

func (s *MarketService) acquireMarketSyncLock(ctx context.Context) (func(), error) {
	const maxAttempts = 30

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var locked bool
		err := s.DB.WithContext(ctx).Raw("SELECT pg_try_advisory_lock(?)", marketSyncLockKey).Scan(&locked).Error
		if err != nil {
			return nil, err
		}
		if locked {
			return func() {
				if err := s.DB.WithContext(ctx).Exec("SELECT pg_advisory_unlock(?)", marketSyncLockKey).Error; err != nil {
					log.Printf("failed to release market sync lock: %v", err)
				}
			}, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		backoff := time.Duration(100+rand.Intn(150)) * time.Millisecond
		time.Sleep(backoff)
	}

	return nil, fmt.Errorf("timeout acquiring market sync lock")
}

func (s *MarketService) releaseMarketSyncLock(ctx context.Context) {
	if err := s.DB.WithContext(ctx).Exec("SELECT pg_advisory_unlock(?)", marketSyncLockKey).Error; err != nil {
		log.Printf("failed to release market sync lock: %v", err)
	}
}

func priceRedisKey(conditionID, tokenID string) string {
	return fmt.Sprintf("price:%s:%s", conditionID, tokenID)
}

func parseStringFloat(value string) float64 {
	if value == "" {
		return 0
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return f
}

func parseUnixTimestamp(value string) *time.Time {
	if value == "" {
		return nil
	}

	if ts, err := strconv.ParseInt(value, 10, 64); err == nil {
		t := time.Unix(0, ts*int64(time.Millisecond))
		return &t
	}

	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return &t
	}

	return nil
}

type QueryActiveMarketsParams struct {
	Category string
	Tag      string
	Sort     string
	Limit    int
	Offset   int
}

func (s *MarketService) QueryActiveMarkets(ctx context.Context, params QueryActiveMarketsParams) ([]models.Market, error) {
	query := s.DB.WithContext(ctx).Model(&models.Market{}).Where("active = ?", true)

	if params.Category != "" {
		query = query.Where("category = ?", params.Category)
	}

	if params.Tag != "" {
		query = query.Where("? = ANY(tags)", params.Tag)
	}

	switch params.Sort {
	case "liquidity":
		query = query.Order("liquidity DESC")
	case "created":
		query = query.Order("created_at DESC")
	case "volume":
		query = query.Order("volume_24h DESC")
	default:
		query = query.Order("created_at DESC")
	}

	if params.Offset < 0 {
		params.Offset = 0
	}

	if params.Limit > 0 {
		query = query.Limit(params.Limit)
	}

	if params.Offset > 0 {
		query = query.Offset(params.Offset)
	}

	var markets []models.Market
	if err := query.Find(&markets).Error; err != nil {
		return nil, err
	}

	s.attachRealtimePrices(ctx, markets)
	return markets, nil
}
