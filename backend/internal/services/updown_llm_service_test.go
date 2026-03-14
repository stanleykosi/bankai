package services

import (
	"testing"
	"time"

	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/integrations/allora"
)

func TestProxyAlloraProbability15mStatusTransitions(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Services: config.ServicesConfig{
			UpDownLLMAlloraSoftLagSeconds: 380,
			UpDownLLMAlloraHardLagSeconds: 440,
		},
	}
	svc := &UpDownLLMService{cfg: cfg}

	now := time.Now().UTC()
	market := &UpDownMarket{
		WindowType:     Window15m,
		EventStartTime: now.Add(-6 * time.Minute),
		EventEndTime:   now.Add(9 * time.Minute),
	}

	fresh := svc.proxyAlloraProbability(market, 0.61, 45, now)
	if fresh.ProxyStatus != "fresh" {
		t.Fatalf("expected fresh, got %s", fresh.ProxyStatus)
	}
	soft := svc.proxyAlloraProbability(market, 0.61, 400, now)
	if soft.ProxyStatus != "stale_soft" {
		t.Fatalf("expected stale_soft, got %s", soft.ProxyStatus)
	}
	hard := svc.proxyAlloraProbability(market, 0.61, 500, now)
	if hard.ProxyStatus != "stale_hard" {
		t.Fatalf("expected stale_hard, got %s", hard.ProxyStatus)
	}
	if fresh.ProxyP15 < 0.01 || fresh.ProxyP15 > 0.99 {
		t.Fatalf("proxy probability out of bounds: %.4f", fresh.ProxyP15)
	}
}

func TestBuildLLMContextHashDeterministic(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Services: config.ServicesConfig{
			UpDownLLMContextDecimals: 6,
		},
	}
	svc := &UpDownLLMService{cfg: cfg}
	now := time.Now().UTC()
	start := now.Add(-2 * time.Minute)
	end := start.Add(5 * time.Minute)
	refStart := 3000.1234567
	refNow := 3001.998877
	pMarket := 0.58
	pSynth := 0.61
	pModel := 0.6
	pLP := 0.57

	market := UpDownMarket{
		Slug:                 "eth-updown-5m-1",
		ConditionID:          "0xabc",
		Asset:                "ETH",
		WindowType:           Window5m,
		ResolutionSourceType: ResolutionSourceChainlink,
		EventStartTime:       start,
		EventEndTime:         end,
		TimeToEndSeconds:     180,
	}
	snapshot := llmIndependentSnapshot{
		Timestamp:             now,
		MarketAgeSeconds:      5,
		SynthAgeSeconds:       7,
		ReferenceStartPrice:   &refStart,
		ReferenceCurrentPrice: &refNow,
		ReferenceSource:       "synth",
		PMarketUp:             &pMarket,
		PSynthUp:              &pSynth,
		PModelUp:              &pModel,
		PLPUp:                 &pLP,
		EVUp:                  0.01234,
		EVDown:                0.0012,
		EVMinThreshold:        0.01,
		Confidence:            0.74,
		Regime:                "volatile",
		ExecutableAskUp:       0.51,
		ExecutableAskDown:     0.49,
		ExecutableBidUp:       0.5,
		ExecutableBidDown:     0.48,
		SpreadUp:              0.01,
		SpreadDown:            0.01,
		ExpectedSlippage:      0.003,
		DepthImbalance:        0.12,
		UpBuyDepth: llmDepthEstimate{
			RequestedSize:         15,
			FillableSize:          12,
			EstimatedAveragePrice: 0.51,
			EstimatedTotalValue:   6.12,
		},
		DownBuyDepth: llmDepthEstimate{
			RequestedSize:         15,
			FillableSize:          13,
			EstimatedAveragePrice: 0.49,
			EstimatedTotalValue:   6.37,
		},
		VolatilityAverageForecast: 78,
		VolatilityAveragePast:     66,
		RiskFlags: UpDownRiskFlags{
			HighVolatility: true,
		},
		Retrieval: llmRetrievalBundle{
			StrategyVersion:  "rag-v1.3",
			RankingPolicy:    "freshness_weighted_reliability_rank",
			CorrectiveAction: "none",
			QualityScore:     0.8,
			Evidence: []llmRetrievalEvidence{
				{Source: "market_microstructure", Status: "fresh", AgeSeconds: 5, Reliability: 0.9, Coverage: 0.9, RetrievalScore: 0.81, FreshnessWeight: 0.95},
			},
		},
	}
	alloraInf := &allora.PriceInference{
		TopicID:   "13",
		Timestamp: now.Add(-20 * time.Second),
	}
	meta := AlloraProxyMeta{
		RawP5:       0.59,
		SmoothedP5:  0.59,
		ProxyP15:    0.59,
		AgeSeconds:  20,
		ProxyStatus: "fresh",
	}
	stability := LLMSnapshotStability{
		SampleCount:      2,
		Stable:           true,
		UpVotes:          2,
		DirectionSummary: "UP",
	}

	_, _, hashA := svc.buildLLMContext(market, snapshot, alloraInf, meta, stability)
	_, _, hashB := svc.buildLLMContext(market, snapshot, alloraInf, meta, stability)
	if hashA == "" || hashB == "" {
		t.Fatalf("expected non-empty hashes")
	}
	if hashA != hashB {
		t.Fatalf("expected deterministic context hash, got %s != %s", hashA, hashB)
	}
}

func TestBuildLLMContextIncludesAllora5mInferencePayload(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Services: config.ServicesConfig{
			UpDownLLMContextDecimals: 6,
		},
	}
	svc := &UpDownLLMService{cfg: cfg}
	now := time.Now().UTC()
	start := now.Add(-2 * time.Minute)
	end := start.Add(5 * time.Minute)
	refStart := 3000.1234567
	refNow := 3001.998877

	market := UpDownMarket{
		Slug:                 "eth-updown-5m-2",
		ConditionID:          "0xdef",
		Asset:                "ETH",
		WindowType:           Window5m,
		ResolutionSourceType: ResolutionSourceChainlink,
		EventStartTime:       start,
		EventEndTime:         end,
		TimeToEndSeconds:     180,
	}
	snapshot := llmIndependentSnapshot{
		Timestamp:             now,
		ReferenceStartPrice:   &refStart,
		ReferenceCurrentPrice: &refNow,
		ReferenceSource:       "synth",
	}
	alloraInf := &allora.PriceInference{
		Asset:                         "ETH",
		Timeframe:                     "5m",
		TopicID:                       "13",
		Timestamp:                     now.Add(-20 * time.Second),
		NetworkInference:              3012.551234,
		ConfidenceIntervalPercentiles: []float64{0.1, 0.5, 0.9},
		ConfidenceIntervalValues:      []float64{2988.1, 3012.5, 3041.8},
	}
	meta := AlloraProxyMeta{
		RawP5:       0.59,
		SmoothedP5:  0.58,
		ProxyP15:    0.57,
		AgeSeconds:  20,
		ProxyStatus: "fresh",
	}
	stability := LLMSnapshotStability{
		SampleCount:      2,
		Stable:           true,
		UpVotes:          2,
		DirectionSummary: "UP",
	}

	ctxData, _, _ := svc.buildLLMContext(market, snapshot, alloraInf, meta, stability)
	if ctxData.Allora.Asset != "ETH" {
		t.Fatalf("expected allora asset ETH, got %q", ctxData.Allora.Asset)
	}
	if ctxData.Allora.Timeframe != "5m" {
		t.Fatalf("expected allora timeframe 5m, got %q", ctxData.Allora.Timeframe)
	}
	if ctxData.Allora.NetworkInference == nil {
		t.Fatalf("expected allora network inference to be present")
	}
	if len(ctxData.Allora.ConfidenceIntervalPercentiles) != 3 {
		t.Fatalf("expected 3 allora confidence percentiles, got %d", len(ctxData.Allora.ConfidenceIntervalPercentiles))
	}
	if len(ctxData.Allora.ConfidenceIntervalValues) != 3 {
		t.Fatalf("expected 3 allora confidence values, got %d", len(ctxData.Allora.ConfidenceIntervalValues))
	}
}

func TestShouldMarkLLMMarketStaleToleratesFreshChainlinkReference(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	market := UpDownMarket{
		WindowType:       Window5m,
		IsActiveWindow:   true,
		EventStartTime:   now.Add(-3 * time.Minute),
		EventEndTime:     now.Add(2 * time.Minute),
		TimeToEndSeconds: 120,
	}
	marketUpdated := now.Add(-46 * time.Second)
	refUpdated := now.Add(-2 * time.Second)
	refCurrent := 70594.81

	stale := shouldMarkLLMMarketStale(
		now,
		market,
		marketUpdated,
		30*time.Second,
		0.53,
		0.48,
		&refUpdated,
		true,
		true,
		&refCurrent,
		nil,
	)
	if stale {
		t.Fatalf("expected stale flag to be tolerated with fresh chainlink reference")
	}
}

func TestShouldMarkLLMMarketStaleWhenNoTolerance(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	market := UpDownMarket{
		WindowType:       Window5m,
		IsActiveWindow:   true,
		EventStartTime:   now.Add(-3 * time.Minute),
		EventEndTime:     now.Add(2 * time.Minute),
		TimeToEndSeconds: 120,
	}
	marketUpdated := now.Add(-46 * time.Second)

	stale := shouldMarkLLMMarketStale(
		now,
		market,
		marketUpdated,
		30*time.Second,
		0,
		0,
		nil,
		true,
		false,
		nil,
		nil,
	)
	if !stale {
		t.Fatalf("expected stale flag when quotes and references are stale or missing")
	}
}

func TestDecodeStrictLLMResponseRejectsUnknownField(t *testing.T) {
	t.Parallel()

	_, err := decodeStrictLLMResponse(`{
		"decision":"NO_TRADE",
		"recommended_side":"NONE",
		"confidence":0.1,
		"expected_value":0,
		"suggested_limit_price":0,
		"suggested_size_shares":0,
		"suggested_notional":0,
		"reason_codes":["staleness_guard","confidence_guard"],
		"invalidation_conditions":["recheck"],
		"extra":"not-allowed"
	}`)
	if err == nil {
		t.Fatalf("expected strict decode error for unknown field")
	}
}

func TestDecodeStrictLLMResponseAllowsMissingInvalidationConditions(t *testing.T) {
	t.Parallel()

	raw, err := decodeStrictLLMResponse(`{
		"decision":"NO_TRADE",
		"recommended_side":"NONE",
		"confidence":0.1,
		"expected_value":0,
		"suggested_limit_price":0,
		"suggested_size_shares":0,
		"suggested_notional":0,
		"reason_codes":["confidence_guard","edge_below_threshold"]
	}`)
	if err != nil {
		t.Fatalf("expected decode success without invalidation_conditions, got: %v", err)
	}
	if len(raw.InvalidationConditions) != 0 {
		t.Fatalf("expected empty invalidation conditions when omitted, got %d", len(raw.InvalidationConditions))
	}
}

func TestEnsurePacketInvalidationConditionsFillsMissing(t *testing.T) {
	t.Parallel()

	market := UpDownMarket{
		Asset:            "BTC",
		WindowType:       Window5m,
		TimeToEndSeconds: 120,
	}
	packet := LLMTradePacket{
		Decision:               "NO_TRADE",
		RecommendedSide:        "NONE",
		InvalidationConditions: []string{},
	}
	ensurePacketInvalidationConditions(&packet, market)
	if len(packet.InvalidationConditions) == 0 {
		t.Fatalf("expected default invalidation conditions to be filled")
	}
}

func TestEnsurePacketInvalidationConditionsSupplementsTradeSingleCondition(t *testing.T) {
	t.Parallel()

	market := UpDownMarket{
		Asset:            "BTC",
		WindowType:       Window5m,
		TimeToEndSeconds: 120,
	}
	packet := LLMTradePacket{
		Decision:               "BUY_UP",
		RecommendedSide:        "UP",
		InvalidationConditions: []string{"Cancel if edge disappears"},
	}
	ensurePacketInvalidationConditions(&packet, market)
	if len(packet.InvalidationConditions) < 2 {
		t.Fatalf("expected trade packet invalidation conditions to be supplemented to at least 2")
	}
}

func TestValidateLLMResponseRawRejectsTradeWithInvalidPrice(t *testing.T) {
	t.Parallel()

	err := validateLLMResponseRaw(llmResponseRaw{
		Decision:               "BUY_UP",
		RecommendedSide:        "UP",
		Confidence:             0.82,
		ExpectedValue:          0.01,
		SuggestedLimitPrice:    1.25,
		SuggestedSizeShares:    10,
		SuggestedNotional:      7,
		ReasonCodes:            []string{"edge_up_positive", "microstructure_support_up", "allora_proxy_fresh"},
		InvalidationConditions: []string{"Cancel if spread widens.", "Cancel if edge decays."},
	})
	if err == nil {
		t.Fatalf("expected validation failure for out-of-range suggested_limit_price")
	}
}

func TestEvaluateSnapshotStabilityMixedVotesUnstable(t *testing.T) {
	t.Parallel()

	out := evaluateSnapshotStability([]llmIndependentSnapshot{
		{
			ConsensusUp:       0.61,
			ExecutableAskUp:   0.52,
			ExecutableAskDown: 0.48,
			EVUp:              0.02,
			EVDown:            0.001,
			EVMinThreshold:    0.01,
		},
		{
			ConsensusUp:       0.44,
			ExecutableAskUp:   0.50,
			ExecutableAskDown: 0.50,
			EVUp:              0.001,
			EVDown:            0.02,
			EVMinThreshold:    0.01,
		},
	})
	if out.Stable {
		t.Fatalf("expected mixed directional votes to be unstable")
	}
	if out.DirectionSummary != "MIXED" {
		t.Fatalf("expected MIXED direction summary, got %s", out.DirectionSummary)
	}
}

func TestSnapshotStabilityHardBlockMixedLowDriftAllowsLLM(t *testing.T) {
	t.Parallel()

	stability := LLMSnapshotStability{
		SampleCount:    2,
		Stable:         false,
		UpVotes:        1,
		DownVotes:      1,
		ConsensusDrift: 0.02,
		AskUpDrift:     0.01,
		AskDownDrift:   0.012,
		BestEVDrift:    0.008,
	}
	if snapshotStabilityHardBlock(stability) {
		t.Fatalf("expected mixed low-drift stability to avoid hard block")
	}
}

func TestSnapshotStabilityHardBlockNoDirectionalVotesDoesNotHardBlock(t *testing.T) {
	t.Parallel()

	stability := LLMSnapshotStability{
		SampleCount:    2,
		Stable:         false,
		UpVotes:        0,
		DownVotes:      0,
		NoTradeVotes:   2,
		ConsensusDrift: 0.01,
		AskUpDrift:     0.008,
		AskDownDrift:   0.007,
		BestEVDrift:    0.004,
	}
	if snapshotStabilityHardBlock(stability) {
		t.Fatalf("expected no-direction snapshot set to avoid hard block")
	}
}

func TestSnapshotStabilityHardBlockTriggersOnHighDrift(t *testing.T) {
	t.Parallel()

	stability := LLMSnapshotStability{
		SampleCount:    2,
		Stable:         false,
		UpVotes:        1,
		DownVotes:      1,
		ConsensusDrift: 0.09,
		AskUpDrift:     0.02,
		AskDownDrift:   0.02,
		BestEVDrift:    0.02,
	}
	if !snapshotStabilityHardBlock(stability) {
		t.Fatalf("expected high-drift mixed stability to hard block")
	}
}

func TestSnapshotStabilityHardBlockMixedTwoSnapshotsHighEVDriftDoesNotHardBlock(t *testing.T) {
	t.Parallel()

	stability := LLMSnapshotStability{
		SampleCount:    2,
		Stable:         false,
		UpVotes:        1,
		DownVotes:      1,
		ConsensusDrift: 0.02,
		AskUpDrift:     0.01,
		AskDownDrift:   0.01,
		BestEVDrift:    0.03,
	}
	if snapshotStabilityHardBlock(stability) {
		t.Fatalf("expected two-snapshot mixed stability to avoid hard block and rely on soft instability handling")
	}
}

func TestSnapshotStabilityHardBlockReasonsNoSampleIncludesSingleSnapshot(t *testing.T) {
	t.Parallel()

	stability := LLMSnapshotStability{
		SampleCount: 0,
	}
	reasons := snapshotStabilityHardBlockReasons(stability)
	if !containsString(reasons, "single_snapshot_risk_reduced") {
		t.Fatalf("expected single snapshot reason for empty sample set")
	}
	if !containsString(reasons, "snapshot_instability_hard_block") {
		t.Fatalf("expected hard block reason to be present")
	}
}

func TestSnapshotStabilityHardBlockReasonsMixedIncludesDisagreementOnlyWhenMixed(t *testing.T) {
	t.Parallel()

	stability := LLMSnapshotStability{
		SampleCount: 2,
		UpVotes:     1,
		DownVotes:   1,
	}
	reasons := snapshotStabilityHardBlockReasons(stability)
	if !containsString(reasons, "multi_snapshot_disagreement") {
		t.Fatalf("expected mixed disagreement reason")
	}
	if containsString(reasons, "single_snapshot_risk_reduced") {
		t.Fatalf("did not expect single snapshot reason for multi-sample stability")
	}
}

func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}
