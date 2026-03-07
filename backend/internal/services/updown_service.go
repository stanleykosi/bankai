package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/integrations/synthdata"
	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/models"
	"github.com/bankai-project/backend/internal/polymarket/gamma"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	upDownSignalChannel   = "updown:signal_updates"
	upDownMarketsCacheKey = "updown:markets:snapshot"
	upDownRecsCacheKey    = "updown:recommendations"
	upDownSignalCachePref = "updown:signal:"
	upDownCacheTTL        = 30 * time.Second
	defaultRiskProfile    = "Balanced"
	upDownEventDebounce   = 250 * time.Millisecond
	upDownPersistTimeout  = 1500 * time.Millisecond
	upDownGammaPageLimit  = 100
	upDownGammaMaxPages   = 8
)

var (
	ErrInvalidUpDownDecisionRequest = errors.New("invalid updown decision request")
)

type marketPriceUpdate struct {
	ConditionID        string   `json:"condition_id"`
	AssetID            string   `json:"asset_id"`
	AssetSymbol        string   `json:"asset_symbol,omitempty"`
	Price              *float64 `json:"price,omitempty"`
	BestBid            *float64 `json:"best_bid,omitempty"`
	BestAsk            *float64 `json:"best_ask,omitempty"`
	Timestamp          *string  `json:"timestamp,omitempty"`
	LastTradePrice     *float64 `json:"last_trade_price,omitempty"`
	LastTradeTimestamp *string  `json:"last_trade_timestamp,omitempty"`
}

type UpDownMarketType string

const (
	UpDownMarketTypeCrypto UpDownMarketType = "updown_crypto"
)

type UpDownResolutionSource string

const (
	ResolutionSourceChainlink UpDownResolutionSource = "chainlink"
	ResolutionSourceBinance   UpDownResolutionSource = "binance"
	ResolutionSourceUnknown   UpDownResolutionSource = "unknown"
)

type UpDownWindowType string

const (
	Window5m      UpDownWindowType = "5m"
	Window15m     UpDownWindowType = "15m"
	Window1h      UpDownWindowType = "1h"
	Window4h      UpDownWindowType = "4h"
	WindowUnknown UpDownWindowType = "unknown"
)

type UpDownMarket struct {
	MarketType            UpDownMarketType       `json:"market_type"`
	Slug                  string                 `json:"slug"`
	ConditionID           string                 `json:"condition_id"`
	Asset                 string                 `json:"asset"`
	WindowType            UpDownWindowType       `json:"window_type"`
	ResolutionSourceType  UpDownResolutionSource `json:"resolution_source_type"`
	Tradable              bool                   `json:"tradable"`
	IsActiveWindow        bool                   `json:"is_active_window"`
	TimeToStartSeconds    int64                  `json:"time_to_start_seconds"`
	TimeToEndSeconds      int64                  `json:"time_to_end_seconds"`
	CreatedAt             *time.Time             `json:"created_at,omitempty"`
	EventStartTime        time.Time              `json:"event_start_time"`
	EventEndTime          time.Time              `json:"event_end_time"`
	ResolutionRuleSummary string                 `json:"resolution_rule_summary"`
	Market                models.Market          `json:"market"`
	OutcomeIndexUp        int                    `json:"outcome_index_up"`
	OutcomeIndexDown      int                    `json:"outcome_index_down"`
	OutcomeLabelUp        string                 `json:"outcome_label_up"`
	OutcomeLabelDown      string                 `json:"outcome_label_down"`
}

type UpDownRiskFlags struct {
	ReadOnly            bool `json:"read_only"`
	KillSwitch          bool `json:"kill_switch"`
	SynthMissing        bool `json:"synth_missing"`
	SynthStale          bool `json:"synth_stale"`
	MarketStale         bool `json:"market_stale"`
	DepthMissing        bool `json:"depth_missing"`
	WideSpread          bool `json:"wide_spread"`
	StatusBoundary      bool `json:"status_boundary"`
	SourceMismatch      bool `json:"source_mismatch"`
	ClockDrift          bool `json:"clock_drift"`
	LowLiquidity        bool `json:"low_liquidity"`
	HighVolatility      bool `json:"high_volatility"`
	DataIntegrityFailed bool `json:"data_integrity_failed"`
}

type RecommendationPrefill struct {
	Side         string  `json:"side"`
	OutcomeIndex int     `json:"outcome_index"`
	LimitPrice   float64 `json:"limit_price"`
	SizeShares   float64 `json:"size_shares"`
	Disabled     bool    `json:"disabled"`
	DisabledWhy  string  `json:"disabled_why,omitempty"`
}

type UpDownRecommendation struct {
	ID                     string                `json:"id"`
	Slug                   string                `json:"slug"`
	ConditionID            string                `json:"condition_id"`
	Asset                  string                `json:"asset"`
	WindowType             UpDownWindowType      `json:"window_type"`
	Profile                string                `json:"profile"`
	Decision               string                `json:"decision"` // BUY_UP | BUY_DOWN | NO_TRADE
	RecommendedSide        string                `json:"recommended_side"`
	OrderSide              string                `json:"order_side"`
	ExpectedValue          float64               `json:"expected_value"`
	Confidence             float64               `json:"confidence"`
	SuggestedLimitPrice    float64               `json:"suggested_limit_price"`
	SuggestedSizeShares    float64               `json:"suggested_size_shares"`
	SuggestedNotional      float64               `json:"suggested_notional"`
	KellyRaw               float64               `json:"kelly_raw"`
	KellyCapped            float64               `json:"kelly_capped"`
	ReasonCodes            []string              `json:"reason_codes"`
	InvalidationConditions []string              `json:"invalidation_conditions"`
	RiskFlags              UpDownRiskFlags       `json:"risk_flags"`
	Prefill                RecommendationPrefill `json:"prefill"`
	GeneratedAt            time.Time             `json:"generated_at"`
}

type UpDownSignal struct {
	Slug                  string                 `json:"slug"`
	ConditionID           string                 `json:"condition_id"`
	Asset                 string                 `json:"asset"`
	WindowType            UpDownWindowType       `json:"window_type"`
	ResolutionSourceType  UpDownResolutionSource `json:"resolution_source_type"`
	Timestamp             time.Time              `json:"timestamp"`
	ReferenceStartPrice   *float64               `json:"reference_start_price,omitempty"`
	ReferenceCurrentPrice *float64               `json:"reference_current_price,omitempty"`
	ReferenceEndPrice     *float64               `json:"reference_end_price,omitempty"`
	ReferenceUpdatedAt    *time.Time             `json:"reference_updated_at,omitempty"`

	PMarketUp *float64 `json:"p_market_up,omitempty"`
	PSynthUp  *float64 `json:"p_synth_up,omitempty"`
	PModelUp  *float64 `json:"p_model_up,omitempty"`
	PLPUp     *float64 `json:"p_lp_up,omitempty"`
	PFinalUp  float64  `json:"p_final_up"`

	ExecutableAskUp   float64 `json:"executable_ask_up"`
	ExecutableAskDown float64 `json:"executable_ask_down"`
	ExecutableBidUp   float64 `json:"executable_bid_up"`
	ExecutableBidDown float64 `json:"executable_bid_down"`
	SpreadUp          float64 `json:"spread_up"`
	SpreadDown        float64 `json:"spread_down"`
	DepthImbalance    float64 `json:"depth_imbalance"`
	ExpectedSlippage  float64 `json:"expected_slippage"`

	EVUp           float64 `json:"ev_up"`
	EVDown         float64 `json:"ev_down"`
	EVMinThreshold float64 `json:"ev_min_threshold"`
	FeesBps        float64 `json:"fees_bps"`
	TimeToExpiryMs int64   `json:"time_to_expiry_ms"`
	Regime         string  `json:"regime"`
	Confidence     float64 `json:"confidence"`

	RiskFlags      UpDownRiskFlags      `json:"risk_flags"`
	ReasonCodes    []string             `json:"reason_codes"`
	Recommendation UpDownRecommendation `json:"recommendation"`
}

type UpDownDecisionLogRequest struct {
	Slug             string   `json:"slug"`
	RecommendationID string   `json:"recommendation_id"`
	Action           string   `json:"action"` // accepted | rejected | manual_override | placed
	ChosenSide       string   `json:"chosen_side,omitempty"`
	OverridePrice    *float64 `json:"override_price,omitempty"`
	OverrideSize     *float64 `json:"override_size,omitempty"`
	Notes            string   `json:"notes,omitempty"`
}

type UpDownDecisionLog struct {
	ID               string    `json:"id"`
	UserID           string    `json:"user_id"`
	Slug             string    `json:"slug"`
	RecommendationID string    `json:"recommendation_id"`
	Action           string    `json:"action"`
	ChosenSide       string    `json:"chosen_side,omitempty"`
	OverridePrice    *float64  `json:"override_price,omitempty"`
	OverrideSize     *float64  `json:"override_size,omitempty"`
	Notes            string    `json:"notes,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

type PerformanceSlice struct {
	Key         string  `json:"key"`
	Trades      int64   `json:"trades"`
	HitRate     float64 `json:"hit_rate"`
	BrierScore  float64 `json:"brier_score"`
	RealizedEV  float64 `json:"realized_ev"`
	MaxDrawdown float64 `json:"max_drawdown"`
}

type UpDownPerformanceSummary struct {
	From            string             `json:"from"`
	To              string             `json:"to"`
	Decisions       int64              `json:"decisions"`
	Accepted        int64              `json:"accepted"`
	Rejected        int64              `json:"rejected"`
	ManualOverrides int64              `json:"manual_overrides"`
	Trades          int64              `json:"trades"`
	HitRate         float64            `json:"hit_rate"`
	BrierScore      float64            `json:"brier_score"`
	RealizedEV      float64            `json:"realized_ev"`
	MaxDrawdown     float64            `json:"max_drawdown"`
	ByAsset         []PerformanceSlice `json:"by_asset"`
	ByWindow        []PerformanceSlice `json:"by_window"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

type assetCalibration struct {
	Asset               string
	Samples             int
	BrierScore          float64
	DirectionalAccuracy float64
	ConfidenceScale     float64
	EdgeBuffer          float64
	Source              string
	UpdatedAt           time.Time
}

type UpDownService struct {
	db        *gorm.DB
	redis     *redis.Client
	cfg       *config.Config
	market    *MarketService
	synth     *synthdata.Client
	streamHub *PriceStreamHub

	mu            sync.RWMutex
	marketsBySlug map[string]UpDownMarket
	signalsBySlug map[string]UpDownSignal
	recs          []UpDownRecommendation
	lastRefresh   time.Time
	lastErr       string

	tokenToSlugs     map[string][]string
	conditionToSlugs map[string][]string
	assetToSlugs     map[string][]string

	recomputeMu   sync.Mutex
	recomputeJobs map[string]*time.Timer

	calibrationMu      sync.RWMutex
	calibrationByAsset map[string]assetCalibration
	calibrationUpdated time.Time
}

func NewUpDownService(db *gorm.DB, rdb *redis.Client, cfg *config.Config, marketService *MarketService, synthClient *synthdata.Client) *UpDownService {
	svc := &UpDownService{
		db:                 db,
		redis:              rdb,
		cfg:                cfg,
		market:             marketService,
		synth:              synthClient,
		marketsBySlug:      make(map[string]UpDownMarket),
		signalsBySlug:      make(map[string]UpDownSignal),
		recs:               make([]UpDownRecommendation, 0),
		tokenToSlugs:       make(map[string][]string),
		conditionToSlugs:   make(map[string][]string),
		assetToSlugs:       make(map[string][]string),
		recomputeJobs:      make(map[string]*time.Timer),
		calibrationByAsset: make(map[string]assetCalibration),
	}
	if rdb != nil && svc.Enabled() {
		svc.streamHub = NewPriceStreamHub(rdb, upDownSignalChannel)
	}
	if svc.Enabled() {
		go svc.runRefreshLoop()
		if svc.streamHub != nil {
			go svc.listenPriceUpdates()
		}
	}
	return svc
}

func (s *UpDownService) Enabled() bool {
	return s != nil && s.cfg != nil && s.cfg.Services.UpDownEnabled
}

func (s *UpDownService) ReadOnly() bool {
	return s == nil || s.cfg == nil || s.cfg.Services.UpDownReadOnly
}

func (s *UpDownService) StreamHub() *PriceStreamHub {
	return s.streamHub
}

func (s *UpDownService) runRefreshLoop() {
	// Warmup once so the page has data at first request.
	if err := s.Refresh(context.Background()); err != nil {
		logger.Error("updown refresh warmup failed: %v", err)
	}
	s.refreshCalibrationIfDue(context.Background(), true)
	lastFull := time.Now().UTC()
	for {
		cadence := s.nextCadence(time.Now().UTC())
		start := time.Now().UTC()
		s.refreshCalibrationIfDue(context.Background(), false)

		// Keep market universe fresh every 5 seconds; between those intervals
		// we only refresh active signals to support sub-second micro windows.
		fullRefresh := start.Sub(lastFull) >= 5*time.Second
		ctxTimeout := maxDuration(900*time.Millisecond, cadence*3)
		ctx, cancel := context.WithTimeout(context.Background(), minDuration(ctxTimeout, 10*time.Second))
		if fullRefresh {
			if err := s.Refresh(ctx); err != nil {
				logger.Error("updown refresh failed: %v", err)
			} else {
				lastFull = time.Now().UTC()
			}
		} else {
			if err := s.refreshSignalsOnly(ctx); err != nil {
				logger.Error("updown signal refresh failed: %v", err)
			}
		}
		cancel()

		elapsed := time.Since(start)
		sleep := cadence - elapsed
		if sleep < 25*time.Millisecond {
			sleep = 25 * time.Millisecond
		}
		time.Sleep(sleep)
	}
}

func (s *UpDownService) refreshCalibrationIfDue(ctx context.Context, force bool) {
	if s.synth == nil || !s.synth.Enabled() {
		return
	}
	s.calibrationMu.RLock()
	last := s.calibrationUpdated
	s.calibrationMu.RUnlock()
	if !force && time.Since(last) < 8*time.Minute {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	assets := s.activeAssetsForCalibration()
	now := time.Now().UTC()
	start := now.Add(-36 * time.Hour)
	next := make(map[string]assetCalibration, len(assets))
	for _, asset := range assets {
		stats, err := s.synth.GetHistoricalCalibrationStats(ctx, asset, start, now)
		if err != nil || stats == nil || stats.Samples < 4 {
			continue
		}
		next[asset] = assetCalibration{
			Asset:               asset,
			Samples:             stats.Samples,
			BrierScore:          stats.BrierScore,
			DirectionalAccuracy: stats.DirectionalAccuracy,
			ConfidenceScale:     stats.ConfidenceScale,
			EdgeBuffer:          stats.EdgeBuffer,
			Source:              stats.Source,
			UpdatedAt:           now,
		}
	}
	if len(next) == 0 {
		return
	}

	s.calibrationMu.Lock()
	s.calibrationByAsset = next
	s.calibrationUpdated = now
	s.calibrationMu.Unlock()
}

func (s *UpDownService) activeAssetsForCalibration() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[string]struct{}{
		"BTC": {},
		"ETH": {},
		"SOL": {},
		"XRP": {},
	}
	for _, market := range s.marketsBySlug {
		if market.Asset != "" {
			seen[strings.ToUpper(market.Asset)] = struct{}{}
		}
	}
	assets := make([]string, 0, len(seen))
	for asset := range seen {
		assets = append(assets, asset)
	}
	sort.Strings(assets)
	return assets
}

func (s *UpDownService) getAssetCalibration(asset string) (assetCalibration, bool) {
	asset = strings.ToUpper(strings.TrimSpace(asset))
	s.calibrationMu.RLock()
	defer s.calibrationMu.RUnlock()
	cal, ok := s.calibrationByAsset[asset]
	if !ok {
		return cal, false
	}
	if !cal.UpdatedAt.IsZero() && time.Since(cal.UpdatedAt) > 6*time.Hour {
		return cal, false
	}
	return cal, ok
}

func (s *UpDownService) nextCadence(now time.Time) time.Duration {
	baseSeconds := s.cfg.Services.UpDownPollIntervalSeconds
	if baseSeconds <= 0 {
		baseSeconds = 2
	}
	base := time.Duration(baseSeconds) * time.Second
	if base < 250*time.Millisecond {
		base = 250 * time.Millisecond
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.marketsBySlug) == 0 {
		return base
	}

	hasFastWindow := false
	urgent := false
	for _, m := range s.marketsBySlug {
		if !m.IsActiveWindow {
			continue
		}
		if m.WindowType != Window5m && m.WindowType != Window15m {
			continue
		}
		hasFastWindow = true
		remaining := m.EventEndTime.Sub(now)
		if remaining <= time.Minute && remaining > 0 {
			urgent = true
		}
	}
	if urgent {
		return 350 * time.Millisecond
	}
	if hasFastWindow {
		return time.Second
	}
	return base
}

func (s *UpDownService) refreshSignalsOnly(ctx context.Context) error {
	s.mu.RLock()
	markets := make([]UpDownMarket, 0, len(s.marketsBySlug))
	for _, m := range s.marketsBySlug {
		markets = append(markets, m)
	}
	s.mu.RUnlock()
	if len(markets) == 0 {
		return nil
	}
	s.requestSignalMarketStreams(ctx, markets)

	now := time.Now().UTC()
	signals := make(map[string]UpDownSignal)
	recs := make([]UpDownRecommendation, 0, len(markets))
	synthCache := make(map[string]*synthdata.PolymarketUpDownResponse)
	percentileCache := make(map[string]*synthdata.PredictionPercentilesResponse)
	volCache := make(map[string]*synthdata.VolatilityResponse)
	lpCache := make(map[string]*synthdata.LPProbabilitiesResponse)

	for _, m := range markets {
		m.IsActiveWindow = !now.Before(m.EventStartTime) && now.Before(m.EventEndTime)
		m.TimeToStartSeconds = int64(math.Round(m.EventStartTime.Sub(now).Seconds()))
		m.TimeToEndSeconds = int64(math.Round(m.EventEndTime.Sub(now).Seconds()))

		if !m.IsActiveWindow && !(m.EventStartTime.After(now) && m.EventStartTime.Before(now.Add(20*time.Minute))) {
			continue
		}
		signal, err := s.buildSignal(ctx, m, synthCache, percentileCache, volCache, lpCache)
		if err != nil {
			continue
		}
		signals[m.Slug] = signal
		recs = append(recs, signal.Recommendation)
		s.publishSignal(ctx, m, signal)
	}

	sort.SliceStable(recs, func(i, j int) bool {
		if recs[i].GeneratedAt.Equal(recs[j].GeneratedAt) {
			return recs[i].ExpectedValue > recs[j].ExpectedValue
		}
		return recs[i].GeneratedAt.After(recs[j].GeneratedAt)
	})

	s.mu.Lock()
	for _, m := range markets {
		if _, ok := s.marketsBySlug[m.Slug]; ok {
			s.marketsBySlug[m.Slug] = m
		}
	}
	for slug, sig := range signals {
		s.signalsBySlug[slug] = sig
	}
	s.recs = recs
	s.lastRefresh = now
	s.mu.Unlock()
	return nil
}

func (s *UpDownService) requestSignalMarketStreams(ctx context.Context, markets []UpDownMarket) {
	if s == nil || s.market == nil || len(markets) == 0 {
		return
	}

	tokens := make([]string, 0, len(markets)*2)
	for _, market := range markets {
		if token := strings.TrimSpace(market.Market.TokenIDYes); token != "" {
			tokens = append(tokens, token)
		}
		if token := strings.TrimSpace(market.Market.TokenIDNo); token != "" {
			tokens = append(tokens, token)
		}
	}
	if len(tokens) == 0 {
		return
	}

	s.market.publishStreamRequest(ctx, dedupeStrings(tokens))
}

func (s *UpDownService) listenPriceUpdates() {
	ctx := context.Background()
	for {
		pubsub := s.redis.Subscribe(ctx, PriceUpdateChannel, ChainlinkPriceUpdateChannel)
		ch := pubsub.Channel(redis.WithChannelSize(8192))
		for msg := range ch {
			var update marketPriceUpdate
			if err := json.Unmarshal([]byte(msg.Payload), &update); err != nil {
				continue
			}
			s.scheduleSignalRecompute(update)
		}
		_ = pubsub.Close()
		time.Sleep(800 * time.Millisecond)
	}
}

func (s *UpDownService) scheduleSignalRecompute(update marketPriceUpdate) {
	assetID := strings.TrimSpace(update.AssetID)
	assetSymbol := CanonicalOracleAsset(update.AssetSymbol)
	conditionID := strings.TrimSpace(update.ConditionID)
	if assetID == "" && assetSymbol == "" && conditionID == "" {
		return
	}

	slugs := make([]string, 0, 4)
	s.mu.RLock()
	if assetID != "" {
		slugs = append(slugs, s.tokenToSlugs[assetID]...)
	}
	if assetSymbol != "" {
		slugs = append(slugs, s.assetToSlugs[assetSymbol]...)
	}
	if conditionID != "" {
		slugs = append(slugs, s.conditionToSlugs[conditionID]...)
	}
	s.mu.RUnlock()
	if len(slugs) == 0 {
		return
	}
	slugs = dedupeStrings(slugs)

	for _, slug := range slugs {
		s.recomputeMu.Lock()
		if t, ok := s.recomputeJobs[slug]; ok {
			t.Reset(upDownEventDebounce)
			s.recomputeMu.Unlock()
			continue
		}
		s.recomputeJobs[slug] = time.AfterFunc(upDownEventDebounce, func() {
			ctx, cancel := context.WithTimeout(context.Background(), 1800*time.Millisecond)
			defer cancel()
			s.recomputeSignalBySlug(ctx, slug)
			s.recomputeMu.Lock()
			delete(s.recomputeJobs, slug)
			s.recomputeMu.Unlock()
		})
		s.recomputeMu.Unlock()
	}
}

func (s *UpDownService) recomputeSignalBySlug(ctx context.Context, slug string) {
	s.mu.RLock()
	market, ok := s.marketsBySlug[slug]
	s.mu.RUnlock()
	if !ok {
		return
	}
	now := time.Now().UTC()
	market.IsActiveWindow = !now.Before(market.EventStartTime) && now.Before(market.EventEndTime)
	market.TimeToStartSeconds = int64(math.Round(market.EventStartTime.Sub(now).Seconds()))
	market.TimeToEndSeconds = int64(math.Round(market.EventEndTime.Sub(now).Seconds()))
	if !market.IsActiveWindow && market.EventStartTime.Before(now.Add(-3*time.Minute)) {
		return
	}

	signal, err := s.buildSignal(
		ctx,
		market,
		map[string]*synthdata.PolymarketUpDownResponse{},
		map[string]*synthdata.PredictionPercentilesResponse{},
		map[string]*synthdata.VolatilityResponse{},
		map[string]*synthdata.LPProbabilitiesResponse{},
	)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.marketsBySlug[slug] = market
	s.signalsBySlug[slug] = signal
	updated := make([]UpDownRecommendation, 0, len(s.signalsBySlug))
	for _, sig := range s.signalsBySlug {
		updated = append(updated, sig.Recommendation)
	}
	sort.SliceStable(updated, func(i, j int) bool {
		if updated[i].GeneratedAt.Equal(updated[j].GeneratedAt) {
			return updated[i].ExpectedValue > updated[j].ExpectedValue
		}
		return updated[i].GeneratedAt.After(updated[j].GeneratedAt)
	})
	s.recs = updated
	s.lastRefresh = now
	s.mu.Unlock()

	s.publishSignal(ctx, market, signal)
}

func (s *UpDownService) Refresh(ctx context.Context) error {
	if !s.Enabled() {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	markets, err := s.discoverMarkets(ctx)
	if err != nil {
		s.setLastError(err)
		return err
	}

	signals := make(map[string]UpDownSignal, len(markets))
	recs := make([]UpDownRecommendation, 0, len(markets))

	synthCache := make(map[string]*synthdata.PolymarketUpDownResponse)
	percentileCache := make(map[string]*synthdata.PredictionPercentilesResponse)
	volCache := make(map[string]*synthdata.VolatilityResponse)
	lpCache := make(map[string]*synthdata.LPProbabilitiesResponse)

	candidateSignals := s.pickSignalCandidates(markets)
	s.requestSignalMarketStreams(ctx, candidateSignals)
	for _, market := range candidateSignals {
		signal, buildErr := s.buildSignal(ctx, market, synthCache, percentileCache, volCache, lpCache)
		if buildErr != nil {
			logger.Error("updown signal build failed for %s: %v", market.Slug, buildErr)
			continue
		}
		signals[market.Slug] = signal
		recs = append(recs, signal.Recommendation)
		s.persistSnapshot(ctx, market, signal)
		s.persistRecommendation(ctx, signal.Recommendation)
		s.publishSignal(ctx, market, signal)
	}

	sort.SliceStable(recs, func(i, j int) bool {
		if recs[i].GeneratedAt.Equal(recs[j].GeneratedAt) {
			return recs[i].ExpectedValue > recs[j].ExpectedValue
		}
		return recs[i].GeneratedAt.After(recs[j].GeneratedAt)
	})

	s.mu.Lock()
	s.marketsBySlug = make(map[string]UpDownMarket, len(markets))
	tokenToSlugs := make(map[string][]string, len(markets)*2)
	conditionToSlugs := make(map[string][]string, len(markets))
	assetToSlugs := make(map[string][]string, len(markets))
	for _, m := range markets {
		s.marketsBySlug[m.Slug] = m
		conditionToSlugs[m.ConditionID] = append(conditionToSlugs[m.ConditionID], m.Slug)
		asset := CanonicalOracleAsset(m.Asset)
		if asset != "" {
			assetToSlugs[asset] = append(assetToSlugs[asset], m.Slug)
		}
		if token := strings.TrimSpace(m.Market.TokenIDYes); token != "" {
			tokenToSlugs[token] = append(tokenToSlugs[token], m.Slug)
		}
		if token := strings.TrimSpace(m.Market.TokenIDNo); token != "" {
			tokenToSlugs[token] = append(tokenToSlugs[token], m.Slug)
		}
	}
	for token, slugs := range tokenToSlugs {
		tokenToSlugs[token] = dedupeStrings(slugs)
	}
	for cond, slugs := range conditionToSlugs {
		conditionToSlugs[cond] = dedupeStrings(slugs)
	}
	for asset, slugs := range assetToSlugs {
		assetToSlugs[asset] = dedupeStrings(slugs)
	}
	s.tokenToSlugs = tokenToSlugs
	s.conditionToSlugs = conditionToSlugs
	s.assetToSlugs = assetToSlugs
	s.signalsBySlug = signals
	s.recs = recs
	s.lastRefresh = time.Now().UTC()
	s.lastErr = ""
	s.mu.Unlock()

	s.persistCaches(ctx, markets, recs, signals)
	return nil
}

func (s *UpDownService) setLastError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastErr = err.Error()
}

func (s *UpDownService) pickSignalCandidates(markets []UpDownMarket) []UpDownMarket {
	if len(markets) == 0 {
		return nil
	}
	now := time.Now().UTC()
	out := make([]UpDownMarket, 0, len(markets))
	seenGroup := make(map[string]struct{})
	for _, m := range markets {
		if m.IsActiveWindow {
			out = append(out, m)
			continue
		}
		if m.EventStartTime.After(now) && m.EventStartTime.Before(now.Add(25*time.Minute)) {
			group := fmt.Sprintf("%s|%s", m.Asset, m.WindowType)
			if _, ok := seenGroup[group]; ok {
				continue
			}
			seenGroup[group] = struct{}{}
			out = append(out, m)
		}
	}
	return out
}

func (s *UpDownService) ListMarkets(ctx context.Context, asset string, window string) ([]UpDownMarket, error) {
	s.ensureFresh(ctx)

	s.mu.RLock()
	defer s.mu.RUnlock()

	asset = strings.ToUpper(strings.TrimSpace(asset))
	window = strings.ToLower(strings.TrimSpace(window))
	out := make([]UpDownMarket, 0, len(s.marketsBySlug))
	for _, m := range s.marketsBySlug {
		if asset != "" && !strings.EqualFold(m.Asset, asset) {
			continue
		}
		if window != "" && string(m.WindowType) != window {
			continue
		}
		out = append(out, m)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].EventStartTime.Equal(out[j].EventStartTime) {
			return out[i].Slug < out[j].Slug
		}
		return out[i].EventStartTime.Before(out[j].EventStartTime)
	})
	return out, nil
}

func (s *UpDownService) GetMarket(ctx context.Context, slug string) (*UpDownMarket, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil, errors.New("slug is required")
	}
	s.ensureFresh(ctx)
	s.mu.RLock()
	if m, ok := s.marketsBySlug[slug]; ok {
		s.mu.RUnlock()
		return &m, nil
	}
	s.mu.RUnlock()

	markets, err := s.discoverMarkets(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range markets {
		if m.Slug == slug {
			return &m, nil
		}
	}
	return nil, nil
}

func (s *UpDownService) GetSignal(ctx context.Context, slug string) (*UpDownSignal, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil, errors.New("slug is required")
	}
	s.ensureFresh(ctx)

	s.mu.RLock()
	if signal, ok := s.signalsBySlug[slug]; ok {
		s.mu.RUnlock()
		return &signal, nil
	}
	market, ok := s.marketsBySlug[slug]
	s.mu.RUnlock()
	if !ok {
		return nil, nil
	}

	signal, err := s.buildSignal(
		ctx,
		market,
		map[string]*synthdata.PolymarketUpDownResponse{},
		map[string]*synthdata.PredictionPercentilesResponse{},
		map[string]*synthdata.VolatilityResponse{},
		map[string]*synthdata.LPProbabilitiesResponse{},
	)
	if err != nil {
		return nil, err
	}
	return &signal, nil
}

func (s *UpDownService) ListRecommendations(ctx context.Context, asset string, limit int) ([]UpDownRecommendation, error) {
	s.ensureFresh(ctx)
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	asset = strings.ToUpper(strings.TrimSpace(asset))

	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]UpDownRecommendation, 0, limit)
	for _, rec := range s.recs {
		if asset != "" && !strings.EqualFold(asset, rec.Asset) {
			continue
		}
		out = append(out, rec)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *UpDownService) LogDecision(ctx context.Context, userID string, req UpDownDecisionLogRequest) (*UpDownDecisionLog, error) {
	if s.db == nil {
		return nil, errors.New("database is not configured")
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, fmt.Errorf("%w: user id is required", ErrInvalidUpDownDecisionRequest)
	}
	req.Slug = strings.TrimSpace(req.Slug)
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))
	if req.Slug == "" {
		return nil, fmt.Errorf("%w: slug is required", ErrInvalidUpDownDecisionRequest)
	}
	switch req.Action {
	case "accepted", "rejected", "manual_override", "placed":
	default:
		return nil, fmt.Errorf("%w: invalid action", ErrInvalidUpDownDecisionRequest)
	}

	var row UpDownDecisionLog
	err := s.db.WithContext(ctx).Raw(`
		INSERT INTO updown_decisions (
			user_id, slug, recommendation_id, action, chosen_side, override_price, override_size, notes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id, user_id, slug, recommendation_id, action, chosen_side, override_price, override_size, notes, created_at
	`,
		userID, req.Slug, strings.TrimSpace(req.RecommendationID), req.Action, strings.ToUpper(strings.TrimSpace(req.ChosenSide)),
		req.OverridePrice, req.OverrideSize, strings.TrimSpace(req.Notes),
	).Scan(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *UpDownService) GetPerformance(ctx context.Context, from, to time.Time) (*UpDownPerformanceSummary, error) {
	from = from.UTC()
	to = to.UTC()
	if to.Before(from) {
		return nil, errors.New("invalid date range")
	}
	summary := &UpDownPerformanceSummary{
		From:      from.Format("2006-01-02"),
		To:        to.Format("2006-01-02"),
		ByAsset:   []PerformanceSlice{},
		ByWindow:  []PerformanceSlice{},
		UpdatedAt: time.Now().UTC(),
	}
	if s.db == nil {
		return summary, nil
	}

	var total struct {
		Trades      int64
		HitRate     float64
		BrierScore  float64
		RealizedEV  float64
		MaxDrawdown float64
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT
			COALESCE(SUM(trades_count), 0) AS trades,
			COALESCE(AVG(hit_rate), 0) AS hit_rate,
			COALESCE(AVG(brier_score), 0) AS brier_score,
			COALESCE(SUM(realized_ev), 0) AS realized_ev,
			COALESCE(MAX(max_drawdown), 0) AS max_drawdown
		FROM updown_performance_daily
		WHERE day BETWEEN ? AND ?
	`, from, to).Scan(&total).Error; err != nil {
		return nil, err
	}
	summary.Trades = total.Trades
	summary.HitRate = total.HitRate
	summary.BrierScore = total.BrierScore
	summary.RealizedEV = total.RealizedEV
	summary.MaxDrawdown = total.MaxDrawdown

	var decisions struct {
		Decisions int64
		Accepted  int64
		Rejected  int64
		Manual    int64
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT
			COUNT(*) AS decisions,
			COALESCE(SUM(CASE WHEN action = 'accepted' THEN 1 ELSE 0 END), 0) AS accepted,
			COALESCE(SUM(CASE WHEN action = 'rejected' THEN 1 ELSE 0 END), 0) AS rejected,
			COALESCE(SUM(CASE WHEN action = 'manual_override' THEN 1 ELSE 0 END), 0) AS manual
		FROM updown_decisions
		WHERE created_at BETWEEN ? AND ?
	`, from, to.Add(24*time.Hour)).Scan(&decisions).Error; err == nil {
		summary.Decisions = decisions.Decisions
		summary.Accepted = decisions.Accepted
		summary.Rejected = decisions.Rejected
		summary.ManualOverrides = decisions.Manual
	}

	var byAsset []PerformanceSlice
	if err := s.db.WithContext(ctx).Raw(`
		SELECT
			asset AS key,
			COALESCE(SUM(trades_count), 0) AS trades,
			COALESCE(AVG(hit_rate), 0) AS hit_rate,
			COALESCE(AVG(brier_score), 0) AS brier_score,
			COALESCE(SUM(realized_ev), 0) AS realized_ev,
			COALESCE(MAX(max_drawdown), 0) AS max_drawdown
		FROM updown_performance_daily
		WHERE day BETWEEN ? AND ?
		GROUP BY asset
		ORDER BY trades DESC, key ASC
	`, from, to).Scan(&byAsset).Error; err == nil {
		summary.ByAsset = byAsset
	}

	var byWindow []PerformanceSlice
	if err := s.db.WithContext(ctx).Raw(`
		SELECT
			window_type AS key,
			COALESCE(SUM(trades_count), 0) AS trades,
			COALESCE(AVG(hit_rate), 0) AS hit_rate,
			COALESCE(AVG(brier_score), 0) AS brier_score,
			COALESCE(SUM(realized_ev), 0) AS realized_ev,
			COALESCE(MAX(max_drawdown), 0) AS max_drawdown
		FROM updown_performance_daily
		WHERE day BETWEEN ? AND ?
		GROUP BY window_type
		ORDER BY trades DESC, key ASC
	`, from, to).Scan(&byWindow).Error; err == nil {
		summary.ByWindow = byWindow
	}

	return summary, nil
}

func (s *UpDownService) ensureFresh(ctx context.Context) {
	s.mu.RLock()
	last := s.lastRefresh
	s.mu.RUnlock()
	if time.Since(last) > 18*time.Second {
		_ = s.Refresh(ctx)
	}
}

func (s *UpDownService) persistCaches(ctx context.Context, markets []UpDownMarket, recs []UpDownRecommendation, signals map[string]UpDownSignal) {
	if s.redis == nil {
		return
	}
	if payload, err := json.Marshal(markets); err == nil {
		_ = s.redis.Set(ctx, upDownMarketsCacheKey, payload, upDownCacheTTL).Err()
	}
	if payload, err := json.Marshal(recs); err == nil {
		_ = s.redis.Set(ctx, upDownRecsCacheKey, payload, upDownCacheTTL).Err()
	}
	for slug, signal := range signals {
		if payload, err := json.Marshal(signal); err == nil {
			_ = s.redis.Set(ctx, upDownSignalCachePref+slug, payload, upDownCacheTTL).Err()
		}
	}
}

func (s *UpDownService) publishSignal(ctx context.Context, market UpDownMarket, signal UpDownSignal) {
	if s.redis == nil {
		return
	}
	payload := map[string]interface{}{
		"type":           "signal_update",
		"slug":           market.Slug,
		"condition_id":   market.ConditionID,
		"asset":          market.Asset,
		"window_type":    market.WindowType,
		"event_start":    market.EventStartTime,
		"event_end":      market.EventEndTime,
		"signal":         signal,
		"recommendation": signal.Recommendation,
		"updated_at":     time.Now().UTC(),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_ = s.redis.Publish(ctx, upDownSignalChannel, data).Err()
}

func (s *UpDownService) discoverMarkets(ctx context.Context) ([]UpDownMarket, error) {
	now := time.Now().UTC()
	maxMarkets := s.cfg.Services.UpDownMaxMarkets
	if maxMarkets <= 0 {
		maxMarkets = 64
	}
	if maxMarkets > 500 {
		maxMarkets = 500
	}

	if markets, err := s.discoverMarketsFromActiveSnapshot(ctx, now, maxMarkets); err == nil {
		if len(markets) > 0 {
			return markets, nil
		}
	} else {
		logger.Error("updown active snapshot discovery failed: %v", err)
	}

	// DB fallback is often empty on deployments where worker DB writes are disabled.
	// Pulling a bounded window directly from Gamma keeps up/down rails populated.
	if markets, err := s.discoverMarketsFromGamma(ctx, now, maxMarkets); err == nil {
		if len(markets) > 0 || s.db == nil {
			return markets, nil
		}
	} else if s.db == nil {
		return nil, err
	} else {
		logger.Error("updown gamma discovery failed: %v", err)
	}

	if s.db == nil {
		return nil, errors.New("database is not configured")
	}

	return s.discoverMarketsFromDB(ctx, now, maxMarkets)
}

func (s *UpDownService) discoverMarketsFromActiveSnapshot(ctx context.Context, now time.Time, maxMarkets int) ([]UpDownMarket, error) {
	if s == nil || s.market == nil {
		return nil, nil
	}
	raw, err := s.market.GetActiveMarkets(ctx)
	if err != nil {
		return nil, err
	}
	return classifyUpDownMarkets(raw, now, maxMarkets), nil
}

func (s *UpDownService) discoverMarketsFromGamma(ctx context.Context, now time.Time, maxMarkets int) ([]UpDownMarket, error) {
	if s == nil || s.market == nil || s.market.GammaClient == nil || maxMarkets <= 0 {
		return []UpDownMarket{}, nil
	}

	active := true
	closed := false
	desc := false
	out := make([]UpDownMarket, 0, maxMarkets)
	seen := make(map[string]struct{}, maxMarkets*2)

	for page := 0; page < upDownGammaMaxPages && len(out) < maxMarkets; page++ {
		offset := page * upDownGammaPageLimit
		events, err := s.market.GammaClient.GetEvents(ctx, gamma.GetEventsParams{
			Limit:     upDownGammaPageLimit,
			Offset:    offset,
			Active:    &active,
			Closed:    &closed,
			Order:     "id",
			Ascending: &desc,
		})
		if err != nil {
			return nil, err
		}
		if len(events) == 0 {
			break
		}

		for _, event := range events {
			for _, gm := range event.Markets {
				conditionID := strings.TrimSpace(gm.ConditionID)
				if conditionID == "" {
					continue
				}
				if _, ok := seen[conditionID]; ok {
					continue
				}
				seen[conditionID] = struct{}{}

				market := gm.ToDBModel()
				if market == nil {
					continue
				}

				tags := make([]string, 0, len(event.Tags))
				for _, tag := range event.Tags {
					if slug := strings.TrimSpace(tag.Slug); slug != "" {
						tags = append(tags, slug)
					}
				}
				market.Tags = tags
				market.Category = "general"
				market.Archived = event.Archived
				market.TokenIDYes, market.TokenIDNo = gamma.ParseTokenIDs(gm.ClobTokenIds)

				classified, ok := classifyUpDownCryptoMarket(*market, now)
				if !ok {
					continue
				}
				out = append(out, classified)
				if len(out) >= maxMarkets {
					break
				}
			}
			if len(out) >= maxMarkets {
				break
			}
		}

		if len(events) < upDownGammaPageLimit {
			break
		}
	}

	return sortAndTrimUpDownMarkets(out, maxMarkets), nil
}

func (s *UpDownService) discoverMarketsFromDB(ctx context.Context, now time.Time, maxMarkets int) ([]UpDownMarket, error) {
	batchSize := maxMarkets * 4
	if batchSize < 200 {
		batchSize = 200
	}
	if batchSize > 1000 {
		batchSize = 1000
	}

	out := make([]UpDownMarket, 0, maxMarkets)
	var cursorStart *time.Time
	cursorConditionID := ""
	for len(out) < maxMarkets {
		q := s.db.WithContext(ctx).
			Where("accepting_orders = ? AND closed = ? AND event_start_time IS NOT NULL AND end_date IS NOT NULL AND end_date > ?", true, false, now)
		if cursorStart != nil {
			// Use keyset pagination with a deterministic tiebreaker to avoid duplicates/skips.
			q = q.Where(
				"(event_start_time > ?) OR (event_start_time = ? AND condition_id > ?)",
				*cursorStart,
				*cursorStart,
				cursorConditionID,
			)
		}

		var raw []models.Market
		if err := q.
			Order("event_start_time ASC").
			Order("condition_id ASC").
			Limit(batchSize).
			Find(&raw).Error; err != nil {
			return nil, err
		}
		if len(raw) == 0 {
			break
		}

		for _, m := range raw {
			classified, ok := classifyUpDownCryptoMarket(m, now)
			if !ok {
				continue
			}
			out = append(out, classified)
			if len(out) >= maxMarkets {
				break
			}
		}
		if len(raw) < batchSize {
			break
		}
		last := raw[len(raw)-1]
		if last.EventStartTime == nil {
			break
		}
		t := last.EventStartTime.UTC()
		cursorStart = &t
		cursorConditionID = last.ConditionID
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}

	return sortAndTrimUpDownMarkets(out, maxMarkets), nil
}

func classifyUpDownMarkets(raw []models.Market, now time.Time, maxMarkets int) []UpDownMarket {
	if len(raw) == 0 || maxMarkets <= 0 {
		return []UpDownMarket{}
	}
	out := make([]UpDownMarket, 0, minInt(len(raw), maxMarkets))
	for _, m := range raw {
		classified, ok := classifyUpDownCryptoMarket(m, now)
		if !ok {
			continue
		}
		out = append(out, classified)
	}
	return sortAndTrimUpDownMarkets(out, maxMarkets)
}

func sortAndTrimUpDownMarkets(markets []UpDownMarket, maxMarkets int) []UpDownMarket {
	if len(markets) == 0 {
		return []UpDownMarket{}
	}
	sort.SliceStable(markets, func(i, j int) bool {
		if markets[i].EventStartTime.Equal(markets[j].EventStartTime) {
			return markets[i].Slug < markets[j].Slug
		}
		return markets[i].EventStartTime.Before(markets[j].EventStartTime)
	})
	if maxMarkets > 0 && len(markets) > maxMarkets {
		markets = markets[:maxMarkets]
	}
	return markets
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func classifyUpDownCryptoMarket(m models.Market, now time.Time) (UpDownMarket, bool) {
	if m.EventStartTime == nil || m.EndDate == nil {
		return UpDownMarket{}, false
	}
	if !m.AcceptingOrders || m.Closed || m.EndDate.Before(now) {
		return UpDownMarket{}, false
	}

	outcomes, ok := parseOutcomesArray(m.Outcomes)
	if !ok || len(outcomes) < 2 {
		return UpDownMarket{}, false
	}
	upIdx, downIdx, ok := classifyUpDownOutcomeIndexes(outcomes)
	if !ok {
		return UpDownMarket{}, false
	}

	asset := detectCryptoAsset(m)
	if asset == "" {
		return UpDownMarket{}, false
	}

	source := detectResolutionSource(m.ResolutionRules)
	window := inferWindowType(*m.EventStartTime, *m.EndDate)
	if window == WindowUnknown {
		return UpDownMarket{}, false
	}
	if !isCanonicalUpDownSeriesMarket(m, asset, window) {
		return UpDownMarket{}, false
	}

	ruleSummary := "Resolve Up if end price >= start price."
	if source == ResolutionSourceBinance {
		ruleSummary = "Resolve Up if 1H Binance candle close >= open."
	}

	return UpDownMarket{
		MarketType:            UpDownMarketTypeCrypto,
		Slug:                  m.Slug,
		ConditionID:           m.ConditionID,
		Asset:                 asset,
		WindowType:            window,
		ResolutionSourceType:  source,
		Tradable:              true,
		IsActiveWindow:        !now.Before(*m.EventStartTime) && now.Before(*m.EndDate),
		TimeToStartSeconds:    int64(math.Round(m.EventStartTime.Sub(now).Seconds())),
		TimeToEndSeconds:      int64(math.Round(m.EndDate.Sub(now).Seconds())),
		CreatedAt:             m.MarketCreatedAt,
		EventStartTime:        m.EventStartTime.UTC(),
		EventEndTime:          m.EndDate.UTC(),
		ResolutionRuleSummary: ruleSummary,
		Market:                m,
		OutcomeIndexUp:        upIdx,
		OutcomeIndexDown:      downIdx,
		OutcomeLabelUp:        outcomes[upIdx],
		OutcomeLabelDown:      outcomes[downIdx],
	}, true
}

func parseOutcomesArray(raw string) ([]string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}
	var arr []string
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		return nil, false
	}
	clean := make([]string, 0, len(arr))
	for _, v := range arr {
		t := strings.TrimSpace(v)
		if t != "" {
			clean = append(clean, t)
		}
	}
	return clean, len(clean) > 0
}

func classifyUpDownOutcomeIndexes(outcomes []string) (int, int, bool) {
	upIdx := -1
	downIdx := -1
	for i, o := range outcomes {
		switch strings.ToLower(strings.TrimSpace(o)) {
		case "up":
			upIdx = i
		case "down":
			downIdx = i
		}
	}
	if upIdx < 0 || downIdx < 0 {
		return 0, 0, false
	}
	return upIdx, downIdx, true
}

func containsAnyPhrase(haystack string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.Contains(haystack, phrase) {
			return true
		}
	}
	return false
}

func isCanonicalUpDownSeriesMarket(m models.Market, asset string, window UpDownWindowType) bool {
	slug := strings.ToLower(strings.TrimSpace(m.Slug))
	title := strings.ToLower(strings.TrimSpace(m.Title))
	if slug == "" || title == "" {
		return false
	}
	if !containsAnyPhrase(title, []string{"up or down", "up/down"}) {
		return false
	}

	for _, token := range assetSlugCandidates(asset) {
		if token == "" {
			continue
		}
		if slugHasCanonicalUpDownWindow(slug, token, window) || slugHasLegacyUpOrDownPrefix(slug, token) {
			return true
		}
	}
	return false
}

func slugHasCanonicalUpDownWindow(slug, assetToken string, window UpDownWindowType) bool {
	windowToken := ""
	switch window {
	case Window5m:
		windowToken = "5m"
	case Window15m:
		windowToken = "15m"
	case Window1h:
		windowToken = "1h"
	case Window4h:
		windowToken = "4h"
	default:
		return false
	}
	prefix := assetToken + "-updown-" + windowToken + "-"
	if !strings.HasPrefix(slug, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(slug, prefix)
	if suffix == "" {
		return false
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func slugHasLegacyUpOrDownPrefix(slug, assetToken string) bool {
	return strings.HasPrefix(slug, assetToken+"-up-or-down-")
}

func assetSlugCandidates(asset string) []string {
	switch strings.ToUpper(strings.TrimSpace(asset)) {
	case "BTC":
		return []string{"btc", "bitcoin"}
	case "ETH":
		return []string{"eth", "ethereum"}
	case "SOL":
		return []string{"sol", "solana"}
	case "XRP":
		return []string{"xrp", "ripple"}
	default:
		return []string{strings.ToLower(strings.TrimSpace(asset))}
	}
}

func detectCryptoAsset(m models.Market) string {
	hay := strings.ToLower(strings.Join([]string{m.Slug, m.Title, m.Description, m.ResolutionRules}, " "))
	tokens := make(map[string]struct{})
	for _, token := range strings.FieldsFunc(hay, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if token == "" {
			continue
		}
		tokens[token] = struct{}{}
	}
	hasToken := func(candidates ...string) bool {
		for _, candidate := range candidates {
			if _, ok := tokens[candidate]; ok {
				return true
			}
		}
		return false
	}

	switch {
	case hasToken("bitcoin", "btc"):
		return "BTC"
	case hasToken("ethereum", "eth"):
		return "ETH"
	case hasToken("solana", "sol"):
		return "SOL"
	case hasToken("xrp", "ripple"):
		return "XRP"
	default:
		return ""
	}
}

func detectResolutionSource(rules string) UpDownResolutionSource {
	x := strings.ToLower(strings.TrimSpace(rules))
	switch {
	case strings.Contains(x, "chainlink") || strings.Contains(x, "data.chain.link"):
		return ResolutionSourceChainlink
	case strings.Contains(x, "binance"):
		return ResolutionSourceBinance
	default:
		return ResolutionSourceUnknown
	}
}

func usesChainlinkReference(market UpDownMarket) bool {
	return market.ResolutionSourceType != ResolutionSourceBinance
}

func inferWindowType(start, end time.Time) UpDownWindowType {
	delta := end.Sub(start)
	switch {
	case withinDuration(delta, 5*time.Minute, 2*time.Minute):
		return Window5m
	case withinDuration(delta, 15*time.Minute, 3*time.Minute):
		return Window15m
	case withinDuration(delta, time.Hour, 8*time.Minute):
		return Window1h
	case withinDuration(delta, 4*time.Hour, 20*time.Minute):
		return Window4h
	default:
		return WindowUnknown
	}
}

func withinDuration(actual, target, tolerance time.Duration) bool {
	diff := actual - target
	if diff < 0 {
		diff = -diff
	}
	return diff <= tolerance
}

func (s *UpDownService) buildSignal(
	ctx context.Context,
	market UpDownMarket,
	synthCache map[string]*synthdata.PolymarketUpDownResponse,
	percentileCache map[string]*synthdata.PredictionPercentilesResponse,
	volCache map[string]*synthdata.VolatilityResponse,
	lpCache map[string]*synthdata.LPProbabilitiesResponse,
) (UpDownSignal, error) {
	now := time.Now().UTC()
	if s.market != nil {
		live := []models.Market{market.Market}
		s.market.attachRealtimePrices(ctx, live)
		if len(live) == 1 {
			market.Market = live[0]
		}
	}

	upToken, downToken, err := tokenIDsByOutcome(market)
	if err != nil {
		return UpDownSignal{}, err
	}

	probeSize := math.Max(s.cfg.Services.UpDownDepthProbeShares, 10)
	upAsk, upBid, upSlippage, upMissingDepth := s.executablePrices(ctx, market.Market, upToken, probeSize)
	downAsk, downBid, downSlippage, downMissingDepth := s.executablePrices(ctx, market.Market, downToken, probeSize)

	feesFrac := upDownClamp(s.cfg.Services.UpDownFeeBps/10_000.0, 0, 0.02)
	horizon := horizonForWindow(market.WindowType)
	synthStaleThreshold := synthStaleThresholdForMarket(s.cfg, market.WindowType)
	synthClockDriftMax := synthClockDriftThresholdForMarket(s.cfg, market.WindowType)
	marketStaleThreshold := marketQuoteStaleThresholdForMarket(s.cfg, market.WindowType)

	var pMarketPtr *float64
	var pSynthPtr *float64
	var pModelPtr *float64
	var pLPPtr *float64
	var referenceStartPrice *float64
	var referenceCurrentPrice *float64
	var referenceEndPrice *float64
	var referenceUpdatedAt *time.Time
	flags := UpDownRiskFlags{
		ReadOnly:   s.cfg.Services.UpDownReadOnly,
		KillSwitch: s.cfg.Services.UpDownKillSwitch,
	}
	reasons := make([]string, 0, 12)

	if marketProb := marketProbability(upAsk, downAsk); marketProb > 0 {
		v := marketProb
		pMarketPtr = &v
	}

	if usesChainlinkReference(market) {
		oracleLatest := GetChainlinkLatest(ctx, s.redis, market.Asset)
		if oracleLatest != nil {
			if oracleLatest.Price > 0 {
				v := oracleLatest.Price
				referenceCurrentPrice = &v
			}
			if !oracleLatest.UpdatedAt.IsZero() {
				refTime := oracleLatest.UpdatedAt
				referenceUpdatedAt = &refTime
			}
			if !oracleLatest.UpdatedAt.Before(market.EventStartTime) {
				_ = CaptureChainlinkStart(ctx, s.redis, market.Asset, market.EventStartTime, *oracleLatest)
			}
			if !oracleLatest.UpdatedAt.Before(market.EventEndTime) {
				_ = CaptureChainlinkEnd(ctx, s.redis, market.Asset, market.EventEndTime, *oracleLatest)
			}
		}
		if startPoint := GetChainlinkStart(ctx, s.redis, market.Asset, market.EventStartTime); startPoint != nil && startPoint.Price > 0 {
			v := startPoint.Price
			referenceStartPrice = &v
			if referenceUpdatedAt == nil || startPoint.UpdatedAt.After(*referenceUpdatedAt) {
				refTime := startPoint.UpdatedAt
				referenceUpdatedAt = &refTime
			}
		}
		if endPoint := GetChainlinkEnd(ctx, s.redis, market.Asset, market.EventEndTime); endPoint != nil && endPoint.Price > 0 {
			v := endPoint.Price
			referenceEndPrice = &v
			if referenceUpdatedAt == nil || endPoint.UpdatedAt.After(*referenceUpdatedAt) {
				refTime := endPoint.UpdatedAt
				referenceUpdatedAt = &refTime
			}
		}
	}

	synthResp := s.getSynthUpDownCached(ctx, market, synthCache)
	var synthClock time.Time
	if synthResp != nil && !synthUpDownResponseMatchesMarket(market, synthResp) {
		flags.SourceMismatch = true
		reasons = append(reasons, "synth_market_window_mismatch")
		synthResp = nil
	}
	if synthResp != nil {
		if synthResp.StartPrice > 0 {
			v := synthResp.StartPrice
			if referenceStartPrice == nil {
				referenceStartPrice = &v
			}
		}
		if synthResp.CurrentPrice > 0 {
			v := synthResp.CurrentPrice
			if referenceCurrentPrice == nil {
				referenceCurrentPrice = &v
			}
		}
		if synthResp.SynthProbabilityUp >= 0 && synthResp.SynthProbabilityUp <= 1 {
			v := synthResp.SynthProbabilityUp
			pSynthPtr = &v
		}
		if t, ok := parseSynthTimestamp(synthResp.CurrentTime); ok {
			synthClock = t
			if referenceUpdatedAt == nil {
				refTime := t
				referenceUpdatedAt = &refTime
			}
		}
		if !synthClock.IsZero() && now.Sub(synthClock) > synthStaleThreshold {
			flags.SynthStale = true
			reasons = append(reasons, "synth_stale")
		}
		if !synthClock.IsZero() && absDuration(now.Sub(synthClock)) > synthClockDriftMax {
			flags.ClockDrift = true
			reasons = append(reasons, "clock_drift")
		}
	}

	var volSummary *synthdata.VolatilityResponse
	if supportsSynthAnalyticsForWindow(market.WindowType) && s.synth != nil && s.synth.Enabled() {
		volSummary = s.getSynthVolatilityCached(ctx, market, volCache)
	}
	if volSummary != nil && volSummary.ForecastFuture.AverageVolatility >= 80 {
		flags.HighVolatility = true
		reasons = append(reasons, "high_volatility_regime")
	}

	thresholdPrice := resolveUpThresholdPrice(referenceStartPrice, synthResp)
	if thresholdPrice > 0 && supportsSynthAnalyticsForWindow(market.WindowType) && s.synth != nil && s.synth.Enabled() {
		lp := s.getSynthLPProbabilitiesCached(ctx, market, lpCache)
		if p, ok := lpProbabilityAtThreshold(lp, horizon, thresholdPrice); ok {
			v := upDownClamp(p, 0.01, 0.99)
			pLPPtr = &v
			reasons = append(reasons, "lp_probability_anchor")
		}
	}

	if thresholdPrice > 0 && supportsSynthAnalyticsForWindow(market.WindowType) && s.cfg.Services.UpDownEnterpriseEnabled && s.synth != nil && s.synth.Enabled() {
		pModel := s.computeModelProbability(ctx, market, thresholdPrice, percentileCache)
		if pModel > 0 {
			v := upDownClamp(pModel, 0.01, 0.99)
			pModelPtr = &v
		}
	}
	if supportsDirectSynthUpDownWindow(market.WindowType) && pSynthPtr == nil {
		flags.SynthMissing = true
		reasons = append(reasons, "synth_missing")
	}

	calibration, hasCalibration := s.getAssetCalibration(market.Asset)
	pFinalUp, confidence := blendProbabilities(pMarketPtr, pSynthPtr, pModelPtr, pLPPtr, upAsk, downAsk, flags)
	if pLPPtr != nil {
		lpDelta := math.Abs(pFinalUp - *pLPPtr)
		switch {
		case lpDelta >= 0.20:
			confidence *= 0.72
			reasons = append(reasons, "lp_disagreement_high")
		case lpDelta >= 0.12:
			confidence *= 0.86
			reasons = append(reasons, "lp_disagreement_moderate")
		case lpDelta <= 0.05:
			confidence *= 1.04
			reasons = append(reasons, "lp_confirmation")
		}
	}
	if hasCalibration {
		confScale := upDownClamp(calibration.ConfidenceScale, 0.55, 1.10)
		if pMarketPtr != nil && confScale < 1 {
			pFinalUp = confScale*pFinalUp + (1-confScale)*(*pMarketPtr)
		}
		confidence *= confScale
		reasons = append(reasons, "historical_calibration_"+strings.ReplaceAll(calibration.Source, "/", "_"))
		if calibration.Samples > 0 {
			reasons = append(reasons, fmt.Sprintf("calibration_samples_%d", calibration.Samples))
		}
		if calibration.BrierScore >= 0.24 {
			reasons = append(reasons, "calibration_weak")
		} else if calibration.BrierScore <= 0.18 {
			reasons = append(reasons, "calibration_strong")
		}
	}
	pFinalUp = upDownClamp(pFinalUp, 0.01, 0.99)

	marketUpdated := latestMarketTimestamp(market.Market)
	if !marketUpdated.IsZero() && now.Sub(marketUpdated) > marketStaleThreshold {
		flags.MarketStale = true
		reasons = append(reasons, "market_stale")
	}

	spreadUp := positiveOrZero(upAsk - upBid)
	spreadDown := positiveOrZero(downAsk - downBid)
	maxSpread := upDownClamp(s.cfg.Services.UpDownMaxSpreadToTrade, 0.01, 0.2)
	flags.WideSpread = spreadUp >= maxSpread || spreadDown >= maxSpread
	if flags.WideSpread {
		reasons = append(reasons, "wide_spread")
	}

	flags.DepthMissing = upMissingDepth || downMissingDepth
	if flags.DepthMissing {
		reasons = append(reasons, "missing_depth")
	}

	if upAsk <= 0 || downAsk <= 0 {
		flags.DataIntegrityFailed = true
		reasons = append(reasons, "non_executable_quotes")
	}
	if market.TimeToEndSeconds < 90 || (market.TimeToStartSeconds > -45 && market.TimeToStartSeconds < 45) {
		flags.StatusBoundary = true
		reasons = append(reasons, "boundary_state")
	}
	if market.Market.Liquidity < 5_000 {
		flags.LowLiquidity = true
		reasons = append(reasons, "low_liquidity")
	}
	minTopDepth := math.Max(s.cfg.Services.UpDownMinTopDepth, 0)
	if minTopDepth > 0 && synthResp != nil {
		topDepth := math.Max(synthResp.BestBidSize, 0) + math.Max(synthResp.BestAskSize, 0)
		if topDepth > 0 && topDepth < minTopDepth {
			flags.LowLiquidity = true
			reasons = append(reasons, "thin_top_of_book")
		}
	}

	evUp := expectedValueBuyBinary(pFinalUp, upAsk, feesFrac)
	evDown := expectedValueBuyBinary(1-pFinalUp, downAsk, feesFrac)

	timeToExpiryMs := int64(math.Round(market.EventEndTime.Sub(now).Seconds() * 1000))
	regime := inferRegime(market.Market, volSummary)
	confidence = adjustConfidenceForVolatility(confidence, volSummary, market.TimeToEndSeconds, flags, hasCalibration, calibration)
	dynamicEVMin := computeDynamicEVThreshold(
		s.cfg.Services.UpDownEVMinThreshold,
		regime,
		market.TimeToEndSeconds,
		volSummary,
		hasCalibration,
		calibration,
	)
	reasons = append(reasons, fmt.Sprintf("dynamic_ev_min_%.4f", dynamicEVMin))
	depthImb := depthImbalance(synthResp)
	expectedSlippage := maxFloat(upSlippage, downSlippage)

	rec := buildRecommendation(now, market, pFinalUp, evUp, evDown, upAsk, downAsk, confidence, dynamicEVMin, flags, reasons, s.cfg)
	signal := UpDownSignal{
		Slug:                  market.Slug,
		ConditionID:           market.ConditionID,
		Asset:                 market.Asset,
		WindowType:            market.WindowType,
		ResolutionSourceType:  market.ResolutionSourceType,
		Timestamp:             now,
		ReferenceStartPrice:   referenceStartPrice,
		ReferenceCurrentPrice: referenceCurrentPrice,
		ReferenceEndPrice:     referenceEndPrice,
		ReferenceUpdatedAt:    referenceUpdatedAt,
		PMarketUp:             pMarketPtr,
		PSynthUp:              pSynthPtr,
		PModelUp:              pModelPtr,
		PLPUp:                 pLPPtr,
		PFinalUp:              pFinalUp,
		ExecutableAskUp:       upAsk,
		ExecutableAskDown:     downAsk,
		ExecutableBidUp:       upBid,
		ExecutableBidDown:     downBid,
		SpreadUp:              spreadUp,
		SpreadDown:            spreadDown,
		DepthImbalance:        depthImb,
		ExpectedSlippage:      expectedSlippage,
		EVUp:                  evUp,
		EVDown:                evDown,
		EVMinThreshold:        dynamicEVMin,
		FeesBps:               s.cfg.Services.UpDownFeeBps,
		TimeToExpiryMs:        timeToExpiryMs,
		Regime:                regime,
		Confidence:            confidence,
		RiskFlags:             flags,
		ReasonCodes:           dedupeStrings(reasons),
		Recommendation:        rec,
	}
	return signal, nil
}

func tokenIDsByOutcome(market UpDownMarket) (string, string, error) {
	yesToken := strings.TrimSpace(market.Market.TokenIDYes)
	noToken := strings.TrimSpace(market.Market.TokenIDNo)
	if yesToken == "" || noToken == "" {
		return "", "", errors.New("market token ids missing")
	}
	upIdx := market.OutcomeIndexUp
	downIdx := market.OutcomeIndexDown
	if upIdx < 0 || downIdx < 0 || upIdx == downIdx {
		outcomes, ok := parseOutcomesArray(market.Market.Outcomes)
		if !ok || len(outcomes) < 2 {
			return "", "", errors.New("market outcomes unavailable")
		}
		resolvedUp, resolvedDown, ok := classifyUpDownOutcomeIndexes(outcomes)
		if !ok {
			return "", "", errors.New("market outcomes are not up/down")
		}
		upIdx = resolvedUp
		downIdx = resolvedDown
	}
	if upIdx == 0 && downIdx == 1 {
		return yesToken, noToken, nil
	}
	return noToken, yesToken, nil
}

func (s *UpDownService) executablePrices(ctx context.Context, market models.Market, tokenID string, probeSize float64) (ask float64, bid float64, slippage float64, missingDepth bool) {
	if tokenID == "" {
		return 0, 0, 0, true
	}
	bestBid, bestAsk, _ := quoteForToken(market, tokenID)
	ask = bestAsk
	bid = bestBid

	var buyDepth *DepthEstimate
	if est, err := s.market.GetDepthEstimate(ctx, market.ConditionID, tokenID, "BUY", probeSize); err == nil && est != nil {
		if est.EstimatedAveragePrice > 0 {
			ask = est.EstimatedAveragePrice
		}
		buyDepth = est
	}
	if est, err := s.market.GetDepthEstimate(ctx, market.ConditionID, tokenID, "SELL", probeSize); err == nil && est != nil {
		if est.EstimatedAveragePrice > 0 {
			bid = est.EstimatedAveragePrice
		}
	}
	if ask <= 0 || bid <= 0 {
		missingDepth = true
	}
	if buyDepth != nil && bestAsk > 0 && buyDepth.EstimatedAveragePrice > 0 {
		slippage = math.Abs(buyDepth.EstimatedAveragePrice-bestAsk) / bestAsk
	}
	return upDownClamp(ask, 0, 0.99), upDownClamp(bid, 0, 0.99), upDownClamp(slippage, 0, 0.5), missingDepth
}

func quoteForToken(market models.Market, tokenID string) (bid float64, ask float64, last float64) {
	switch tokenID {
	case market.TokenIDYes:
		return market.YesBestBid, market.YesBestAsk, market.YesPrice
	case market.TokenIDNo:
		return market.NoBestBid, market.NoBestAsk, market.NoPrice
	default:
		return 0, 0, 0
	}
}

func marketProbability(upAsk, downAsk float64) float64 {
	if upAsk > 0 && downAsk > 0 {
		total := upAsk + downAsk
		if total > 0 {
			return upDownClamp(upAsk/total, 0.01, 0.99)
		}
	}
	if upAsk > 0 {
		return upDownClamp(upAsk, 0.01, 0.99)
	}
	if downAsk > 0 {
		return upDownClamp(1-downAsk, 0.01, 0.99)
	}
	return 0
}

func expectedValueBuyBinary(pWin, ask, fee float64) float64 {
	if ask <= 0 || ask >= 1 {
		return -1
	}
	return pWin*(1-ask-fee) - (1-pWin)*ask
}

func blendProbabilities(pMarket, pSynth, pModel, pLP *float64, upAsk, downAsk float64, flags UpDownRiskFlags) (float64, float64) {
	type weighted struct {
		p float64
		w float64
	}
	parts := make([]weighted, 0, 4)
	if pMarket != nil && *pMarket > 0 {
		w := 0.18
		if upAsk <= 0 || downAsk <= 0 {
			w = 0.08
		}
		parts = append(parts, weighted{p: *pMarket, w: w})
	}
	if pSynth != nil && *pSynth > 0 {
		w := 0.34
		if flags.SynthStale {
			w = 0.18
		}
		parts = append(parts, weighted{p: *pSynth, w: w})
	}
	if pModel != nil && *pModel > 0 {
		parts = append(parts, weighted{p: *pModel, w: 0.30})
	}
	if pLP != nil && *pLP > 0 {
		parts = append(parts, weighted{p: *pLP, w: 0.18})
	}
	if len(parts) == 0 {
		return 0.5, 0.1
	}

	var weightSum float64
	var mean float64
	for _, part := range parts {
		weightSum += part.w
		mean += part.p * part.w
	}
	if weightSum <= 0 {
		return 0.5, 0.1
	}
	mean /= weightSum

	var variance float64
	for _, part := range parts {
		delta := part.p - mean
		variance += part.w * delta * delta
	}
	variance /= weightSum
	std := math.Sqrt(math.Max(variance, 0))

	consensus := 1 - upDownClamp(std*3, 0, 0.9)
	confidence := 0.25 + 0.75*consensus
	if flags.WideSpread {
		confidence *= 0.8
	}
	if flags.DepthMissing {
		confidence *= 0.7
	}
	if flags.KillSwitch || flags.DataIntegrityFailed {
		confidence *= 0.2
	}
	return mean, upDownClamp(confidence, 0, 1)
}

func inferRegime(m models.Market, vol *synthdata.VolatilityResponse) string {
	change := math.Abs(m.OneHourPriceChange)
	if vol != nil && vol.ForecastFuture.AverageVolatility > 70 {
		return "volatile"
	}
	if change >= 0.02 {
		return "momentum"
	}
	if change >= 0.008 {
		return "transitional"
	}
	return "mean_reversion"
}

func adjustConfidenceForVolatility(
	baseConfidence float64,
	vol *synthdata.VolatilityResponse,
	timeToEndSeconds int64,
	flags UpDownRiskFlags,
	hasCalibration bool,
	cal assetCalibration,
) float64 {
	conf := upDownClamp(baseConfidence, 0, 1)

	volScale := 1.0
	if vol != nil {
		v := vol.ForecastFuture.AverageVolatility
		switch {
		case v >= 120:
			volScale = 0.52
		case v >= 90:
			volScale = 0.64
		case v >= 70:
			volScale = 0.76
		case v >= 55:
			volScale = 0.86
		case v <= 25:
			volScale = 1.05
		}
		// Penalize volatility regime expansion vs recent past.
		past := vol.ForecastPast.AverageVolatility
		if past > 0 {
			expansion := v / past
			if expansion >= 1.4 {
				volScale *= 0.82
			} else if expansion >= 1.2 {
				volScale *= 0.9
			}
		}
	}
	conf *= volScale

	// Time-decay confidence near expiry where microstructure noise dominates.
	switch {
	case timeToEndSeconds <= 45:
		conf *= 0.60
	case timeToEndSeconds <= 120:
		conf *= 0.74
	case timeToEndSeconds <= 300:
		conf *= 0.86
	}

	if hasCalibration {
		conf *= upDownClamp(cal.ConfidenceScale, 0.55, 1.10)
	}
	if flags.SynthStale {
		conf *= 0.82
	}
	if flags.WideSpread {
		conf *= 0.78
	}
	if flags.DepthMissing {
		conf *= 0.76
	}
	if flags.KillSwitch || flags.DataIntegrityFailed {
		conf *= 0.25
	}

	return upDownClamp(conf, 0.02, 1.0)
}

func computeDynamicEVThreshold(
	base float64,
	regime string,
	timeToEndSeconds int64,
	vol *synthdata.VolatilityResponse,
	hasCalibration bool,
	cal assetCalibration,
) float64 {
	threshold := upDownClamp(base, 0.001, 0.2)

	switch regime {
	case "volatile":
		threshold += 0.010
	case "momentum":
		threshold += 0.005
	case "transitional":
		threshold += 0.003
	default:
		threshold += 0.001
	}

	switch {
	case timeToEndSeconds <= 45:
		threshold += 0.025
	case timeToEndSeconds <= 120:
		threshold += 0.014
	case timeToEndSeconds <= 300:
		threshold += 0.008
	}

	if vol != nil {
		v := vol.ForecastFuture.AverageVolatility
		switch {
		case v >= 120:
			threshold += 0.020
		case v >= 90:
			threshold += 0.012
		case v >= 70:
			threshold += 0.008
		case v >= 55:
			threshold += 0.004
		}
	}

	if hasCalibration {
		threshold += upDownClamp(cal.EdgeBuffer, 0, 0.05)
		if cal.Samples >= 10 && cal.DirectionalAccuracy >= 0.62 {
			threshold -= 0.002
		}
	}

	return upDownClamp(threshold, base, 0.14)
}

func depthImbalance(resp *synthdata.PolymarketUpDownResponse) float64 {
	if resp == nil {
		return 0
	}
	a := math.Max(resp.BestAskSize, 0)
	b := math.Max(resp.BestBidSize, 0)
	if a+b <= 0 {
		return 0
	}
	return upDownClamp((b-a)/(a+b), -1, 1)
}

func buildRecommendation(
	now time.Time,
	market UpDownMarket,
	pFinalUp float64,
	evUp float64,
	evDown float64,
	upAsk float64,
	downAsk float64,
	confidence float64,
	minEV float64,
	flags UpDownRiskFlags,
	reasons []string,
	cfg *config.Config,
) UpDownRecommendation {
	reasonCodes := append([]string{}, reasons...)
	decision := "NO_TRADE"
	side := "NONE"
	bestEV := evUp
	bestPrice := upAsk
	outcomeIdx := market.OutcomeIndexUp
	if evDown > bestEV {
		bestEV = evDown
		bestPrice = downAsk
		side = "DOWN"
		decision = "BUY_DOWN"
		outcomeIdx = market.OutcomeIndexDown
	} else {
		side = "UP"
		decision = "BUY_UP"
	}

	disabledReason := ""
	if bestEV <= minEV {
		decision = "NO_TRADE"
		disabledReason = "Expected value below strategy threshold."
		reasonCodes = append(reasonCodes, "ev_below_threshold")
	}
	if flags.KillSwitch || flags.DataIntegrityFailed || flags.StatusBoundary {
		decision = "NO_TRADE"
		if disabledReason == "" {
			disabledReason = "Risk control block active for this market."
		}
		reasonCodes = append(reasonCodes, "hard_risk_block")
	}
	if flags.WideSpread {
		decision = "NO_TRADE"
		if disabledReason == "" {
			disabledReason = "Spread is above configured max spread guard."
		}
		reasonCodes = append(reasonCodes, "spread_guard")
	}
	if flags.LowLiquidity {
		decision = "NO_TRADE"
		if disabledReason == "" {
			disabledReason = "Insufficient top-of-book liquidity."
		}
		reasonCodes = append(reasonCodes, "liquidity_guard")
	}
	if flags.SynthStale || flags.MarketStale {
		decision = "NO_TRADE"
		if disabledReason == "" {
			disabledReason = "Signal timestamps are stale."
		}
		reasonCodes = append(reasonCodes, "staleness_guard")
	}
	if confidence < upDownClamp(cfg.Services.UpDownMinConfidence, 0.05, 0.98) {
		decision = "NO_TRADE"
		if disabledReason == "" {
			disabledReason = "Confidence below configured minimum."
		}
		reasonCodes = append(reasonCodes, "confidence_guard")
	}
	cutoffSeconds := noTradeCutoffForWindow(market.WindowType, cfg)
	if cutoffSeconds > 0 && market.TimeToEndSeconds <= int64(cutoffSeconds) {
		decision = "NO_TRADE"
		if disabledReason == "" {
			disabledReason = fmt.Sprintf("No-trade window active (%ds to expiry).", cutoffSeconds)
		}
		reasonCodes = append(reasonCodes, "time_cutoff_guard")
	}

	pWin := pFinalUp
	if side == "DOWN" {
		pWin = 1 - pFinalUp
	}
	kellyRaw := 0.0
	if bestPrice > 0 && bestPrice < 1 {
		kellyRaw = (pWin - bestPrice) / math.Max(1-bestPrice, 1e-6)
	}
	kellyRaw = upDownClamp(kellyRaw, 0, 1)
	kelly := kellyRaw * upDownClamp(cfg.Services.UpDownKellyFraction, 0.01, 1.0)
	kelly *= (0.45 + 0.55*confidence)
	kelly = math.Min(kelly, cfg.Services.UpDownMaxFractionPerTrade)
	kelly = math.Min(kelly, cfg.Services.UpDownAssetExposureCap)
	kelly *= upDownClamp(cfg.Services.UpDownDailyDrawdownThrottle, 0.2, 1.0)
	kelly = upDownClamp(kelly, 0, cfg.Services.UpDownMaxFractionPerTrade)

	notional := cfg.Services.UpDownNotionalBankroll * kelly
	shares := 0.0
	if bestPrice > 0 {
		shares = notional / bestPrice
	}
	if shares < 1 {
		shares = 0
	}

	prefill := RecommendationPrefill{
		Side:         "BUY",
		OutcomeIndex: outcomeIdx,
		LimitPrice:   upDownClamp(bestPrice, 0.01, 0.99),
		SizeShares:   shares,
	}
	if decision == "NO_TRADE" {
		prefill.Disabled = true
		if disabledReason == "" {
			disabledReason = "No positive EV setup under current risk filters."
		}
		prefill.DisabledWhy = disabledReason
	}
	if flags.ReadOnly {
		prefill.Disabled = true
		prefill.DisabledWhy = "Read-only mode enabled."
	}

	invalidation := []string{
		fmt.Sprintf("Cancel recommendation if spread exceeds %.2f.", upDownClamp(cfg.Services.UpDownMaxSpreadToTrade, 0.01, 0.2)),
		"Cancel if synth/market timestamps become stale.",
		"Cancel if expected value falls below threshold.",
		fmt.Sprintf("Cancel inside final %d seconds to expiry.", maxInt(cutoffSeconds, 30)),
	}
	if side == "UP" {
		invalidation = append(invalidation, "Cancel if p_final_up drops below market implied probability.")
	} else if side == "DOWN" {
		invalidation = append(invalidation, "Cancel if p_final_up rises above market implied probability.")
	}

	return UpDownRecommendation{
		ID:                     fmt.Sprintf("%s-%d", market.Slug, now.Unix()),
		Slug:                   market.Slug,
		ConditionID:            market.ConditionID,
		Asset:                  market.Asset,
		WindowType:             market.WindowType,
		Profile:                defaultRiskProfile,
		Decision:               decision,
		RecommendedSide:        side,
		OrderSide:              "BUY",
		ExpectedValue:          bestEV,
		Confidence:             confidence,
		SuggestedLimitPrice:    prefill.LimitPrice,
		SuggestedSizeShares:    prefill.SizeShares,
		SuggestedNotional:      notional,
		KellyRaw:               kellyRaw,
		KellyCapped:            kelly,
		ReasonCodes:            dedupeStrings(reasonCodes),
		InvalidationConditions: invalidation,
		RiskFlags:              flags,
		Prefill:                prefill,
		GeneratedAt:            now,
	}
}

func noTradeCutoffForWindow(window UpDownWindowType, cfg *config.Config) int {
	if cfg == nil {
		return 60
	}
	switch window {
	case Window5m:
		return maxInt(cfg.Services.UpDownNoTradeCutoff5mSeconds, 0)
	case Window15m:
		return maxInt(cfg.Services.UpDownNoTradeCutoff15mSeconds, 0)
	case Window1h:
		return maxInt(cfg.Services.UpDownNoTradeCutoff1hSeconds, 0)
	case Window4h:
		return maxInt(cfg.Services.UpDownNoTradeCutoff4hSeconds, 0)
	default:
		return 60
	}
}

func (s *UpDownService) getSynthUpDownCached(ctx context.Context, market UpDownMarket, cache map[string]*synthdata.PolymarketUpDownResponse) *synthdata.PolymarketUpDownResponse {
	if s.synth == nil || !s.synth.Enabled() {
		return nil
	}
	window := synthWindowForMarket(market.WindowType)
	if window == "" {
		return nil
	}
	horizon := horizonForWindow(market.WindowType)
	key := synthUpDownCacheKey(market)
	if key == "" {
		key = strings.ToUpper(strings.TrimSpace(market.Asset)) + "|" + string(window) + "|" + strings.ToLower(strings.TrimSpace(horizon))
	}
	if cached, ok := cache[key]; ok {
		return cached
	}
	resp, err := s.synth.GetPolymarketUpDown(ctx, market.Asset, window, horizon, 14, 10)
	if err != nil {
		cache[key] = nil
		return nil
	}
	cache[key] = resp
	return resp
}

func synthUpDownCacheKey(market UpDownMarket) string {
	asset := strings.ToUpper(strings.TrimSpace(market.Asset))
	if asset == "" {
		return ""
	}
	window := synthWindowForMarket(market.WindowType)
	if window == "" {
		return ""
	}
	horizon := strings.ToLower(strings.TrimSpace(horizonForWindow(market.WindowType)))
	return asset + "|" + string(window) + "|" + horizon
}

func (s *UpDownService) getSynthPercentilesCached(ctx context.Context, market UpDownMarket, cache map[string]*synthdata.PredictionPercentilesResponse) *synthdata.PredictionPercentilesResponse {
	if s.synth == nil || !s.synth.Enabled() {
		return nil
	}
	key := market.Asset + "|" + horizonForWindow(market.WindowType)
	if cached, ok := cache[key]; ok {
		return cached
	}
	resp, err := s.synth.GetPredictionPercentiles(ctx, market.Asset, horizonForWindow(market.WindowType), 14, 10)
	if err != nil {
		cache[key] = nil
		return nil
	}
	cache[key] = resp
	return resp
}

func (s *UpDownService) getSynthVolatilityCached(ctx context.Context, market UpDownMarket, cache map[string]*synthdata.VolatilityResponse) *synthdata.VolatilityResponse {
	if s.synth == nil || !s.synth.Enabled() {
		return nil
	}
	key := market.Asset + "|" + horizonForWindow(market.WindowType)
	if cached, ok := cache[key]; ok {
		return cached
	}
	resp, err := s.synth.GetVolatility(ctx, market.Asset, horizonForWindow(market.WindowType), 14, 10)
	if err != nil {
		cache[key] = nil
		return nil
	}
	cache[key] = resp
	return resp
}

func (s *UpDownService) getSynthLPProbabilitiesCached(ctx context.Context, market UpDownMarket, cache map[string]*synthdata.LPProbabilitiesResponse) *synthdata.LPProbabilitiesResponse {
	if s.synth == nil || !s.synth.Enabled() {
		return nil
	}
	key := market.Asset + "|" + horizonForWindow(market.WindowType)
	if cached, ok := cache[key]; ok {
		return cached
	}
	resp, err := s.synth.GetLPProbabilities(ctx, market.Asset, horizonForWindow(market.WindowType), 14, 10)
	if err != nil {
		cache[key] = nil
		return nil
	}
	cache[key] = resp
	return resp
}

func resolveUpThresholdPrice(referenceStartPrice *float64, synthResp *synthdata.PolymarketUpDownResponse) float64 {
	if referenceStartPrice != nil && *referenceStartPrice > 0 {
		return *referenceStartPrice
	}
	if synthResp != nil && synthResp.StartPrice > 0 {
		return synthResp.StartPrice
	}
	return 0
}

func lpProbabilityAtThreshold(resp *synthdata.LPProbabilitiesResponse, horizon string, thresholdPrice float64) (float64, bool) {
	if resp == nil || len(resp.Data) == 0 || thresholdPrice <= 0 {
		return 0, false
	}

	var section synthdata.LPProbabilityHorizon
	ok := false
	for _, key := range lpHorizonCandidates(horizon) {
		if value, exists := resp.Data[key]; exists {
			section = value
			ok = true
			break
		}
	}
	if !ok {
		for _, value := range resp.Data {
			section = value
			ok = true
			break
		}
	}
	if !ok {
		return 0, false
	}

	if p, found := nearestLPProbability(section.ProbabilityAbove, thresholdPrice); found {
		return upDownClamp(p, 0, 1), true
	}
	if p, found := nearestLPProbability(section.ProbabilityBelow, thresholdPrice); found {
		return upDownClamp(1-p, 0, 1), true
	}
	return 0, false
}

func lpHorizonCandidates(horizon string) []string {
	h := strings.ToLower(strings.TrimSpace(horizon))
	switch h {
	case "1h":
		return []string{"1h", "1H", "60m", "60min"}
	case "24h":
		return []string{"24h", "24H", "1d", "daily"}
	default:
		return []string{horizon}
	}
}

func nearestLPProbability(levels map[string]float64, thresholdPrice float64) (float64, bool) {
	if len(levels) == 0 || thresholdPrice <= 0 {
		return 0, false
	}
	bestDiff := math.MaxFloat64
	bestProb := 0.0
	found := false
	for rawPrice, rawProb := range levels {
		price, err := strconv.ParseFloat(strings.TrimSpace(rawPrice), 64)
		if err != nil || price <= 0 || math.IsNaN(price) {
			continue
		}
		diff := math.Abs(price - thresholdPrice)
		if diff < bestDiff {
			bestDiff = diff
			bestProb = rawProb
			found = true
		}
	}
	if !found {
		return 0, false
	}
	return upDownClamp(bestProb, 0, 1), true
}

func (s *UpDownService) computeModelProbability(ctx context.Context, market UpDownMarket, threshold float64, percentileCache map[string]*synthdata.PredictionPercentilesResponse) float64 {
	if s.synth == nil || !s.synth.Enabled() {
		return 0
	}
	timeToExpiry := market.EventEndTime.Sub(time.Now().UTC())
	if timeToExpiry <= 0 {
		return 0
	}

	if threshold <= 0 {
		return 0
	}

	pp := s.getSynthPercentilesCached(ctx, market, percentileCache)
	if pp == nil || len(pp.ForecastFuture.Percentiles) == 0 {
		return 0
	}

	stepSeconds := synthSamplingInterval(horizonForWindow(market.WindowType))
	targetStep := int(math.Ceil(timeToExpiry.Seconds() / stepSeconds.Seconds()))
	if targetStep < 1 {
		targetStep = 1
	}
	timeIncrement := int(stepSeconds.Seconds())
	if timeIncrement <= 0 {
		timeIncrement = 300
	}
	timeLength := targetStep * timeIncrement
	prob, err := s.synth.GetEnterpriseProbabilityUp(ctx, market.Asset, timeIncrement, timeLength, targetStep, threshold)
	if err == nil && prob != nil && prob.ProbabilityUp >= 0 && prob.ProbabilityUp <= 1 {
		return prob.ProbabilityUp
	}
	latestProb, latestErr := s.synth.GetLatestProbabilityUp(ctx, market.Asset, timeIncrement, timeLength, targetStep, threshold)
	if latestErr == nil && latestProb != nil && latestProb.ProbabilityUp >= 0 && latestProb.ProbabilityUp <= 1 {
		return latestProb.ProbabilityUp
	}

	p, e := synthdata.EstimateProbabilityUpFromPercentiles(pp.ForecastFuture.Percentiles, targetStep, threshold)
	if e != nil {
		return 0
	}
	return p
}

func synthWindowForMarket(window UpDownWindowType) synthdata.UpDownWindow {
	switch window {
	case Window5m:
		return synthdata.UpDownWindow5m
	case Window15m:
		return synthdata.UpDownWindow15m
	case Window1h:
		return synthdata.UpDownWindow1h
	default:
		return ""
	}
}

func horizonForWindow(window UpDownWindowType) string {
	switch window {
	case Window5m, Window15m, Window1h:
		return "1h"
	default:
		return "24h"
	}
}

func supportsDirectSynthUpDownWindow(window UpDownWindowType) bool {
	switch window {
	case Window5m, Window15m, Window1h:
		return true
	default:
		return false
	}
}

func supportsSynthAnalyticsForWindow(window UpDownWindowType) bool {
	return supportsDirectSynthUpDownWindow(window)
}

func synthSamplingInterval(horizon string) time.Duration {
	switch strings.ToLower(strings.TrimSpace(horizon)) {
	case "1h":
		return time.Minute
	default:
		return 5 * time.Minute
	}
}

func synthStaleThresholdForMarket(cfg *config.Config, window UpDownWindowType) time.Duration {
	base := 8 * time.Second
	if cfg != nil && cfg.Services.UpDownStaleThresholdSeconds > 0 {
		base = time.Duration(cfg.Services.UpDownStaleThresholdSeconds) * time.Second
	}
	minimum := synthSamplingInterval(horizonForWindow(window)) * 2
	if minimum > base {
		return minimum
	}
	return base
}

func synthClockDriftThresholdForMarket(cfg *config.Config, window UpDownWindowType) time.Duration {
	base := 5 * time.Second
	if cfg != nil && cfg.Services.UpDownClockDriftMaxSeconds > 0 {
		base = time.Duration(cfg.Services.UpDownClockDriftMaxSeconds) * time.Second
	}
	minimum := synthSamplingInterval(horizonForWindow(window)) * 2
	if minimum > base {
		return minimum
	}
	return base
}

func marketQuoteStaleThresholdForMarket(cfg *config.Config, window UpDownWindowType) time.Duration {
	base := 30 * time.Second
	if cfg != nil && cfg.Services.UpDownStaleThresholdSeconds > 0 {
		configured := time.Duration(cfg.Services.UpDownStaleThresholdSeconds) * time.Second
		if configured > base {
			base = configured
		}
	}
	switch window {
	case Window1h:
		if base < time.Minute {
			return time.Minute
		}
	case Window4h:
		if base < 2*time.Minute {
			return 2 * time.Minute
		}
	}
	return base
}

func synthUpDownResponseMatchesMarket(market UpDownMarket, resp *synthdata.PolymarketUpDownResponse) bool {
	if resp == nil {
		return false
	}
	if slug := strings.TrimSpace(resp.Slug); slug != "" && strings.EqualFold(slug, strings.TrimSpace(market.Slug)) {
		return true
	}

	start, startOK := parseSynthTimestamp(resp.EventStartTime)
	end, endOK := parseSynthTimestamp(resp.EventEndTime)
	if !startOK || !endOK {
		return false
	}

	startDelta := absDuration(market.EventStartTime.Sub(start))
	endDelta := absDuration(market.EventEndTime.Sub(end))
	return startDelta <= 2*time.Minute && endDelta <= 2*time.Minute
}

func parseSynthTimestamp(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	ts, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return ts.UTC(), true
}

func latestMarketTimestamp(m models.Market) time.Time {
	var latest time.Time
	for _, t := range []*time.Time{m.YesPriceUpdated, m.NoPriceUpdated, m.MarketUpdatedAt} {
		if t == nil || t.IsZero() {
			continue
		}
		if latest.IsZero() || t.After(latest) {
			latest = t.UTC()
		}
	}
	return latest
}

func (s *UpDownService) persistSnapshot(ctx context.Context, market UpDownMarket, signal UpDownSignal) {
	if s.db == nil {
		return
	}
	payload, err := json.Marshal(signal)
	if err != nil {
		return
	}
	reasons, _ := json.Marshal(signal.ReasonCodes)
	dbCtx, cancel := context.WithTimeout(context.Background(), upDownPersistTimeout)
	defer cancel()
	_ = s.db.WithContext(dbCtx).Exec(`
		INSERT INTO updown_signal_snapshots (
			slug, condition_id, asset, window_type, resolution_source_type,
			p_market_up, p_synth_up, p_model_up, p_final_up,
			ev_up, ev_down, confidence, signal_payload, reason_codes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?::jsonb)
	`,
		market.Slug, market.ConditionID, market.Asset, string(market.WindowType), string(market.ResolutionSourceType),
		signal.PMarketUp, signal.PSynthUp, signal.PModelUp, signal.PFinalUp,
		signal.EVUp, signal.EVDown, signal.Confidence, string(payload), string(reasons),
	).Error
}

func (s *UpDownService) persistRecommendation(ctx context.Context, rec UpDownRecommendation) {
	if s.db == nil {
		return
	}
	payload, err := json.Marshal(rec)
	if err != nil {
		return
	}
	dbCtx, cancel := context.WithTimeout(context.Background(), upDownPersistTimeout)
	defer cancel()
	_ = s.db.WithContext(dbCtx).Exec(`
		INSERT INTO updown_recommendations (
			recommendation_id, slug, condition_id, asset, window_type,
			decision, recommended_side, expected_value, confidence,
			suggested_limit_price, suggested_size_shares, kelly_raw, kelly_capped,
			risk_flags, recommendation_payload
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?::jsonb)
	`,
		rec.ID, rec.Slug, rec.ConditionID, rec.Asset, string(rec.WindowType),
		rec.Decision, rec.RecommendedSide, rec.ExpectedValue, rec.Confidence,
		rec.SuggestedLimitPrice, rec.SuggestedSizeShares, rec.KellyRaw, rec.KellyCapped,
		toJSONMap(rec.RiskFlags), string(payload),
	).Error
}

func toJSONMap(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func upDownClamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func positiveOrZero(v float64) float64 {
	if v < 0 || math.IsNaN(v) {
		return 0
	}
	return v
}

func absDuration(v time.Duration) time.Duration {
	if v < 0 {
		return -v
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func dedupeStrings(items []string) []string {
	if len(items) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}
