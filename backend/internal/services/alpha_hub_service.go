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
	cacheKey := fmt.Sprintf("analysis:smart:%d", int(window.Seconds()))
	if cached, err := s.redis.Get(ctx, cacheKey).Bytes(); err == nil && len(cached) > 0 {
		var resp SmartMoneyResponse
		if unmarshalErr := json.Unmarshal(cached, &resp); unmarshalErr == nil {
			return &resp, nil
		}
	}

	trades, err := s.consumeRecentTrades(ctx, window)
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

	walletTierCache := make(map[string]WalletSnapshot)
	marketAgg := make(map[string]*MarketSignal)
	whales := make([]WhaleEvent, 0)
	entrySums := make(map[string]struct {
		Notional float64
		Size     float64
	})

	for _, t := range trades {
		marketID := strings.TrimSpace(t.Market)
		if marketID == "" {
			continue
		}
		wallet := strings.TrimSpace(t.Taker)
		if wallet == "" {
			wallet = strings.TrimSpace(t.Maker)
		}
		tierInfo := s.getWalletTier(ctx, wallet, walletTierCache)

		value := t.Value
		if value == 0 && t.Price > 0 && t.Size > 0 {
			value = t.Price * t.Size
		}

		agg := marketAgg[marketID]
		if agg == nil {
			agg = &MarketSignal{MarketID: marketID}
			marketAgg[marketID] = agg
		}

		if tierInfo.Address != "" {
			agg.SmartMoney.WalletsConsidered++
			agg.WalletsSample = append(agg.WalletsSample, tierInfo)
		}

		switch t.Side {
		case clob.BUY:
			agg.SmartMoney.NetBuyUSD += value
			agg.SmartMoney.BuysCount++
			switch tierInfo.Tier {
			case "Gold":
				agg.SmartMoney.GoldBuys += value
			case "Silver":
				agg.SmartMoney.SilverBuys += value
			default:
				agg.SmartMoney.BronzeBuys += value
			}
		case clob.SELL:
			agg.SmartMoney.NetSellUSD += value
			agg.SmartMoney.SellsCount++
		}

		if value >= defaultWhaleThreshold {
			whales = append(whales, WhaleEvent{
				Timestamp:   time.Unix(t.MatchTime, 0).UTC(),
				MarketID:    marketID,
				TokenID:     t.TokenID,
				Side:        string(t.Side),
				SizeUSD:     value,
				Price:       t.Price,
				Wallet:      wallet,
				WalletTier:  tierInfo.Tier,
				WinRate:     tierInfo.WinRate,
				RealizedPnL: tierInfo.RealizedPnL,
			})
			agg.SmartMoney.WhaleHitsCount++
		}

		// Track average entry vs mid (weighted)
		if t.Price > 0 && t.Size > 0 {
			cur := entrySums[marketID]
			cur.Notional += t.Price * t.Size
			cur.Size += t.Size
			entrySums[marketID] = cur
		}
	}

	// Hydrate market metadata and compute scores
	signals := make([]MarketSignal, 0, len(marketAgg))
	for marketID, agg := range marketAgg {
		metadata, mErr := s.marketService.GetMarketByConditionID(ctx, marketID)
		if mErr != nil {
			logger.Error("AlphaHub: failed to get market meta %s: %v", marketID, mErr)
		}
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
		agg.Score = scoreMarket(*agg)
		signals = append(signals, *agg)
	}

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
	if s.openaiClient == nil {
		return nil, fmt.Errorf("openai client not configured")
	}
	if s.tavilyClient == nil {
		return nil, fmt.Errorf("tavily client not configured")
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
				return &cachedResp, nil
			}
		}
	}

	topMarkets := smart.Markets
	if len(topMarkets) > defaultAIMarketLimit {
		topMarkets = topMarkets[:defaultAIMarketLimit]
	}

	// Enrich with news for top markets (required for analysis richness)
	type newsItem struct {
		Title   string  `json:"title"`
		URL     string  `json:"url"`
		Content string  `json:"content"`
		Score   float64 `json:"score"`
	}
	newsByMarket := make(map[string][]newsItem)
	for _, m := range topMarkets {
		results, err := s.tavilyClient.Search(ctx, m.Title, "polymarket.com")
		if err != nil {
			logger.Error("AlphaHub: tavily search failed for %s: %v", m.MarketID, err)
			return nil, fmt.Errorf("tavily failed for %s: %w", m.MarketID, err)
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

	payload := map[string]interface{}{
		"markets":             topMarkets,
		"whale_events":        smart.Whales,
		"news":                newsByMarket,
		"as_of":               time.Now().UTC().Format(time.RFC3339),
		"data_freshness_secs": int(time.Since(smart.GeneratedAt).Seconds()),
	}
	userPromptBytes, _ := json.Marshal(payload)
	content, err := s.openaiClient.Analyze(ctx, prompts.AlphaHubSystemPrompt, string(userPromptBytes))
	if err != nil {
		logger.Error("AlphaHub: OpenAI analyze failed: %v | payload=%s", err, string(userPromptBytes))
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

	stats, err := s.profileService.GetTraderStats(ctx, address)
	if err != nil {
		logger.Error("AlphaHub: stats fetch failed for %s: %v", address, err)
	}
	tier := "Bronze"
	winRate := 0.0
	realized := 0.0
	if stats != nil {
		winRate = stats.WinRate / 100
		realized = stats.RealizedPnL
		if stats.WinRate >= 70 && stats.RealizedPnL > 0 {
			tier = "Gold"
		} else if stats.WinRate >= 60 {
			tier = "Silver"
		}
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
