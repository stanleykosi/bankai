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
	"sort"
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
	CacheKeyMarketMeta    = "markets:meta"
	CacheKeyMarketLanes   = "markets:lanes"
	CacheKeyFreshDrops    = "markets:fresh"
	CacheTTL              = 5 * time.Minute

	PriceUpdateChannel = "market:price_updates"

	marketSyncLockKey = 42
	lanePoolCap       = 2000
)

type MarketService struct {
	DB          *gorm.DB
	Redis       *redis.Client
	GammaClient *gamma.Client
	streamHub   *PriceStreamHub
}

type MarketMeta struct {
	Total      int         `json:"total"`
	Categories []MetaEntry `json:"categories"`
	Tags       []MetaEntry `json:"tags"`
}

type MetaEntry struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

type MarketLanes struct {
	FreshDrops    []models.Market `json:"fresh"`
	HighVelocity  []models.Market `json:"high_velocity"`
	DeepLiquidity []models.Market `json:"deep_liquidity"`
}

type MarketLaneParams struct {
	Category string
	Tag      string
	PoolSort string
}

func NewMarketService(db *gorm.DB, redis *redis.Client, gammaClient *gamma.Client) *MarketService {
	return &MarketService{
		DB:          db,
		Redis:       redis,
		GammaClient: gammaClient,
		streamHub:   NewPriceStreamHub(redis, PriceUpdateChannel),
	}
}

func (s *MarketService) StreamHub() *PriceStreamHub {
	return s.streamHub
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
		s.cacheDerivedSnapshots(ctx, allMarkets)
	}

	return nil
}

func (s *MarketService) loadAllActiveMarkets(ctx context.Context) ([]models.Market, error) {
	val, err := s.Redis.Get(ctx, CacheKeyActiveMarkets).Result()
	if err == nil {
		var markets []models.Market
		if err := json.Unmarshal([]byte(val), &markets); err == nil {
			return markets, nil
		}
	}

	var markets []models.Market
	if err := s.DB.WithContext(ctx).Where("active = ?", true).Order("created_at DESC").Find(&markets).Error; err != nil {
		return nil, err
	}

	if data, err := json.Marshal(markets); err == nil {
		_ = s.Redis.Set(ctx, CacheKeyActiveMarkets, data, CacheTTL).Err()
		s.cacheDerivedSnapshots(ctx, markets)
	}

	return markets, nil
}

// GetActiveMarkets returns the full active market snapshot (without pagination).
func (s *MarketService) GetActiveMarkets(ctx context.Context) ([]models.Market, error) {
	return s.loadAllActiveMarkets(ctx)
}

// GetActiveMarketsPaged returns a slice of the cached snapshot plus the total count.
func (s *MarketService) GetActiveMarketsPaged(ctx context.Context, limit, offset int) ([]models.Market, int, error) {
	if limit <= 0 {
		limit = 500
	}
	if limit > 1000 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}

	markets, err := s.loadAllActiveMarkets(ctx)
	if err != nil {
		return nil, 0, err
	}

	total := len(markets)
	if offset >= total {
		return []models.Market{}, total, nil
	}

	end := offset + limit
	if end > total {
		end = total
	}

	page := make([]models.Market, end-offset)
	copy(page, markets[offset:end])

	s.attachRealtimePrices(ctx, page)
	return page, total, nil
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

func (s *MarketService) GetActiveMarketsMeta(ctx context.Context) (*MarketMeta, error) {
	if val, err := s.Redis.Get(ctx, CacheKeyMarketMeta).Result(); err == nil {
		var meta MarketMeta
		if err := json.Unmarshal([]byte(val), &meta); err == nil {
			return &meta, nil
		}
	}

	markets, err := s.loadAllActiveMarkets(ctx)
	if err != nil {
		return nil, err
	}

	meta := computeMarketMeta(markets)
	s.cacheMarketMeta(ctx, meta)
	return meta, nil
}

func (s *MarketService) GetMarketLanes(ctx context.Context, params MarketLaneParams) (*MarketLanes, error) {
	useCache := params.Category == "" && params.Tag == "" && (params.PoolSort == "" || params.PoolSort == "all")
	if useCache {
		if val, err := s.Redis.Get(ctx, CacheKeyMarketLanes).Result(); err == nil {
			var lanes MarketLanes
			if err := json.Unmarshal([]byte(val), &lanes); err == nil {
				s.attachRealtimePrices(ctx, lanes.FreshDrops)
				s.attachRealtimePrices(ctx, lanes.HighVelocity)
				s.attachRealtimePrices(ctx, lanes.DeepLiquidity)
				return &lanes, nil
			}
		}
	}

	markets, err := s.loadAllActiveMarkets(ctx)
	if err != nil {
		return nil, err
	}

	lanes := computeMarketLanesFromSlice(markets, params)

	if useCache {
		s.cacheMarketLanes(ctx, lanes)
	}

	s.attachRealtimePrices(ctx, lanes.FreshDrops)
	s.attachRealtimePrices(ctx, lanes.HighVelocity)
	s.attachRealtimePrices(ctx, lanes.DeepLiquidity)

	return lanes, nil
}

func buildMetaEntries(source map[string]int, limit int) []MetaEntry {
	entries := make([]MetaEntry, 0, len(source))
	for value, count := range source {
		entries = append(entries, MetaEntry{Value: value, Count: count})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Count == entries[j].Count {
			return entries[i].Value < entries[j].Value
		}
		return entries[i].Count > entries[j].Count
	})

	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}

	return entries
}

func filterMarkets(markets []models.Market, category, tag string) []models.Market {
	if category == "" && tag == "" {
		return cloneMarkets(markets)
	}

	var filtered []models.Market
	for _, market := range markets {
		if category != "" && market.Category != category {
			continue
		}
		if tag != "" {
			found := false
			for _, t := range market.Tags {
				if t == tag {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		filtered = append(filtered, market)
	}
	return filtered
}

func applyPoolSort(markets []models.Market, sortKey string) []models.Market {
	pool := cloneMarkets(markets)

	switch sortKey {
	case "volume":
		sort.SliceStable(pool, func(i, j int) bool {
			return pool[i].Volume24h > pool[j].Volume24h
		})
	case "liquidity":
		sort.SliceStable(pool, func(i, j int) bool {
			return pool[i].Liquidity > pool[j].Liquidity
		})
	case "created":
		sort.SliceStable(pool, func(i, j int) bool {
			return pool[i].CreatedAt.After(pool[j].CreatedAt)
		})
	default:
		// Leave as-is for the "all" pool
	}

	if len(pool) > lanePoolCap && sortKey != "all" {
		return pool[:lanePoolCap]
	}
	return pool
}

func selectTop(source []models.Market, comparator func(models.Market, models.Market) bool, limit int) []models.Market {
	if len(source) == 0 || limit <= 0 {
		return []models.Market{}
	}

	cp := cloneMarkets(source)
	sort.Slice(cp, func(i, j int) bool {
		return comparator(cp[i], cp[j])
	})

	if len(cp) > limit {
		cp = cp[:limit]
	}
	return cp
}

func cloneMarkets(source []models.Market) []models.Market {
	cp := make([]models.Market, len(source))
	copy(cp, source)
	return cp
}

func computeMarketMeta(markets []models.Market) *MarketMeta {
	categoryCounts := make(map[string]int)
	tagCounts := make(map[string]int)

	for _, market := range markets {
		if market.Category != "" {
			categoryCounts[market.Category]++
		}
		for _, tag := range market.Tags {
			if tag != "" {
				tagCounts[tag]++
			}
		}
	}

	return &MarketMeta{
		Total:      len(markets),
		Categories: buildMetaEntries(categoryCounts, 25),
		Tags:       buildMetaEntries(tagCounts, 75),
	}
}

func computeMarketLanesFromSlice(markets []models.Market, params MarketLaneParams) *MarketLanes {
	filtered := filterMarkets(markets, params.Category, params.Tag)
	pool := applyPoolSort(filtered, params.PoolSort)

	fresh := selectTop(pool, func(i, j models.Market) bool {
		return i.CreatedAt.After(j.CreatedAt)
	}, 20)

	velocity := selectTop(pool, func(i, j models.Market) bool {
		return i.VolumeAllTime > j.VolumeAllTime
	}, 20)

	liquidity := selectTop(pool, func(i, j models.Market) bool {
		return i.Liquidity > j.Liquidity
	}, 20)

	return &MarketLanes{
		FreshDrops:    fresh,
		HighVelocity:  velocity,
		DeepLiquidity: liquidity,
	}
}

func (s *MarketService) cacheMarketMeta(ctx context.Context, meta *MarketMeta) {
	if meta == nil {
		return
	}
	if data, err := json.Marshal(meta); err == nil {
		_ = s.Redis.Set(ctx, CacheKeyMarketMeta, data, CacheTTL).Err()
	}
}

func (s *MarketService) cacheMarketLanes(ctx context.Context, lanes *MarketLanes) {
	if lanes == nil {
		return
	}
	if data, err := json.Marshal(lanes); err == nil {
		_ = s.Redis.Set(ctx, CacheKeyMarketLanes, data, CacheTTL).Err()
	}
}

func (s *MarketService) cacheDerivedSnapshots(ctx context.Context, markets []models.Market) {
	meta := computeMarketMeta(markets)
	s.cacheMarketMeta(ctx, meta)

	lanes := computeMarketLanesFromSlice(markets, MarketLaneParams{})
	s.cacheMarketLanes(ctx, lanes)
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
