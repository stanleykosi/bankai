/**
 * @description
 * Profile Service for trader data aggregation.
 * Fetches and combines data from Polymarket Data API and Gamma.
 * Uses Redis for caching to improve performance.
 *
 * @dependencies
 * - backend/internal/polymarket/data_api
 * - backend/internal/polymarket/gamma
 * - github.com/redis/go-redis/v9
 */

package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/polymarket/clob"
	"github.com/bankai-project/backend/internal/polymarket/data_api"
	"github.com/bankai-project/backend/internal/polymarket/gamma"
	"github.com/redis/go-redis/v9"
)

// Cache TTLs for different data types
const (
	ProfileCacheTTL   = 5 * time.Minute  // Profile info changes rarely
	StatsCacheTTL     = 2 * time.Minute  // Stats update with trades
	PositionsCacheTTL = 1 * time.Minute  // Positions change more frequently
	ActivityCacheTTL  = 10 * time.Minute // Activity heatmap is historical
	TradesCacheTTL    = 1 * time.Minute  // Recent trades
	HoldersCacheTTL   = 5 * time.Minute  // Holders don't change often
)

// ProfileService handles trader profile operations
type ProfileService struct {
	dataAPIClient *data_api.Client
	gammaClient   *gamma.Client
	clobClient    *clob.Client
	redis         *redis.Client
}

// NewProfileService creates a new ProfileService
func NewProfileService(dataAPIClient *data_api.Client, gammaClient *gamma.Client, clobClient *clob.Client, rdb *redis.Client) *ProfileService {
	return &ProfileService{
		dataAPIClient: dataAPIClient,
		gammaClient:   gammaClient,
		clobClient:    clobClient,
		redis:         rdb,
	}
}

// cacheKey generates a Redis cache key
func cacheKey(prefix, address string) string {
	return fmt.Sprintf("profile:%s:%s", prefix, strings.ToLower(address))
}

func normalizeAddress(address string) string {
	return strings.ToLower(strings.TrimSpace(address))
}

func matchProfile(address string, profiles []gamma.Profile) *gamma.Profile {
	normalized := normalizeAddress(address)
	for i := range profiles {
		if profiles[i].ProxyWallet != "" && strings.EqualFold(profiles[i].ProxyWallet, normalized) {
			return &profiles[i]
		}
		if profiles[i].BaseAddress != "" && strings.EqualFold(profiles[i].BaseAddress, normalized) {
			return &profiles[i]
		}
	}
	return nil
}

func (s *ProfileService) resolveProfileAddress(ctx context.Context, address string) string {
	normalized := normalizeAddress(address)
	if normalized == "" || s.gammaClient == nil {
		return normalized
	}

	profiles, err := s.gammaClient.SearchProfiles(ctx, normalized, 5)
	if err != nil || len(profiles) == 0 {
		return normalized
	}

	if match := matchProfile(normalized, profiles); match != nil && match.ProxyWallet != "" {
		return strings.ToLower(match.ProxyWallet)
	}

	return normalized
}

// getFromCache attempts to get data from Redis cache
func getFromCache[T any](ctx context.Context, rdb *redis.Client, key string) (*T, error) {
	if rdb == nil {
		return nil, nil
	}
	
	data, err := rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil // Cache miss
	}
	if err != nil {
		return nil, err
	}
	
	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// setInCache stores data in Redis cache with TTL
func setInCache(ctx context.Context, rdb *redis.Client, key string, data interface{}, ttl time.Duration) error {
	if rdb == nil {
		return nil
	}
	
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	
	return rdb.Set(ctx, key, jsonData, ttl).Err()
}

// GetTraderProfile fetches complete trader profile with stats
func (s *ProfileService) GetTraderProfile(ctx context.Context, address string) (*data_api.TraderProfile, error) {
	address = normalizeAddress(address)
	if address == "" {
		return nil, nil
	}

	// Check cache first
	key := cacheKey("info", address)
	cached, err := getFromCache[data_api.TraderProfile](ctx, s.redis, key)
	if err != nil {
		logger.Error("ProfileService: Cache error: %v", err)
	}
	if cached != nil {
		return cached, nil
	}

	profile := &data_api.TraderProfile{
		Address: address,
	}

	// Try to get profile from Gamma search
	profiles, err := s.gammaClient.SearchProfiles(ctx, address, 5)
	if err == nil && len(profiles) > 0 {
		if match := matchProfile(address, profiles); match != nil {
			profile.ProxyWallet = match.ProxyWallet
			profile.ProfileName = match.Name
			if profile.ProfileName == "" {
				profile.ProfileName = match.Pseudonym
			}
			profile.ProfileImage = match.ProfileImage
			profile.Bio = match.Bio
			profile.JoinedAt = match.CreatedAt
		}
	}

	// Get stats
	stats, err := s.GetTraderStats(ctx, address)
	if err != nil {
		logger.Error("ProfileService: Failed to get trader stats: %v", err)
	} else {
		profile.Stats = stats
	}

	// Cache the result
	if err := setInCache(ctx, s.redis, key, profile, ProfileCacheTTL); err != nil {
		logger.Error("ProfileService: Failed to cache profile: %v", err)
	}

	return profile, nil
}

// GetTraderStats calculates performance metrics for a trader
func (s *ProfileService) GetTraderStats(ctx context.Context, address string) (*data_api.TraderStats, error) {
	address = normalizeAddress(address)
	profileAddress := s.resolveProfileAddress(ctx, address)

	// Check cache first
	key := cacheKey("stats", address)
	cached, err := getFromCache[data_api.TraderStats](ctx, s.redis, key)
	if err != nil {
		logger.Error("ProfileService: Stats cache error: %v", err)
	}
	if cached != nil {
		return cached, nil
	}

	stats := &data_api.TraderStats{}

	// Get PnL data (this also gets positions internally)
	pnlData, err := s.dataAPIClient.GetPnL(ctx, profileAddress)
	if err != nil {
		logger.Error("ProfileService: Failed to get PnL: %v", err)
	} else {
		stats.WinRate = pnlData.WinRate
		stats.RealizedPnL = pnlData.RealizedPnL
		stats.WinningTrades = pnlData.WinningTrades
		stats.LosingTrades = pnlData.LosingTrades
		stats.TotalTrades = pnlData.WinningTrades + pnlData.LosingTrades
	}

	// Compute volume stats from trades
	tradeVolume, avgTradeSize, tradeCount, err := s.aggregateTradeVolume(ctx, profileAddress)
	if err != nil {
		logger.Error("ProfileService: Failed to aggregate trade volume: %v", err)
	} else {
		stats.TotalVolume = tradeVolume
		stats.AvgTradeSize = avgTradeSize
		if tradeCount > 0 {
			stats.TotalTrades = tradeCount
		}
	}

	// Get open positions count
	openCount, err := s.countOpenPositions(ctx, profileAddress)
	if err != nil {
		logger.Error("ProfileService: Failed to count positions: %v", err)
	} else {
		stats.OpenPositions = openCount
	}

	// Get closed positions count
	closedCount, err := s.countClosedPositions(ctx, profileAddress)
	if err != nil {
		logger.Error("ProfileService: Failed to count closed positions: %v", err)
	} else {
		stats.ClosedPositions = closedCount
	}

	// Cache the result
	if err := setInCache(ctx, s.redis, key, stats, StatsCacheTTL); err != nil {
		logger.Error("ProfileService: Failed to cache stats: %v", err)
	}

	return stats, nil
}

// GetOpenPositions fetches current open positions for "Positions Spy"
func (s *ProfileService) GetOpenPositions(ctx context.Context, address string, limit, offset int) ([]data_api.Position, error) {
	address = normalizeAddress(address)
	profileAddress := s.resolveProfileAddress(ctx, address)

	if limit <= 0 {
		limit = 50
	}

	// Check cache first (only cache first page)
	if offset == 0 {
		key := cacheKey(fmt.Sprintf("positions:%d", limit), address)
		cached, err := getFromCache[[]data_api.Position](ctx, s.redis, key)
		if err != nil {
			logger.Error("ProfileService: Positions cache error: %v", err)
		}
		if cached != nil {
			return *cached, nil
		}
	}

	positions, err := s.dataAPIClient.GetPositions(ctx, profileAddress, &data_api.PositionsParams{
		Limit:         limit,
		Offset:        offset,
		SortBy:        "SIZE",
		SortDirection: "DESC",
	})
	if err != nil {
		return nil, err
	}

	// Cache first page
	if offset == 0 {
		key := cacheKey(fmt.Sprintf("positions:%d", limit), address)
		if err := setInCache(ctx, s.redis, key, positions, PositionsCacheTTL); err != nil {
			logger.Error("ProfileService: Failed to cache positions: %v", err)
		}
	}

	return positions, nil
}

// GetActivityHeatmap fetches trade activity for GitHub-style heatmap
func (s *ProfileService) GetActivityHeatmap(ctx context.Context, address string) ([]data_api.ActivityDataPoint, error) {
	address = normalizeAddress(address)
	profileAddress := s.resolveProfileAddress(ctx, address)

	// Check Redis cache first
	key := cacheKey("activity", address)
	cached, err := getFromCache[[]data_api.ActivityDataPoint](ctx, s.redis, key)
	if err != nil {
		logger.Error("ProfileService: Activity cache error: %v", err)
	}
	if cached != nil {
		logger.Info("ProfileService: Activity cache hit for %s", address)
		return *cached, nil
	}

	// Cache miss - fetch from API
	activity, err := s.dataAPIClient.GetActivityHeatmap(ctx, profileAddress)
	if err != nil {
		return nil, err
	}

	// Cache with longer TTL (activity is historical, changes slowly)
	if err := setInCache(ctx, s.redis, key, activity, ActivityCacheTTL); err != nil {
		logger.Error("ProfileService: Failed to cache activity: %v", err)
	}

	return activity, nil
}

// GetRecentTrades fetches recent trades for a trader
func (s *ProfileService) GetRecentTrades(ctx context.Context, address string, limit int) ([]data_api.Trade, error) {
	address = normalizeAddress(address)
	profileAddress := s.resolveProfileAddress(ctx, address)

	if limit <= 0 {
		limit = 20
	}

	// Check cache first
	key := cacheKey(fmt.Sprintf("trades:%d", limit), address)
	cached, err := getFromCache[[]data_api.Trade](ctx, s.redis, key)
	if err != nil {
		logger.Error("ProfileService: Trades cache error: %v", err)
	}
	if cached != nil {
		return *cached, nil
	}

	trades, err := s.dataAPIClient.GetTrades(ctx, profileAddress, &data_api.TradesParams{Limit: limit})
	if err != nil {
		return nil, err
	}

	// Cache the result
	if err := setInCache(ctx, s.redis, key, trades, TradesCacheTTL); err != nil {
		logger.Error("ProfileService: Failed to cache trades: %v", err)
	}

	return trades, nil
}

func (s *ProfileService) aggregateTradeVolume(ctx context.Context, address string) (float64, float64, int, error) {
	const limit = 1000
	const maxOffset = 10000

	totalVolume := 0.0
	totalTrades := 0
	offset := 0

	for {
		trades, err := s.dataAPIClient.GetTrades(ctx, address, &data_api.TradesParams{
			Limit:  limit,
			Offset: offset,
		})
		if err != nil {
			return totalVolume, 0, totalTrades, err
		}

		if len(trades) == 0 {
			break
		}

		for _, trade := range trades {
			value := trade.Value
			if value == 0 && trade.Price > 0 && trade.Size > 0 {
				value = trade.Price * trade.Size
			}
			totalVolume += value
		}
		totalTrades += len(trades)

		if len(trades) < limit {
			break
		}

		offset += limit
		if offset > maxOffset {
			break
		}
	}

	avgTradeSize := 0.0
	if totalTrades > 0 {
		avgTradeSize = totalVolume / float64(totalTrades)
	}

	return totalVolume, avgTradeSize, totalTrades, nil
}

func (s *ProfileService) countOpenPositions(ctx context.Context, address string) (int, error) {
	const limit = 500
	const maxOffset = 10000

	total := 0
	offset := 0

	for {
		positions, err := s.dataAPIClient.GetPositions(ctx, address, &data_api.PositionsParams{
			Limit:  limit,
			Offset: offset,
		})
		if err != nil {
			return total, err
		}

		total += len(positions)
		if len(positions) < limit {
			break
		}

		offset += limit
		if offset > maxOffset {
			break
		}
	}

	return total, nil
}

func (s *ProfileService) countClosedPositions(ctx context.Context, address string) (int, error) {
	const limit = 500
	const maxOffset = 10000

	total := 0
	offset := 0

	for {
		positions, err := s.dataAPIClient.GetClosedPositions(ctx, address, limit, offset)
		if err != nil {
			return total, err
		}

		total += len(positions)
		if len(positions) < limit {
			break
		}

		offset += limit
		if offset > maxOffset {
			break
		}
	}

	return total, nil
}

// GetMarketHolders fetches top holders for a market
func (s *ProfileService) GetMarketHolders(ctx context.Context, conditionID, tokenID string, limit int) ([]data_api.Holder, error) {
	if limit <= 0 {
		limit = 10
	}

	// Check cache first
	key := fmt.Sprintf("holders:%s:%s:%d", conditionID, tokenID, limit)
	cached, err := getFromCache[[]data_api.Holder](ctx, s.redis, key)
	if err != nil {
		logger.Error("ProfileService: Holders cache error: %v", err)
	}
	if cached != nil {
		return s.applyHolderValues(ctx, conditionID, tokenID, *cached), nil
	}

	holders, err := s.dataAPIClient.GetHolders(ctx, conditionID, tokenID, limit)
	if err != nil {
		return nil, err
	}

	// Cache the result
	if err := setInCache(ctx, s.redis, key, holders, HoldersCacheTTL); err != nil {
		logger.Error("ProfileService: Failed to cache holders: %v", err)
	}

	return s.applyHolderValues(ctx, conditionID, tokenID, holders), nil
}

func (s *ProfileService) applyHolderValues(ctx context.Context, conditionID, tokenID string, holders []data_api.Holder) []data_api.Holder {
	if len(holders) == 0 {
		return holders
	}

	price, ok := s.getDisplayPrice(ctx, conditionID, tokenID)
	if !ok {
		return holders
	}

	for i := range holders {
		holders[i].Value = holders[i].Size * price
	}

	return holders
}

func (s *ProfileService) getDisplayPrice(ctx context.Context, conditionID, tokenID string) (float64, bool) {
	if conditionID != "" && tokenID != "" && s.redis != nil {
		key := fmt.Sprintf("price:%s:%s", conditionID, tokenID)
		result, err := s.redis.HGetAll(ctx, key).Result()
		if err == nil && len(result) > 0 {
			bestBid := parseStringFloat(result["best_bid"])
			bestAsk := parseStringFloat(result["best_ask"])
			lastTradePrice := parseStringFloat(result["last_trade_price"])
			if price, ok := calculateDisplayPrice(bestBid, bestAsk, lastTradePrice); ok {
				return price, true
			}
		}
	}

	if s.clobClient == nil || tokenID == "" {
		return 0, false
	}

	midpoint, midErr := s.clobClient.GetMidpoint(ctx, tokenID)
	spread, spreadErr := s.clobClient.GetSpread(ctx, tokenID)
	if midErr == nil && midpoint > 0 {
		if spreadErr == nil && spread > maxDisplaySpread {
			lastTradePrice, _, err := s.clobClient.GetLastTradePrice(ctx, tokenID)
			if err == nil && lastTradePrice > 0 {
				return lastTradePrice, true
			}
		}
		return midpoint, true
	}

	lastTradePrice, _, err := s.clobClient.GetLastTradePrice(ctx, tokenID)
	if err == nil && lastTradePrice > 0 {
		return lastTradePrice, true
	}

	return 0, false
}
