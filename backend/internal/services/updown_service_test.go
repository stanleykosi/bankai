package services

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/integrations/synthdata"
	"github.com/bankai-project/backend/internal/models"
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
		Slug:            "eth-up-or-down-hourly",
		ConditionID:     "0xdef",
		Title:           "ETH up/down this hour",
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

func TestDiscoverMarketsScansBeyondInitialNonUpDownRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Market{}); err != nil {
		t.Fatalf("automigrate market: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	nonUpDownCount := 130 // intentionally larger than the old 120-row fetch cap
	for i := 0; i < nonUpDownCount; i++ {
		start := now.Add(time.Duration(i+1) * time.Minute)
		end := start.Add(10 * time.Minute)
		market := models.Market{
			ConditionID:     fmt.Sprintf("non-updown-%03d", i),
			Slug:            fmt.Sprintf("general-market-%03d", i),
			Title:           "General prediction market",
			Description:     "Not an up/down market",
			ResolutionRules: "Resolved by committee vote.",
			Outcomes:        `["Yes","No"]`,
			AcceptingOrders: true,
			Closed:          false,
			EventStartTime:  &start,
			EndDate:         &end,
			TokenIDYes:      fmt.Sprintf("yes-%03d", i),
			TokenIDNo:       fmt.Sprintf("no-%03d", i),
		}
		if err := db.Create(&market).Error; err != nil {
			t.Fatalf("create non-updown market %d: %v", i, err)
		}
	}

	// Place valid Up/Down markets after the initial non-updown block.
	for i := 0; i < 2; i++ {
		start := now.Add(time.Duration(nonUpDownCount+i+1) * time.Minute)
		end := start.Add(5 * time.Minute)
		market := models.Market{
			ConditionID:     fmt.Sprintf("updown-%03d", i),
			Slug:            fmt.Sprintf("btc-updown-5m-%03d", i),
			Title:           "BTC Up or Down in 5 Minutes?",
			Description:     "Up/Down crypto market",
			ResolutionRules: "Resolved by Chainlink feed data.chain.link",
			Outcomes:        `["Up","Down"]`,
			AcceptingOrders: true,
			Closed:          false,
			EventStartTime:  &start,
			EndDate:         &end,
			TokenIDYes:      fmt.Sprintf("up-yes-%03d", i),
			TokenIDNo:       fmt.Sprintf("up-no-%03d", i),
		}
		if err := db.Create(&market).Error; err != nil {
			t.Fatalf("create updown market %d: %v", i, err)
		}
	}

	svc := &UpDownService{
		db:  db,
		cfg: &config.Config{Services: config.ServicesConfig{UpDownMaxMarkets: 2}},
	}
	markets, err := svc.discoverMarkets(context.Background())
	if err != nil {
		t.Fatalf("discover markets: %v", err)
	}
	if len(markets) != 2 {
		t.Fatalf("expected 2 up/down markets, got %d", len(markets))
	}
	for _, market := range markets {
		if market.WindowType != Window5m {
			t.Fatalf("expected 5m window, got %s", market.WindowType)
		}
		if market.Asset != "BTC" {
			t.Fatalf("expected BTC asset, got %s", market.Asset)
		}
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
