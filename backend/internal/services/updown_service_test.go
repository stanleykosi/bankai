package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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

func TestSynthWindowForMarketUsesNative5m(t *testing.T) {
	if got := synthWindowForMarket(Window5m); got != synthdata.UpDownWindow5m {
		t.Fatalf("expected 5m synth window to use native 5min endpoint, got %q", got)
	}
	if got := synthWindowForMarket(Window15m); got != synthdata.UpDownWindow15m {
		t.Fatalf("expected 15m synth window to remain 15min, got %q", got)
	}
}

func TestSynthUpDownCacheKeyScopesByWindowStart(t *testing.T) {
	startA := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	startB := startA.Add(5 * time.Minute)

	keyA := synthUpDownCacheKey(UpDownMarket{
		Asset:          "BTC",
		WindowType:     Window5m,
		EventStartTime: startA,
	})
	keyB := synthUpDownCacheKey(UpDownMarket{
		Asset:          "BTC",
		WindowType:     Window5m,
		EventStartTime: startB,
	})
	if keyA == "" || keyB == "" {
		t.Fatalf("expected non-empty synth up/down cache keys")
	}
	if keyA == keyB {
		t.Fatalf("expected different cache keys across different window starts, got %q", keyA)
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

	// Seed stale order-book snapshots with materially different depth estimates.
	// p_market should still use top-of-book from RTDS best bid/ask, not these stale levels.
	yesBook, err := json.Marshal(orderBookSnapshot{
		AssetID: live.TokenIDYes,
		Market:  live.ConditionID,
		Bids: []orderBookLevel{
			{Price: "0.40", Size: "200"},
		},
		Asks: []orderBookLevel{
			{Price: "0.70", Size: "200"},
		},
	})
	if err != nil {
		t.Fatalf("marshal yes book: %v", err)
	}
	noBook, err := json.Marshal(orderBookSnapshot{
		AssetID: live.TokenIDNo,
		Market:  live.ConditionID,
		Bids: []orderBookLevel{
			{Price: "0.30", Size: "200"},
		},
		Asks: []orderBookLevel{
			{Price: "0.80", Size: "200"},
		},
	})
	if err != nil {
		t.Fatalf("marshal no book: %v", err)
	}
	if err := rdb.Set(ctx, "book:"+live.ConditionID+":"+live.TokenIDYes, string(yesBook), time.Minute).Err(); err != nil {
		t.Fatalf("seed yes book: %v", err)
	}
	if err := rdb.Set(ctx, "book:"+live.ConditionID+":"+live.TokenIDNo, string(noBook), time.Minute).Err(); err != nil {
		t.Fatalf("seed no book: %v", err)
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

func TestBuildSignalStatusBoundaryClearsWhenStartSnapshotExists(t *testing.T) {
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
	start := now.Add(-3 * time.Second)
	end := start.Add(5 * time.Minute)

	live := models.Market{
		ConditionID:     "0xstatus-boundary",
		Slug:            "btc-updown-5m-boundary",
		Title:           "Bitcoin Up or Down - Boundary Guard",
		Outcomes:        `["Up","Down"]`,
		ResolutionRules: "Resolved by Chainlink reference feed https://data.chain.link",
		TokenIDYes:      "yes-boundary",
		TokenIDNo:       "no-boundary",
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
	if err := StoreChainlinkLatest(ctx, rdb, "BTC", 68123.45, now); err != nil {
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
		TimeToStartSeconds:   int64(start.Sub(now).Seconds()),
		TimeToEndSeconds:     int64(end.Sub(now).Seconds()),
		OutcomeIndexUp:       0,
		OutcomeIndexDown:     1,
		Market:               live,
	}, map[string]*synthdata.PolymarketUpDownResponse{}, map[string]*synthdata.PredictionPercentilesResponse{}, map[string]*synthdata.VolatilityResponse{}, map[string]*synthdata.LPProbabilitiesResponse{})
	if err != nil {
		t.Fatalf("build signal: %v", err)
	}
	if signal.ReferenceStartPrice == nil {
		t.Fatalf("expected chainlink start snapshot to be available")
	}
	if signal.RiskFlags.StatusBoundary {
		t.Fatalf("expected status boundary to clear when start snapshot exists")
	}
}

func TestBuildSignalSuppressesMarketStaleWithFreshReference(t *testing.T) {
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
	staleUpdated := now.Add(-3 * time.Minute).Format(time.RFC3339Nano)

	live := models.Market{
		ConditionID:     "0xmarket-stale",
		Slug:            "btc-updown-5m-stale",
		Title:           "Bitcoin Up or Down - Stale Guard",
		Outcomes:        `["Up","Down"]`,
		ResolutionRules: "Resolved by Chainlink reference feed https://data.chain.link",
		TokenIDYes:      "yes-stale",
		TokenIDNo:       "no-stale",
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

	if err := rdb.HSet(ctx, priceRedisKey(live.ConditionID, live.TokenIDYes), map[string]any{
		"best_bid": "0.50",
		"best_ask": "0.51",
		"updated":  staleUpdated,
	}).Err(); err != nil {
		t.Fatalf("seed yes price: %v", err)
	}
	if err := rdb.HSet(ctx, priceRedisKey(live.ConditionID, live.TokenIDNo), map[string]any{
		"best_bid": "0.49",
		"best_ask": "0.50",
		"updated":  staleUpdated,
	}).Err(); err != nil {
		t.Fatalf("seed no price: %v", err)
	}
	if err := StoreChainlinkLatest(ctx, rdb, "BTC", 68234.10, now); err != nil {
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
		TimeToStartSeconds:   int64(start.Sub(now).Seconds()),
		TimeToEndSeconds:     int64(end.Sub(now).Seconds()),
		OutcomeIndexUp:       0,
		OutcomeIndexDown:     1,
		Market:               live,
	}, map[string]*synthdata.PolymarketUpDownResponse{}, map[string]*synthdata.PredictionPercentilesResponse{}, map[string]*synthdata.VolatilityResponse{}, map[string]*synthdata.LPProbabilitiesResponse{})
	if err != nil {
		t.Fatalf("build signal: %v", err)
	}
	if signal.RiskFlags.MarketStale {
		t.Fatalf("expected market stale to be suppressed when chainlink reference is fresh")
	}
}

func TestBuildSignalUsesPercentilesWhenDirectSynthUnavailable(t *testing.T) {
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

	synthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/insights/polymarket/up-down/5min":
			_, _ = w.Write([]byte(`"No prediction available"`))
		case "/insights/prediction-percentiles":
			_, _ = w.Write([]byte(`{"current_price":68000,"forecast_future":{"percentiles":[{"0.005":67000,"0.05":67400,"0.2":67700,"0.35":67800,"0.5":68000,"0.65":68200,"0.8":68400,"0.95":68600,"0.995":69000}]}}`))
		case "/insights/volatility":
			_, _ = w.Write([]byte(`{"current_price":68000,"forecast_future":{"average_volatility":32,"volatility":[0.01,0.02]},"forecast_past":{"average_volatility":28,"volatility":[0.01,0.02]}}`))
		case "/insights/lp-probabilities":
			_, _ = w.Write([]byte(`{"current_price":68000,"data":{"1h":{"probability_above":{"68000":0.51},"probability_below":{"68000":0.49}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer synthSrv.Close()

	live := models.Market{
		ConditionID:     "0xsynth-fallback",
		Slug:            "btc-updown-5m-synth-fallback",
		Title:           "Bitcoin Up or Down - Synth Fallback",
		Outcomes:        `["Up","Down"]`,
		ResolutionRules: "Resolved by Chainlink reference feed https://data.chain.link",
		TokenIDYes:      "yes-synth",
		TokenIDNo:       "no-synth",
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
	if err := StoreChainlinkLatest(ctx, rdb, "BTC", 68000.0, now); err != nil {
		t.Fatalf("store chainlink latest: %v", err)
	}

	cfg := &config.Config{
		Services: config.ServicesConfig{
			UpDownEnabled:             true,
			UpDownEnterpriseEnabled:   false,
			UpDownFeeBps:              10,
			UpDownDepthProbeShares:    10,
			UpDownMaxSpreadToTrade:    0.10,
			UpDownEVMinThreshold:      0.01,
			UpDownKellyFraction:       0.25,
			UpDownMaxFractionPerTrade: 0.05,
			UpDownAssetExposureCap:    0.20,
			UpDownNotionalBankroll:    1000,
			SynthDataAPIKey:           "test-key",
			SynthDataBaseURL:          synthSrv.URL,
		},
	}

	svc := &UpDownService{
		cfg:                  cfg,
		redis:                rdb,
		market:               NewMarketService(db, rdb, nil, nil),
		synth:                synthdata.NewClient(cfg),
		synthUpDownCache:     make(map[string]cachedSynthUpDown),
		synthPercentileCache: make(map[string]cachedSynthPercentile),
		synthVolatilityCache: make(map[string]cachedSynthVolatility),
		synthLPCache:         make(map[string]cachedSynthLP),
		synthModelProbCache:  make(map[string]cachedSynthModelProb),
	}

	signal, err := svc.buildSignal(ctx, UpDownMarket{
		Slug:                 live.Slug,
		ConditionID:          live.ConditionID,
		Asset:                "BTC",
		WindowType:           Window5m,
		ResolutionSourceType: ResolutionSourceChainlink,
		EventStartTime:       start,
		EventEndTime:         end,
		TimeToStartSeconds:   int64(start.Sub(now).Seconds()),
		TimeToEndSeconds:     int64(end.Sub(now).Seconds()),
		OutcomeIndexUp:       0,
		OutcomeIndexDown:     1,
		Market:               live,
	}, map[string]*synthdata.PolymarketUpDownResponse{}, map[string]*synthdata.PredictionPercentilesResponse{}, map[string]*synthdata.VolatilityResponse{}, map[string]*synthdata.LPProbabilitiesResponse{})
	if err != nil {
		t.Fatalf("build signal: %v", err)
	}
	if signal.PSynthUp == nil {
		t.Fatalf("expected p_synth to be derived from percentile fallback")
	}
	if signal.PModelUp == nil {
		t.Fatalf("expected p_model to be derived from percentile proxy when enterprise is disabled")
	}
	if signal.ModelDiagnosticCode != "enterprise_disabled_percentile_proxy" {
		t.Fatalf("expected enterprise_disabled_percentile_proxy diagnostic, got %s", signal.ModelDiagnosticCode)
	}
	if signal.RiskFlags.SynthMissing {
		t.Fatalf("expected synth_missing to clear when percentile fallback is available")
	}
}

func TestGetSynthUpDownCachedRespectsFailureBackoff(t *testing.T) {
	var callCount atomic.Int32
	synthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/insights/polymarket/up-down/5min" {
			callCount.Add(1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`"No prediction available"`))
			return
		}
		http.NotFound(w, r)
	}))
	defer synthSrv.Close()

	cfg := &config.Config{
		Services: config.ServicesConfig{
			UpDownEnabled:    true,
			SynthDataAPIKey:  "test-key",
			SynthDataBaseURL: synthSrv.URL,
		},
	}
	svc := &UpDownService{
		cfg:                  cfg,
		synth:                synthdata.NewClient(cfg),
		synthUpDownCache:     make(map[string]cachedSynthUpDown),
		synthPercentileCache: make(map[string]cachedSynthPercentile),
		synthVolatilityCache: make(map[string]cachedSynthVolatility),
		synthLPCache:         make(map[string]cachedSynthLP),
		synthModelProbCache:  make(map[string]cachedSynthModelProb),
	}

	now := time.Now().UTC()
	market := UpDownMarket{
		Asset:          "BTC",
		WindowType:     Window5m,
		EventStartTime: now.Add(-2 * time.Minute),
		EventEndTime:   now.Add(3 * time.Minute),
	}

	_ = svc.getSynthUpDownCached(context.Background(), market, map[string]*synthdata.PolymarketUpDownResponse{}, true)
	_ = svc.getSynthUpDownCached(context.Background(), market, map[string]*synthdata.PolymarketUpDownResponse{}, true)

	if got := callCount.Load(); got != 1 {
		t.Fatalf("expected one synth fetch attempt during backoff window, got %d", got)
	}
}

func TestGetSynthUpDownCachedUsesShortFailureRetryForActiveWindow(t *testing.T) {
	var callCount atomic.Int32
	synthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/insights/polymarket/up-down/5min" {
			callCount.Add(1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`"No prediction available"`))
			return
		}
		http.NotFound(w, r)
	}))
	defer synthSrv.Close()

	cfg := &config.Config{
		Services: config.ServicesConfig{
			UpDownEnabled:    true,
			SynthDataAPIKey:  "test-key",
			SynthDataBaseURL: synthSrv.URL,
		},
	}
	svc := &UpDownService{
		cfg:                  cfg,
		synth:                synthdata.NewClient(cfg),
		synthUpDownCache:     make(map[string]cachedSynthUpDown),
		synthPercentileCache: make(map[string]cachedSynthPercentile),
		synthVolatilityCache: make(map[string]cachedSynthVolatility),
		synthLPCache:         make(map[string]cachedSynthLP),
		synthModelProbCache:  make(map[string]cachedSynthModelProb),
	}

	now := time.Now().UTC()
	market := UpDownMarket{
		Asset:          "BTC",
		WindowType:     Window5m,
		EventStartTime: now.Add(-30 * time.Second),
		EventEndTime:   now.Add(4 * time.Hour),
	}

	before := time.Now().UTC()
	_ = svc.getSynthUpDownCached(context.Background(), market, map[string]*synthdata.PolymarketUpDownResponse{}, true)
	if got := callCount.Load(); got != 1 {
		t.Fatalf("expected one synth fetch call, got %d", got)
	}

	key := synthUpDownCacheKey(market)
	svc.synthCacheMu.RLock()
	entry, ok := svc.synthUpDownCache[key]
	svc.synthCacheMu.RUnlock()
	if !ok {
		t.Fatalf("expected cache entry for failed synth fetch")
	}

	backoff := entry.NextFetchAt.Sub(before)
	if backoff < 5*time.Second {
		t.Fatalf("expected short retry backoff, got %s", backoff)
	}
	if backoff > 20*time.Second {
		t.Fatalf("expected retry backoff under 20s, got %s", backoff)
	}
}

func TestRunRefreshWarmupTriggersCalibrationRefresh(t *testing.T) {
	var historicalCalls atomic.Int32
	synthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/prediction/historical" {
			historicalCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"items":[
					{"predictions":[[100,101,102,103],[100,99,98,97]],"realized":[100,101,102,103]},
					{"predictions":[[101,102,103,104],[101,100,99,98]],"realized":[101,102,103,104]},
					{"predictions":[[102,103,104,105],[102,101,100,99]],"realized":[102,103,104,105]},
					{"predictions":[[103,104,105,106],[103,102,101,100]],"realized":[103,104,105,106]}
				]
			}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer synthSrv.Close()

	cfg := &config.Config{
		Services: config.ServicesConfig{
			UpDownEnabled:    false,
			SynthDataAPIKey:  "test-key",
			SynthDataBaseURL: synthSrv.URL,
		},
	}
	now := time.Now().UTC().Truncate(time.Second)
	svc := &UpDownService{
		cfg:                cfg,
		synth:              synthdata.NewClient(cfg),
		marketsBySlug:      make(map[string]UpDownMarket),
		calibrationByAsset: make(map[string]assetCalibration),
	}
	svc.marketsBySlug["btc-updown-5m-test"] = UpDownMarket{
		Slug:           "btc-updown-5m-test",
		Asset:          "BTC",
		WindowType:     Window5m,
		EventStartTime: now.Add(-time.Minute),
		EventEndTime:   now.Add(time.Minute),
	}

	svc.runRefreshWarmup()

	if got := historicalCalls.Load(); got == 0 {
		t.Fatalf("expected warmup to trigger calibration fetch")
	}
	cal, ok := svc.getAssetCalibration("BTC")
	if !ok {
		t.Fatalf("expected BTC calibration to be populated during warmup")
	}
	if cal.Samples < 4 {
		t.Fatalf("expected calibration sample count >= 4, got %d", cal.Samples)
	}
}

func TestRefreshCalibrationIfDueRefreshesMissingActiveAssets(t *testing.T) {
	var btcCalls atomic.Int32
	var ethCalls atomic.Int32
	synthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/prediction/historical" {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Query().Get("asset") {
		case "BTC":
			btcCalls.Add(1)
		case "ETH":
			ethCalls.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"items":[
				{"predictions":[[100,101,102,103],[100,99,98,97]],"realized":[100,101,102,103]},
				{"predictions":[[101,102,103,104],[101,100,99,98]],"realized":[101,102,103,104]},
				{"predictions":[[102,103,104,105],[102,101,100,99]],"realized":[102,103,104,105]},
				{"predictions":[[103,104,105,106],[103,102,101,100]],"realized":[103,104,105,106]}
			]
		}`))
	}))
	defer synthSrv.Close()

	cfg := &config.Config{
		Services: config.ServicesConfig{
			UpDownEnabled:    true,
			SynthDataAPIKey:  "test-key",
			SynthDataBaseURL: synthSrv.URL,
		},
	}
	now := time.Now().UTC().Truncate(time.Second)
	svc := &UpDownService{
		cfg:                cfg,
		synth:              synthdata.NewClient(cfg),
		marketsBySlug:      make(map[string]UpDownMarket),
		calibrationByAsset: make(map[string]assetCalibration),
	}
	svc.calibrationUpdated = now
	svc.calibrationByAsset["BTC"] = assetCalibration{
		Asset:     "BTC",
		Samples:   12,
		Source:    "seeded",
		UpdatedAt: now,
	}
	svc.marketsBySlug["btc-updown-5m-test"] = UpDownMarket{
		Slug:           "btc-updown-5m-test",
		Asset:          "BTC",
		WindowType:     Window5m,
		EventStartTime: now.Add(-time.Minute),
		EventEndTime:   now.Add(time.Minute),
	}
	svc.marketsBySlug["eth-updown-5m-test"] = UpDownMarket{
		Slug:           "eth-updown-5m-test",
		Asset:          "ETH",
		WindowType:     Window5m,
		EventStartTime: now.Add(-time.Minute),
		EventEndTime:   now.Add(time.Minute),
	}

	svc.refreshCalibrationIfDue(context.Background(), false)

	if got := btcCalls.Load(); got != 0 {
		t.Fatalf("expected BTC calibration to stay cached, got %d refresh calls", got)
	}
	if got := ethCalls.Load(); got != 1 {
		t.Fatalf("expected ETH calibration refresh call, got %d", got)
	}
	btcCal, ok := svc.getAssetCalibration("BTC")
	if !ok {
		t.Fatalf("expected BTC calibration to remain available")
	}
	if btcCal.Source != "seeded" {
		t.Fatalf("expected BTC calibration to be preserved, got source=%s", btcCal.Source)
	}
	if ethCal, ok := svc.getAssetCalibration("ETH"); !ok || ethCal.Samples < 4 {
		t.Fatalf("expected ETH calibration to be populated, got ok=%v samples=%d", ok, ethCal.Samples)
	}
}

func TestRefreshCalibrationIfDueBacksOffRepeatedFailuresWithoutBaseline(t *testing.T) {
	var ethCalls atomic.Int32
	synthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/prediction/historical" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("asset") == "ETH" {
			ethCalls.Add(1)
		}
		http.Error(w, "temporary synth error", http.StatusBadGateway)
	}))
	defer synthSrv.Close()

	cfg := &config.Config{
		Services: config.ServicesConfig{
			UpDownEnabled:    true,
			SynthDataAPIKey:  "test-key",
			SynthDataBaseURL: synthSrv.URL,
		},
	}
	now := time.Now().UTC().Truncate(time.Second)
	svc := &UpDownService{
		cfg:                cfg,
		synth:              synthdata.NewClient(cfg),
		marketsBySlug:      make(map[string]UpDownMarket),
		calibrationByAsset: make(map[string]assetCalibration),
		calibrationRetryAt: make(map[string]time.Time),
	}
	svc.marketsBySlug["eth-updown-5m-failure-backoff"] = UpDownMarket{
		Slug:           "eth-updown-5m-failure-backoff",
		Asset:          "ETH",
		WindowType:     Window5m,
		EventStartTime: now.Add(-time.Minute),
		EventEndTime:   now.Add(time.Minute),
	}

	svc.refreshCalibrationIfDue(context.Background(), false)
	svc.refreshCalibrationIfDue(context.Background(), false)

	if got := ethCalls.Load(); got != 1 {
		t.Fatalf("expected only one calibration fetch attempt inside failure backoff window, got %d", got)
	}
	svc.calibrationMu.RLock()
	nextRetryAt, ok := svc.calibrationRetryAt["ETH"]
	svc.calibrationMu.RUnlock()
	if !ok {
		t.Fatalf("expected retry backoff timestamp to be stored for failed ETH calibration")
	}
	if !nextRetryAt.After(time.Now().UTC()) {
		t.Fatalf("expected retry backoff timestamp in the future, got %s", nextRetryAt)
	}
}

func TestWindowAwareSynthFetchesOneCallPerEndpointPerWindow(t *testing.T) {
	var upDownCalls atomic.Int32
	var percentileCalls atomic.Int32
	var volatilityCalls atomic.Int32
	var lpCalls atomic.Int32
	var modelCalls atomic.Int32

	now := time.Now().UTC().Truncate(time.Second)
	synthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/insights/polymarket/up-down/5min":
			upDownCalls.Add(1)
			_, _ = w.Write([]byte(`{
				"slug":"btc-updown-5m-test",
				"start_price":68000,
				"current_time":"` + now.Format(time.RFC3339Nano) + `",
				"current_price":68020,
				"synth_probability_up":0.54,
				"event_start_time":"` + now.Add(-time.Minute).Format(time.RFC3339Nano) + `",
				"event_end_time":"` + now.Add(4*time.Minute).Format(time.RFC3339Nano) + `"
			}`))
		case "/insights/prediction-percentiles":
			percentileCalls.Add(1)
			_, _ = w.Write([]byte(`{"current_price":68000,"forecast_future":{"percentiles":[{"0.005":67600,"0.05":67800,"0.2":67900,"0.35":67950,"0.5":68000,"0.65":68050,"0.8":68100,"0.95":68200,"0.995":68400}]}}`))
		case "/insights/volatility":
			volatilityCalls.Add(1)
			_, _ = w.Write([]byte(`{"current_price":68000,"forecast_future":{"average_volatility":32,"volatility":[0.01,0.02]},"forecast_past":{"average_volatility":30,"volatility":[0.01,0.02]}}`))
		case "/insights/lp-probabilities":
			lpCalls.Add(1)
			_, _ = w.Write([]byte(`{"current_price":68000,"data":{"1h":{"probability_above":{"68000":0.53},"probability_below":{"68000":0.47}}}}`))
		case "/v2/prediction/best":
			modelCalls.Add(1)
			_, _ = w.Write([]byte(`{"predictions":[[67980,67990,68010,68020,68030,68040,68050]]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer synthSrv.Close()

	cfg := &config.Config{
		Services: config.ServicesConfig{
			UpDownEnabled:           true,
			UpDownEnterpriseEnabled: true,
			SynthDataAPIKey:         "test-key",
			SynthDataBaseURL:        synthSrv.URL,
		},
	}
	svc := &UpDownService{
		cfg:                  cfg,
		synth:                synthdata.NewClient(cfg),
		synthUpDownCache:     make(map[string]cachedSynthUpDown),
		synthPercentileCache: make(map[string]cachedSynthPercentile),
		synthVolatilityCache: make(map[string]cachedSynthVolatility),
		synthLPCache:         make(map[string]cachedSynthLP),
		synthModelProbCache:  make(map[string]cachedSynthModelProb),
	}

	market := UpDownMarket{
		Asset:          "BTC",
		WindowType:     Window5m,
		EventStartTime: now.Add(-time.Minute),
		EventEndTime:   now.Add(4 * time.Minute),
	}

	for i := 0; i < 2; i++ {
		if resp := svc.getSynthUpDownCached(context.Background(), market, map[string]*synthdata.PolymarketUpDownResponse{}, true); resp == nil {
			t.Fatalf("expected up/down synth response")
		}
		if resp := svc.getSynthPercentilesCached(context.Background(), market, map[string]*synthdata.PredictionPercentilesResponse{}, true); resp == nil {
			t.Fatalf("expected percentile response")
		}
		if resp := svc.getSynthVolatilityCached(context.Background(), market, map[string]*synthdata.VolatilityResponse{}, true); resp == nil {
			t.Fatalf("expected volatility response")
		}
		if resp := svc.getSynthLPProbabilitiesCached(context.Background(), market, map[string]*synthdata.LPProbabilitiesResponse{}, true); resp == nil {
			t.Fatalf("expected lp probability response")
		}
		pModel, code := svc.computeModelProbability(context.Background(), market, 68000, map[string]*synthdata.PredictionPercentilesResponse{}, true)
		if pModel <= 0 {
			t.Fatalf("expected p_model > 0, got %.4f (%s)", pModel, code)
		}
	}

	if got := upDownCalls.Load(); got != 1 {
		t.Fatalf("expected 1 up/down call in a window, got %d", got)
	}
	if got := percentileCalls.Load(); got != 1 {
		t.Fatalf("expected 1 percentile call in a window, got %d", got)
	}
	if got := volatilityCalls.Load(); got != 1 {
		t.Fatalf("expected 1 volatility call in a window, got %d", got)
	}
	if got := lpCalls.Load(); got != 1 {
		t.Fatalf("expected 1 lp call in a window, got %d", got)
	}
	if got := modelCalls.Load(); got != 1 {
		t.Fatalf("expected 1 model call in a window, got %d", got)
	}
}

func TestSynthWindowModelCacheKeyStableWithinWindow(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	market := UpDownMarket{
		Asset:          "BTC",
		WindowType:     Window5m,
		EventStartTime: now.Add(-time.Minute),
		EventEndTime:   now.Add(4 * time.Minute),
	}

	keyA := synthWindowModelCacheKey(market, 60, 3600, 68000)
	keyB := synthWindowModelCacheKey(market, 60, 3600, 68000)
	if keyA == "" || keyB == "" {
		t.Fatalf("expected non-empty model cache keys")
	}
	if keyA != keyB {
		t.Fatalf("expected stable model cache key within same window")
	}
}

func TestShouldAllowSynthFetchForMarketWindowPrefetch(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	active := UpDownMarket{
		EventStartTime: now.Add(-time.Minute),
		EventEndTime:   now.Add(4 * time.Minute),
	}
	if !shouldAllowSynthFetchForMarket(now, active) {
		t.Fatalf("expected active market to allow synth fetch")
	}

	nearStart := UpDownMarket{
		EventStartTime: now.Add(10 * time.Second),
		EventEndTime:   now.Add(5*time.Minute + 10*time.Second),
	}
	if !shouldAllowSynthFetchForMarket(now, nearStart) {
		t.Fatalf("expected near-start market to allow synth prefetch")
	}

	farUpcoming := UpDownMarket{
		EventStartTime: now.Add(3 * time.Minute),
		EventEndTime:   now.Add(8 * time.Minute),
	}
	if shouldAllowSynthFetchForMarket(now, farUpcoming) {
		t.Fatalf("expected far-upcoming market to deny synth fetch")
	}

	closed := UpDownMarket{
		EventStartTime: now.Add(-10 * time.Minute),
		EventEndTime:   now.Add(-time.Minute),
	}
	if shouldAllowSynthFetchForMarket(now, closed) {
		t.Fatalf("expected closed market to deny synth fetch")
	}
}

func TestSynthWindowCacheTTLPreStartExpiresAtWindowStart(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	market := UpDownMarket{
		EventStartTime: now.Add(10 * time.Minute),
		EventEndTime:   now.Add(15 * time.Minute),
	}

	ttl := synthWindowCacheTTL(now, market)
	if ttl < 9*time.Minute+59*time.Second || ttl > 10*time.Minute+time.Second {
		t.Fatalf("expected pre-start ttl to align with start boundary, got %s", ttl)
	}
}

func TestBuildSignalDoesNotFetchSynthBeforeWindowStarts(t *testing.T) {
	var upDownCalls atomic.Int32
	now := time.Now().UTC().Truncate(time.Second)
	slug := "btc-updown-5m-prestart-gate"
	mini := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mini.Addr()})

	synthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/insights/polymarket/up-down/5min":
			upDownCalls.Add(1)
			_, _ = w.Write([]byte(`{
				"slug":"` + slug + `",
				"start_price":68000,
				"current_time":"` + now.Format(time.RFC3339Nano) + `",
				"current_price":68010,
				"synth_probability_up":0.53
			}`))
		case "/insights/volatility":
			_, _ = w.Write([]byte(`{"current_price":68000,"forecast_future":{"average_volatility":32,"volatility":[0.01,0.02]},"forecast_past":{"average_volatility":30,"volatility":[0.01,0.02]}}`))
		case "/insights/prediction-percentiles":
			_, _ = w.Write([]byte(`{"current_price":68000,"forecast_future":{"percentiles":[{"0.005":67600,"0.05":67800,"0.2":67900,"0.35":67950,"0.5":68000,"0.65":68050,"0.8":68100,"0.95":68200,"0.995":68400}]}}`))
		case "/insights/lp-probabilities":
			_, _ = w.Write([]byte(`{"current_price":68000,"data":{"1h":{"probability_above":{"68000":0.53},"probability_below":{"68000":0.47}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer synthSrv.Close()

	cfg := &config.Config{
		Services: config.ServicesConfig{
			UpDownEnabled:    true,
			SynthDataAPIKey:  "test-key",
			SynthDataBaseURL: synthSrv.URL,
		},
	}
	svc := &UpDownService{
		cfg:                  cfg,
		redis:                rdb,
		market:               NewMarketService(nil, rdb, nil, nil),
		synth:                synthdata.NewClient(cfg),
		synthUpDownCache:     make(map[string]cachedSynthUpDown),
		synthPercentileCache: make(map[string]cachedSynthPercentile),
		synthVolatilityCache: make(map[string]cachedSynthVolatility),
		synthLPCache:         make(map[string]cachedSynthLP),
		synthModelProbCache:  make(map[string]cachedSynthModelProb),
	}
	live := models.Market{
		ConditionID: "0xprestart-gate",
		Slug:        slug,
		Outcomes:    `["Up","Down"]`,
		TokenIDYes:  "yes-prestart-gate",
		TokenIDNo:   "no-prestart-gate",
	}

	_, err := svc.buildSignal(
		context.Background(),
		UpDownMarket{
			Slug:                 slug,
			ConditionID:          live.ConditionID,
			Asset:                "BTC",
			WindowType:           Window5m,
			ResolutionSourceType: ResolutionSourceBinance,
			EventStartTime:       now.Add(2 * time.Minute),
			EventEndTime:         now.Add(7 * time.Minute),
			OutcomeIndexUp:       0,
			OutcomeIndexDown:     1,
			Market:               live,
		},
		map[string]*synthdata.PolymarketUpDownResponse{},
		map[string]*synthdata.PredictionPercentilesResponse{},
		map[string]*synthdata.VolatilityResponse{},
		map[string]*synthdata.LPProbabilitiesResponse{},
	)
	if err != nil {
		t.Fatalf("build scheduled signal: %v", err)
	}
	if got := upDownCalls.Load(); got != 0 {
		t.Fatalf("expected no pre-start synth fetch, got %d up/down calls", got)
	}

	_, err = svc.buildSignal(
		context.Background(),
		UpDownMarket{
			Slug:                 slug,
			ConditionID:          live.ConditionID,
			Asset:                "BTC",
			WindowType:           Window5m,
			ResolutionSourceType: ResolutionSourceBinance,
			EventStartTime:       now.Add(-2 * time.Minute),
			EventEndTime:         now.Add(3 * time.Minute),
			OutcomeIndexUp:       0,
			OutcomeIndexDown:     1,
			Market:               live,
		},
		map[string]*synthdata.PolymarketUpDownResponse{},
		map[string]*synthdata.PredictionPercentilesResponse{},
		map[string]*synthdata.VolatilityResponse{},
		map[string]*synthdata.LPProbabilitiesResponse{},
	)
	if err != nil {
		t.Fatalf("build active signal: %v", err)
	}
	if got := upDownCalls.Load(); got != 1 {
		t.Fatalf("expected one active-window synth fetch, got %d", got)
	}
}

func TestBuildSignalClosedMarketUsesPersistedReferenceSnapshot(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Market{}); err != nil {
		t.Fatalf("automigrate market: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE updown_market_windows (
			condition_id TEXT NOT NULL,
			event_start_time DATETIME NOT NULL,
			reference_start_price REAL,
			reference_current_price REAL,
			reference_end_price REAL,
			signal_timestamp DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create updown_market_windows: %v", err)
	}

	mini := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(-6 * time.Minute)
	end := now.Add(-time.Minute)

	live := models.Market{
		ConditionID:     "0xpersisted-reference",
		Slug:            "btc-updown-5m-persisted-reference",
		Title:           "Bitcoin Up or Down - Persisted Reference",
		Outcomes:        `["Up","Down"]`,
		ResolutionRules: "Resolution source: Binance 1h candle open and close.",
		TokenIDYes:      "yes-persisted-reference",
		TokenIDNo:       "no-persisted-reference",
		AcceptingOrders: false,
		Active:          false,
		Closed:          true,
		EventStartTime:  &start,
		EndDate:         &end,
		Liquidity:       20_000,
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

	persistedStart := 68000.0
	persistedCurrent := 68042.0
	signalTimestamp := end.Add(-20 * time.Second)
	if err := db.Exec(`
		INSERT INTO updown_market_windows (
			condition_id, event_start_time, reference_start_price, reference_current_price, reference_end_price, signal_timestamp
		) VALUES (?, ?, ?, ?, ?, ?)
	`,
		live.ConditionID,
		start.UTC(),
		persistedStart,
		persistedCurrent,
		nil,
		signalTimestamp.UTC(),
	).Error; err != nil {
		t.Fatalf("insert persisted window snapshot: %v", err)
	}

	svc := &UpDownService{
		db:    db,
		redis: rdb,
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

	signal, err := svc.buildSignalWithOptions(
		ctx,
		UpDownMarket{
			Slug:                 live.Slug,
			ConditionID:          live.ConditionID,
			Asset:                "BTC",
			WindowType:           Window5m,
			ResolutionSourceType: ResolutionSourceBinance,
			EventStartTime:       start,
			EventEndTime:         end,
			TimeToStartSeconds:   int64(start.Sub(now).Seconds()),
			TimeToEndSeconds:     int64(end.Sub(now).Seconds()),
			OutcomeIndexUp:       0,
			OutcomeIndexDown:     1,
			Market:               live,
		},
		map[string]*synthdata.PolymarketUpDownResponse{},
		map[string]*synthdata.PredictionPercentilesResponse{},
		map[string]*synthdata.VolatilityResponse{},
		map[string]*synthdata.LPProbabilitiesResponse{},
		signalBuildOptions{allowSynthFetch: false},
	)
	if err != nil {
		t.Fatalf("build signal: %v", err)
	}
	if signal.ReferenceCurrentPrice == nil || *signal.ReferenceCurrentPrice != persistedCurrent {
		t.Fatalf("expected reference current price from persisted snapshot, got %+v", signal.ReferenceCurrentPrice)
	}
	if signal.ReferenceEndPrice == nil || *signal.ReferenceEndPrice != persistedCurrent {
		t.Fatalf("expected reference end price from persisted snapshot fallback, got %+v", signal.ReferenceEndPrice)
	}
	if outcome, _ := resolveUpDownOutcome(signal); outcome == upDownOutcomePending {
		t.Fatalf("expected closed signal to resolve with persisted reference snapshot")
	}
}

func TestBuildSignalRejectsMismatchedSynthWindowFor5m(t *testing.T) {
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

	synthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/insights/polymarket/up-down/5min":
			_, _ = w.Write([]byte(`{
				"slug":"btc-updown-mismatch",
				"start_price":68000,
				"current_time":"` + now.Format(time.RFC3339Nano) + `",
				"current_price":68110,
				"synth_probability_up":0.99,
				"event_start_time":"` + start.Add(-5*time.Minute).Format(time.RFC3339Nano) + `",
				"event_end_time":"` + end.Add(5*time.Minute).Format(time.RFC3339Nano) + `",
				"best_bid_size":300,
				"best_ask_size":280
			}`))
		case "/insights/prediction-percentiles":
			_, _ = w.Write([]byte(`{"current_price":68000,"forecast_future":{"percentiles":[{"0.005":67000,"0.05":67500,"0.2":67850,"0.35":67950,"0.5":68000,"0.65":68050,"0.8":68150,"0.95":68500,"0.995":69000}]}}`))
		case "/insights/volatility":
			_, _ = w.Write([]byte(`{"current_price":68000,"forecast_future":{"average_volatility":34,"volatility":[0.01,0.02]},"forecast_past":{"average_volatility":30,"volatility":[0.01,0.02]}}`))
		case "/insights/lp-probabilities":
			_, _ = w.Write([]byte(`{"current_price":68000,"data":{"1h":{"probability_above":{"68000":0.52},"probability_below":{"68000":0.48}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer synthSrv.Close()

	live := models.Market{
		ConditionID:     "0xproxy-ignore",
		Slug:            "btc-updown-5m-proxy-ignore",
		Title:           "Bitcoin Up or Down - Proxy Ignore",
		Outcomes:        `["Up","Down"]`,
		ResolutionRules: "Resolved by Chainlink reference feed https://data.chain.link",
		TokenIDYes:      "yes-proxy",
		TokenIDNo:       "no-proxy",
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
	if err := StoreChainlinkLatest(ctx, rdb, "BTC", 68000.0, now); err != nil {
		t.Fatalf("store chainlink latest: %v", err)
	}

	cfg := &config.Config{
		Services: config.ServicesConfig{
			UpDownEnabled:             true,
			UpDownEnterpriseEnabled:   false,
			UpDownFeeBps:              10,
			UpDownDepthProbeShares:    10,
			UpDownMaxSpreadToTrade:    0.10,
			UpDownEVMinThreshold:      0.01,
			UpDownKellyFraction:       0.25,
			UpDownMaxFractionPerTrade: 0.05,
			UpDownAssetExposureCap:    0.20,
			UpDownNotionalBankroll:    1000,
			SynthDataAPIKey:           "test-key",
			SynthDataBaseURL:          synthSrv.URL,
		},
	}

	svc := &UpDownService{
		cfg:                  cfg,
		redis:                rdb,
		market:               NewMarketService(db, rdb, nil, nil),
		synth:                synthdata.NewClient(cfg),
		synthUpDownCache:     make(map[string]cachedSynthUpDown),
		synthPercentileCache: make(map[string]cachedSynthPercentile),
		synthVolatilityCache: make(map[string]cachedSynthVolatility),
		synthLPCache:         make(map[string]cachedSynthLP),
		synthModelProbCache:  make(map[string]cachedSynthModelProb),
	}

	signal, err := svc.buildSignal(ctx, UpDownMarket{
		Slug:                 live.Slug,
		ConditionID:          live.ConditionID,
		Asset:                "BTC",
		WindowType:           Window5m,
		ResolutionSourceType: ResolutionSourceChainlink,
		EventStartTime:       start,
		EventEndTime:         end,
		TimeToStartSeconds:   int64(start.Sub(now).Seconds()),
		TimeToEndSeconds:     int64(end.Sub(now).Seconds()),
		OutcomeIndexUp:       0,
		OutcomeIndexDown:     1,
		Market:               live,
	}, map[string]*synthdata.PolymarketUpDownResponse{}, map[string]*synthdata.PredictionPercentilesResponse{}, map[string]*synthdata.VolatilityResponse{}, map[string]*synthdata.LPProbabilitiesResponse{})
	if err != nil {
		t.Fatalf("build signal: %v", err)
	}
	if !signal.RiskFlags.SourceMismatch {
		t.Fatalf("expected strict synth window mismatch to trigger source mismatch")
	}
	if signal.PSynthUp == nil {
		t.Fatalf("expected synth probability via fallback path")
	}
	if *signal.PSynthUp >= 0.95 {
		t.Fatalf("expected mismatched synth direct probability to be rejected, got %.4f", *signal.PSynthUp)
	}

	foundMismatchReason := false
	for _, reason := range signal.ReasonCodes {
		if reason == "synth_market_window_mismatch" {
			foundMismatchReason = true
			break
		}
	}
	if !foundMismatchReason {
		t.Fatalf("expected synth_market_window_mismatch reason code")
	}
}

func TestBuildSignalUsesLastTradeProbabilityWhenOrderbookQuotesMissing(t *testing.T) {
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
		ConditionID:     "0xlasttrade-pmarket-fallback",
		Slug:            "btc-updown-5m-lasttrade-pmarket-fallback",
		Title:           "Bitcoin Up or Down - Last Trade P Market Fallback",
		Outcomes:        `["Up","Down"]`,
		ResolutionRules: "Resolved by Chainlink reference feed https://data.chain.link",
		TokenIDYes:      "yes-pmarket",
		TokenIDNo:       "no-pmarket",
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

	// Seed last-trade only (no best_bid / best_ask) to emulate temporary book absence.
	if err := rdb.HSet(ctx, priceRedisKey(live.ConditionID, live.TokenIDYes), map[string]any{
		"last_trade_price":   "0.55",
		"last_trade_updated": now.Format(time.RFC3339Nano),
	}).Err(); err != nil {
		t.Fatalf("seed yes last trade: %v", err)
	}
	if err := rdb.HSet(ctx, priceRedisKey(live.ConditionID, live.TokenIDNo), map[string]any{
		"last_trade_price":   "0.45",
		"last_trade_updated": now.Format(time.RFC3339Nano),
	}).Err(); err != nil {
		t.Fatalf("seed no last trade: %v", err)
	}

	cfg := &config.Config{
		Services: config.ServicesConfig{
			UpDownEnabled:             true,
			UpDownEnterpriseEnabled:   false,
			UpDownFeeBps:              10,
			UpDownDepthProbeShares:    10,
			UpDownMaxSpreadToTrade:    0.10,
			UpDownEVMinThreshold:      0.01,
			UpDownKellyFraction:       0.25,
			UpDownMaxFractionPerTrade: 0.05,
			UpDownAssetExposureCap:    0.20,
			UpDownNotionalBankroll:    1000,
		},
	}

	svc := &UpDownService{
		cfg:                  cfg,
		redis:                rdb,
		market:               NewMarketService(db, rdb, nil, nil),
		synthUpDownCache:     make(map[string]cachedSynthUpDown),
		synthPercentileCache: make(map[string]cachedSynthPercentile),
		synthVolatilityCache: make(map[string]cachedSynthVolatility),
		synthLPCache:         make(map[string]cachedSynthLP),
		synthModelProbCache:  make(map[string]cachedSynthModelProb),
	}

	signal, err := svc.buildSignal(ctx, UpDownMarket{
		Slug:                 live.Slug,
		ConditionID:          live.ConditionID,
		Asset:                "BTC",
		WindowType:           Window5m,
		ResolutionSourceType: ResolutionSourceChainlink,
		EventStartTime:       start,
		EventEndTime:         end,
		TimeToStartSeconds:   int64(start.Sub(now).Seconds()),
		TimeToEndSeconds:     int64(end.Sub(now).Seconds()),
		OutcomeIndexUp:       0,
		OutcomeIndexDown:     1,
		Market:               live,
	}, map[string]*synthdata.PolymarketUpDownResponse{}, map[string]*synthdata.PredictionPercentilesResponse{}, map[string]*synthdata.VolatilityResponse{}, map[string]*synthdata.LPProbabilitiesResponse{})
	if err != nil {
		t.Fatalf("build signal: %v", err)
	}
	if signal.PMarketUp == nil {
		t.Fatalf("expected p_market to fallback from last trade prices")
	}
	if got := *signal.PMarketUp; got < 0.549 || got > 0.551 {
		t.Fatalf("expected p_market ~0.55 from last trade prices, got %f", got)
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

func TestSynthUpDownResponseMatchesMarketRejectsWindowSpanMismatch(t *testing.T) {
	start := time.Date(2026, 3, 6, 8, 0, 0, 0, time.UTC)
	end := start.Add(5 * time.Minute)
	market := UpDownMarket{
		Slug:           "btc-updown-5m-1772860800",
		EventStartTime: start,
		EventEndTime:   end,
	}
	resp := &synthdata.PolymarketUpDownResponse{
		Slug:           "btc-updown-15m-proxy",
		EventStartTime: start.Add(-5 * time.Minute).Format(time.RFC3339),
		EventEndTime:   end.Add(5 * time.Minute).Format(time.RFC3339),
	}

	if synthUpDownResponseMatchesMarket(market, resp) {
		t.Fatalf("expected strict matcher to reject cross-window synth payload")
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

func TestLogDecisionHonorsDBWritePauseFlag(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	svc := &UpDownService{
		db: db,
		cfg: &config.Config{
			Services: config.ServicesConfig{
				UpDownDBWritesPaused: true,
			},
		},
	}

	_, err = svc.LogDecision(context.Background(), "user-1", UpDownDecisionLogRequest{
		Slug:   "btc-updown-5m-1",
		Action: "accepted",
	})
	if !errors.Is(err, ErrUpDownDBWritesPaused) {
		t.Fatalf("expected ErrUpDownDBWritesPaused, got: %v", err)
	}
}

func TestBuildSignalRealtimeSkipsSynthFetchWhenCacheMiss(t *testing.T) {
	var calls atomic.Int32
	synthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`"No prediction available"`))
	}))
	defer synthSrv.Close()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.Market{}); err != nil {
		t.Fatalf("automigrate market: %v", err)
	}

	mini := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mini.Addr()})

	cfg := &config.Config{
		Services: config.ServicesConfig{
			UpDownEnabled:             true,
			UpDownEnterpriseEnabled:   false,
			UpDownFeeBps:              10,
			UpDownDepthProbeShares:    10,
			UpDownMaxSpreadToTrade:    0.10,
			UpDownEVMinThreshold:      0.01,
			UpDownKellyFraction:       0.25,
			UpDownMaxFractionPerTrade: 0.05,
			UpDownAssetExposureCap:    0.20,
			UpDownNotionalBankroll:    1000,
			SynthDataAPIKey:           "test-key",
			SynthDataBaseURL:          synthSrv.URL,
		},
	}

	svc := &UpDownService{
		cfg:                  cfg,
		redis:                rdb,
		market:               NewMarketService(db, rdb, nil, nil),
		synth:                synthdata.NewClient(cfg),
		synthUpDownCache:     make(map[string]cachedSynthUpDown),
		synthPercentileCache: make(map[string]cachedSynthPercentile),
		synthVolatilityCache: make(map[string]cachedSynthVolatility),
		synthLPCache:         make(map[string]cachedSynthLP),
		synthModelProbCache:  make(map[string]cachedSynthModelProb),
	}

	now := time.Now().UTC()
	market := UpDownMarket{
		Slug:                 "btc-updown-5m-cacheonly",
		ConditionID:          "0xcacheonly",
		Asset:                "BTC",
		WindowType:           Window5m,
		ResolutionSourceType: ResolutionSourceUnknown,
		EventStartTime:       now.Add(-2 * time.Minute),
		EventEndTime:         now.Add(3 * time.Minute),
		TimeToStartSeconds:   -120,
		TimeToEndSeconds:     180,
		OutcomeIndexUp:       0,
		OutcomeIndexDown:     1,
		Market: models.Market{
			ConditionID: "0xcacheonly",
			TokenIDYes:  "yes-cacheonly",
			TokenIDNo:   "no-cacheonly",
			YesBestBid:  0.49,
			YesBestAsk:  0.51,
			NoBestBid:   0.48,
			NoBestAsk:   0.50,
			Liquidity:   25_000,
		},
	}

	_, err = svc.buildSignalWithOptions(
		context.Background(),
		market,
		map[string]*synthdata.PolymarketUpDownResponse{},
		map[string]*synthdata.PredictionPercentilesResponse{},
		map[string]*synthdata.VolatilityResponse{},
		map[string]*synthdata.LPProbabilitiesResponse{},
		signalBuildOptions{allowSynthFetch: false},
	)
	if err != nil {
		t.Fatalf("build realtime signal: %v", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("expected realtime/cache-only path to avoid synth fetches, got %d calls", got)
	}

	_, err = svc.buildSignalWithOptions(
		context.Background(),
		market,
		map[string]*synthdata.PolymarketUpDownResponse{},
		map[string]*synthdata.PredictionPercentilesResponse{},
		map[string]*synthdata.VolatilityResponse{},
		map[string]*synthdata.LPProbabilitiesResponse{},
		signalBuildOptions{allowSynthFetch: true},
	)
	if err != nil {
		t.Fatalf("build refresh signal: %v", err)
	}
	if got := calls.Load(); got == 0 {
		t.Fatalf("expected refresh path to fetch synth at least once")
	}
}

func TestBuildSignalRealtimeUsesDepthFallbackWithoutSynthFetch(t *testing.T) {
	var calls atomic.Int32
	synthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`"No prediction available"`))
	}))
	defer synthSrv.Close()

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
		ConditionID:     "0xcacheonly-depth",
		Slug:            "btc-updown-5m-cacheonly-depth",
		Title:           "Bitcoin Up or Down - Cache Only Depth",
		Outcomes:        `["Up","Down"]`,
		ResolutionRules: "Resolved by Chainlink reference feed https://data.chain.link",
		TokenIDYes:      "yes-cacheonly-depth",
		TokenIDNo:       "no-cacheonly-depth",
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
		"updated": updated,
	}).Err(); err != nil {
		t.Fatalf("seed yes price timestamp: %v", err)
	}
	if err := rdb.HSet(ctx, priceRedisKey(live.ConditionID, live.TokenIDNo), map[string]any{
		"updated": updated,
	}).Err(); err != nil {
		t.Fatalf("seed no price timestamp: %v", err)
	}

	yesBook, err := json.Marshal(orderBookSnapshot{
		AssetID: live.TokenIDYes,
		Market:  live.ConditionID,
		Bids: []orderBookLevel{
			{Price: "0.49", Size: "200"},
		},
		Asks: []orderBookLevel{
			{Price: "0.52", Size: "200"},
		},
	})
	if err != nil {
		t.Fatalf("marshal yes book: %v", err)
	}
	noBook, err := json.Marshal(orderBookSnapshot{
		AssetID: live.TokenIDNo,
		Market:  live.ConditionID,
		Bids: []orderBookLevel{
			{Price: "0.47", Size: "200"},
		},
		Asks: []orderBookLevel{
			{Price: "0.50", Size: "200"},
		},
	})
	if err != nil {
		t.Fatalf("marshal no book: %v", err)
	}
	if err := rdb.Set(ctx, "book:"+live.ConditionID+":"+live.TokenIDYes, string(yesBook), time.Minute).Err(); err != nil {
		t.Fatalf("seed yes book: %v", err)
	}
	if err := rdb.Set(ctx, "book:"+live.ConditionID+":"+live.TokenIDNo, string(noBook), time.Minute).Err(); err != nil {
		t.Fatalf("seed no book: %v", err)
	}

	cfg := &config.Config{
		Services: config.ServicesConfig{
			UpDownEnabled:             true,
			UpDownEnterpriseEnabled:   false,
			UpDownFeeBps:              10,
			UpDownDepthProbeShares:    10,
			UpDownMaxSpreadToTrade:    0.10,
			UpDownEVMinThreshold:      0.01,
			UpDownKellyFraction:       0.25,
			UpDownMaxFractionPerTrade: 0.05,
			UpDownAssetExposureCap:    0.20,
			UpDownNotionalBankroll:    1000,
			SynthDataAPIKey:           "test-key",
			SynthDataBaseURL:          synthSrv.URL,
		},
	}

	svc := &UpDownService{
		cfg:                  cfg,
		redis:                rdb,
		market:               NewMarketService(db, rdb, nil, nil),
		synth:                synthdata.NewClient(cfg),
		synthUpDownCache:     make(map[string]cachedSynthUpDown),
		synthPercentileCache: make(map[string]cachedSynthPercentile),
		synthVolatilityCache: make(map[string]cachedSynthVolatility),
		synthLPCache:         make(map[string]cachedSynthLP),
		synthModelProbCache:  make(map[string]cachedSynthModelProb),
	}

	signal, err := svc.buildSignalWithOptions(
		context.Background(),
		UpDownMarket{
			Slug:                 live.Slug,
			ConditionID:          live.ConditionID,
			Asset:                "BTC",
			WindowType:           Window5m,
			ResolutionSourceType: ResolutionSourceUnknown,
			EventStartTime:       start,
			EventEndTime:         end,
			TimeToStartSeconds:   int64(start.Sub(now).Seconds()),
			TimeToEndSeconds:     int64(end.Sub(now).Seconds()),
			OutcomeIndexUp:       0,
			OutcomeIndexDown:     1,
			Market:               live,
		},
		map[string]*synthdata.PolymarketUpDownResponse{},
		map[string]*synthdata.PredictionPercentilesResponse{},
		map[string]*synthdata.VolatilityResponse{},
		map[string]*synthdata.LPProbabilitiesResponse{},
		signalBuildOptions{allowSynthFetch: false},
	)
	if err != nil {
		t.Fatalf("build realtime signal: %v", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("expected realtime/cache-only path to avoid synth fetches, got %d calls", got)
	}
	if signal.ExecutableAskUp <= 0 || signal.ExecutableAskDown <= 0 {
		t.Fatalf("expected depth fallback to populate executable asks, got up=%.4f down=%.4f", signal.ExecutableAskUp, signal.ExecutableAskDown)
	}
	if signal.RiskFlags.DataIntegrityFailed {
		t.Fatalf("expected depth fallback to avoid data integrity failure")
	}
}

func TestLockRecommendationAtMidWindowPersistsPreviousLock(t *testing.T) {
	start := time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC)
	end := start.Add(5 * time.Minute)
	now := start.Add(3 * time.Minute)
	market := UpDownMarket{
		Slug:           "btc-updown-5m-lock",
		EventStartTime: start,
		EventEndTime:   end,
	}

	initial := UpDownRecommendation{
		ID:          "rec-1",
		Slug:        market.Slug,
		Decision:    "BUY_UP",
		GeneratedAt: now,
	}

	lockedRec, lockedPtr, lockedAt := lockRecommendationAtMidWindow(now, market, nil, initial)
	if lockedPtr == nil || lockedAt == nil {
		t.Fatalf("expected recommendation to lock after midpoint")
	}
	if lockedRec.Decision != "BUY_UP" {
		t.Fatalf("expected locked decision BUY_UP, got %s", lockedRec.Decision)
	}

	next := UpDownRecommendation{
		ID:          "rec-2",
		Slug:        market.Slug,
		Decision:    "BUY_DOWN",
		GeneratedAt: now.Add(20 * time.Second),
	}
	prev := &UpDownSignal{
		LockedRecommendation:   lockedPtr,
		RecommendationLockedAt: lockedAt,
	}
	lockedAgain, lockedPtrAgain, _ := lockRecommendationAtMidWindow(now.Add(20*time.Second), market, prev, next)
	if lockedPtrAgain == nil {
		t.Fatalf("expected prior lock to persist")
	}
	if lockedAgain.Decision != "BUY_UP" {
		t.Fatalf("expected persisted lock decision BUY_UP, got %s", lockedAgain.Decision)
	}
}

func TestLockRecommendationAtMidWindowKeepsPreviousLockOnNoTrade(t *testing.T) {
	start := time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC)
	end := start.Add(5 * time.Minute)
	now := start.Add(3 * time.Minute)
	market := UpDownMarket{
		Slug:           "btc-updown-5m-lock-no-trade",
		EventStartTime: start,
		EventEndTime:   end,
	}

	lockedAt := now.Add(-20 * time.Second).UTC()
	prevLocked := UpDownRecommendation{
		ID:          "rec-prev-lock",
		Slug:        market.Slug,
		Decision:    "BUY_DOWN",
		GeneratedAt: lockedAt,
	}
	prev := &UpDownSignal{
		LockedRecommendation:   &prevLocked,
		RecommendationLockedAt: &lockedAt,
	}
	currentNoTrade := UpDownRecommendation{
		ID:          "rec-current",
		Slug:        market.Slug,
		Decision:    "NO_TRADE",
		GeneratedAt: now,
	}

	got, gotLocked, gotLockedAt := lockRecommendationAtMidWindow(now, market, prev, currentNoTrade)
	if gotLocked == nil || gotLockedAt == nil {
		t.Fatalf("expected previous lock metadata to persist")
	}
	if got.Decision != "BUY_DOWN" {
		t.Fatalf("expected previous lock decision BUY_DOWN, got %s", got.Decision)
	}
	if !gotLockedAt.Equal(lockedAt) {
		t.Fatalf("expected locked timestamp %s, got %s", lockedAt, gotLockedAt.UTC())
	}
}

func TestLockRecommendationAtMidWindowSkipsNoTradeDecisions(t *testing.T) {
	start := time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC)
	end := start.Add(5 * time.Minute)
	now := start.Add(3 * time.Minute)
	market := UpDownMarket{
		Slug:           "btc-updown-5m-no-trade-lock",
		EventStartTime: start,
		EventEndTime:   end,
	}
	noTrade := UpDownRecommendation{
		ID:          "rec-nt",
		Slug:        market.Slug,
		Decision:    "NO_TRADE",
		GeneratedAt: now,
	}
	rec, locked, lockedAt := lockRecommendationAtMidWindow(now, market, nil, noTrade)
	if rec.Decision != "NO_TRADE" {
		t.Fatalf("expected decision to remain NO_TRADE, got %s", rec.Decision)
	}
	if locked != nil || lockedAt != nil {
		t.Fatalf("expected NO_TRADE recommendations not to be locked")
	}
}

func TestLockRecommendationAtMidWindowReleasesLockAfterWindowClose(t *testing.T) {
	start := time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC)
	end := start.Add(5 * time.Minute)
	lockTime := start.Add(3 * time.Minute)
	market := UpDownMarket{
		Slug:           "btc-updown-5m-lock-release",
		EventStartTime: start,
		EventEndTime:   end,
	}

	locked := UpDownRecommendation{
		ID:          "rec-locked",
		Slug:        market.Slug,
		Decision:    "BUY_UP",
		GeneratedAt: lockTime,
	}
	_, lockedPtr, lockedAt := lockRecommendationAtMidWindow(lockTime, market, nil, locked)
	if lockedPtr == nil || lockedAt == nil {
		t.Fatalf("expected initial lock to be set")
	}

	current := UpDownRecommendation{
		ID:          "rec-current",
		Slug:        market.Slug,
		Decision:    "NO_TRADE",
		GeneratedAt: end.Add(10 * time.Second),
	}
	prev := &UpDownSignal{
		LockedRecommendation:   lockedPtr,
		RecommendationLockedAt: lockedAt,
	}
	got, gotLocked, gotLockedAt := lockRecommendationAtMidWindow(end.Add(10*time.Second), market, prev, current)
	if gotLocked != nil || gotLockedAt != nil {
		t.Fatalf("expected lock metadata to be cleared after window close")
	}
	if got.ID != current.ID || got.Decision != current.Decision {
		t.Fatalf("expected current recommendation after close, got id=%s decision=%s", got.ID, got.Decision)
	}
}
