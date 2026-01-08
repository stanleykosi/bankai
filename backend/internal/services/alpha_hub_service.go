package services

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/integrations/openai"
	"github.com/bankai-project/backend/internal/integrations/tavily"
	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/polymarket/clob"
	"github.com/bankai-project/backend/internal/polymarket/data_api"
	"github.com/bankai-project/backend/internal/services/prompts"
	"github.com/redis/go-redis/v9"
)

const (
	defaultSmartMoneyTTL   = 45 * time.Second
	defaultWhaleThreshold  = 5_000.0
	defaultAIMarketLimit   = 10
	defaultNewsMarketLimit = 3
	maxWhalesToEnrich      = 200
	washTradeWindow        = 10 * time.Minute
	washTradeSizeThreshold = 0.10 // 10% size similarity window
	washPriceThreshold     = 0.05 // accept 5c or tighter price differences
	walletStatsTimeout     = 4 * time.Second
	tavilySearchTimeout    = 15 * time.Second
	maxGlobalTrades        = 500
	tradeBufferTTL         = 6 * time.Hour
	minResolutionHorizon   = 6 * time.Hour
	maxResolutionHorizon   = 60 * 24 * time.Hour
)

// AlphaHubService orchestrates smart-money + whale flow + AI picks for the /analysis dashboard.
type AlphaHubService struct {
	marketService  *MarketService
	profileService *ProfileService
	clobClient     *clob.Client
	tavilyClient   *tavily.Client
	openaiClient   *openai.Client
	dataAPIClient  *data_api.Client
	redis          *redis.Client
}

type SmartMoneyStats struct {
	NetBuyUSD         float64 `json:"net_buy_usd"`
	NetSellUSD        float64 `json:"net_sell_usd"`
	BuysCount         int     `json:"buys_count"`
	SellsCount        int     `json:"sells_count"`
	WhaleHitsCount    int     `json:"whale_hits_count"`
	WalletsConsidered int     `json:"wallets_considered"`
	GoldBuys          float64 `json:"gold_buys"`
	SilverBuys        float64 `json:"silver_buys"`
	BronzeBuys        float64 `json:"bronze_buys"`
	AvgEntryVsMidBps  float64 `json:"avg_entry_vs_mid_bps"`
}

type MarketSignal struct {
	MarketID      string           `json:"market_id"`
	TokenID       string           `json:"token_id"`
	Title         string           `json:"title"`
	Slug          string           `json:"slug"`
	Category      string           `json:"category"`
	Resolution    string           `json:"resolves_at,omitempty"`
	YesPrice      float64          `json:"yes_price"`
	BestBid       float64          `json:"best_bid"`
	BestAsk       float64          `json:"best_ask"`
	SpreadBps     float64          `json:"spread_bps"`
	Volume24h     float64          `json:"volume_24h"`
	Volume7d      float64          `json:"volume_7d"`
	Momentum1h    float64          `json:"p1h"`
	Momentum24h   float64          `json:"p24h"`
	Momentum7d    float64          `json:"p7d"`
	SmartMoney    SmartMoneyStats  `json:"smart_money"`
	Score         float64          `json:"score"`
	WalletsSample []WalletSnapshot `json:"wallets_sample,omitempty"`
}

type WalletSnapshot struct {
	Address     string  `json:"address"`
	Tier        string  `json:"tier"`
	WinRate     float64 `json:"win_rate"`
	RealizedPnL float64 `json:"realized_pnl"`
}

type WhaleEvent struct {
	Timestamp   time.Time `json:"ts"`
	MarketID    string    `json:"market_id"`
	TokenID     string    `json:"token_id"`
	Side        string    `json:"side"`
	SizeUSD     float64   `json:"size_usd"`
	Price       float64   `json:"price"`
	Wallet      string    `json:"wallet"`
	WalletTier  string    `json:"wallet_tier"`
	WinRate     float64   `json:"win_rate"`
	RealizedPnL float64   `json:"realized_pnl"`
	Slug        string    `json:"slug,omitempty"`
	Title       string    `json:"title,omitempty"`
	SpreadBps   float64   `json:"spread_bps,omitempty"`
	IsWashTrade bool      `json:"is_wash_trade"`
}

type washTradeSnapshot struct {
	Key       string
	MarketID  string
	Side      clob.OrderSide
	Notional  float64
	Price     float64
	Timestamp time.Time
}

// WashTradeDetector tracks recent wallet activity to flag wash trading patterns.
type WashTradeDetector struct {
	walletTrades  map[string][]washTradeSnapshot
	timeWindow    time.Duration
	sizeThreshold float64
}

func NewWashTradeDetector(timeWindow time.Duration, sizeThreshold float64) *WashTradeDetector {
	return &WashTradeDetector{
		walletTrades:  make(map[string][]washTradeSnapshot),
		timeWindow:    timeWindow,
		sizeThreshold: sizeThreshold,
	}
}

// IsWashTrade returns true when the trade matches a recent opposite-side fill of similar size/price.
func (d *WashTradeDetector) IsWashTrade(trade clob.TradeEvent, wallet string, notional float64, key string) (bool, []string) {
	wallet = strings.ToLower(strings.TrimSpace(wallet))
	if wallet == "" || notional <= 0 {
		return false, nil
	}

	ts := time.Unix(normalizeTradeTimestamp(trade.MatchTime), 0)
	cutoff := ts.Add(-d.timeWindow)
	snapshots := d.walletTrades[wallet]
	pruned := snapshots[:0]
	matchedKeys := make([]string, 0)

	for _, snap := range snapshots {
		if snap.Timestamp.Before(cutoff) {
			continue
		}
		pruned = append(pruned, snap)

		if snap.MarketID != trade.Market || snap.Side == trade.Side {
			continue
		}
		if !similarSize(snap.Notional, notional, d.sizeThreshold) {
			continue
		}
		if !closePrices(snap.Price, trade.Price) {
			continue
		}
		matchedKeys = append(matchedKeys, snap.Key)
	}

	snapshot := washTradeSnapshot{
		Key:       key,
		MarketID:  trade.Market,
		Side:      trade.Side,
		Notional:  notional,
		Price:     trade.Price,
		Timestamp: ts,
	}
	pruned = append(pruned, snapshot)
	d.walletTrades[wallet] = pruned

	return len(matchedKeys) > 0, matchedKeys
}

type SmartMoneyResponse struct {
	WindowSeconds int            `json:"window_seconds"`
	Markets       []MarketSignal `json:"markets"`
	Whales        []WhaleEvent   `json:"whales"`
	GeneratedAt   time.Time      `json:"generated_at"`
}

type AIPick struct {
	MarketID       string  `json:"market_id"`
	Slug           string  `json:"slug"`
	Title          string  `json:"title"`
	ProbabilityYes float64 `json:"probability_yes"`
	Conviction     string  `json:"conviction"`
	Action         string  `json:"action"`
	Rationale      string  `json:"rationale"`
}

type AIResponse struct {
	Picks      []AIPick `json:"ai_picks"`
	RawContent string   `json:"raw_content"`
	Model      string   `json:"model"`
}

func NewAlphaHubService(marketService *MarketService, profileService *ProfileService, clobClient *clob.Client, tavilyClient *tavily.Client, openaiClient *openai.Client, dataAPIClient *data_api.Client, redis *redis.Client) *AlphaHubService {
	return &AlphaHubService{
		marketService:  marketService,
		profileService: profileService,
		clobClient:     clobClient,
		tavilyClient:   tavilyClient,
		openaiClient:   openaiClient,
		dataAPIClient:  dataAPIClient,
		redis:          redis,
	}
}

// GetSmartMoneySignals aggregates last-hour trades, tags wallets by tier, and computes per-market scores.
// Source of trades: RTDS-ingested trades buffered in Redis (no CLOB /data/trades dependency).
func (s *AlphaHubService) GetSmartMoneySignals(ctx context.Context, window time.Duration) (*SmartMoneyResponse, error) {
	startTime := time.Now()
	logger.Info("AlphaHub: GetSmartMoneySignals started, window=%v", window)

	cacheKey := fmt.Sprintf("analysis:smart:%d", int(window.Seconds()))
	if cached, err := s.redis.Get(ctx, cacheKey).Bytes(); err == nil && len(cached) > 0 {
		var resp SmartMoneyResponse
		if unmarshalErr := json.Unmarshal(cached, &resp); unmarshalErr == nil {
			logger.Info("AlphaHub: returning cached response, elapsed=%v", time.Since(startTime))
			return &resp, nil
		}
	}

	tradesStart := time.Now()
	trades, err := s.consumeRecentTrades(ctx, window)
	logger.Info("AlphaHub: consumeRecentTrades completed, count=%d, elapsed=%v", len(trades), time.Since(tradesStart))
	if err != nil {
		logger.Error("AlphaHub: trades fetch failed: %v", err)
		// Degrade gracefully with empty signals
		return &SmartMoneyResponse{
			WindowSeconds: int(window.Seconds()),
			Markets:       []MarketSignal{},
			Whales:        []WhaleEvent{},
			GeneratedAt:   time.Now().UTC(),
		}, nil
	}

	if s.dataAPIClient == nil {
		logger.Warn("AlphaHub: data API client not configured; wallet tiers will default to Bronze")
	}

	walletTierCache := make(map[string]WalletSnapshot)
	marketAgg := make(map[string]*MarketSignal)
	whales := make([]WhaleEvent, 0)
	entrySums := make(map[string]struct {
		Notional float64
		Size     float64
	})

	// Normalize trade order for wash-trade detection
	sort.Slice(trades, func(i, j int) bool {
		return normalizeTradeTimestamp(trades[i].MatchTime) < normalizeTradeTimestamp(trades[j].MatchTime)
	})

	type enrichedTrade struct {
		trade  clob.TradeEvent
		wallet string
		value  float64
		key    string
	}

	// First pass: flag wash trades so they can be excluded from smart-money math.
	washDetector := NewWashTradeDetector(washTradeWindow, washTradeSizeThreshold)
	washFlags := make(map[string]struct{})
	enriched := make([]enrichedTrade, 0, len(trades))

	for idx, t := range trades {
		marketID := strings.TrimSpace(t.Market)
		if marketID == "" {
			continue
		}
		wallet := strings.TrimSpace(t.Taker)
		if wallet == "" {
			wallet = strings.TrimSpace(t.Maker)
		}

		value := t.Value
		if value == 0 && t.Price > 0 && t.Size > 0 {
			value = t.Price * t.Size
		}

		key := buildTradeKey(t, wallet, idx)
		if isWash, matches := washDetector.IsWashTrade(t, wallet, value, key); isWash {
			washFlags[key] = struct{}{}
			for _, mk := range matches {
				washFlags[mk] = struct{}{}
			}
		}

		enriched = append(enriched, enrichedTrade{
			trade:  t,
			wallet: wallet,
			value:  value,
			key:    key,
		})
	}

	// Track potential whale trades for tier enrichment later
	type potentialWhale struct {
		trade  clob.TradeEvent
		wallet string
		value  float64
		isWash bool
	}
	potentialWhales := make([]potentialWhale, 0)

	aggregationStart := time.Now()
	for _, t := range enriched {
		marketID := strings.TrimSpace(t.trade.Market)
		if marketID == "" {
			continue
		}
		wallet := t.wallet
		value := t.value
		_, isWash := washFlags[t.key]

		agg := marketAgg[marketID]
		if agg == nil {
			agg = &MarketSignal{MarketID: marketID}
			marketAgg[marketID] = agg
		}

		if value >= defaultWhaleThreshold && wallet != "" {
			potentialWhales = append(potentialWhales, potentialWhale{trade: t.trade, wallet: wallet, value: value, isWash: isWash})
		}

		if isWash {
			continue
		}

		// Aggregate trade data without wallet tier lookup
		switch t.trade.Side {
		case clob.BUY:
			agg.SmartMoney.NetBuyUSD += value
			agg.SmartMoney.BuysCount++
			agg.SmartMoney.BronzeBuys += value // Default to bronze, will be corrected for whales
		case clob.SELL:
			agg.SmartMoney.NetSellUSD += value
			agg.SmartMoney.SellsCount++
		}

		// Track average entry vs mid (weighted)
		if t.trade.Price > 0 && t.trade.Size > 0 {
			cur := entrySums[marketID]
			cur.Notional += t.trade.Price * t.trade.Size
			cur.Size += t.trade.Size
			entrySums[marketID] = cur
		}
	}
	logger.Info("AlphaHub: trade aggregation completed, markets=%d, potentialWhales=%d, washTrades=%d, elapsed=%v",
		len(marketAgg), len(potentialWhales), len(washFlags), time.Since(aggregationStart))

	// Only fetch wallet tiers for potential whale trades (limited to reduce API calls)
	whaleEnrichStart := time.Now()
	if len(potentialWhales) > maxWhalesToEnrich {
		potentialWhales = potentialWhales[:maxWhalesToEnrich]
	}
	washWhaleCount := 0
	for _, pw := range potentialWhales {
		if pw.isWash {
			washWhaleCount++
		}
		tierInfo := s.getWalletTier(ctx, pw.wallet, walletTierCache)

		agg := marketAgg[pw.trade.Market]
		if agg != nil && !pw.isWash {
			if tierInfo.Address != "" {
				agg.SmartMoney.WalletsConsidered++
				agg.WalletsSample = append(agg.WalletsSample, tierInfo)
			}

			// Correct the tier distribution for this whale trade
			switch tierInfo.Tier {
			case "Gold":
				agg.SmartMoney.GoldBuys += pw.value
				agg.SmartMoney.BronzeBuys -= pw.value // Remove from default bronze
			case "Silver":
				agg.SmartMoney.SilverBuys += pw.value
				agg.SmartMoney.BronzeBuys -= pw.value // Remove from default bronze
			}
			agg.SmartMoney.WhaleHitsCount++
		}

		whales = append(whales, WhaleEvent{
			Timestamp:   time.Unix(normalizeTradeTimestamp(pw.trade.MatchTime), 0).UTC(),
			MarketID:    pw.trade.Market,
			TokenID:     pw.trade.TokenID,
			Side:        string(pw.trade.Side),
			SizeUSD:     pw.value,
			Price:       pw.trade.Price,
			Wallet:      pw.wallet,
			WalletTier:  tierInfo.Tier,
			WinRate:     tierInfo.WinRate,
			RealizedPnL: tierInfo.RealizedPnL,
			IsWashTrade: pw.isWash,
		})
	}
	logger.Info("AlphaHub: whale tier enrichment completed, whales=%d, washWhales=%d, walletsCached=%d, elapsed=%v",
		len(whales), washWhaleCount, len(walletTierCache), time.Since(whaleEnrichStart))

	// Hydrate market metadata and compute scores using batch query
	marketHydrateStart := time.Now()

	// Collect all market IDs for batch query
	marketIDs := make([]string, 0, len(marketAgg))
	marketIDSet := make(map[string]struct{})
	for marketID := range marketAgg {
		marketIDs = append(marketIDs, marketID)
		marketIDSet[marketID] = struct{}{}
	}
	for _, pw := range potentialWhales {
		if _, ok := marketIDSet[pw.trade.Market]; !ok {
			marketIDSet[pw.trade.Market] = struct{}{}
			marketIDs = append(marketIDs, pw.trade.Market)
		}
	}

	// Single batch query instead of N individual queries
	marketsMap, batchErr := s.marketService.GetMarketsByConditionIDs(ctx, marketIDs)
	if batchErr != nil {
		logger.Error("AlphaHub: batch market query failed: %v", batchErr)
	}
	logger.Info("AlphaHub: batch market query completed, requested=%d, found=%d, elapsed=%v",
		len(marketIDs), len(marketsMap), time.Since(marketHydrateStart))

	// Hydrate whale tape with market metadata for UI clarity
	for i := range whales {
		if meta := marketsMap[whales[i].MarketID]; meta != nil {
			whales[i].Slug = meta.Slug
			whales[i].Title = meta.Title
			whales[i].SpreadBps = spreadToBps(meta.Spread)
		}
	}

	signals := make([]MarketSignal, 0, len(marketAgg))
	for marketID, agg := range marketAgg {
		metadata := marketsMap[marketID]
		if metadata != nil {
			// Filter by resolution horizon
			if metadata.EndDate != nil {
				horizon := metadata.EndDate.Sub(time.Now())
				if horizon < minResolutionHorizon || horizon > maxResolutionHorizon {
					continue
				}
			}
			agg.Title = metadata.Title
			agg.Slug = metadata.Slug
			agg.Category = metadata.Category
			if metadata.EndDate != nil {
				agg.Resolution = metadata.EndDate.UTC().Format(time.RFC3339)
			}
			agg.YesPrice = metadata.YesPrice
			agg.BestBid = metadata.YesBestBid
			agg.BestAsk = metadata.YesBestAsk
			agg.Volume24h = metadata.Volume24h
			agg.Volume7d = metadata.Volume1Week
			agg.Momentum1h = metadata.OneHourPriceChange
			agg.Momentum24h = metadata.OneDayPriceChange
			agg.Momentum7d = metadata.OneWeekPriceChange
			agg.SpreadBps = spreadToBps(metadata.Spread)
			agg.TokenID = metadata.TokenIDYes

			// Compute avg entry vs mid (bps) using current mid
			mid := resolveMid(agg.BestBid, agg.BestAsk, agg.YesPrice)
			if sums, ok := entrySums[marketID]; ok && sums.Size > 0 && mid > 0 {
				avgEntry := sums.Notional / sums.Size
				agg.SmartMoney.AvgEntryVsMidBps = ((avgEntry - mid) / mid) * 10000
			}
		}

		// Skip markets that only contained wash trades within the window
		if agg.SmartMoney.BuysCount == 0 && agg.SmartMoney.SellsCount == 0 {
			continue
		}
		agg.Score = scoreMarket(*agg)
		signals = append(signals, *agg)
	}
	logger.Info("AlphaHub: market hydration completed, signals=%d, elapsed=%v", len(signals), time.Since(marketHydrateStart))

	sort.Slice(signals, func(i, j int) bool {
		if signals[i].Score == signals[j].Score {
			return signals[i].SmartMoney.NetBuyUSD > signals[j].SmartMoney.NetBuyUSD
		}
		return signals[i].Score > signals[j].Score
	})

	resp := SmartMoneyResponse{
		WindowSeconds: int(window.Seconds()),
		Markets:       signals,
		Whales:        whales,
		GeneratedAt:   time.Now().UTC(),
	}

	if data, err := json.Marshal(resp); err == nil {
		_ = s.redis.Set(ctx, cacheKey, data, defaultSmartMoneyTTL).Err()
	}

	logger.Info("AlphaHub: GetSmartMoneySignals completed, totalElapsed=%v", time.Since(startTime))
	return &resp, nil
}

// consumeRecentTrades pulls recent trades from Redis (populated by RTDS handlers).
// Expected key pattern: rtds:trades:{unixMinute} -> list of JSON-encoded TradeEvent.
func (s *AlphaHubService) consumeRecentTrades(ctx context.Context, window time.Duration) ([]clob.TradeEvent, error) {
	var merged []clob.TradeEvent
	now := time.Now().Unix()
	start := now - int64(window.Seconds())

	// 1) Try cached RTDS trades
	if s.redis != nil {
		for ts := start; ts <= now; ts += 60 {
			key := fmt.Sprintf("rtds:trades:%d", ts/60)
			raw, err := s.redis.LRange(ctx, key, 0, -1).Result()
			if err != nil && err != redis.Nil {
				logger.Error("AlphaHub: trade buffer read failed for %s: %v", key, err)
				continue
			}
			for _, item := range raw {
				var ev clob.TradeEvent
				if err := json.Unmarshal([]byte(item), &ev); err == nil && ev.MatchTime >= start {
					merged = append(merged, ev)
				}
			}
		}
	}

	// 2) If empty, backfill from Data API global trades
	if len(merged) == 0 && s.dataAPIClient != nil {
		after := time.Unix(start, 0).UTC().Format(time.RFC3339)
		apiTrades, err := s.dataAPIClient.GetGlobalTrades(ctx, &data_api.TradesParams{
			After: after,
			Limit: maxGlobalTrades,
		})
		if err != nil {
			logger.Error("AlphaHub: data API global trades failed: %v", err)
		} else {
			for _, t := range apiTrades {
				matchTime := normalizeTradeTimestamp(t.Timestamp)
				merged = append(merged, clob.TradeEvent{
					ID:        t.ID,
					Market:    t.ConditionID,
					TokenID:   t.TokenID,
					Side:      clob.OrderSide(strings.ToUpper(strings.TrimSpace(t.Side))),
					Price:     t.Price,
					Size:      t.Size,
					Value:     resolveTradeValue(t.Value, t.Price, t.Size),
					Taker:     t.Taker,
					Maker:     t.Maker,
					MatchTime: matchTime,
				})
			}
		}
	}

	// Deduplicate by tx hash + market if available
	seen := make(map[string]struct{})
	out := make([]clob.TradeEvent, 0, len(merged))
	for _, ev := range merged {
		key := ev.ID
		if key == "" && ev.MatchTime > 0 {
			key = fmt.Sprintf("%s-%d-%s", ev.Market, ev.MatchTime, ev.Taker)
		}
		if key == "" {
			out = append(out, ev)
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ev)
	}

	return out, nil
}

func buildTradeKey(t clob.TradeEvent, wallet string, idx int) string {
	if t.ID != "" {
		return t.ID
	}
	ts := normalizeTradeTimestamp(t.MatchTime)
	return fmt.Sprintf("%s-%s-%d-%s-%d", strings.TrimSpace(t.Market), strings.ToUpper(string(t.Side)), ts, strings.ToLower(strings.TrimSpace(wallet)), idx)
}

func normalizeTradeTimestamp(ts int64) int64 {
	// Data API uses ms; RTDS uses seconds
	if ts > 1_000_000_000_000 {
		return ts / 1000
	}
	return ts
}

func resolveTradeValue(value float64, price float64, size float64) float64 {
	if value > 0 {
		return value
	}
	if price > 0 && size > 0 {
		return price * size
	}
	return 0
}

func resolveMid(bid float64, ask float64, last float64) float64 {
	if bid > 0 && ask > 0 {
		return (bid + ask) / 2
	}
	if last > 0 {
		return last
	}
	return 0
}

func similarSize(a float64, b float64, threshold float64) bool {
	if a <= 0 || b <= 0 {
		return false
	}
	ratio := math.Min(a, b) / math.Max(a, b)
	return ratio >= 1-threshold
}

func closePrices(a float64, b float64) bool {
	if a == 0 || b == 0 {
		// Not enough info to compare - treat as close to avoid missing obvious wash signals.
		return true
	}
	diff := math.Abs(a - b)
	return diff <= washPriceThreshold || (diff/math.Max(a, b)) <= washTradeSizeThreshold
}

func valueEdgeScore(m MarketSignal) float64 {
	price := m.YesPrice
	// Penalize near-certain pricing unless smart money entered cheaper
	if price > 0.9 {
		edge := clamp(-((price - 0.9) / 0.1), -1, 0)
		edge += clamp(-m.SmartMoney.AvgEntryVsMidBps/1000, -0.5, 0.5)
		return edge
	}
	// Reward tradable band and smart entry edge
	bandScore := 0.0
	if price >= 0.15 && price <= 0.85 {
		bandScore = 0.5
	}
	entryEdge := clamp(-m.SmartMoney.AvgEntryVsMidBps/500, -1, 1) // negative bps = entered cheaper
	timeBias := 0.0
	if m.Resolution != "" {
		if t, err := time.Parse(time.RFC3339, m.Resolution); err == nil {
			days := t.Sub(time.Now()).Hours() / 24
			if days <= 30 {
				timeBias = 0.2
			}
		}
	}
	return clamp(bandScore+entryEdge+timeBias, -1, 1)
}

// GenerateAIPicks runs the Alpha Hub prompt against the smart-money payload and optional Tavily news.
func (s *AlphaHubService) GenerateAIPicks(ctx context.Context, smart *SmartMoneyResponse) (*AIResponse, error) {
	startTime := time.Now()
	logger.Info("AlphaHub: GenerateAIPicks started")

	if s.openaiClient == nil {
		return nil, fmt.Errorf("openai client not configured")
	}
	if smart == nil || len(smart.Markets) == 0 {
		// Graceful fallback: no signals yet; return empty AI response.
		return &AIResponse{Picks: []AIPick{}, RawContent: "", Model: s.openaiClient.Model()}, nil
	}

	cacheKey := fmt.Sprintf("analysis:ai:%d", smart.WindowSeconds)
	if s.redis != nil {
		if cached, err := s.redis.Get(ctx, cacheKey).Bytes(); err == nil && len(cached) > 0 {
			var cachedResp AIResponse
			if err := json.Unmarshal(cached, &cachedResp); err == nil {
				logger.Info("AlphaHub: GenerateAIPicks returning cached response, elapsed=%v", time.Since(startTime))
				return &cachedResp, nil
			}
		}
	}

	// Limit to top markets for AI picks/news lookups
	topMarkets := smart.Markets
	if len(topMarkets) > defaultAIMarketLimit {
		topMarkets = topMarkets[:defaultAIMarketLimit]
	}

	// Enrich with news for top markets (optional - continue even if it fails)
	type newsItem struct {
		Title   string  `json:"title"`
		URL     string  `json:"url"`
		Content string  `json:"content"`
		Score   float64 `json:"score"`
	}
	newsByMarket := make(map[string][]newsItem)

	// Only do news enrichment if Tavily client is configured
	if s.tavilyClient != nil {
		newsStart := time.Now()
		newsMarkets := topMarkets
		if len(newsMarkets) > defaultNewsMarketLimit {
			newsMarkets = newsMarkets[:defaultNewsMarketLimit]
		}
		for _, m := range newsMarkets {
			// Use a short timeout for each Tavily search
			searchCtx, cancel := context.WithTimeout(ctx, tavilySearchTimeout)
			results, err := s.tavilyClient.Search(searchCtx, m.Title, "polymarket.com")
			cancel()

			if err != nil {
				logger.Error("AlphaHub: tavily search failed for %s: %v (continuing without news)", m.MarketID, err)
				continue // Don't fail the entire request, just skip news for this market
			}
			for _, r := range results {
				newsByMarket[m.MarketID] = append(newsByMarket[m.MarketID], newsItem{
					Title:   r.Title,
					URL:     r.URL,
					Content: r.Content,
					Score:   r.Score,
				})
			}
		}
		logger.Info("AlphaHub: Tavily news enrichment completed, markets=%d, newsItems=%d, elapsed=%v",
			len(newsMarkets), len(newsByMarket), time.Since(newsStart))
	}

	payload := map[string]interface{}{
		"markets":             topMarkets,
		"whale_events":        smart.Whales,
		"news":                newsByMarket,
		"as_of":               time.Now().UTC().Format(time.RFC3339),
		"data_freshness_secs": int(time.Since(smart.GeneratedAt).Seconds()),
	}
	userPromptBytes, _ := json.Marshal(payload)

	openaiStart := time.Now()
	content, err := s.openaiClient.Analyze(ctx, prompts.AlphaHubSystemPrompt, string(userPromptBytes))
	logger.Info("AlphaHub: OpenAI analyze completed, elapsed=%v", time.Since(openaiStart))
	if err != nil {
		logger.Error("AlphaHub: OpenAI analyze failed: %v", err)
		return nil, err
	}

	var parsed struct {
		AIPicks []AIPick `json:"ai_picks"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return &AIResponse{RawContent: content, Model: s.openaiClient.Model()}, nil
	}

	resp := &AIResponse{
		Picks:      parsed.AIPicks,
		RawContent: content,
		Model:      s.openaiClient.Model(),
	}

	if s.redis != nil {
		if data, err := json.Marshal(resp); err == nil {
			_ = s.redis.Set(ctx, cacheKey, data, 5*time.Minute).Err()
		}
	}

	logger.Info("AlphaHub: GenerateAIPicks completed, picks=%d, totalElapsed=%v", len(resp.Picks), time.Since(startTime))
	return resp, nil
}

func (s *AlphaHubService) getWalletTier(ctx context.Context, address string, cache map[string]WalletSnapshot) WalletSnapshot {
	address = strings.ToLower(strings.TrimSpace(address))
	if address == "" {
		return WalletSnapshot{}
	}
	if cached, ok := cache[address]; ok {
		return cached
	}

	// Use lightweight stats lookup with short timeout to avoid blocking
	winRate, realized, err := s.getLightweightWalletStats(ctx, address)
	if err != nil {
		// Graceful degradation: default to Bronze tier on failure
		logger.Error("AlphaHub: lightweight stats fetch failed for %s: %v", address, err)
	}

	tier := "Bronze"
	if winRate >= 0.70 && realized > 0 {
		tier = "Gold"
	} else if winRate >= 0.60 {
		tier = "Silver"
	}

	snapshot := WalletSnapshot{
		Address:     address,
		Tier:        tier,
		WinRate:     winRate,
		RealizedPnL: realized,
	}
	cache[address] = snapshot
	return snapshot
}

// getLightweightWalletStats fetches only the minimal data needed for wallet tier classification.
// Uses a short timeout and only fetches closed positions (single API call) instead of the full
// GetTraderStats which makes 7+ sequential API calls.
func (s *AlphaHubService) getLightweightWalletStats(ctx context.Context, address string) (winRate float64, realizedPnL float64, err error) {
	if s.dataAPIClient == nil {
		return 0, 0, fmt.Errorf("data API client not configured")
	}

	// Check Redis cache first for previously computed lightweight stats
	cacheKey := fmt.Sprintf("alphahub:wallet:%s", address)
	if s.redis != nil {
		if cached, cacheErr := s.redis.HGetAll(ctx, cacheKey).Result(); cacheErr == nil && len(cached) > 0 {
			if wr, ok := cached["win_rate"]; ok {
				winRate, _ = parseFloat(wr)
			}
			if pnl, ok := cached["realized_pnl"]; ok {
				realizedPnL, _ = parseFloat(pnl)
			}
			return winRate, realizedPnL, nil
		}
	}

	// Use a short timeout context to avoid blocking the entire request
	timeoutCtx, cancel := context.WithTimeout(ctx, walletStatsTimeout)
	defer cancel()

	// Fetch closed positions only - single API call instead of 7+
	closedPositions, err := s.dataAPIClient.GetClosedPositions(timeoutCtx, address, 200, 0)
	if err != nil {
		return 0, 0, err
	}

	// Calculate win rate and realized PnL from closed positions
	var winningTrades, losingTrades int
	for _, pos := range closedPositions {
		realizedPnL += pos.RealizedPnL
		if pos.RealizedPnL > 0 {
			winningTrades++
		} else if pos.RealizedPnL < 0 {
			losingTrades++
		}
	}

	totalTrades := winningTrades + losingTrades
	if totalTrades > 0 {
		winRate = float64(winningTrades) / float64(totalTrades)
	}

	// Cache the result for 2 minutes
	if s.redis != nil {
		_ = s.redis.HSet(ctx, cacheKey, map[string]interface{}{
			"win_rate":     fmt.Sprintf("%f", winRate),
			"realized_pnl": fmt.Sprintf("%f", realizedPnL),
		}).Err()
		_ = s.redis.Expire(ctx, cacheKey, 2*time.Minute).Err()
	}

	return winRate, realizedPnL, nil
}

func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}

func scoreMarket(m MarketSignal) float64 {
	liquidityPenalty := 0.0
	if m.SpreadBps > 150 {
		liquidityPenalty += 10
	}
	smartWeight := 0.30 * normalizeScore(m.SmartMoney.NetBuyUSD)
	momentumWeight := 0.15 * clamp(m.Momentum1h/0.1, -1, 1)
	liquidityWeight := 0.10 * clamp(1-(m.SpreadBps/300), -1, 1)
	volumeWeight := 0.10 * clamp(m.Volume24h/200000, 0, 1)
	valueWeight := 0.20 * valueEdgeScore(m)
	fundamentalsWeight := 0.15 // placeholder for rules/resolution clarity baked via penalties above
	base := (smartWeight + momentumWeight + liquidityWeight + volumeWeight + valueWeight + fundamentalsWeight) * 100
	return math.Max(base-liquidityPenalty, 0)
}

func spreadToBps(spread float64) float64 {
	if spread == 0 {
		return 0
	}
	return spread * 10000
}

func normalizeScore(value float64) float64 {
	if value <= 0 {
		return 0
	}
	// Log-scale to avoid runaway scores
	return math.Log10(1 + value/1000)
}

func clamp(val, min, max float64) float64 {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}
