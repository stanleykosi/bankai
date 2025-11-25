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
	"math"
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

const activeWhereClause = "active = ? AND closed = ? AND accepting_orders = ?"

func (s *MarketService) loadAllActiveMarkets(ctx context.Context) ([]models.Market, error) {
	val, err := s.Redis.Get(ctx, CacheKeyActiveMarkets).Result()
	if err == nil {
		var markets []models.Market
		if err := json.Unmarshal([]byte(val), &markets); err == nil {
			return markets, nil
		}
	}

	var markets []models.Market
	if err := s.DB.WithContext(ctx).
		Where(activeWhereClause, true, false, true).
		Order("created_at DESC").Find(&markets).Error; err != nil {
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

	lanes, err := s.buildMarketLanes(ctx, params)
	if err != nil {
		return nil, err
	}

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

func (s *MarketService) queryTopMarkets(ctx context.Context, orderBy string, params MarketLaneParams) ([]models.Market, error) {
	query := s.DB.WithContext(ctx).Model(&models.Market{}).Where(activeWhereClause, true, false, true)

	if params.Category != "" {
		query = query.Where("category = ?", params.Category)
	}

	if params.Tag != "" {
		query = query.Where("? = ANY(tags)", params.Tag)
	}

	var markets []models.Market
	if err := query.Order(orderBy).Limit(20).Find(&markets).Error; err != nil {
		return nil, err
	}
	return markets, nil
}

func (s *MarketService) buildMarketLanes(ctx context.Context, params MarketLaneParams) (*MarketLanes, error) {
	fresh, err := s.queryTopMarkets(ctx, "created_at DESC", params)
	if err != nil {
		return nil, err
	}

	velocity, err := s.queryTopMarkets(ctx, "volume_all_time DESC", params)
	if err != nil {
		return nil, err
	}

	liquidity, err := s.queryTopMarkets(ctx, "liquidity DESC", params)
	if err != nil {
		return nil, err
	}

	return &MarketLanes{
		FreshDrops:    fresh,
		HighVelocity:  velocity,
		DeepLiquidity: liquidity,
	}, nil
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

	if lanes, err := s.buildMarketLanes(ctx, MarketLaneParams{}); err == nil {
		s.cacheMarketLanes(ctx, lanes)
	}
}

type QueryActiveMarketsParams struct {
	Category string
	Tag      string
	Sort     string
	Limit    int
	Offset   int
}

func (s *MarketService) QueryActiveMarkets(ctx context.Context, params QueryActiveMarketsParams) ([]models.Market, error) {
	if params.Sort == "trending" {
		return s.queryTrendingMarkets(ctx, params)
	}

	query := s.DB.WithContext(ctx).Model(&models.Market{}).Where(activeWhereClause, true, false, true)

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
	case "volume_all_time":
		query = query.Order("volume_all_time DESC")
	case "spread":
		query = query.Order("spread DESC")
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

func (s *MarketService) queryTrendingMarkets(ctx context.Context, params QueryActiveMarketsParams) ([]models.Market, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 200
	}

	candidateLimit := limit * 3
	if candidateLimit < 200 {
		candidateLimit = 200
	}

	query := s.DB.WithContext(ctx).Where(activeWhereClause, true, false, true)

	if params.Category != "" {
		query = query.Where("category = ?", params.Category)
	}

	if params.Tag != "" {
		query = query.Where("? = ANY(tags)", params.Tag)
	}

	var markets []models.Market
	if err := query.Order("volume_24h DESC").Limit(candidateLimit).Find(&markets).Error; err != nil {
		return nil, err
	}

	if len(markets) == 0 {
		return markets, nil
	}

	velocities := s.fetchVelocityScores(ctx, markets)

	type metrics struct {
		momentum float64
		liquidity float64
		velocity float64
	}

	values := make([]metrics, len(markets))

	volMin, volMax := math.MaxFloat64, 0.0
	liqMin, liqMax := math.MaxFloat64, 0.0
	velMin, velMax := math.MaxFloat64, 0.0

	for i, market := range markets {
		momentum := market.Volume24h
		if market.Volume1Week > 0 {
			momentum = market.Volume24h / market.Volume1Week
		}
		if momentum < volMin {
			volMin = momentum
		}
		if momentum > volMax {
			volMax = momentum
		}

		liquidity := market.Liquidity
		if liquidity < liqMin {
			liqMin = liquidity
		}
		if liquidity > liqMax {
			liqMax = liquidity
		}

		velocity := velocities[market.ConditionID]
		if velocity < velMin {
			velMin = velocity
		}
		if velocity > velMax {
			velMax = velocity
		}

		values[i] = metrics{momentum: momentum, liquidity: liquidity, velocity: velocity}
	}

	normalize := func(value, min, max float64) float64 {
		if max <= min {
			if max == 0 {
				return 0
			}
			return 0
		}
		norm := (value - min) / (max - min)
		if norm < 0 {
			return 0
		}
		return norm
	}

	for i := range markets {
		m := values[i]
		normMomentum := normalize(m.momentum, volMin, volMax)
		normLiquidity := normalize(m.liquidity, liqMin, liqMax)
		normVelocity := normalize(m.velocity, velMin, velMax)

		markets[i].TrendingScore = 0.5*normMomentum + 0.2*normLiquidity + 0.3*normVelocity
	}

	sort.SliceStable(markets, func(i, j int) bool {
		if markets[i].TrendingScore == markets[j].TrendingScore {
			return markets[i].Volume24h > markets[j].Volume24h
		}
		return markets[i].TrendingScore > markets[j].TrendingScore
	})

	offset := params.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= len(markets) {
		return []models.Market{}, nil
	}

	end := offset + limit
	if end > len(markets) {
		end = len(markets)
	}

	markets = markets[offset:end]
	s.attachRealtimePrices(ctx, markets)
	return markets, nil
}

func (s *MarketService) fetchVelocityScores(ctx context.Context, markets []models.Market) map[string]float64 {
	result := make(map[string]float64, len(markets))
	if len(markets) == 0 {
		return result
	}

	pipe := s.Redis.Pipeline()
	cmds := make([]*redis.FloatCmd, len(markets))
	for i, market := range markets {
		cmds[i] = pipe.ZScore(ctx, "market:velocity", market.ConditionID)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return result
	}

	for i, market := range markets {
		if score, err := cmds[i].Result(); err == nil {
			result[market.ConditionID] = score
		} else {
			result[market.ConditionID] = 0
		}
	}

	return result
}
