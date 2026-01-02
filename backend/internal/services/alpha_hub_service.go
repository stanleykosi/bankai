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
	"github.com/bankai-project/backend/internal/services/prompts"
	"github.com/redis/go-redis/v9"
)

const (
	defaultSmartMoneyTTL   = 45 * time.Second
	defaultWhaleThreshold  = 5_000.0
	defaultAIMarketLimit   = 6
	defaultNewsMarketLimit = 3
)

// AlphaHubService orchestrates smart-money + whale flow + AI picks for the /analysis dashboard.
type AlphaHubService struct {
	marketService  *MarketService
	profileService *ProfileService
	clobClient     *clob.Client
	tavilyClient   *tavily.Client
	openaiClient   *openai.Client
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

func NewAlphaHubService(marketService *MarketService, profileService *ProfileService, clobClient *clob.Client, tavilyClient *tavily.Client, openaiClient *openai.Client, redis *redis.Client) *AlphaHubService {
	return &AlphaHubService{
		marketService:  marketService,
		profileService: profileService,
		clobClient:     clobClient,
		tavilyClient:   tavilyClient,
		openaiClient:   openaiClient,
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
		return nil, fmt.Errorf("failed to fetch trades: %w", err)
	}

	walletTierCache := make(map[string]WalletSnapshot)
	marketAgg := make(map[string]*MarketSignal)
	whales := make([]WhaleEvent, 0)

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
	}

	// Hydrate market metadata and compute scores
	signals := make([]MarketSignal, 0, len(marketAgg))
	for marketID, agg := range marketAgg {
		metadata, mErr := s.marketService.GetMarketByConditionID(ctx, marketID)
		if mErr != nil {
			logger.Error("AlphaHub: failed to get market meta %s: %v", marketID, mErr)
		}
		if metadata != nil {
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
	if s.redis == nil {
		return nil, fmt.Errorf("redis not configured")
	}

	now := time.Now().Unix()
	start := now - int64(window.Seconds())
	var trades []clob.TradeEvent

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
				trades = append(trades, ev)
			}
		}
	}
	return trades, nil
}

// GenerateAIPicks runs the Alpha Hub prompt against the smart-money payload and optional Tavily news.
func (s *AlphaHubService) GenerateAIPicks(ctx context.Context, smart *SmartMoneyResponse) (*AIResponse, error) {
	if s.openaiClient == nil {
		return nil, fmt.Errorf("openai client not configured")
	}
	if smart == nil || len(smart.Markets) == 0 {
		return nil, fmt.Errorf("no smart money signals available")
	}

	topMarkets := smart.Markets
	if len(topMarkets) > defaultAIMarketLimit {
		topMarkets = topMarkets[:defaultAIMarketLimit]
	}

	// Optionally enrich with news for top markets
	type newsItem struct {
		Title   string  `json:"title"`
		URL     string  `json:"url"`
		Content string  `json:"content"`
		Score   float64 `json:"score"`
	}
	newsByMarket := make(map[string][]newsItem)
	if s.tavilyClient != nil {
		for idx, m := range topMarkets {
			if idx >= defaultNewsMarketLimit {
				break
			}
			results, err := s.tavilyClient.Search(ctx, m.Title, "polymarket.com")
			if err != nil {
				logger.Error("AlphaHub: tavily search failed for %s: %v", m.MarketID, err)
				continue
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
	}

	payload := map[string]interface{}{
		"markets":             topMarkets,
		"whale_events":        smart.Whales,
		"news":                newsByMarket,
		"data_freshness_secs": int(time.Since(smart.GeneratedAt).Seconds()),
	}
	userPromptBytes, _ := json.Marshal(payload)
	content, err := s.openaiClient.Analyze(ctx, prompts.AlphaHubSystemPrompt, string(userPromptBytes))
	if err != nil {
		return nil, err
	}

	var parsed struct {
		AIPicks []AIPick `json:"ai_picks"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return &AIResponse{RawContent: content, Model: s.openaiClient.Model()}, nil
	}

	return &AIResponse{
		Picks:      parsed.AIPicks,
		RawContent: content,
		Model:      s.openaiClient.Model(),
	}, nil
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
	smartWeight := 0.35 * normalizeScore(m.SmartMoney.NetBuyUSD)
	momentumWeight := 0.15 * clamp(m.Momentum1h/0.1, -1, 1)
	liquidityWeight := 0.15 * clamp(1-(m.SpreadBps/300), -1, 1)
	volumeWeight := 0.1 * clamp(m.Volume24h/200000, 0, 1)
	base := (smartWeight + momentumWeight + liquidityWeight + volumeWeight) * 100
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
