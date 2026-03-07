package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/integrations/synthdata"
	"github.com/bankai-project/backend/internal/models"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestClassifyUpDownCryptoMarketChainlink(t *testing.T) {
	now := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)
	start := now.Add(-2 * time.Minute)
	end := start.Add(5 * time.Minute)
	m := models.Market{
		Slug:            "btc-updown-5m-123",
		ConditionID:     "0xabc",
		Title:           "BTC Up or Down in 5 Minutes?",
		Outcomes:        `["Up","Down"]`,
		ResolutionRules: "Resolved by Chainlink reference feed https://data.chain.link",
		AcceptingOrders: true,
		Closed:          false,
		EventStartTime:  &start,
		EndDate:         &end,
		TokenIDYes:      "yes",
		TokenIDNo:       "no",
	}

	out, ok := classifyUpDownCryptoMarket(m, now)
	if !ok {
		t.Fatalf("expected market to be classified")
	}
	if out.Asset != "BTC" {
		t.Fatalf("expected asset BTC, got %s", out.Asset)
	}
	if out.ResolutionSourceType != ResolutionSourceChainlink {
		t.Fatalf("expected chainlink source, got %s", out.ResolutionSourceType)
	}
	if out.WindowType != Window5m {
		t.Fatalf("expected 5m window, got %s", out.WindowType)
	}
	if !out.IsActiveWindow {
		t.Fatalf("expected active window")
	}
}

func TestClassifyUpDownCryptoMarketBinance(t *testing.T) {
	now := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)
	start := now.Add(5 * time.Minute)
	end := start.Add(1 * time.Hour)
	m := models.Market{
		Slug:            "eth-updown-1h-1772816400",
		ConditionID:     "0xdef",
		Title:           "Ethereum Up or Down - March 6, 12:00PM-1:00PM ET",
		Outcomes:        `["Down","Up"]`,
		ResolutionRules: "Resolution source: Binance 1h candle open and close.",
		AcceptingOrders: true,
		Closed:          false,
		EventStartTime:  &start,
		EndDate:         &end,
	}

	out, ok := classifyUpDownCryptoMarket(m, now)
	if !ok {
		t.Fatalf("expected market to be classified")
	}
	if out.Asset != "ETH" {
		t.Fatalf("expected asset ETH, got %s", out.Asset)
	}
	if out.ResolutionSourceType != ResolutionSourceBinance {
		t.Fatalf("expected binance source, got %s", out.ResolutionSourceType)
	}
	if out.WindowType != Window1h {
		t.Fatalf("expected 1h window, got %s", out.WindowType)
	}
	if out.OutcomeIndexUp != 1 || out.OutcomeIndexDown != 0 {
		t.Fatalf("unexpected up/down indexes: up=%d down=%d", out.OutcomeIndexUp, out.OutcomeIndexDown)
	}
}

func TestClassifyUpDownCryptoMarketUpDownUnknownSourceCanonicalSlug(t *testing.T) {
	now := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)
	start := now.Add(2 * time.Minute)
	end := start.Add(1 * time.Hour)
	m := models.Market{
		Slug:            "btc-updown-1h-1772816400",
		ConditionID:     "0xupdownunknownsource",
		Title:           "Bitcoin Up or Down - March 6, 12:00PM-1:00PM ET",
		Description:     "Resolves to Up if end price is greater than or equal to start price.",
		Outcomes:        `["Up","Down"]`,
		ResolutionRules: "Primary resolution source is market consensus reporting.",
		AcceptingOrders: true,
		Closed:          false,
		EventStartTime:  &start,
		EndDate:         &end,
		TokenIDYes:      "yes-token",
		TokenIDNo:       "no-token",
	}

	out, ok := classifyUpDownCryptoMarket(m, now)
	if !ok {
		t.Fatalf("expected canonical up/down market with unknown source to be classified")
	}
	if out.WindowType != Window1h {
		t.Fatalf("expected 1h window, got %s", out.WindowType)
	}
	if out.ResolutionSourceType != ResolutionSourceUnknown {
		t.Fatalf("expected unknown resolution source fallback, got %s", out.ResolutionSourceType)
	}
}

func TestClassifyUpDownCryptoMarketRejectsNonCanonicalUpDownSlug(t *testing.T) {
	now := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)
	start := now.Add(2 * time.Minute)
	end := start.Add(15 * time.Minute)
	m := models.Market{
		Slug:            "btc-open-interest-up-or-down-15m-123",
		ConditionID:     "0xupdownnoncanonical",
		Title:           "BTC Open Interest Up or Down in 15 minutes?",
		Description:     "Resolves to Up if open interest is higher at end.",
		Outcomes:        `["Up","Down"]`,
		ResolutionRules: "Primary resolution source is market consensus reporting.",
		AcceptingOrders: true,
		Closed:          false,
		EventStartTime:  &start,
		EndDate:         &end,
		TokenIDYes:      "yes-token",
		TokenIDNo:       "no-token",
	}

	if _, ok := classifyUpDownCryptoMarket(m, now); ok {
		t.Fatalf("expected non-canonical up/down market slug to be rejected")
	}
}

func TestClassifyUpDownCryptoMarketRejectsYesNoMarkets(t *testing.T) {
	now := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)
	start := now.Add(2 * time.Minute)
	end := start.Add(15 * time.Minute)
	m := models.Market{
		Slug:            "btc-updown-15m-1772442000",
		ConditionID:     "0xyesno",
		Title:           "Bitcoin Up or Down - March 2, 4:00AM-4:15AM ET",
		Description:     "This market resolves Yes if BTC closes above the start price and No otherwise.",
		Outcomes:        `["Yes","No"]`,
		ResolutionRules: "Primary resolution source is market consensus reporting.",
		AcceptingOrders: true,
		Closed:          false,
		EventStartTime:  &start,
		EndDate:         &end,
		TokenIDYes:      "yes-token",
		TokenIDNo:       "no-token",
	}

	if _, ok := classifyUpDownCryptoMarket(m, now); ok {
		t.Fatalf("expected yes/no market to be rejected")
	}
}

func TestClassifyUpDownCryptoMarketAcceptsLegacyUpOrDownSlug(t *testing.T) {
	now := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)
	start := now.Add(2 * time.Minute)
	end := start.Add(15 * time.Minute)
	m := models.Market{
		Slug:            "bitcoin-up-or-down-january-10-1pm-et",
		ConditionID:     "0xlegacyslug",
		Title:           "Bitcoin Up or Down - March 2, 4:00AM-4:15AM ET",
		Description:     "This market will resolve to Up if the Bitcoin price at the end of the time range is greater than or equal to the price at the beginning.",
		Outcomes:        `["Up","Down"]`,
		ResolutionRules: "Resolution source is Chainlink BTC/USD.",
		AcceptingOrders: true,
		Closed:          false,
		EventStartTime:  &start,
		EndDate:         &end,
	}

	out, ok := classifyUpDownCryptoMarket(m, now)
	if !ok {
		t.Fatalf("expected legacy up-or-down slug to be classified")
	}
	if out.Asset != "BTC" {
		t.Fatalf("expected BTC asset, got %s", out.Asset)
	}
	if out.WindowType != Window15m {
		t.Fatalf("expected 15m window, got %s", out.WindowType)
	}
}

func TestExpectedValueBuyBinary(t *testing.T) {
	ev := expectedValueBuyBinary(0.62, 0.55, 0.001)
	if ev <= 0 {
		t.Fatalf("expected positive EV, got %f", ev)
	}
	evBad := expectedValueBuyBinary(0.40, 0.55, 0.001)
	if evBad >= 0 {
		t.Fatalf("expected negative EV, got %f", evBad)
	}
}

func TestBuildSignalUsesLiveMarketQuotesForPMarket(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Market{}); err != nil {
		t.Fatalf("automigrate market: %v", err)
	}

	mini := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(-2 * time.Minute)
	end := now.Add(3 * time.Minute)

	live := models.Market{
		ConditionID:     "0xlive-pmarket",
		Slug:            "btc-updown-5m-live",
		Title:           "Bitcoin Up or Down - Live",
		Outcomes:        `["Up","Down"]`,
		ResolutionRules: "Resolved by Chainlink reference feed https://data.chain.link",
		TokenIDYes:      "yes-live",
		TokenIDNo:       "no-live",
		AcceptingOrders: true,
		Active:          true,
		Closed:          false,
		EventStartTime:  &start,
		EndDate:         &end,
		Liquidity:       25_000,
	}
	if err := db.Create(&live).Error; err != nil {
		t.Fatalf("create market: %v", err)
	}

	updated := now.Format(time.RFC3339Nano)
	if err := rdb.HSet(ctx, priceRedisKey(live.ConditionID, live.TokenIDYes), map[string]any{
		"best_bid": "0.53",
		"best_ask": "0.54",
		"updated":  updated,
	}).Err(); err != nil {
		t.Fatalf("seed yes price: %v", err)
	}
	if err := rdb.HSet(ctx, priceRedisKey(live.ConditionID, live.TokenIDNo), map[string]any{
		"best_bid": "0.45",
		"best_ask": "0.46",
		"updated":  updated,
	}).Err(); err != nil {
		t.Fatalf("seed no price: %v", err)
	}

	svc := &UpDownService{
		cfg: &config.Config{
			Services: config.ServicesConfig{
				UpDownEnabled:             true,
				UpDownFeeBps:              10,
				UpDownDepthProbeShares:    10,
				UpDownMaxSpreadToTrade:    0.10,
				UpDownEVMinThreshold:      0.01,
				UpDownKellyFraction:       0.25,
				UpDownMaxFractionPerTrade: 0.05,
				UpDownAssetExposureCap:    0.20,
				UpDownNotionalBankroll:    1000,
			},
		},
		market: NewMarketService(db, rdb, nil, nil),
	}

	signal, err := svc.buildSignal(ctx, UpDownMarket{
		Slug:                 live.Slug,
		ConditionID:          live.ConditionID,
		Asset:                "BTC",
		WindowType:           Window5m,
		ResolutionSourceType: ResolutionSourceChainlink,
		EventStartTime:       start,
		EventEndTime:         end,
		TimeToEndSeconds:     int64(end.Sub(now).Seconds()),
		OutcomeIndexUp:       0,
		OutcomeIndexDown:     1,
		Market:               live,
	}, map[string]*synthdata.PolymarketUpDownResponse{}, map[string]*synthdata.PredictionPercentilesResponse{}, map[string]*synthdata.VolatilityResponse{}, map[string]*synthdata.LPProbabilitiesResponse{})
	if err != nil {
		t.Fatalf("build signal: %v", err)
	}
	if signal.PMarketUp == nil {
		t.Fatalf("expected live market probability to be populated")
	}
	if got := *signal.PMarketUp; got < 0.539 || got > 0.541 {
		t.Fatalf("expected p_market from live quotes to be ~0.54, got %.6f", got)
	}
}

func TestBuildSignalUsesChainlinkOracleSnapshotForReferencePrices(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Market{}); err != nil {
		t.Fatalf("automigrate market: %v", err)
	}

	mini := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(-90 * time.Second)
	end := now.Add(210 * time.Second)

	live := models.Market{
		ConditionID:     "0xoracle-ref",
		Slug:            "btc-updown-5m-oracle",
		Title:           "Bitcoin Up or Down - Oracle",
		Outcomes:        `["Up","Down"]`,
		ResolutionRules: "Resolved by Chainlink reference feed https://data.chain.link",
		TokenIDYes:      "yes-oracle",
		TokenIDNo:       "no-oracle",
		AcceptingOrders: true,
		Active:          true,
		Closed:          false,
		EventStartTime:  &start,
		EndDate:         &end,
		Liquidity:       25_000,
	}
	if err := db.Create(&live).Error; err != nil {
		t.Fatalf("create market: %v", err)
	}

	updated := now.Format(time.RFC3339Nano)
	if err := rdb.HSet(ctx, priceRedisKey(live.ConditionID, live.TokenIDYes), map[string]any{
		"best_bid": "0.50",
		"best_ask": "0.51",
		"updated":  updated,
	}).Err(); err != nil {
		t.Fatalf("seed yes price: %v", err)
	}
	if err := rdb.HSet(ctx, priceRedisKey(live.ConditionID, live.TokenIDNo), map[string]any{
		"best_bid": "0.49",
		"best_ask": "0.50",
		"updated":  updated,
	}).Err(); err != nil {
		t.Fatalf("seed no price: %v", err)
	}
	if err := StoreChainlinkLatest(ctx, rdb, "BTC", 67234.5, now); err != nil {
		t.Fatalf("store chainlink latest: %v", err)
	}

	svc := &UpDownService{
		cfg: &config.Config{
			Services: config.ServicesConfig{
				UpDownEnabled:             true,
				UpDownFeeBps:              10,
				UpDownDepthProbeShares:    10,
				UpDownMaxSpreadToTrade:    0.10,
				UpDownEVMinThreshold:      0.01,
				UpDownKellyFraction:       0.25,
				UpDownMaxFractionPerTrade: 0.05,
				UpDownAssetExposureCap:    0.20,
				UpDownNotionalBankroll:    1000,
			},
		},
		redis:  rdb,
		market: NewMarketService(db, rdb, nil, nil),
	}

	signal, err := svc.buildSignal(ctx, UpDownMarket{
		Slug:                 live.Slug,
		ConditionID:          live.ConditionID,
		Asset:                "BTC",
		WindowType:           Window5m,
		ResolutionSourceType: ResolutionSourceChainlink,
		EventStartTime:       start,
		EventEndTime:         end,
		TimeToEndSeconds:     int64(end.Sub(now).Seconds()),
		OutcomeIndexUp:       0,
		OutcomeIndexDown:     1,
		Market:               live,
	}, map[string]*synthdata.PolymarketUpDownResponse{}, map[string]*synthdata.PredictionPercentilesResponse{}, map[string]*synthdata.VolatilityResponse{}, map[string]*synthdata.LPProbabilitiesResponse{})
	if err != nil {
		t.Fatalf("build signal: %v", err)
	}
	if signal.ReferenceCurrentPrice == nil || *signal.ReferenceCurrentPrice != 67234.5 {
		t.Fatalf("expected current oracle price from chainlink, got %+v", signal.ReferenceCurrentPrice)
	}
	if signal.ReferenceStartPrice == nil || *signal.ReferenceStartPrice != 67234.5 {
		t.Fatalf("expected start oracle snapshot to be captured from chainlink, got %+v", signal.ReferenceStartPrice)
	}
}

func TestBuildSignalDoesNotApplyChainlinkSnapshotsToBinanceMarkets(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Market{}); err != nil {
		t.Fatalf("automigrate market: %v", err)
	}

	mini := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(-90 * time.Second)
	end := now.Add(30 * time.Minute)

	live := models.Market{
		ConditionID:     "0xbinance-ref",
		Slug:            "eth-updown-1h-binance",
		Title:           "Ethereum Up or Down - Binance",
		Outcomes:        `["Up","Down"]`,
		ResolutionRules: "Resolution source: Binance 1h candle open and close.",
		TokenIDYes:      "yes-binance",
		TokenIDNo:       "no-binance",
		AcceptingOrders: true,
		Active:          true,
		Closed:          false,
		EventStartTime:  &start,
		EndDate:         &end,
		Liquidity:       25_000,
	}
	if err := db.Create(&live).Error; err != nil {
		t.Fatalf("create market: %v", err)
	}

	updated := now.Format(time.RFC3339Nano)
	if err := rdb.HSet(ctx, priceRedisKey(live.ConditionID, live.TokenIDYes), map[string]any{
		"best_bid": "0.50",
		"best_ask": "0.51",
		"updated":  updated,
	}).Err(); err != nil {
		t.Fatalf("seed yes price: %v", err)
	}
	if err := rdb.HSet(ctx, priceRedisKey(live.ConditionID, live.TokenIDNo), map[string]any{
		"best_bid": "0.49",
		"best_ask": "0.50",
		"updated":  updated,
	}).Err(); err != nil {
		t.Fatalf("seed no price: %v", err)
	}
	if err := StoreChainlinkLatest(ctx, rdb, "ETH", 3911.44, now); err != nil {
		t.Fatalf("store chainlink latest: %v", err)
	}

	svc := &UpDownService{
		cfg: &config.Config{
			Services: config.ServicesConfig{
				UpDownEnabled:             true,
				UpDownFeeBps:              10,
				UpDownDepthProbeShares:    10,
				UpDownMaxSpreadToTrade:    0.10,
				UpDownEVMinThreshold:      0.01,
				UpDownKellyFraction:       0.25,
				UpDownMaxFractionPerTrade: 0.05,
				UpDownAssetExposureCap:    0.20,
				UpDownNotionalBankroll:    1000,
			},
		},
		redis:  rdb,
		market: NewMarketService(db, rdb, nil, nil),
	}

	signal, err := svc.buildSignal(ctx, UpDownMarket{
		Slug:                 live.Slug,
		ConditionID:          live.ConditionID,
		Asset:                "ETH",
		WindowType:           Window1h,
		ResolutionSourceType: ResolutionSourceBinance,
		EventStartTime:       start,
		EventEndTime:         end,
		TimeToEndSeconds:     int64(end.Sub(now).Seconds()),
		OutcomeIndexUp:       0,
		OutcomeIndexDown:     1,
		Market:               live,
	}, map[string]*synthdata.PolymarketUpDownResponse{}, map[string]*synthdata.PredictionPercentilesResponse{}, map[string]*synthdata.VolatilityResponse{}, map[string]*synthdata.LPProbabilitiesResponse{})
	if err != nil {
		t.Fatalf("build signal: %v", err)
	}
	if signal.ReferenceCurrentPrice != nil {
		t.Fatalf("expected binance market to ignore chainlink current price, got %+v", signal.ReferenceCurrentPrice)
	}
	if signal.ReferenceStartPrice != nil {
		t.Fatalf("expected binance market to ignore chainlink start snapshot, got %+v", signal.ReferenceStartPrice)
	}
}

func TestBuildRecommendationNoTradeWhenEdgeTooLow(t *testing.T) {
	now := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)
	cfg := &config.Config{
		Services: config.ServicesConfig{
			UpDownEVMinThreshold:        0.02,
			UpDownKellyFraction:         0.35,
			UpDownMaxFractionPerTrade:   0.06,
			UpDownAssetExposureCap:      0.20,
			UpDownDailyDrawdownThrottle: 0.7,
			UpDownNotionalBankroll:      1000,
		},
	}
	market := UpDownMarket{
		Slug:             "btc-updown-15m-1",
		ConditionID:      "0x1",
		Asset:            "BTC",
		WindowType:       Window15m,
		OutcomeIndexUp:   0,
		OutcomeIndexDown: 1,
	}
	rec := buildRecommendation(
		now,
		market,
		0.52,
		0.001,  // ev up
		0.0005, // ev down
		0.51,
		0.49,
		0.61,
		0.02,
		UpDownRiskFlags{},
		[]string{"test"},
		cfg,
	)
	if rec.Decision != "NO_TRADE" {
		t.Fatalf("expected NO_TRADE decision, got %s", rec.Decision)
	}
	if !rec.Prefill.Disabled {
		t.Fatalf("expected prefill disabled when decision is NO_TRADE")
	}
}

func TestComputeDynamicEVThresholdTightensNearExpiry(t *testing.T) {
	base := 0.0125
	low := computeDynamicEVThreshold(base, "mean_reversion", 900, nil, false, assetCalibration{})
	high := computeDynamicEVThreshold(base, "volatile", 30, nil, false, assetCalibration{})
	if high <= low {
		t.Fatalf("expected higher threshold near expiry and volatile regime: low=%f high=%f", low, high)
	}
}

func TestAdjustConfidenceForVolatilityReducesOnStress(t *testing.T) {
	conf := adjustConfidenceForVolatility(
		0.8,
		nil,
		40,
		UpDownRiskFlags{WideSpread: true, DepthMissing: true},
		false,
		assetCalibration{},
	)
	if conf >= 0.8 {
		t.Fatalf("expected stressed confidence < base confidence, got %f", conf)
	}
}

func TestLPProbabilityAtThresholdNearestLevel(t *testing.T) {
	resp := &synthdata.LPProbabilitiesResponse{
		Data: map[string]synthdata.LPProbabilityHorizon{
			"1h": {
				ProbabilityAbove: map[string]float64{
					"100.0": 0.52,
					"105.0": 0.39,
					"110.0": 0.25,
				},
			},
		},
	}

	p, ok := lpProbabilityAtThreshold(resp, "1h", 104.6)
	if !ok {
		t.Fatalf("expected lp probability to be found")
	}
	if p != 0.39 {
		t.Fatalf("expected nearest lp probability 0.39, got %f", p)
	}
}

func TestBuildRecommendationNoTradeNearExpiryCutoff(t *testing.T) {
	now := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)
	cfg := &config.Config{
		Services: config.ServicesConfig{
			UpDownEVMinThreshold:          0.01,
			UpDownKellyFraction:           0.35,
			UpDownMaxFractionPerTrade:     0.06,
			UpDownAssetExposureCap:        0.20,
			UpDownDailyDrawdownThrottle:   0.7,
			UpDownNotionalBankroll:        1000,
			UpDownNoTradeCutoff15mSeconds: 30,
		},
	}
	market := UpDownMarket{
		Slug:             "btc-updown-15m-2",
		ConditionID:      "0x2",
		Asset:            "BTC",
		WindowType:       Window15m,
		TimeToEndSeconds: 25,
		OutcomeIndexUp:   0,
		OutcomeIndexDown: 1,
	}
	rec := buildRecommendation(
		now,
		market,
		0.61,
		0.04,
		-0.01,
		0.55,
		0.45,
		0.74,
		0.01,
		UpDownRiskFlags{},
		[]string{"test"},
		cfg,
	)
	if rec.Decision != "NO_TRADE" {
		t.Fatalf("expected NO_TRADE inside cutoff window, got %s", rec.Decision)
	}
	if !rec.Prefill.Disabled {
		t.Fatalf("expected prefill disabled inside no-trade cutoff")
	}
}

func TestDiscoverMarketsUsesSharedActiveSnapshot(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(-2 * time.Minute)
	end := start.Add(15 * time.Minute)

	cacheMarkets := []models.Market{
		{
			Active:          true,
			AcceptingOrders: true,
			Closed:          false,
			ConditionID:     "btc-current",
			Slug:            "btc-updown-15m-1772860500",
			Title:           "Bitcoin Up or Down - March 7, 12:15AM-12:30AM ET",
			Description:     "Resolve Up if the ending BTC price is at least the starting price.",
			ResolutionRules: "Resolution source: Chainlink BTC/USD.",
			Outcomes:        `["Up","Down"]`,
			EventStartTime:  &start,
			EndDate:         &end,
			TokenIDYes:      "btc-up",
			TokenIDNo:       "btc-down",
		},
		{
			Active:          true,
			AcceptingOrders: true,
			Closed:          false,
			ConditionID:     "noise",
			Slug:            "will-it-rain",
			Title:           "Will it rain tomorrow?",
			Outcomes:        `["Yes","No"]`,
			EventStartTime:  &start,
			EndDate:         &end,
		},
	}

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		_ = rdb.Close()
	})

	payload, err := json.Marshal(cacheMarkets)
	if err != nil {
		t.Fatalf("marshal cache markets: %v", err)
	}
	if err := rdb.Set(context.Background(), CacheKeyActiveMarkets, payload, time.Minute).Err(); err != nil {
		t.Fatalf("seed active markets cache: %v", err)
	}

	svc := &UpDownService{
		cfg:    &config.Config{Services: config.ServicesConfig{UpDownMaxMarkets: 4}},
		market: &MarketService{Redis: rdb},
	}

	markets, err := svc.discoverMarkets(context.Background())
	if err != nil {
		t.Fatalf("discover markets from shared snapshot: %v", err)
	}
	if len(markets) != 1 {
		t.Fatalf("expected one up/down market from shared snapshot, got %d", len(markets))
	}
	if markets[0].Slug != "btc-updown-15m-1772860500" {
		t.Fatalf("unexpected market surfaced from shared snapshot: %s", markets[0].Slug)
	}
}

func TestSynthUpDownCacheKeyUsesRequestDimensions(t *testing.T) {
	m1 := UpDownMarket{Slug: "btc-updown-5m-a", Asset: "btc", WindowType: Window5m}
	m2 := UpDownMarket{Slug: "btc-updown-5m-b", Asset: "BTC", WindowType: Window5m}
	m3 := UpDownMarket{Slug: "btc-updown-1h-a", Asset: "BTC", WindowType: Window1h}

	key1 := synthUpDownCacheKey(m1)
	key2 := synthUpDownCacheKey(m2)
	key3 := synthUpDownCacheKey(m3)

	if key1 == "" || key2 == "" || key3 == "" {
		t.Fatalf("expected non-empty synth cache keys")
	}
	if key1 != key2 {
		t.Fatalf("expected same cache key for same asset/window request dimensions: %s vs %s", key1, key2)
	}
	if key1 == key3 {
		t.Fatalf("expected different cache keys for different request dimensions: %s vs %s", key1, key3)
	}
}

func TestSupportsDirectSynthUpDownWindow(t *testing.T) {
	if !supportsDirectSynthUpDownWindow(Window5m) {
		t.Fatalf("expected 5m synth up/down support")
	}
	if !supportsDirectSynthUpDownWindow(Window15m) {
		t.Fatalf("expected 15m synth up/down support")
	}
	if !supportsDirectSynthUpDownWindow(Window1h) {
		t.Fatalf("expected 1h synth up/down support")
	}
	if supportsDirectSynthUpDownWindow(Window4h) {
		t.Fatalf("expected 4h synth up/down to remain unsupported")
	}
}

func TestSynthSamplingIntervalUsesDocumentedCadence(t *testing.T) {
	if got := synthSamplingInterval("1h"); got != time.Minute {
		t.Fatalf("expected 1h synth cadence to be 1m, got %s", got)
	}
	if got := synthSamplingInterval("24h"); got != 5*time.Minute {
		t.Fatalf("expected 24h synth cadence to be 5m, got %s", got)
	}
}

func TestSynthUpDownResponseMatchesMarketByWindowTimes(t *testing.T) {
	start := time.Date(2026, 3, 6, 8, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	market := UpDownMarket{
		Slug:           "btc-updown-1h-1772860800",
		EventStartTime: start,
		EventEndTime:   end,
	}
	resp := &synthdata.PolymarketUpDownResponse{
		Slug:           "bitcoin-up-or-down-march-6-8am-et",
		EventStartTime: start.Format(time.RFC3339),
		EventEndTime:   end.Format(time.RFC3339),
	}

	if !synthUpDownResponseMatchesMarket(market, resp) {
		t.Fatalf("expected synth response to match market by event window")
	}
}

func TestTokenIDsByOutcomeUsesStoredUpDownIndexes(t *testing.T) {
	market := UpDownMarket{
		OutcomeIndexUp:   1,
		OutcomeIndexDown: 0,
		Market: models.Market{
			TokenIDYes: "yes-token",
			TokenIDNo:  "no-token",
		},
	}
	upToken, downToken, err := tokenIDsByOutcome(market)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if upToken != "no-token" || downToken != "yes-token" {
		t.Fatalf("expected up/down tokens to respect stored indexes, got up=%s down=%s", upToken, downToken)
	}
}

func TestLogDecisionValidationErrorClassification(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	svc := &UpDownService{db: db}

	_, err = svc.LogDecision(context.Background(), "user-1", UpDownDecisionLogRequest{
		Slug:   "",
		Action: "accepted",
	})
	if !errors.Is(err, ErrInvalidUpDownDecisionRequest) {
		t.Fatalf("expected invalid decision request sentinel for missing slug, got: %v", err)
	}

	_, err = svc.LogDecision(context.Background(), "user-1", UpDownDecisionLogRequest{
		Slug:   "btc-updown-5m-1",
		Action: "unsupported",
	})
	if !errors.Is(err, ErrInvalidUpDownDecisionRequest) {
		t.Fatalf("expected invalid decision request sentinel for invalid action, got: %v", err)
	}

	noDBSvc := &UpDownService{}
	_, err = noDBSvc.LogDecision(context.Background(), "user-1", UpDownDecisionLogRequest{
		Slug:   "btc-updown-5m-1",
		Action: "accepted",
	})
	if err == nil {
		t.Fatalf("expected db configuration error")
	}
	if errors.Is(err, ErrInvalidUpDownDecisionRequest) {
		t.Fatalf("expected db error not to be classified as invalid request: %v", err)
	}
}
