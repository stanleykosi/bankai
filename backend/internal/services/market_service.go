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
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/models"
	"github.com/bankai-project/backend/internal/polymarket/clob"
	"github.com/bankai-project/backend/internal/polymarket/gamma"
	"github.com/jackc/pgconn"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	CacheKeyActiveMarkets   = "markets:active"
	CacheKeyMarketMeta      = "markets:meta"
	CacheKeyMarketLanes     = "markets:lanes"
	CacheKeyMarketAssets    = "markets:assets"
	CacheKeyFreshDrops      = "markets:fresh"
	CacheKeyPriceHistory    = "history:%s:%s"
	streamRequestTokenKey   = "markets:stream:requested"
	streamRequestTokenTTL   = 30 * time.Minute
	streamRequestPubSubChan = "markets:stream:requests"
	CacheTTL                = 15 * time.Minute
	HistoryCacheTTL         = 5 * time.Minute

	PriceUpdateChannel = "market:price_updates"

	marketSyncLockKey = 42
	lanePoolCap       = 2000
)

var (
	ErrOrderBookUnavailable = errors.New("order book snapshot not available")
	ErrMarketHasNoTokens    = errors.New("market has no tradable tokens")
)

type MarketService struct {
	DB          *gorm.DB
	Redis       *redis.Client
	GammaClient *gamma.Client
	ClobClient  *clob.Client
	streamHub   *PriceStreamHub
}

type StreamRequestPayload struct {
	Tokens []string `json:"tokens"`
}

type depthOrderSummary struct {
	Price float64
	Size  float64
}

type orderBookSnapshot struct {
	AssetID string           `json:"asset_id,omitempty"`
	Market  string           `json:"market,omitempty"`
	Bids    []orderBookLevel `json:"bids"`
	Asks    []orderBookLevel `json:"asks"`
}

type orderBookLevel struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

type DepthLevel struct {
	Price           float64 `json:"price"`
	Available       float64 `json:"available"`
	Used            float64 `json:"used"`
	CumulativeSize  float64 `json:"cumulativeSize"`
	CumulativeValue float64 `json:"cumulativeValue"`
}

type DepthEstimate struct {
	MarketID              string       `json:"marketId"`
	TokenID               string       `json:"tokenId"`
	Side                  string       `json:"side"`
	RequestedSize         float64      `json:"requestedSize"`
	FillableSize          float64      `json:"fillableSize"`
	EstimatedAveragePrice float64      `json:"estimatedAveragePrice"`
	EstimatedTotalValue   float64      `json:"estimatedTotalValue"`
	InsufficientLiquidity bool         `json:"insufficientLiquidity"`
	Levels                []DepthLevel `json:"levels"`
}

type MarketAsset struct {
	ConditionID string  `json:"condition_id"`
	TokenIDYes  string  `json:"token_id_yes"`
	TokenIDNo   string  `json:"token_id_no"`
	Liquidity   float64 `json:"liquidity"`
	Volume24h   float64 `json:"volume_24h"`
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

func NewMarketService(db *gorm.DB, redis *redis.Client, gammaClient *gamma.Client, clobClient *clob.Client) *MarketService {
	return &MarketService{
		DB:          db,
		Redis:       redis,
		GammaClient: gammaClient,
		ClobClient:  clobClient,
		streamHub:   NewPriceStreamHub(redis, PriceUpdateChannel),
	}
}

func (s *MarketService) StreamHub() *PriceStreamHub {
	return s.streamHub
}

// GetPriceHistory proxies history requests to the Polymarket CLOB API and caches the result in Redis.
// This avoids storing large volumes of ticks locally while keeping charts responsive.
func (s *MarketService) GetPriceHistory(ctx context.Context, conditionID, rangeParam string) ([]clob.HistoryPoint, error) {
	if s.ClobClient == nil {
		return nil, errors.New("clob client not configured")
	}

	conditionID = strings.TrimSpace(conditionID)
	if conditionID == "" {
		return nil, errors.New("condition id is required")
	}

	tokenID := ""
	if cached := s.getCachedActiveMarketByConditionID(ctx, conditionID); cached != nil {
		tokenID = strings.TrimSpace(cached.TokenIDYes)
	} else {
		var market models.Market
		if err := s.DB.WithContext(ctx).
			Select("condition_id, token_id_yes").
			Where("condition_id = ?", conditionID).
			First(&market).Error; err != nil {
			return nil, err
		}
		tokenID = strings.TrimSpace(market.TokenIDYes)
	}
	if tokenID == "" {
		return []clob.HistoryPoint{}, nil
	}

	interval, fidelity := resolveHistoryWindow(rangeParam)
	cacheKey := historyCacheKey(tokenID, interval, fidelity)

	if cached, err := s.Redis.Get(ctx, cacheKey).Result(); err == nil {
		var history []clob.HistoryPoint
		if unmarshalErr := json.Unmarshal([]byte(cached), &history); unmarshalErr == nil {
			return history, nil
		}
	}

	params := clob.PriceHistoryParams{
		Market:   tokenID,
		Interval: interval,
	}
	if fidelity > 0 {
		params.Fidelity = fidelity
	}

	history, err := s.ClobClient.GetPriceHistory(ctx, params)
	if err != nil {
		log.Printf("price history fetch failed for %s (token %s, interval %s, fidelity %d): %v", conditionID, tokenID, interval, fidelity, err)
		return []clob.HistoryPoint{}, nil
	}

	if len(history) > 0 {
		if data, marshalErr := json.Marshal(history); marshalErr == nil {
			_ = s.Redis.Set(ctx, cacheKey, data, HistoryCacheTTL).Err()
		}
	}

	return history, nil
}

// SyncActiveMarkets fetches top active markets from Gamma and updates DB + Cache
func (s *MarketService) SyncActiveMarkets(ctx context.Context) error {
	return s.syncActiveMarketsCache(ctx)
}

func (s *MarketService) PersistActiveMarkets(ctx context.Context) error {
	markets, err := s.loadAllActiveMarkets(ctx)
	if err != nil {
		return err
	}

	if len(markets) == 0 {
		return nil
	}

	top := selectTopMarkets(markets, 800)

	unlock, lockErr := s.acquireMarketSyncLock(ctx)
	if lockErr != nil {
		return fmt.Errorf("failed to acquire market sync lock: %w", lockErr)
	}
	defer unlock()

	const maxRetries = 5
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
		}).CreateInBatches(top, 50).Error
		if err == nil {
			return nil
		}

		if pgErr, ok := err.(*pgconn.PgError); ok && (pgErr.Code == "40P01" || pgErr.Code == "40001") {
			backoff := time.Duration(attempt*100+rand.Intn(100)) * time.Millisecond
			time.Sleep(backoff)
			continue
		}
		break
	}

	return fmt.Errorf("failed to persist markets to db: %w", err)
}

func (s *MarketService) syncActiveMarketsCache(ctx context.Context) error {
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

func (s *MarketService) loadActiveMarketsFromCache(ctx context.Context) ([]models.Market, error) {
	val, err := s.Redis.Get(ctx, CacheKeyActiveMarkets).Result()
	if err != nil {
		return nil, err
	}

	var markets []models.Market
	if err := json.Unmarshal([]byte(val), &markets); err != nil {
		return nil, err
	}

	return filterActiveMarkets(markets, time.Now().UTC()), nil
}

func (s *MarketService) loadAllActiveMarkets(ctx context.Context) ([]models.Market, error) {
	if markets, err := s.loadActiveMarketsFromCache(ctx); err == nil {
		return markets, nil
	} else if !errors.Is(err, redis.Nil) {
		log.Printf("Active markets cache unavailable: %v", err)
	} else if s.GammaClient != nil {
		if err := s.syncActiveMarketsCache(ctx); err == nil {
			if markets, err := s.loadActiveMarketsFromCache(ctx); err == nil {
				return markets, nil
			}
		}
	}

	// Fall back to DB if cache is missing or unavailable.

	now := time.Now().UTC()

	var markets []models.Market
	if err := s.DB.WithContext(ctx).
		Where(activeWhereClause, true, false, true).
		Where("end_date IS NULL OR end_date > ?", now).
		Order("created_at DESC").Find(&markets).Error; err != nil {
		return nil, err
	}

	markets = filterActiveMarkets(markets, now)

	if data, err := json.Marshal(markets); err == nil {
		_ = s.Redis.Set(ctx, CacheKeyActiveMarkets, data, CacheTTL).Err()
		s.cacheDerivedSnapshots(ctx, markets)
	}

	return markets, nil
}

func (s *MarketService) getCachedActiveMarket(ctx context.Context, match func(models.Market) bool) *models.Market {
	markets, err := s.loadActiveMarketsFromCache(ctx)
	if errors.Is(err, redis.Nil) && s.GammaClient != nil {
		if syncErr := s.syncActiveMarketsCache(ctx); syncErr == nil {
			markets, err = s.loadActiveMarketsFromCache(ctx)
		}
	}
	if err != nil {
		return nil
	}

	for _, market := range markets {
		if match(market) {
			matched := market
			return &matched
		}
	}

	return nil
}

func (s *MarketService) getCachedActiveMarketByConditionID(ctx context.Context, conditionID string) *models.Market {
	conditionID = strings.TrimSpace(conditionID)
	if conditionID == "" {
		return nil
	}
	return s.getCachedActiveMarket(ctx, func(market models.Market) bool {
		return market.ConditionID == conditionID
	})
}

func (s *MarketService) getCachedActiveMarketBySlug(ctx context.Context, slug string) *models.Market {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil
	}
	return s.getCachedActiveMarket(ctx, func(market models.Market) bool {
		return market.Slug == slug
	})
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

// GetMarketByConditionID fetches a single market by condition_id and attaches the latest price snapshot.
func (s *MarketService) GetMarketByConditionID(ctx context.Context, conditionID string) (*models.Market, error) {
	conditionID = strings.TrimSpace(conditionID)
	if conditionID == "" {
		return nil, fmt.Errorf("condition_id is required")
	}

	if cached := s.getCachedActiveMarketByConditionID(ctx, conditionID); cached != nil {
		markets := []models.Market{*cached}
		s.attachRealtimePrices(ctx, markets)
		market := markets[0]
		return &market, nil
	}

	var market models.Market
	if err := s.DB.WithContext(ctx).Where("condition_id = ?", conditionID).First(&market).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("failed to query market: %w", err)
		}
		return nil, nil
	}

	markets := []models.Market{market}
	s.attachRealtimePrices(ctx, markets)
	market = markets[0]

	return &market, nil
}

// GetMarketBySlug fetches a single market by its slug and attaches the latest price snapshot.
func (s *MarketService) GetMarketBySlug(ctx context.Context, slug string) (*models.Market, error) {
	if slug == "" {
		return nil, fmt.Errorf("slug is required")
	}

	if cached := s.getCachedActiveMarketBySlug(ctx, slug); cached != nil {
		markets := []models.Market{*cached}
		s.attachRealtimePrices(ctx, markets)
		market := markets[0]
		return &market, nil
	}

	var market models.Market
	if err := s.DB.WithContext(ctx).Where("slug = ?", slug).First(&market).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("failed to query market: %w", err)
		}
		return nil, nil
	}

	markets := []models.Market{market}
	s.attachRealtimePrices(ctx, markets)
	market = markets[0]

	return &market, nil
}

// GetDepthEstimate returns an estimated execution summary for a requested size.
func (s *MarketService) GetDepthEstimate(ctx context.Context, marketID, tokenID, side string, size float64) (*DepthEstimate, error) {
	marketID = strings.TrimSpace(marketID)
	tokenID = strings.TrimSpace(tokenID)
	side = strings.ToUpper(strings.TrimSpace(side))

	if marketID == "" || tokenID == "" {
		return nil, fmt.Errorf("marketId and tokenId are required")
	}
	if side != "BUY" && side != "SELL" {
		return nil, fmt.Errorf("invalid side: %s", side)
	}
	if size <= 0 {
		return nil, fmt.Errorf("size must be greater than zero")
	}

	key := fmt.Sprintf("book:%s:%s", marketID, tokenID)
	raw, err := s.Redis.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		// Ask RTDS worker to (re)subscribe in case the book never landed.
		s.publishStreamRequest(ctx, []string{tokenID})
		// Try a synchronous fetch from CLOB as a fallback.
		if fetched, fetchErr := s.fetchAndCacheOrderBook(ctx, marketID, tokenID); fetchErr == nil {
			raw = fetched
		} else {
			return nil, ErrOrderBookUnavailable
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read order book snapshot: %w", err)
	}

	var snapshot orderBookSnapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return nil, fmt.Errorf("failed to decode order book snapshot: %w", err)
	}

	source := snapshot.Asks
	if side == "SELL" {
		source = snapshot.Bids
	}
	if len(source) == 0 {
		return nil, ErrOrderBookUnavailable
	}

	levels := make([]depthOrderSummary, 0, len(source))
	for _, lvl := range source {
		price, err := strconv.ParseFloat(lvl.Price, 64)
		if err != nil || price <= 0 {
			continue
		}
		sizeFloat, err := strconv.ParseFloat(lvl.Size, 64)
		if err != nil || sizeFloat <= 0 {
			continue
		}
		levels = append(levels, depthOrderSummary{
			Price: price,
			Size:  sizeFloat,
		})
	}

	if len(levels) == 0 {
		return nil, ErrOrderBookUnavailable
	}

	sort.Slice(levels, func(i, j int) bool {
		if side == "BUY" {
			return levels[i].Price < levels[j].Price
		}
		return levels[i].Price > levels[j].Price
	})

	remaining := size
	var cumulativeSize float64
	var cumulativeValue float64
	resultLevels := make([]DepthLevel, 0, len(levels))

	for _, lvl := range levels {
		if remaining <= 0 {
			break
		}
		use := math.Min(lvl.Size, remaining)
		if use <= 0 {
			continue
		}
		cumulativeSize += use
		cumulativeValue += use * lvl.Price
		resultLevels = append(resultLevels, DepthLevel{
			Price:           lvl.Price,
			Available:       lvl.Size,
			Used:            use,
			CumulativeSize:  cumulativeSize,
			CumulativeValue: cumulativeValue,
		})
		remaining -= use
	}

	if len(resultLevels) == 0 {
		return nil, ErrOrderBookUnavailable
	}

	fillable := cumulativeSize
	avgPrice := 0.0
	if fillable > 0 {
		avgPrice = cumulativeValue / fillable
	}

	estimate := &DepthEstimate{
		MarketID:              marketID,
		TokenID:               tokenID,
		Side:                  side,
		RequestedSize:         size,
		FillableSize:          fillable,
		EstimatedAveragePrice: avgPrice,
		EstimatedTotalValue:   cumulativeValue,
		InsufficientLiquidity: fillable+1e-9 < size,
		Levels:                resultLevels,
	}

	return estimate, nil
}

// fetchAndCacheOrderBook pulls a book snapshot from the CLOB API when RTDS hasn't provided one yet.
func (s *MarketService) fetchAndCacheOrderBook(ctx context.Context, marketID, tokenID string) (string, error) {
	if s.ClobClient == nil {
		return "", errors.New("clob client not configured")
	}

	book, err := s.ClobClient.GetBook(ctx, tokenID)
	if err != nil {
		return "", err
	}

	snapshot := orderBookSnapshot{
		AssetID: tokenID,
		Market:  marketID,
		Bids:    nil,
		Asks:    nil,
	}

	for _, bid := range book.Bids {
		snapshot.Bids = append(snapshot.Bids, orderBookLevel{
			Price: bid.Price,
			Size:  bid.Size,
		})
	}
	for _, ask := range book.Asks {
		snapshot.Asks = append(snapshot.Asks, orderBookLevel{
			Price: ask.Price,
			Size:  ask.Size,
		})
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}

	key := fmt.Sprintf("book:%s:%s", marketID, tokenID)
	if err := s.Redis.Set(ctx, key, data, 15*time.Minute).Err(); err != nil {
		return "", err
	}

	return string(data), nil
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

		bestBid := parseStringFloat(result["best_bid"])
		bestAsk := parseStringFloat(result["best_ask"])
		lastTradePrice := parseStringFloat(result["last_trade_price"])
		ts := parseUnixTimestamp(result["updated"])

		if meta.side == "yes" {
			markets[meta.index].YesPrice = lastTradePrice
			markets[meta.index].YesBestBid = bestBid
			markets[meta.index].YesBestAsk = bestAsk
			markets[meta.index].YesPriceUpdated = ts
		} else {
			markets[meta.index].NoPrice = lastTradePrice
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

func historyCacheKey(tokenID string, interval clob.PriceHistoryInterval, fidelity int) string {
	base := fmt.Sprintf(CacheKeyPriceHistory, tokenID, string(interval))
	if fidelity > 0 {
		return fmt.Sprintf("%s:f%d", base, fidelity)
	}
	return base
}

func resolveHistoryWindow(rangeParam string) (clob.PriceHistoryInterval, int) {
	switch strings.ToLower(strings.TrimSpace(rangeParam)) {
	case "1h", "1hr", "1hour":
		return clob.PriceHistoryInterval1h, 1
	case "6h", "6hr", "6hour":
		return clob.PriceHistoryInterval6h, 3
	case "1w", "1week", "7d":
		return clob.PriceHistoryInterval1w, 10
	case "1m", "30d", "month":
		return clob.PriceHistoryIntervalMax, 0
	case "max", "all":
		return clob.PriceHistoryIntervalMax, 0
	case "1d", "24h", "day", "":
		return clob.PriceHistoryInterval1d, 5
	default:
		return clob.PriceHistoryInterval1d, 5
	}
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
				filterMarketLanes(&lanes, time.Now().UTC())
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

	lanes, err := s.buildMarketLanesFromSnapshot(ctx, markets, params)
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

func (s *MarketService) buildMarketLanesFromSnapshot(ctx context.Context, markets []models.Market, params MarketLaneParams) (*MarketLanes, error) {
	filtered := filterMarketsForParams(markets, params.Category, params.Tag)
	if len(filtered) == 0 {
		return &MarketLanes{}, nil
	}

	fresh := topMarkets(filtered, 20, func(a, b models.Market) bool {
		return marketCreatedAt(a).After(marketCreatedAt(b))
	})

	velocity := topMarkets(filtered, 20, func(a, b models.Market) bool {
		if a.VolumeAllTime == b.VolumeAllTime {
			return a.Volume24h > b.Volume24h
		}
		return a.VolumeAllTime > b.VolumeAllTime
	})

	liquidity := topMarkets(filtered, 20, func(a, b models.Market) bool {
		if a.Liquidity == b.Liquidity {
			return a.Volume24h > b.Volume24h
		}
		return a.Liquidity > b.Liquidity
	})

	return &MarketLanes{
		FreshDrops:    fresh,
		HighVelocity:  velocity,
		DeepLiquidity: liquidity,
	}, nil
}

func filterActiveMarkets(markets []models.Market, now time.Time) []models.Market {
	if len(markets) == 0 {
		return []models.Market{}
	}

	result := make([]models.Market, 0, len(markets))
	for _, market := range markets {
		if !isMarketActive(market, now) {
			continue
		}
		result = append(result, market)
	}
	return result
}

func marketCreatedAt(market models.Market) time.Time {
	if market.MarketCreatedAt != nil && !market.MarketCreatedAt.IsZero() {
		return *market.MarketCreatedAt
	}
	return market.CreatedAt
}

func filterMarketLanes(lanes *MarketLanes, now time.Time) {
	if lanes == nil {
		return
	}
	lanes.FreshDrops = filterActiveMarkets(lanes.FreshDrops, now)
	lanes.HighVelocity = filterActiveMarkets(lanes.HighVelocity, now)
	lanes.DeepLiquidity = filterActiveMarkets(lanes.DeepLiquidity, now)
}

func isMarketActive(market models.Market, now time.Time) bool {
	if !market.Active || market.Closed || !market.AcceptingOrders {
		return false
	}
	if market.EndDate != nil && !market.EndDate.After(now) {
		return false
	}
	return true
}

func filterMarketsForParams(markets []models.Market, category, tag string) []models.Market {
	now := time.Now().UTC()

	tag = strings.TrimSpace(tag)
	category = strings.TrimSpace(category)

	result := make([]models.Market, 0, len(markets))
	for _, market := range markets {
		if !isMarketActive(market, now) {
			continue
		}
		if category != "" && !strings.EqualFold(market.Category, category) {
			continue
		}
		if tag != "" && !containsTag(market.Tags, tag) {
			continue
		}
		result = append(result, market)
	}
	return result
}

func containsTag(tags []string, needle string) bool {
	for _, tag := range tags {
		if strings.EqualFold(tag, needle) {
			return true
		}
	}
	return false
}

func topMarkets(source []models.Market, limit int, compare func(a, b models.Market) bool) []models.Market {
	if limit <= 0 || len(source) == 0 {
		return []models.Market{}
	}

	work := make([]models.Market, len(source))
	copy(work, source)
	sort.SliceStable(work, func(i, j int) bool {
		return compare(work[i], work[j])
	})

	if len(work) > limit {
		work = work[:limit]
	}

	result := make([]models.Market, len(work))
	copy(result, work)
	return result
}

func sortMarketsByParam(markets []models.Market, sortKey string) {
	switch sortKey {
	case "liquidity":
		sort.SliceStable(markets, func(i, j int) bool {
			if markets[i].Liquidity == markets[j].Liquidity {
				return markets[i].Volume24h > markets[j].Volume24h
			}
			return markets[i].Liquidity > markets[j].Liquidity
		})
	case "volume_all_time":
		sort.SliceStable(markets, func(i, j int) bool {
			return markets[i].VolumeAllTime > markets[j].VolumeAllTime
		})
	case "spread":
		sort.SliceStable(markets, func(i, j int) bool {
			return markets[i].Spread > markets[j].Spread
		})
	case "volume":
		sort.SliceStable(markets, func(i, j int) bool {
			return markets[i].Volume24h > markets[j].Volume24h
		})
	case "created":
		sort.SliceStable(markets, func(i, j int) bool {
			return marketCreatedAt(markets[i]).After(marketCreatedAt(markets[j]))
		})
	default:
		sort.SliceStable(markets, func(i, j int) bool {
			return marketCreatedAt(markets[i]).After(marketCreatedAt(markets[j]))
		})
	}
}

func (s *MarketService) rankTrendingMarkets(ctx context.Context, markets []models.Market) {
	if len(markets) == 0 {
		return
	}

	velocities := s.fetchVelocityScores(ctx, markets)

	type metrics struct {
		momentum  float64
		liquidity float64
		velocity  float64
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

func (s *MarketService) cacheMarketAssets(ctx context.Context, markets []models.Market) {
	if len(markets) == 0 {
		return
	}

	assets := make([]MarketAsset, 0, len(markets))
	for _, market := range markets {
		if market.TokenIDYes == "" && market.TokenIDNo == "" {
			continue
		}
		assets = append(assets, MarketAsset{
			ConditionID: market.ConditionID,
			TokenIDYes:  market.TokenIDYes,
			TokenIDNo:   market.TokenIDNo,
			Liquidity:   market.Liquidity,
			Volume24h:   market.Volume24h,
		})
	}

	if len(assets) == 0 {
		return
	}

	if data, err := json.Marshal(assets); err == nil {
		_ = s.Redis.Set(ctx, CacheKeyMarketAssets, data, CacheTTL).Err()
	}
}

func (s *MarketService) cacheDerivedSnapshots(ctx context.Context, markets []models.Market) {
	meta := computeMarketMeta(markets)
	s.cacheMarketMeta(ctx, meta)

	s.cacheMarketAssets(ctx, markets)

	if lanes, err := s.buildMarketLanesFromSnapshot(ctx, markets, MarketLaneParams{}); err == nil {
		s.cacheMarketLanes(ctx, lanes)
	}
}

func (s *MarketService) GetMarketAssets(ctx context.Context, maxCount int) ([]MarketAsset, error) {
	var assets []MarketAsset
	if val, err := s.Redis.Get(ctx, CacheKeyMarketAssets).Result(); err == nil {
		if err := json.Unmarshal([]byte(val), &assets); err == nil {
			return trimMarketAssets(assets, maxCount), nil
		}
	}

	markets, err := s.loadAllActiveMarkets(ctx)
	if err != nil {
		return nil, err
	}

	assets = make([]MarketAsset, 0, len(markets))
	for _, market := range markets {
		if market.TokenIDYes == "" && market.TokenIDNo == "" {
			continue
		}
		assets = append(assets, MarketAsset{
			ConditionID: market.ConditionID,
			TokenIDYes:  market.TokenIDYes,
			TokenIDNo:   market.TokenIDNo,
			Liquidity:   market.Liquidity,
			Volume24h:   market.Volume24h,
		})
	}

	s.cacheMarketAssets(ctx, markets)
	return trimMarketAssets(assets, maxCount), nil
}

func trimMarketAssets(assets []MarketAsset, maxCount int) []MarketAsset {
	if maxCount <= 0 || len(assets) <= maxCount {
		return assets
	}

	sort.SliceStable(assets, func(i, j int) bool {
		if assets[i].Liquidity == assets[j].Liquidity {
			return assets[i].Volume24h > assets[j].Volume24h
		}
		return assets[i].Liquidity > assets[j].Liquidity
	})

	return assets[:maxCount]
}

func selectTopMarkets(markets []models.Market, max int) []models.Market {
	if max <= 0 || len(markets) <= max {
		return markets
	}

	sorted := make([]models.Market, len(markets))
	copy(sorted, markets)

	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Liquidity == sorted[j].Liquidity {
			return sorted[i].Volume24h > sorted[j].Volume24h
		}
		return sorted[i].Liquidity > sorted[j].Liquidity
	})

	return sorted[:max]
}

func (s *MarketService) SubscribeStreamRequests(ctx context.Context) *redis.PubSub {
	return s.Redis.Subscribe(ctx, streamRequestPubSubChan)
}

func (s *MarketService) publishStreamRequest(ctx context.Context, tokens []string) {
	if len(tokens) == 0 {
		return
	}
	payload := StreamRequestPayload{Tokens: tokens}
	if data, err := json.Marshal(payload); err == nil {
		_ = s.Redis.Publish(ctx, streamRequestPubSubChan, data).Err()
	}
}

func (s *MarketService) RequestMarketStream(ctx context.Context, conditionID string) error {
	conditionID = strings.TrimSpace(conditionID)
	if conditionID == "" {
		return errors.New("condition id is required")
	}

	var market models.Market
	if cached := s.getCachedActiveMarketByConditionID(ctx, conditionID); cached != nil {
		market = *cached
	} else {
		if err := s.DB.WithContext(ctx).Where("condition_id = ?", conditionID).First(&market).Error; err != nil {
			return err
		}
	}

	tokenValues := make([]string, 0, 2)
	if market.TokenIDYes != "" {
		tokenValues = append(tokenValues, market.TokenIDYes)
	}
	if market.TokenIDNo != "" {
		tokenValues = append(tokenValues, market.TokenIDNo)
	}

	if len(tokenValues) == 0 {
		log.Printf("RequestMarketStream: market %s has no token IDs; cannot subscribe", conditionID)
		return ErrMarketHasNoTokens
	}

	args := make([]interface{}, len(tokenValues))
	for i, token := range tokenValues {
		args[i] = token
	}

	if err := s.Redis.SAdd(ctx, streamRequestTokenKey, args...).Err(); err != nil {
		return fmt.Errorf("failed to queue stream tokens: %w", err)
	}
	_ = s.Redis.Expire(ctx, streamRequestTokenKey, streamRequestTokenTTL).Err()

	s.publishStreamRequest(ctx, tokenValues)
	return nil
}

func (s *MarketService) PopRequestedStreamTokens(ctx context.Context, max int) ([]string, error) {
	if max <= 0 {
		max = 50
	}

	tokens, err := s.Redis.SPopN(ctx, streamRequestTokenKey, int64(max)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return tokens, nil
}

type QueryActiveMarketsParams struct {
	Category string
	Tag      string
	Sort     string
	Limit    int
	Offset   int
}

func (s *MarketService) QueryActiveMarkets(ctx context.Context, params QueryActiveMarketsParams) ([]models.Market, error) {
	markets, err := s.loadAllActiveMarkets(ctx)
	if err != nil {
		return nil, err
	}

	filtered := filterMarketsForParams(markets, params.Category, params.Tag)
	if len(filtered) == 0 {
		return []models.Market{}, nil
	}

	if params.Limit <= 0 {
		params.Limit = 200
	}

	if params.Sort == "trending" {
		candidateLimit := params.Limit * 3
		if candidateLimit < 200 {
			candidateLimit = 200
		}
		if candidateLimit > len(filtered) {
			candidateLimit = len(filtered)
		}

		sort.SliceStable(filtered, func(i, j int) bool {
			return filtered[i].Volume24h > filtered[j].Volume24h
		})
		filtered = filtered[:candidateLimit]
		s.rankTrendingMarkets(ctx, filtered)
	} else {
		sortMarketsByParam(filtered, params.Sort)
	}

	if params.Offset < 0 {
		params.Offset = 0
	}
	if params.Offset >= len(filtered) {
		return []models.Market{}, nil
	}

	end := params.Offset + params.Limit
	if end > len(filtered) {
		end = len(filtered)
	}

	page := make([]models.Market, end-params.Offset)
	copy(page, filtered[params.Offset:end])

	s.attachRealtimePrices(ctx, page)
	return page, nil
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
