package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/integrations/allora"
	"github.com/bankai-project/backend/internal/integrations/openai"
	"github.com/bankai-project/backend/internal/integrations/synthdata"
	"github.com/bankai-project/backend/internal/models"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	upDownLLMCachePrefix          = "updown:llm:packet:"
	upDownLLMLatestPref           = "updown:llm:latest:"
	upDownLLMSnapshotCountDefault = 2
	upDownLLMSnapshotSpacing5m    = 250 * time.Millisecond
	upDownLLMSnapshotSpacing15m   = 450 * time.Millisecond
	upDownLLMSnapshotDriftSoftMax = 0.035
	upDownLLMSnapshotDriftHardMax = 0.085
	upDownLLMSnapshotEVHardMin    = 0.015
	upDownLLMCalibrationTTL       = 5 * time.Minute
	upDownLLMAlloraLastGoodMaxAge = 12 * time.Minute
)

var (
	ErrUpDownLLMDisabled  = errors.New("updown llm service disabled")
	ErrUpDownLLMNotFound  = errors.New("updown llm packet not found")
	llmResponseSchemaKeys = map[string]struct{}{
		"decision":                {},
		"recommended_side":        {},
		"confidence":              {},
		"expected_value":          {},
		"suggested_limit_price":   {},
		"suggested_size_shares":   {},
		"suggested_notional":      {},
		"reason_codes":            {},
		"invalidation_conditions": {},
	}
	llmResponseRequiredKeys = map[string]struct{}{
		"decision":              {},
		"recommended_side":      {},
		"confidence":            {},
		"expected_value":        {},
		"suggested_limit_price": {},
		"suggested_size_shares": {},
		"suggested_notional":    {},
		"reason_codes":          {},
	}
)

type UpDownLLMGenerateRequest struct {
	Slug         string `json:"slug"`
	ForceRefresh bool   `json:"force_refresh"`
}

type LLMContextFreshness struct {
	SynthAgeSeconds  int64  `json:"synth_age_seconds"`
	AlloraAgeSeconds int64  `json:"allora_age_seconds"`
	MarketAgeSeconds int64  `json:"market_age_seconds"`
	ContextHash      string `json:"context_hash"`
}

type LLMTraceMeta struct {
	PromptHash       string    `json:"prompt_hash"`
	Model            string    `json:"model"`
	LatencyMs        int64     `json:"latency_ms"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	GeneratedAt      time.Time `json:"generated_at"`
	CacheHit         bool      `json:"cache_hit"`
	SnapshotCount    int       `json:"snapshot_count"`
	SnapshotStable   bool      `json:"snapshot_stable"`
}

type AlloraProxyMeta struct {
	RawP5       float64 `json:"raw_p5"`
	SmoothedP5  float64 `json:"smoothed_p5"`
	ProxyP15    float64 `json:"proxy_p15"`
	AgeSeconds  int64   `json:"age_seconds"`
	ProxyStatus string  `json:"proxy_status"` // fresh | stale_soft | stale_hard
	UsedCached  bool    `json:"used_cached"`
}

type LLMEntryMeta struct {
	ReadyToBet              bool     `json:"ready_to_bet"`
	EntryScore              float64  `json:"entry_score"`
	ConfidenceLB90          float64  `json:"confidence_lb90"`
	ConfidenceLB95          float64  `json:"confidence_lb95"`
	ProbabilityChosenSide   float64  `json:"probability_chosen_side"`
	EdgeChosenSide          float64  `json:"edge_chosen_side"`
	SharpeChosenSide        float64  `json:"sharpe_chosen_side"`
	SharpeUp                float64  `json:"sharpe_up"`
	SharpeDown              float64  `json:"sharpe_down"`
	DeviationRatio          float64  `json:"deviation_ratio"`
	DeviationZScore         float64  `json:"deviation_zscore"`
	ConfidenceThresholdUsed float64  `json:"confidence_threshold_used"`
	CalibrationSamples      int64    `json:"calibration_samples"`
	CalibrationLLMBrier     float64  `json:"calibration_llm_brier"`
	CalibrationDetBrier     float64  `json:"calibration_det_brier"`
	CalibrationConfidence   float64  `json:"calibration_confidence_scale"`
	CalibrationEdgeBuffer   float64  `json:"calibration_edge_buffer"`
	GateReasons             []string `json:"gate_reasons"`
}

type LLMSnapshotStability struct {
	SampleCount      int     `json:"sample_count"`
	Stable           bool    `json:"stable"`
	UpVotes          int     `json:"up_votes"`
	DownVotes        int     `json:"down_votes"`
	NoTradeVotes     int     `json:"no_trade_votes"`
	ConsensusDrift   float64 `json:"consensus_drift"`
	AskUpDrift       float64 `json:"ask_up_drift"`
	AskDownDrift     float64 `json:"ask_down_drift"`
	BestEVDrift      float64 `json:"best_ev_drift"`
	DirectionSummary string  `json:"direction_summary"`
}

type LLMTradePacket struct {
	Slug                   string               `json:"slug"`
	ConditionID            string               `json:"condition_id"`
	Asset                  string               `json:"asset"`
	WindowType             UpDownWindowType     `json:"window_type"`
	Decision               string               `json:"decision"` // BUY_UP | BUY_DOWN | NO_TRADE
	RecommendedSide        string               `json:"recommended_side"`
	Confidence             float64              `json:"confidence"`
	ExpectedValue          float64              `json:"expected_value"`
	SuggestedLimitPrice    float64              `json:"suggested_limit_price"`
	SuggestedSizeShares    float64              `json:"suggested_size_shares"`
	SuggestedNotional      float64              `json:"suggested_notional"`
	ReasonCodes            []string             `json:"reason_codes"`
	InvalidationConditions []string             `json:"invalidation_conditions"`
	EffectiveGuardBlocks   []string             `json:"effective_guard_blocks"`
	RiskFlags              UpDownRiskFlags      `json:"risk_flags"`
	Freshness              LLMContextFreshness  `json:"freshness"`
	Trace                  LLMTraceMeta         `json:"trace"`
	AlloraProxy            AlloraProxyMeta      `json:"allora_proxy"`
	SnapshotStability      LLMSnapshotStability `json:"snapshot_stability"`
	Entry                  LLMEntryMeta         `json:"entry"`
	GeneratedAt            time.Time            `json:"generated_at"`
}

type UpDownLLMHealth struct {
	Enabled           bool       `json:"enabled"`
	ShadowMode        bool       `json:"shadow_mode"`
	OpenAIConfigured  bool       `json:"openai_configured"`
	SynthConfigured   bool       `json:"synth_configured"`
	AlloraConfigured  bool       `json:"allora_configured"`
	CacheTTLSeconds   int        `json:"cache_ttl_seconds"`
	TimeoutSeconds    int        `json:"timeout_seconds"`
	MaxTokens         int        `json:"max_tokens"`
	ExecutionPolicy   string     `json:"execution_policy"`
	LastAlloraFetchAt *time.Time `json:"last_allora_fetch_at,omitempty"`
	LastAlloraError   string     `json:"last_allora_error,omitempty"`
}

type llmResponseRaw struct {
	Decision               string   `json:"decision"`
	RecommendedSide        string   `json:"recommended_side"`
	Confidence             float64  `json:"confidence"`
	ExpectedValue          float64  `json:"expected_value"`
	SuggestedLimitPrice    float64  `json:"suggested_limit_price"`
	SuggestedSizeShares    float64  `json:"suggested_size_shares"`
	SuggestedNotional      float64  `json:"suggested_notional"`
	ReasonCodes            []string `json:"reason_codes"`
	InvalidationConditions []string `json:"invalidation_conditions"`
}

type llmOrderBookLevel struct {
	Price           float64 `json:"price"`
	Available       float64 `json:"available"`
	Used            float64 `json:"used"`
	CumulativeSize  float64 `json:"cumulative_size"`
	CumulativeValue float64 `json:"cumulative_value"`
}

type llmDepthEstimate struct {
	RequestedSize         float64             `json:"requested_size"`
	FillableSize          float64             `json:"fillable_size"`
	EstimatedAveragePrice float64             `json:"estimated_average_price"`
	EstimatedTotalValue   float64             `json:"estimated_total_value"`
	InsufficientLiquidity bool                `json:"insufficient_liquidity"`
	Levels                []llmOrderBookLevel `json:"levels"`
}

type llmRetrievalEvidence struct {
	Source          string   `json:"source"`
	Status          string   `json:"status"`
	AgeSeconds      int64    `json:"age_seconds"`
	Reliability     float64  `json:"reliability"`
	Coverage        float64  `json:"coverage"`
	RetrievalScore  float64  `json:"retrieval_score"`
	FreshnessWeight float64  `json:"freshness_weight"`
	Notes           []string `json:"notes,omitempty"`
}

type llmRetrievalBundle struct {
	StrategyVersion  string                 `json:"strategy_version"`
	RankingPolicy    string                 `json:"ranking_policy"`
	CorrectiveAction string                 `json:"corrective_action"`
	QualityScore     float64                `json:"quality_score"`
	Evidence         []llmRetrievalEvidence `json:"evidence"`
}

type llmDeterministicFormulaMeta struct {
	FormulaVersion      string   `json:"formula_version"`
	BlendModel          string   `json:"blend_model"`
	EVFormula           string   `json:"ev_formula"`
	KellyFormula        string   `json:"kelly_formula"`
	SharpeFormula       string   `json:"sharpe_formula"`
	BlendWeightMarket   float64  `json:"blend_weight_market"`
	BlendWeightSynth    float64  `json:"blend_weight_synth"`
	BlendWeightModel    float64  `json:"blend_weight_model"`
	BlendWeightLP       float64  `json:"blend_weight_lp"`
	PFinalUp            float64  `json:"p_final_up"`
	ConfidenceRaw       float64  `json:"confidence_raw"`
	ConfidenceAdj       float64  `json:"confidence_adj"`
	EdgeUp              float64  `json:"edge_up"`
	EdgeDown            float64  `json:"edge_down"`
	EVUp                float64  `json:"ev_up"`
	EVDown              float64  `json:"ev_down"`
	EVMinThreshold      float64  `json:"ev_min_threshold"`
	KellyRawUp          float64  `json:"kelly_raw_up"`
	KellyRawDown        float64  `json:"kelly_raw_down"`
	KellyCappedUp       float64  `json:"kelly_capped_up"`
	KellyCappedDown     float64  `json:"kelly_capped_down"`
	SharpeUp            float64  `json:"sharpe_up"`
	SharpeDown          float64  `json:"sharpe_down"`
	BaselineDecision    string   `json:"baseline_decision"`
	BaselineSide        string   `json:"baseline_side"`
	BaselineReasonCodes []string `json:"baseline_reason_codes"`
}

type llmIndependentSnapshot struct {
	Timestamp        time.Time
	MarketAgeSeconds int64
	SynthAgeSeconds  int64
	RiskFlags        UpDownRiskFlags

	ReferenceStartPrice   *float64
	ReferenceCurrentPrice *float64
	ReferenceEndPrice     *float64
	ReferenceUpdatedAt    *time.Time
	ReferenceSource       string

	PMarketUp *float64
	PSynthUp  *float64
	PModelUp  *float64
	PLPUp     *float64

	EVUp           float64
	EVDown         float64
	EVMinThreshold float64
	FeesBps        float64
	Confidence     float64
	ConsensusUp    float64
	Disagreement   float64
	Regime         string

	ExecutableAskUp    float64
	ExecutableAskDown  float64
	ExecutableBidUp    float64
	ExecutableBidDown  float64
	ExecutableLastUp   float64
	ExecutableLastDown float64
	SpreadUp           float64
	SpreadDown         float64
	ExpectedSlippage   float64
	DepthImbalance     float64

	UpBuyDepth    llmDepthEstimate
	DownBuyDepth  llmDepthEstimate
	UpSellDepth   llmDepthEstimate
	DownSellDepth llmDepthEstimate

	SynthDirectProbability *float64
	SynthPercentileProxy   *float64
	ModelDiagnosticCode    string
	ModelDiagnosticDetail  string
	SynthClockUnix         int64
	SynthWindowProxy       bool

	VolatilityAverageForecast float64
	VolatilityAveragePast     float64
	VolatilityHeadForecast    []float64
	VolatilityHeadPast        []float64

	ReasonCodes []string
	Retrieval   llmRetrievalBundle

	DeviationRatio  float64
	DeviationZScore float64
}

type llmStructuredContext struct {
	Version string `json:"version"`
	Query   struct {
		GeneratedUnix int64 `json:"generated_unix"`
	} `json:"query"`
	DataSemantics struct {
		Synth struct {
			Role              string `json:"role"`
			FreshMaxSeconds   int64  `json:"fresh_max_seconds"`
			StaleSoftSeconds  int64  `json:"stale_soft_seconds"`
			WindowNote        string `json:"window_note"`
			VolatilityNote    string `json:"volatility_note"`
			ProbabilityNote   string `json:"probability_note"`
			ReliabilityPolicy string `json:"reliability_policy"`
		} `json:"synth"`
		Allora struct {
			Role               string `json:"role"`
			SourceTimeframe    string `json:"source_timeframe"`
			FreshMaxSeconds5m  int64  `json:"fresh_max_seconds_5m"`
			SoftLagSeconds15m  int64  `json:"soft_lag_seconds_15m"`
			HardLagSeconds15m  int64  `json:"hard_lag_seconds_15m"`
			ProxyFormula       string `json:"proxy_formula"`
			StatusPolicy       string `json:"status_policy"`
			ReliabilityPolicy  string `json:"reliability_policy"`
			DecisionBlockRules string `json:"decision_block_rules"`
		} `json:"allora"`
		Deterministic struct {
			Role           string `json:"role"`
			FormulaVersion string `json:"formula_version"`
			Description    string `json:"description"`
		} `json:"deterministic"`
		Retrieval struct {
			Role           string `json:"role"`
			RankingPolicy  string `json:"ranking_policy"`
			CorrectiveNote string `json:"corrective_note"`
		} `json:"retrieval"`
	} `json:"data_semantics"`
	Market struct {
		Slug                 string           `json:"slug"`
		ConditionID          string           `json:"condition_id"`
		Asset                string           `json:"asset"`
		WindowType           UpDownWindowType `json:"window_type"`
		ResolutionSourceType string           `json:"resolution_source_type"`
		EventStartUnix       int64            `json:"event_start_unix"`
		EventEndUnix         int64            `json:"event_end_unix"`
		TimeToEndSeconds     int64            `json:"time_to_end_seconds"`
		IsActiveWindow       bool             `json:"is_active_window"`
		Liquidity            float64          `json:"liquidity"`
		Volume24h            float64          `json:"volume_24h"`
	} `json:"market"`
	Reference struct {
		StartPrice   *float64 `json:"start_price,omitempty"`
		CurrentPrice *float64 `json:"current_price,omitempty"`
		EndPrice     *float64 `json:"end_price,omitempty"`
		UpdatedUnix  int64    `json:"updated_unix"`
		Source       string   `json:"source"`
	} `json:"reference"`
	Microstructure struct {
		ExecutableAskUp    float64          `json:"executable_ask_up"`
		ExecutableAskDown  float64          `json:"executable_ask_down"`
		ExecutableBidUp    float64          `json:"executable_bid_up"`
		ExecutableBidDown  float64          `json:"executable_bid_down"`
		ExecutableLastUp   float64          `json:"executable_last_up"`
		ExecutableLastDown float64          `json:"executable_last_down"`
		SpreadUp           float64          `json:"spread_up"`
		SpreadDown         float64          `json:"spread_down"`
		ExpectedSlippage   float64          `json:"expected_slippage"`
		DepthImbalance     float64          `json:"depth_imbalance"`
		UpBuyDepth         llmDepthEstimate `json:"up_buy_depth"`
		DownBuyDepth       llmDepthEstimate `json:"down_buy_depth"`
		UpSellDepth        llmDepthEstimate `json:"up_sell_depth"`
		DownSellDepth      llmDepthEstimate `json:"down_sell_depth"`
	} `json:"microstructure"`
	Synth struct {
		PMarketUp                 *float64  `json:"p_market_up,omitempty"`
		PSynthUp                  *float64  `json:"p_synth_up,omitempty"`
		PModelUp                  *float64  `json:"p_model_up,omitempty"`
		PLPUp                     *float64  `json:"p_lp_up,omitempty"`
		SynthDirectProbability    *float64  `json:"synth_direct_probability,omitempty"`
		SynthPercentileProxy      *float64  `json:"synth_percentile_proxy,omitempty"`
		ConsensusUp               float64   `json:"consensus_up"`
		Disagreement              float64   `json:"disagreement"`
		EVUp                      float64   `json:"ev_up"`
		EVDown                    float64   `json:"ev_down"`
		EVMinThreshold            float64   `json:"ev_min_threshold"`
		Confidence                float64   `json:"confidence"`
		TimestampUnix             int64     `json:"timestamp_unix"`
		Regime                    string    `json:"regime"`
		SynthClockUnix            int64     `json:"synth_clock_unix"`
		SynthWindowProxy          bool      `json:"synth_window_proxy"`
		ModelDiagnosticCode       string    `json:"model_diagnostic_code"`
		ModelDiagnosticDetail     string    `json:"model_diagnostic_detail"`
		VolatilityAverageForecast float64   `json:"volatility_average_forecast"`
		VolatilityAveragePast     float64   `json:"volatility_average_past"`
		VolatilityHeadForecast    []float64 `json:"volatility_head_forecast"`
		VolatilityHeadPast        []float64 `json:"volatility_head_past"`
		DeviationRatio            float64   `json:"deviation_ratio"`
		DeviationZScore           float64   `json:"deviation_zscore"`
	} `json:"synth"`
	Guards struct {
		RiskFlags   UpDownRiskFlags `json:"risk_flags"`
		ReasonCodes []string        `json:"reason_codes"`
	} `json:"guards"`
	Allora struct {
		Asset                         string    `json:"asset"`
		Timeframe                     string    `json:"timeframe"`
		RawP5                         float64   `json:"raw_p5"`
		SmoothedP5                    float64   `json:"smoothed_p5"`
		ProxyP15                      float64   `json:"proxy_p15"`
		AgeSeconds                    int64     `json:"age_seconds"`
		Status                        string    `json:"status"`
		UsedCached                    bool      `json:"used_cached"`
		TopicID                       string    `json:"topic_id"`
		Timestamp                     int64     `json:"timestamp_unix"`
		NetworkInference              *float64  `json:"network_inference,omitempty"`
		ConfidenceIntervalPercentiles []float64 `json:"confidence_interval_percentiles,omitempty"`
		ConfidenceIntervalValues      []float64 `json:"confidence_interval_values,omitempty"`
	} `json:"allora"`
	SnapshotStability LLMSnapshotStability        `json:"snapshot_stability"`
	Deterministic     llmDeterministicFormulaMeta `json:"deterministic_baseline"`
	Retrieval         llmRetrievalBundle          `json:"retrieval"`
}

type percentileValuePair struct {
	pct float64
	val float64
}

type alloraInferenceCacheValue struct {
	Inference allora.PriceInference
	FetchedAt time.Time
}

type alloraProbabilityCacheValue struct {
	Value     float64
	Timestamp time.Time
}

type llmClosedLoopCalibration struct {
	Samples            int64
	LLMBrier           float64
	DeterministicBrier float64
	EVDelta            float64
	ConfidenceScale    float64
	EdgeBuffer         float64
	UpdatedAt          time.Time
}

type UpDownLLMService struct {
	db     *gorm.DB
	redis  *redis.Client
	cfg    *config.Config
	updown *UpDownService
	openai *openai.Client
	synth  *synthdata.Client
	allora *allora.Client

	metaMu            sync.RWMutex
	lastAlloraFetchAt *time.Time
	lastAlloraError   string

	alloraCacheMu        sync.RWMutex
	lastGoodAlloraByKey  map[string]alloraInferenceCacheValue
	lastAlloraP5ByKey    map[string]alloraProbabilityCacheValue
	calibrationMu        sync.RWMutex
	calibrationByKey     map[string]llmClosedLoopCalibration
	calibrationFetchedAt map[string]time.Time
}

func NewUpDownLLMService(
	db *gorm.DB,
	rdb *redis.Client,
	cfg *config.Config,
	updown *UpDownService,
	openaiClient *openai.Client,
	synthClient *synthdata.Client,
	alloraClient *allora.Client,
) *UpDownLLMService {
	return &UpDownLLMService{
		db:                   db,
		redis:                rdb,
		cfg:                  cfg,
		updown:               updown,
		openai:               openaiClient,
		synth:                synthClient,
		allora:               alloraClient,
		lastGoodAlloraByKey:  make(map[string]alloraInferenceCacheValue),
		lastAlloraP5ByKey:    make(map[string]alloraProbabilityCacheValue),
		calibrationByKey:     make(map[string]llmClosedLoopCalibration),
		calibrationFetchedAt: make(map[string]time.Time),
	}
}

func (s *UpDownLLMService) Enabled() bool {
	return s != nil &&
		s.cfg != nil &&
		s.cfg.Services.UpDownEnabled &&
		s.cfg.Services.UpDownLLMEnabled &&
		s.updown != nil &&
		s.updown.Enabled() &&
		s.openai != nil &&
		strings.TrimSpace(s.cfg.Services.OpenAIAPIKey) != "" &&
		s.allora != nil &&
		s.allora.Enabled()
}

func (s *UpDownLLMService) Health() *UpDownLLMHealth {
	if s == nil || s.cfg == nil {
		return &UpDownLLMHealth{}
	}
	s.metaMu.RLock()
	lastFetch := s.lastAlloraFetchAt
	lastErr := s.lastAlloraError
	s.metaMu.RUnlock()
	return &UpDownLLMHealth{
		Enabled:           s.Enabled(),
		ShadowMode:        s.cfg.Services.UpDownLLMShadowMode,
		OpenAIConfigured:  s.openai != nil && strings.TrimSpace(s.cfg.Services.OpenAIAPIKey) != "",
		SynthConfigured:   s.synth != nil && s.synth.Enabled(),
		AlloraConfigured:  s.allora != nil && s.allora.Enabled(),
		CacheTTLSeconds:   maxInt(s.cfg.Services.UpDownLLMCacheTTLSeconds, 30),
		TimeoutSeconds:    maxInt(s.cfg.Services.UpDownLLMTimeoutSeconds, 8),
		MaxTokens:         maxInt(s.cfg.Services.UpDownLLMMaxTokens, 20000),
		ExecutionPolicy:   upDownExecutionSourcePolicy(s.cfg),
		LastAlloraFetchAt: lastFetch,
		LastAlloraError:   lastErr,
	}
}

func (s *UpDownLLMService) Generate(ctx context.Context, req UpDownLLMGenerateRequest) (*LLMTradePacket, error) {
	if !s.Enabled() {
		return nil, ErrUpDownLLMDisabled
	}
	req.Slug = strings.TrimSpace(req.Slug)
	if req.Slug == "" {
		return nil, fmt.Errorf("slug is required")
	}

	market, err := s.updown.GetMarket(ctx, req.Slug)
	if err != nil {
		return nil, err
	}
	if market == nil {
		return nil, fmt.Errorf("market not found")
	}
	if !strings.EqualFold(market.Asset, "BTC") && !strings.EqualFold(market.Asset, "ETH") {
		return nil, fmt.Errorf("asset %s is unsupported for llm engine", market.Asset)
	}
	if market.WindowType != Window5m && market.WindowType != Window15m {
		return nil, fmt.Errorf("window %s is unsupported for llm engine", market.WindowType)
	}

	latestKey := s.latestPacketCacheKey(*market)
	if !req.ForceRefresh {
		if latest := s.readPacketCache(ctx, latestKey); latest != nil {
			latest.Trace.CacheHit = true
			return latest, nil
		}
	}

	snapshots, stability, err := s.collectSnapshotSeries(ctx, market)
	if err != nil {
		return nil, err
	}
	snapshot := mergeSnapshotSeries(snapshots)
	if snapshot == nil {
		return nil, fmt.Errorf("unable to build market snapshot")
	}
	if snapshot.ReferenceStartPrice == nil || *snapshot.ReferenceStartPrice <= 0 {
		return nil, fmt.Errorf("reference start price missing for allora probability map")
	}

	alloraInference, usedCachedAllora, err := s.fetchAlloraInferenceWithSmoothing(ctx, market.Asset, allora.Timeframe5m)
	s.recordAlloraFetch(err)
	if err != nil {
		return nil, fmt.Errorf("allora inference unavailable: %w", err)
	}
	rawP5 := probabilityUpFromInference(alloraInference, *snapshot.ReferenceStartPrice)
	smoothedP5 := s.smoothAlloraProbability(market.Asset, market.WindowType, rawP5, alloraInference.Timestamp)
	now := time.Now().UTC()
	alloraAge := int64(math.Round(now.Sub(alloraInference.Timestamp).Seconds()))
	alloraMeta := s.proxyAlloraProbability(market, smoothedP5, alloraAge, now)
	alloraMeta.RawP5 = upDownClamp(rawP5, 0.01, 0.99)
	alloraMeta.SmoothedP5 = upDownClamp(smoothedP5, 0.01, 0.99)
	alloraMeta.UsedCached = usedCachedAllora

	snapshot.Retrieval = buildLLMRetrievalBundle(now, market.WindowType, *snapshot, alloraMeta)
	contextData, contextJSON, contextHash := s.buildLLMContext(*market, *snapshot, alloraInference, alloraMeta, stability)
	cacheKey := s.packetCacheKey(*market, contextHash)

	if !req.ForceRefresh {
		if cached := s.readPacketCache(ctx, cacheKey); cached != nil {
			cached.Trace.CacheHit = true
			return cached, nil
		}
	}
	calibration := s.getClosedLoopCalibration(ctx, *market)

	systemPrompt := upDownLLMSystemPrompt()
	userPrompt := "RAG bundle follows. Use only this canonical context JSON and return valid JSON only:\n" + contextJSON
	promptHash := hashHex(systemPrompt + "\n" + userPrompt)

	timeout := maxInt(s.cfg.Services.UpDownLLMTimeoutSeconds, 8)
	llmCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	start := time.Now().UTC()
	analysis, err := s.openai.AnalyzeWithOptions(llmCtx, systemPrompt, userPrompt, openai.AnalyzeOptions{
		Temperature:      float64Ptr(0.0),
		TopP:             float64Ptr(1.0),
		FrequencyPenalty: float64Ptr(0),
		PresencePenalty:  float64Ptr(0),
		MaxTokens:        maxInt(s.cfg.Services.UpDownLLMMaxTokens, 20000),
		ResponseFormat: &openai.ResponseFormat{
			Type: "json_object",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("llm generation failed: %w", err)
	}
	latencyMs := time.Since(start).Milliseconds()

	raw, err := decodeStrictLLMResponse(analysis.Content)
	if err != nil {
		return nil, err
	}
	if err := validateLLMResponseRaw(raw); err != nil {
		return nil, err
	}

	packet := normalizeLLMResponse(raw)
	packet.Slug = market.Slug
	packet.ConditionID = market.ConditionID
	packet.Asset = market.Asset
	packet.WindowType = market.WindowType
	packet.RiskFlags = snapshot.RiskFlags
	packet.AlloraProxy = alloraMeta
	packet.SnapshotStability = stability
	packet.Freshness = LLMContextFreshness{
		SynthAgeSeconds:  snapshot.SynthAgeSeconds,
		AlloraAgeSeconds: alloraAge,
		MarketAgeSeconds: snapshot.MarketAgeSeconds,
		ContextHash:      contextHash,
	}
	packet.Trace = LLMTraceMeta{
		PromptHash:       promptHash,
		Model:            s.openai.Model(),
		LatencyMs:        latencyMs,
		PromptTokens:     analysis.Usage.PromptTokens,
		CompletionTokens: analysis.Usage.CompletionTokens,
		TotalTokens:      analysis.Usage.TotalTokens,
		GeneratedAt:      now,
		CacheHit:         false,
		SnapshotCount:    stability.SampleCount,
		SnapshotStable:   stability.Stable,
	}
	packet.GeneratedAt = now

	guardBlocks := computeEffectiveGuardBlocks(*market, snapshot.RiskFlags, alloraMeta, packet.Freshness, s.cfg, snapshot.Retrieval, stability)
	if len(guardBlocks) > 0 {
		packet.Decision = "NO_TRADE"
		packet.RecommendedSide = "NONE"
		packet.SuggestedSizeShares = 0
		packet.SuggestedNotional = 0
		packet.EffectiveGuardBlocks = guardBlocks
		packet.ReasonCodes = dedupeStrings(append(packet.ReasonCodes, guardBlocks...))
	}
	if alloraMeta.ProxyStatus == "stale_soft" {
		packet.Confidence = upDownClamp(packet.Confidence*0.86, 0, 1)
		packet.ReasonCodes = dedupeStrings(append(packet.ReasonCodes, "allora_proxy_stale_soft"))
	}
	if snapshot.Retrieval.CorrectiveAction == "degrade_confidence" {
		packet.Confidence = upDownClamp(packet.Confidence*0.9, 0, 1)
		packet.ReasonCodes = dedupeStrings(append(packet.ReasonCodes, "retrieval_quality_degraded"))
	}
	if snapshot.Retrieval.CorrectiveAction == "force_no_trade" {
		packet.Decision = "NO_TRADE"
		packet.RecommendedSide = "NONE"
		packet.SuggestedSizeShares = 0
		packet.SuggestedNotional = 0
		packet.EffectiveGuardBlocks = dedupeStrings(append(packet.EffectiveGuardBlocks, "retrieval_quality_low"))
		packet.ReasonCodes = dedupeStrings(append(packet.ReasonCodes, "retrieval_quality_low"))
	}
	if !stability.Stable {
		packet.Confidence = upDownClamp(packet.Confidence*0.92, 0, 1)
		packet.ReasonCodes = dedupeStrings(append(packet.ReasonCodes, "snapshot_instability_soft"))
	}
	packet.Confidence = upDownClamp(packet.Confidence, 0, 1)

	ensurePacketInvalidationConditions(&packet, *market)
	if packet.ExpectedValue == 0 {
		if packet.RecommendedSide == "DOWN" {
			packet.ExpectedValue = snapshot.EVDown
		} else {
			packet.ExpectedValue = snapshot.EVUp
		}
	}
	if packet.SuggestedLimitPrice == 0 {
		if packet.RecommendedSide == "DOWN" {
			packet.SuggestedLimitPrice = snapshot.ExecutableAskDown
		} else if packet.RecommendedSide == "UP" {
			packet.SuggestedLimitPrice = snapshot.ExecutableAskUp
		}
	}
	if packet.SuggestedSizeShares == 0 && packet.Decision != "NO_TRADE" && packet.SuggestedLimitPrice > 0 {
		targetNotional := math.Max(1, s.cfg.Services.UpDownNotionalBankroll*0.0025*upDownClamp(packet.Confidence, 0.2, 1.0))
		packet.SuggestedSizeShares = targetNotional / packet.SuggestedLimitPrice
	}
	if packet.SuggestedNotional <= 0 && packet.SuggestedLimitPrice > 0 && packet.SuggestedSizeShares > 0 {
		packet.SuggestedNotional = packet.SuggestedLimitPrice * packet.SuggestedSizeShares
	}

	if packet.SuggestedLimitPrice > 0 {
		packet.SuggestedLimitPrice = upDownClamp(packet.SuggestedLimitPrice, 0.01, 0.99)
	}
	if packet.SuggestedSizeShares < 0 {
		packet.SuggestedSizeShares = 0
	}
	if packet.SuggestedNotional < 0 {
		packet.SuggestedNotional = 0
	}
	packet.Entry = computeEntryMeta(*market, *snapshot, packet, alloraMeta, calibration)

	s.writePacketCache(ctx, cacheKey, latestKey, packet)
	_ = s.persistPacket(ctx, *market, contextData, packet)
	return &packet, nil
}

func (s *UpDownLLMService) GetPacket(ctx context.Context, slug string) (*LLMTradePacket, error) {
	if !s.Enabled() {
		return nil, ErrUpDownLLMDisabled
	}
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil, fmt.Errorf("slug is required")
	}
	market, err := s.updown.GetMarket(ctx, slug)
	if err != nil {
		return nil, err
	}
	if market == nil {
		return nil, ErrUpDownLLMNotFound
	}
	latestKey := s.latestPacketCacheKey(*market)
	if cached := s.readPacketCache(ctx, latestKey); cached != nil {
		cached.Trace.CacheHit = true
		return cached, nil
	}
	packet, err := s.readLatestPersistedPacket(ctx, slug)
	if err != nil {
		return nil, err
	}
	if packet == nil {
		return nil, ErrUpDownLLMNotFound
	}
	return packet, nil
}

func (s *UpDownLLMService) collectIndependentSnapshot(ctx context.Context, market *UpDownMarket) (*llmIndependentSnapshot, error) {
	if market == nil {
		return nil, fmt.Errorf("market is required")
	}
	now := time.Now().UTC()
	flags := UpDownRiskFlags{
		ReadOnly:   s.cfg.Services.UpDownReadOnly,
		KillSwitch: s.cfg.Services.UpDownKillSwitch,
	}
	reasons := make([]string, 0, 20)

	marketCopy := *market
	if s.updown != nil && s.updown.market != nil {
		live := []models.Market{marketCopy.Market}
		s.updown.market.attachRealtimePrices(ctx, live)
		if len(live) == 1 {
			marketCopy.Market = live[0]
		}
	}

	upToken, downToken, err := tokenIDsByOutcome(marketCopy)
	if err != nil {
		return nil, err
	}
	probeSize := math.Max(s.cfg.Services.UpDownDepthProbeShares, 10)
	upAsk, upBid, upSlippage, upMissingDepth := s.updown.executablePrices(ctx, marketCopy.Market, upToken, probeSize)
	downAsk, downBid, downSlippage, downMissingDepth := s.updown.executablePrices(ctx, marketCopy.Market, downToken, probeSize)
	_, _, upLast := quoteForToken(marketCopy.Market, upToken)
	_, _, downLast := quoteForToken(marketCopy.Market, downToken)

	pMarket := 0.0
	if v := marketProbability(upAsk, downAsk); v > 0 {
		pMarket = v
	} else if v := marketProbability(upLast, downLast); v > 0 {
		pMarket = v
		reasons = append(reasons, "market_probability_last_trade_fallback")
	}
	var pMarketPtr *float64
	if pMarket > 0 {
		v := upDownClamp(pMarket, 0.01, 0.99)
		pMarketPtr = &v
	} else {
		reasons = append(reasons, "market_probability_missing")
	}

	upBuyDepth := toLLMDepthEstimate(s.getDepthEstimate(ctx, marketCopy.ConditionID, upToken, "BUY", probeSize), 5)
	downBuyDepth := toLLMDepthEstimate(s.getDepthEstimate(ctx, marketCopy.ConditionID, downToken, "BUY", probeSize), 5)
	upSellDepth := toLLMDepthEstimate(s.getDepthEstimate(ctx, marketCopy.ConditionID, upToken, "SELL", probeSize), 5)
	downSellDepth := toLLMDepthEstimate(s.getDepthEstimate(ctx, marketCopy.ConditionID, downToken, "SELL", probeSize), 5)

	chainlinkReference := usesChainlinkReference(marketCopy)
	referenceSource := "synth"
	var referenceStartPrice *float64
	var referenceCurrentPrice *float64
	var referenceEndPrice *float64
	var referenceUpdatedAt *time.Time
	referenceCurrentFromChainlink := false
	if chainlinkReference {
		referenceSource = "chainlink"
		oracleLatest := GetChainlinkLatest(ctx, s.redis, marketCopy.Asset)
		if oracleLatest != nil {
			if oracleLatest.Price > 0 {
				v := oracleLatest.Price
				referenceCurrentPrice = &v
				referenceCurrentFromChainlink = true
			}
			if !oracleLatest.UpdatedAt.IsZero() {
				ts := oracleLatest.UpdatedAt.UTC()
				referenceUpdatedAt = &ts
			}
			if !oracleLatest.UpdatedAt.Before(marketCopy.EventStartTime) {
				_ = CaptureChainlinkStart(ctx, s.redis, marketCopy.Asset, marketCopy.EventStartTime, *oracleLatest)
			}
			if !oracleLatest.UpdatedAt.Before(marketCopy.EventEndTime) {
				_ = CaptureChainlinkEnd(ctx, s.redis, marketCopy.Asset, marketCopy.EventEndTime, *oracleLatest)
			}
		}
		if startPoint := GetChainlinkStart(ctx, s.redis, marketCopy.Asset, marketCopy.EventStartTime); startPoint != nil && startPoint.Price > 0 {
			v := startPoint.Price
			referenceStartPrice = &v
			if referenceUpdatedAt == nil || startPoint.UpdatedAt.After(*referenceUpdatedAt) {
				ts := startPoint.UpdatedAt.UTC()
				referenceUpdatedAt = &ts
			}
		}
		if endPoint := GetChainlinkEnd(ctx, s.redis, marketCopy.Asset, marketCopy.EventEndTime); endPoint != nil && endPoint.Price > 0 {
			v := endPoint.Price
			referenceEndPrice = &v
			if referenceUpdatedAt == nil || endPoint.UpdatedAt.After(*referenceUpdatedAt) {
				ts := endPoint.UpdatedAt.UTC()
				referenceUpdatedAt = &ts
			}
		}
	}

	var synthResp *synthdata.PolymarketUpDownResponse
	var volSummary *synthdata.VolatilityResponse
	var percentile *synthdata.PredictionPercentilesResponse
	var lpSummary *synthdata.LPProbabilitiesResponse
	var synthUpDownErr error
	var synthVolErr error
	var synthPercentileErr error
	var synthLPErr error
	horizon := horizonForWindow(marketCopy.WindowType)
	if s.synth != nil && s.synth.Enabled() && supportsSynthAnalyticsForWindow(marketCopy.WindowType) {
		var wg sync.WaitGroup
		synthWindow := synthWindowForMarket(marketCopy.WindowType)
		if synthWindow != "" {
			wg.Add(1)
			go func() {
				defer wg.Done()
				reqCtx, cancel := synthRequestContext(ctx, marketCopy.WindowType)
				defer cancel()
				synthResp, synthUpDownErr = s.synth.GetPolymarketUpDown(reqCtx, marketCopy.Asset, synthWindow, horizon, 14, 10)
			}()
		} else {
			synthUpDownErr = fmt.Errorf("synth window unsupported")
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			reqCtx, cancel := synthRequestContext(ctx, marketCopy.WindowType)
			defer cancel()
			volSummary, synthVolErr = s.synth.GetVolatility(reqCtx, marketCopy.Asset, horizon, 14, 10)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			reqCtx, cancel := synthRequestContext(ctx, marketCopy.WindowType)
			defer cancel()
			percentile, synthPercentileErr = s.synth.GetPredictionPercentiles(reqCtx, marketCopy.Asset, horizon, 14, 10)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			reqCtx, cancel := synthRequestContext(ctx, marketCopy.WindowType)
			defer cancel()
			lpSummary, synthLPErr = s.synth.GetLPProbabilities(reqCtx, marketCopy.Asset, horizon, 14, 10)
		}()
		wg.Wait()
	}
	if code := synthFetchReasonCode("synth_updown", synthUpDownErr); code != "" {
		reasons = append(reasons, code)
	}
	if code := synthFetchReasonCode("synth_volatility", synthVolErr); code != "" {
		reasons = append(reasons, code)
	}
	if code := synthFetchReasonCode("synth_percentiles", synthPercentileErr); code != "" {
		reasons = append(reasons, code)
	}
	if code := synthFetchReasonCode("synth_lp", synthLPErr); code != "" {
		reasons = append(reasons, code)
	}

	proxySynthWindow := false
	if synthResp != nil && !synthUpDownResponseMatchesMarket(marketCopy, synthResp) {
		flags.SourceMismatch = true
		reasons = append(reasons, "synth_market_window_mismatch")
		synthResp = nil
	}

	var synthClock time.Time
	if synthResp != nil {
		if synthResp.StartPrice > 0 && referenceStartPrice == nil {
			v := synthResp.StartPrice
			referenceStartPrice = &v
		}
		if synthResp.CurrentPrice > 0 && referenceCurrentPrice == nil {
			v := synthResp.CurrentPrice
			referenceCurrentPrice = &v
		}
		if t, ok := parseSynthTimestamp(synthResp.CurrentTime); ok {
			synthClock = t.UTC()
			if referenceUpdatedAt == nil || synthClock.After(*referenceUpdatedAt) {
				ts := synthClock
				referenceUpdatedAt = &ts
			}
		}
	}
	if referenceCurrentPrice == nil && chainlinkReference && referenceStartPrice != nil {
		v := *referenceStartPrice
		referenceCurrentPrice = &v
	}
	if referenceStartPrice == nil && referenceCurrentPrice != nil && !now.Before(marketCopy.EventStartTime) {
		v := *referenceCurrentPrice
		referenceStartPrice = &v
		reasons = append(reasons, "start_snapshot_fallback_current")
	}
	if referenceEndPrice == nil && referenceCurrentPrice != nil && !now.Before(marketCopy.EventEndTime) {
		v := *referenceCurrentPrice
		referenceEndPrice = &v
		reasons = append(reasons, "end_snapshot_fallback_current")
	}

	synthStaleThreshold := synthStaleThresholdForMarket(s.cfg, marketCopy.WindowType)
	synthClockDriftMax := synthClockDriftThresholdForMarket(s.cfg, marketCopy.WindowType)
	if !synthClock.IsZero() && now.Sub(synthClock) > synthStaleThreshold {
		flags.SynthStale = true
		reasons = append(reasons, "synth_stale")
	}
	if !synthClock.IsZero() && absDuration(now.Sub(synthClock)) > synthClockDriftMax {
		flags.ClockDrift = true
		reasons = append(reasons, "clock_drift")
	}

	var pSynthDirectPtr *float64
	var pSynthPercentilePtr *float64
	var pSynthPtr *float64
	if synthResp != nil && synthResp.SynthProbabilityUp >= 0 && synthResp.SynthProbabilityUp <= 1 {
		v := upDownClamp(synthResp.SynthProbabilityUp, 0.01, 0.99)
		pSynthDirectPtr = &v
		pSynthPtr = &v
	}

	thresholdPrice := resolveUpThresholdPrice(referenceStartPrice, synthResp)
	if thresholdPrice > 0 && percentile != nil && len(percentile.ForecastFuture.Percentiles) > 0 {
		stepSeconds := synthSamplingInterval(horizonForWindow(marketCopy.WindowType))
		targetStep := int(math.Ceil(math.Max(1, marketCopy.EventEndTime.Sub(now).Seconds()) / math.Max(stepSeconds.Seconds(), 1)))
		if targetStep < 1 {
			targetStep = 1
		}
		if p, pErr := synthdata.EstimateProbabilityUpFromPercentiles(percentile.ForecastFuture.Percentiles, targetStep, thresholdPrice); pErr == nil {
			v := upDownClamp(p, 0.01, 0.99)
			pSynthPercentilePtr = &v
		}
	}

	var pLPPtr *float64
	if thresholdPrice > 0 && lpSummary != nil {
		if p, ok := lpProbabilityAtThreshold(lpSummary, horizon, thresholdPrice); ok {
			v := upDownClamp(p, 0.01, 0.99)
			pLPPtr = &v
			reasons = append(reasons, "lp_probability_anchor")
		}
	}

	modelDiagnosticCode := "not_requested"
	modelDiagnosticDetail := ""
	var pModelPtr *float64
	var pSynthPrevSignalPtr *float64
	var prevSignalForStale *UpDownSignal
	if s.updown != nil {
		s.updown.mu.RLock()
		if prevSignal, ok := s.updown.signalsBySlug[marketCopy.Slug]; ok && now.Sub(prevSignal.Timestamp) <= upDownSignalHistoryTTL {
			prevCopy := prevSignal
			prevSignalForStale = &prevCopy
			if prevSignal.PSynthUp != nil {
				v := upDownClamp(*prevSignal.PSynthUp, 0.01, 0.99)
				pSynthPrevSignalPtr = &v
			}
		}
		s.updown.mu.RUnlock()
	}
	if thresholdPrice <= 0 {
		modelDiagnosticCode = "threshold_missing"
	} else if s.synth == nil || !s.synth.Enabled() {
		modelDiagnosticCode = "synth_disabled"
	} else {
		stepSeconds := synthSamplingInterval(horizonForWindow(marketCopy.WindowType))
		targetStep := int(math.Ceil(math.Max(1, marketCopy.EventEndTime.Sub(now).Seconds()) / math.Max(stepSeconds.Seconds(), 1)))
		if targetStep < 1 {
			targetStep = 1
		}
		timeIncrement, timeLength := synthPredictionShapeForWindow(marketCopy.WindowType)
		if timeIncrement <= 0 {
			timeIncrement = int(stepSeconds.Seconds())
		}
		if timeIncrement <= 0 {
			timeIncrement = 300
		}
		if timeLength <= 0 {
			timeLength = maxInt(timeIncrement, targetStep*timeIncrement)
		}
		maxStep := int(math.Ceil(float64(timeLength) / float64(timeIncrement)))
		if maxStep < 1 {
			maxStep = 1
		}
		if targetStep > maxStep {
			targetStep = maxStep
		}
		reqCtx, cancel := synthRequestContext(ctx, marketCopy.WindowType)
		defer cancel()
		pred, predErr := s.synth.GetEnterpriseProbabilityUp(
			reqCtx,
			marketCopy.Asset,
			timeIncrement,
			timeLength,
			targetStep,
			quantizeThreshold(thresholdPrice),
		)
		if predErr != nil {
			modelDiagnosticCode = "enterprise_fetch_failed"
			modelDiagnosticDetail = compactError(predErr)
			reasons = append(reasons, synthFetchReasonCode("synth_enterprise", predErr))
		} else if pred != nil && pred.ProbabilityUp >= 0 && pred.ProbabilityUp <= 1 {
			v := upDownClamp(pred.ProbabilityUp, 0.01, 0.99)
			pModelPtr = &v
			modelDiagnosticCode = "ok"
		} else {
			modelDiagnosticCode = "enterprise_empty"
		}
	}
	if pSynthPtr == nil {
		flags.SynthMissing = true
		reasons = append(reasons, "synth_missing")
		if pSynthPercentilePtr != nil {
			reasons = append(reasons, "synth_percentile_available_but_blocked")
		}
		if pModelPtr != nil {
			reasons = append(reasons, "synth_model_available_but_blocked")
		}
		if pSynthPrevSignalPtr != nil {
			reasons = append(reasons, "synth_previous_available_but_blocked")
		}
	}

	spreadUp := positiveOrZero(upAsk - upBid)
	spreadDown := positiveOrZero(downAsk - downBid)
	flags.DepthMissing = upMissingDepth || downMissingDepth
	if flags.DepthMissing {
		reasons = append(reasons, "missing_depth")
	}
	maxSpread := upDownClamp(s.cfg.Services.UpDownMaxSpreadToTrade, 0.01, 0.2)
	flags.WideSpread = spreadUp >= maxSpread || spreadDown >= maxSpread
	if flags.WideSpread {
		reasons = append(reasons, "wide_spread")
	}
	if upAsk <= 0 || downAsk <= 0 {
		flags.DataIntegrityFailed = true
		reasons = append(reasons, "non_executable_quotes")
	}
	if volSummary != nil && volSummary.ForecastFuture.AverageVolatility >= 80 {
		flags.HighVolatility = true
		reasons = append(reasons, "high_volatility_regime")
	}
	if marketCopy.Market.Liquidity < 5_000 {
		flags.LowLiquidity = true
		reasons = append(reasons, "low_liquidity")
	}
	minTopDepth := math.Max(s.cfg.Services.UpDownMinTopDepth, 0)
	if minTopDepth > 0 {
		topDepth := upBuyDepth.FillableSize + downBuyDepth.FillableSize
		if topDepth > 0 && topDepth < minTopDepth {
			flags.LowLiquidity = true
			reasons = append(reasons, "thin_top_of_book")
		}
	}

	startLead, startLag, endTail := statusBoundaryGuardSeconds(marketCopy.WindowType)
	nearStartBoundary := marketCopy.TimeToStartSeconds <= startLead && marketCopy.TimeToStartSeconds >= -startLag
	nearEndBoundary := marketCopy.TimeToEndSeconds <= endTail && marketCopy.TimeToEndSeconds >= -5
	if (nearStartBoundary && referenceStartPrice == nil) || (nearEndBoundary && referenceEndPrice == nil) {
		flags.StatusBoundary = true
		reasons = append(reasons, "boundary_state")
	}

	marketStaleThreshold := marketQuoteStaleThresholdForMarket(s.cfg, marketCopy.WindowType)
	marketUpdated := latestMarketTimestamp(marketCopy.Market)
	marketAgeSeconds := int64(0)
	if !marketUpdated.IsZero() {
		marketAgeSeconds = maxInt64(int64(math.Round(now.Sub(marketUpdated).Seconds())), 0)
		if shouldMarkLLMMarketStale(
			now,
			marketCopy,
			marketUpdated,
			marketStaleThreshold,
			upAsk,
			downAsk,
			referenceUpdatedAt,
			chainlinkReference,
			referenceCurrentFromChainlink,
			referenceCurrentPrice,
			prevSignalForStale,
		) {
			flags.MarketStale = true
			reasons = append(reasons, "market_stale")
		}
	}

	depthImb := 0.0
	if synthResp != nil {
		depthImb = depthImbalance(synthResp)
	} else {
		totalDepth := upBuyDepth.FillableSize + downBuyDepth.FillableSize
		if totalDepth > 0 {
			depthImb = upDownClamp((upBuyDepth.FillableSize-downBuyDepth.FillableSize)/totalDepth, -1, 1)
		}
	}
	expectedSlippage := maxFloat(upSlippage, downSlippage)
	regime := inferRegime(marketCopy.Market, volSummary)

	pConsensus := 0.5
	sum := 0.0
	addWeighted := func(prob *float64, weight float64) {
		if prob == nil {
			return
		}
		pConsensus += (*prob - 0.5) * weight
		sum += weight
	}
	volAvg := 0.0
	if volSummary != nil {
		volAvg = math.Max(volSummary.ForecastFuture.AverageVolatility, volSummary.ForecastPast.AverageVolatility)
	}
	wMarket, wSynth, wModel, wLP := 0.34, 0.28, 0.20, 0.18
	if volAvg >= 90 {
		wMarket += 0.08
		wLP += 0.06
		wSynth -= 0.08
		wModel -= 0.06
	}
	addWeighted(pMarketPtr, wMarket)
	addWeighted(pSynthPtr, wSynth)
	addWeighted(pModelPtr, wModel)
	addWeighted(pLPPtr, wLP)
	if sum <= 0 {
		pConsensus = 0.5
	} else {
		pConsensus = upDownClamp(0.5+(pConsensus-0.5)/sum, 0.01, 0.99)
	}

	probs := make([]float64, 0, 4)
	for _, ptr := range []*float64{pMarketPtr, pSynthPtr, pModelPtr, pLPPtr} {
		if ptr == nil {
			continue
		}
		probs = append(probs, upDownClamp(*ptr, 0.01, 0.99))
	}
	disagreement := 0.0
	if len(probs) >= 2 {
		minP := probs[0]
		maxP := probs[0]
		for _, p := range probs[1:] {
			if p < minP {
				minP = p
			}
			if p > maxP {
				maxP = p
			}
		}
		disagreement = upDownClamp(maxP-minP, 0, 1)
	}

	feesFrac := upDownClamp(s.cfg.Services.UpDownFeeBps/10_000.0, 0, 0.02)
	evUp := expectedValueBuyBinary(pConsensus, upAsk, feesFrac)
	evDown := expectedValueBuyBinary(1-pConsensus, downAsk, feesFrac)
	confidence := upDownClamp(0.35+0.65*math.Abs(pConsensus-0.5)*2, 0.02, 1.0)
	confidence = adjustConfidenceForVolatility(confidence, volSummary, marketCopy.TimeToEndSeconds, flags, false, assetCalibration{})
	evMin := computeDynamicEVThreshold(s.cfg.Services.UpDownEVMinThreshold, regime, marketCopy.TimeToEndSeconds, volSummary, false, assetCalibration{})
	reasons = append(reasons, fmt.Sprintf("dynamic_ev_min_%.4f", evMin))

	synthAgeSeconds := int64(0)
	synthClockUnix := int64(0)
	if !synthClock.IsZero() {
		synthAgeSeconds = maxInt64(int64(math.Round(now.Sub(synthClock).Seconds())), 0)
		synthClockUnix = synthClock.UTC().Unix()
	}

	volHeadForecast := []float64{}
	volHeadPast := []float64{}
	volAvgForecast := 0.0
	volAvgPast := 0.0
	if volSummary != nil {
		volHeadForecast = append(volHeadForecast, volSummary.ForecastFuture.Volatility...)
		volHeadPast = append(volHeadPast, volSummary.ForecastPast.Volatility...)
		volAvgForecast = volSummary.ForecastFuture.AverageVolatility
		volAvgPast = volSummary.ForecastPast.AverageVolatility
	}

	deviationRatio := 0.0
	deviationZ := 0.0
	if referenceStartPrice != nil && referenceCurrentPrice != nil && *referenceStartPrice > 0 && *referenceCurrentPrice > 0 {
		deviationRatio = math.Abs(*referenceCurrentPrice-*referenceStartPrice) / *referenceStartPrice
		expectedMove := 0.01
		if volAvgForecast > 0 {
			expectedMove = upDownClamp(volAvgForecast/1000.0, 0.003, 0.08)
		}
		if expectedMove > 0 {
			deviationZ = upDownClamp(deviationRatio/expectedMove, 0, 8)
		}
	}

	out := &llmIndependentSnapshot{
		Timestamp:                 now,
		MarketAgeSeconds:          marketAgeSeconds,
		SynthAgeSeconds:           synthAgeSeconds,
		RiskFlags:                 flags,
		ReferenceStartPrice:       referenceStartPrice,
		ReferenceCurrentPrice:     referenceCurrentPrice,
		ReferenceEndPrice:         referenceEndPrice,
		ReferenceUpdatedAt:        referenceUpdatedAt,
		ReferenceSource:           referenceSource,
		PMarketUp:                 pMarketPtr,
		PSynthUp:                  pSynthPtr,
		PModelUp:                  pModelPtr,
		PLPUp:                     pLPPtr,
		EVUp:                      evUp,
		EVDown:                    evDown,
		EVMinThreshold:            evMin,
		FeesBps:                   s.cfg.Services.UpDownFeeBps,
		Confidence:                confidence,
		ConsensusUp:               pConsensus,
		Disagreement:              disagreement,
		Regime:                    regime,
		ExecutableAskUp:           upAsk,
		ExecutableAskDown:         downAsk,
		ExecutableBidUp:           upBid,
		ExecutableBidDown:         downBid,
		ExecutableLastUp:          upLast,
		ExecutableLastDown:        downLast,
		SpreadUp:                  spreadUp,
		SpreadDown:                spreadDown,
		ExpectedSlippage:          expectedSlippage,
		DepthImbalance:            depthImb,
		UpBuyDepth:                upBuyDepth,
		DownBuyDepth:              downBuyDepth,
		UpSellDepth:               upSellDepth,
		DownSellDepth:             downSellDepth,
		SynthDirectProbability:    pSynthDirectPtr,
		SynthPercentileProxy:      pSynthPercentilePtr,
		ModelDiagnosticCode:       modelDiagnosticCode,
		ModelDiagnosticDetail:     modelDiagnosticDetail,
		SynthClockUnix:            synthClockUnix,
		SynthWindowProxy:          proxySynthWindow,
		VolatilityAverageForecast: volAvgForecast,
		VolatilityAveragePast:     volAvgPast,
		VolatilityHeadForecast:    volHeadForecast,
		VolatilityHeadPast:        volHeadPast,
		ReasonCodes:               dedupeStrings(reasons),
		DeviationRatio:            deviationRatio,
		DeviationZScore:           deviationZ,
	}
	return out, nil
}

func (s *UpDownLLMService) collectSnapshotSeries(ctx context.Context, market *UpDownMarket) ([]llmIndependentSnapshot, LLMSnapshotStability, error) {
	if market == nil {
		return nil, LLMSnapshotStability{}, fmt.Errorf("market is required")
	}
	sampleCount := upDownLLMSnapshotCountDefault
	spacing := snapshotSpacingForWindow(market.WindowType)
	if dl, ok := ctx.Deadline(); ok {
		remaining := time.Until(dl)
		if remaining <= 2*spacing {
			sampleCount = 1
		}
	}
	if sampleCount < 1 {
		sampleCount = 1
	}

	samples := make([]llmIndependentSnapshot, 0, sampleCount)
	for i := 0; i < sampleCount; i++ {
		snap, err := s.collectIndependentSnapshot(ctx, market)
		if err != nil {
			return nil, LLMSnapshotStability{}, err
		}
		samples = append(samples, *snap)
		if i == sampleCount-1 {
			break
		}
		timer := time.NewTimer(spacing)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, LLMSnapshotStability{}, ctx.Err()
		case <-timer.C:
		}
	}
	return samples, evaluateSnapshotStability(samples), nil
}

func snapshotSpacingForWindow(window UpDownWindowType) time.Duration {
	switch window {
	case Window5m:
		return upDownLLMSnapshotSpacing5m
	case Window15m:
		return upDownLLMSnapshotSpacing15m
	default:
		return upDownLLMSnapshotSpacing15m
	}
}

func snapshotDirectionalVote(snapshot llmIndependentSnapshot) string {
	if snapshot.EVUp > snapshot.EVDown && snapshot.EVUp > snapshot.EVMinThreshold {
		return "UP"
	}
	if snapshot.EVDown > snapshot.EVUp && snapshot.EVDown > snapshot.EVMinThreshold {
		return "DOWN"
	}
	return "NONE"
}

func evaluateSnapshotStability(samples []llmIndependentSnapshot) LLMSnapshotStability {
	out := LLMSnapshotStability{
		SampleCount: len(samples),
		Stable:      len(samples) > 0,
	}
	if len(samples) == 0 {
		out.Stable = false
		out.DirectionSummary = "NONE"
		return out
	}
	minConsensus := samples[0].ConsensusUp
	maxConsensus := samples[0].ConsensusUp
	minAskUp := samples[0].ExecutableAskUp
	maxAskUp := samples[0].ExecutableAskUp
	minAskDown := samples[0].ExecutableAskDown
	maxAskDown := samples[0].ExecutableAskDown
	minBestEV := math.Max(samples[0].EVUp, samples[0].EVDown)
	maxBestEV := minBestEV

	for _, sample := range samples {
		switch snapshotDirectionalVote(sample) {
		case "UP":
			out.UpVotes++
		case "DOWN":
			out.DownVotes++
		default:
			out.NoTradeVotes++
		}
		if sample.ConsensusUp < minConsensus {
			minConsensus = sample.ConsensusUp
		}
		if sample.ConsensusUp > maxConsensus {
			maxConsensus = sample.ConsensusUp
		}
		if sample.ExecutableAskUp < minAskUp {
			minAskUp = sample.ExecutableAskUp
		}
		if sample.ExecutableAskUp > maxAskUp {
			maxAskUp = sample.ExecutableAskUp
		}
		if sample.ExecutableAskDown < minAskDown {
			minAskDown = sample.ExecutableAskDown
		}
		if sample.ExecutableAskDown > maxAskDown {
			maxAskDown = sample.ExecutableAskDown
		}
		bestEV := math.Max(sample.EVUp, sample.EVDown)
		if bestEV < minBestEV {
			minBestEV = bestEV
		}
		if bestEV > maxBestEV {
			maxBestEV = bestEV
		}
	}

	out.ConsensusDrift = upDownClamp(maxConsensus-minConsensus, 0, 1)
	out.AskUpDrift = upDownClamp(maxAskUp-minAskUp, 0, 1)
	out.AskDownDrift = upDownClamp(maxAskDown-minAskDown, 0, 1)
	out.BestEVDrift = maxBestEV - minBestEV

	switch {
	case out.UpVotes > 0 && out.DownVotes == 0:
		out.DirectionSummary = "UP"
	case out.DownVotes > 0 && out.UpVotes == 0:
		out.DirectionSummary = "DOWN"
	case out.UpVotes == 0 && out.DownVotes == 0:
		out.DirectionSummary = "NONE"
	default:
		out.DirectionSummary = "MIXED"
	}

	winnerVotes := maxInt(out.UpVotes, out.DownVotes)
	if winnerVotes == 0 {
		out.Stable = false
	}
	if out.UpVotes > 0 && out.DownVotes > 0 {
		out.Stable = false
	}
	if out.ConsensusDrift > upDownLLMSnapshotDriftSoftMax {
		out.Stable = false
	}
	if out.AskUpDrift > upDownLLMSnapshotDriftSoftMax || out.AskDownDrift > upDownLLMSnapshotDriftSoftMax {
		out.Stable = false
	}
	return out
}

func snapshotStabilityHardBlock(stability LLMSnapshotStability) bool {
	if stability.SampleCount <= 0 {
		return true
	}
	if stability.ConsensusDrift > upDownLLMSnapshotDriftHardMax {
		return true
	}
	if stability.AskUpDrift > upDownLLMSnapshotDriftHardMax || stability.AskDownDrift > upDownLLMSnapshotDriftHardMax {
		return true
	}
	if stability.UpVotes > 0 && stability.DownVotes > 0 {
		// Two-snapshot mode is intentionally jitter-tolerant at short window rollovers.
		// Mixed votes here should degrade confidence, not hard-block execution.
		if stability.SampleCount < 3 {
			return false
		}
		if stability.BestEVDrift >= upDownLLMSnapshotEVHardMin {
			return true
		}
		if stability.ConsensusDrift >= upDownLLMSnapshotDriftSoftMax {
			return true
		}
		if stability.AskUpDrift >= upDownLLMSnapshotDriftSoftMax || stability.AskDownDrift >= upDownLLMSnapshotDriftSoftMax {
			return true
		}
	}
	return false
}

func snapshotStabilityHardBlockReasons(stability LLMSnapshotStability) []string {
	reasons := []string{
		"snapshot_instability_guard",
		"snapshot_instability_hard_block",
	}
	if stability.SampleCount <= 1 {
		reasons = append(reasons, "single_snapshot_risk_reduced")
	}
	if stability.UpVotes > 0 && stability.DownVotes > 0 {
		reasons = append(reasons, "multi_snapshot_disagreement")
	}
	if stability.ConsensusDrift > upDownLLMSnapshotDriftHardMax {
		reasons = append(reasons, "snapshot_consensus_drift_hard")
	}
	if stability.AskUpDrift > upDownLLMSnapshotDriftHardMax || stability.AskDownDrift > upDownLLMSnapshotDriftHardMax {
		reasons = append(reasons, "snapshot_quote_drift_hard")
	}
	if stability.UpVotes > 0 && stability.DownVotes > 0 && stability.SampleCount >= 3 && stability.BestEVDrift >= upDownLLMSnapshotEVHardMin {
		reasons = append(reasons, "snapshot_ev_drift_hard")
	}
	return dedupeStrings(reasons)
}

func mergeSnapshotSeries(samples []llmIndependentSnapshot) *llmIndependentSnapshot {
	if len(samples) == 0 {
		return nil
	}
	out := samples[len(samples)-1]
	reasons := append([]string{}, out.ReasonCodes...)
	for i := range samples {
		sample := samples[i]
		if out.ReferenceStartPrice == nil && sample.ReferenceStartPrice != nil {
			v := *sample.ReferenceStartPrice
			out.ReferenceStartPrice = &v
		}
		if out.ReferenceCurrentPrice == nil && sample.ReferenceCurrentPrice != nil {
			v := *sample.ReferenceCurrentPrice
			out.ReferenceCurrentPrice = &v
		}
		if out.ReferenceEndPrice == nil && sample.ReferenceEndPrice != nil {
			v := *sample.ReferenceEndPrice
			out.ReferenceEndPrice = &v
		}
		if out.ReferenceUpdatedAt == nil && sample.ReferenceUpdatedAt != nil {
			ts := sample.ReferenceUpdatedAt.UTC()
			out.ReferenceUpdatedAt = &ts
		}
		if out.MarketAgeSeconds <= 0 && sample.MarketAgeSeconds > out.MarketAgeSeconds {
			out.MarketAgeSeconds = sample.MarketAgeSeconds
		}
		if out.SynthAgeSeconds <= 0 && sample.SynthAgeSeconds > out.SynthAgeSeconds {
			out.SynthAgeSeconds = sample.SynthAgeSeconds
		}
	}
	out.RiskFlags = mergeRiskFlagsByVotes(samples)
	out.ReasonCodes = dedupeStrings(reasons)
	return &out
}

func mergeRiskFlagsByVotes(samples []llmIndependentSnapshot) UpDownRiskFlags {
	if len(samples) == 0 {
		return UpDownRiskFlags{}
	}
	majority := len(samples)/2 + 1
	trueCount := func(selector func(UpDownRiskFlags) bool) int {
		count := 0
		for _, sample := range samples {
			if selector(sample.RiskFlags) {
				count++
			}
		}
		return count
	}
	any := func(selector func(UpDownRiskFlags) bool) bool {
		return trueCount(selector) > 0
	}
	majorityOn := func(selector func(UpDownRiskFlags) bool) bool {
		return trueCount(selector) >= majority
	}

	return UpDownRiskFlags{
		ReadOnly:            any(func(f UpDownRiskFlags) bool { return f.ReadOnly }),
		KillSwitch:          any(func(f UpDownRiskFlags) bool { return f.KillSwitch }),
		StatusBoundary:      any(func(f UpDownRiskFlags) bool { return f.StatusBoundary }),
		DataIntegrityFailed: any(func(f UpDownRiskFlags) bool { return f.DataIntegrityFailed }),
		SynthMissing:        majorityOn(func(f UpDownRiskFlags) bool { return f.SynthMissing }),
		SynthStale:          majorityOn(func(f UpDownRiskFlags) bool { return f.SynthStale }),
		MarketStale:         majorityOn(func(f UpDownRiskFlags) bool { return f.MarketStale }),
		DepthMissing:        majorityOn(func(f UpDownRiskFlags) bool { return f.DepthMissing }),
		WideSpread:          majorityOn(func(f UpDownRiskFlags) bool { return f.WideSpread }),
		SourceMismatch:      majorityOn(func(f UpDownRiskFlags) bool { return f.SourceMismatch }),
		ClockDrift:          majorityOn(func(f UpDownRiskFlags) bool { return f.ClockDrift }),
		LowLiquidity:        majorityOn(func(f UpDownRiskFlags) bool { return f.LowLiquidity }),
		HighVolatility:      majorityOn(func(f UpDownRiskFlags) bool { return f.HighVolatility }),
	}
}

func (s *UpDownLLMService) getDepthEstimate(ctx context.Context, conditionID, tokenID, side string, size float64) *DepthEstimate {
	if s == nil || s.updown == nil || s.updown.market == nil || strings.TrimSpace(conditionID) == "" || strings.TrimSpace(tokenID) == "" {
		return nil
	}
	reqCtx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
	defer cancel()
	depth, err := s.updown.market.GetDepthEstimate(reqCtx, conditionID, tokenID, side, size)
	if err != nil {
		return nil
	}
	return depth
}

func toLLMDepthEstimate(est *DepthEstimate, maxLevels int) llmDepthEstimate {
	out := llmDepthEstimate{}
	if est == nil {
		return out
	}
	out.RequestedSize = est.RequestedSize
	out.FillableSize = est.FillableSize
	out.EstimatedAveragePrice = est.EstimatedAveragePrice
	out.EstimatedTotalValue = est.EstimatedTotalValue
	out.InsufficientLiquidity = est.InsufficientLiquidity
	if maxLevels <= 0 {
		maxLevels = len(est.Levels)
	}
	for i, lvl := range est.Levels {
		if i >= maxLevels {
			break
		}
		out.Levels = append(out.Levels, llmOrderBookLevel{
			Price:           lvl.Price,
			Available:       lvl.Available,
			Used:            lvl.Used,
			CumulativeSize:  lvl.CumulativeSize,
			CumulativeValue: lvl.CumulativeValue,
		})
	}
	return out
}

func defaultLLMInvalidations(market UpDownMarket, side string) []string {
	cutoff := maxInt(noTradeCutoffForWindow(market.WindowType, nil), 30)
	out := []string{
		"Cancel if any guard block becomes active.",
		"Cancel if market spread exceeds configured max spread.",
		"Cancel if synth/allora freshness becomes stale.",
		fmt.Sprintf("Cancel inside final %d seconds to expiry.", cutoff),
	}
	switch strings.ToUpper(strings.TrimSpace(side)) {
	case "UP":
		out = append(out, "Cancel if UP edge collapses against market implied probability.")
	case "DOWN":
		out = append(out, "Cancel if DOWN edge collapses against market implied probability.")
	}
	return dedupeStrings(out)
}

func buildLLMRetrievalBundle(now time.Time, window UpDownWindowType, snapshot llmIndependentSnapshot, alloraMeta AlloraProxyMeta) llmRetrievalBundle {
	retrieval := llmRetrievalBundle{
		StrategyVersion:  "rag-v1.3",
		RankingPolicy:    "freshness_weighted_reliability_rank",
		CorrectiveAction: "none",
		Evidence:         make([]llmRetrievalEvidence, 0, 10),
	}
	marketFreshMax := llmRetrievalMarketFreshMax(window)
	synthFreshMax := llmRetrievalSynthFreshMax(window)
	chainlinkFreshMax := llmRetrievalChainlinkFreshMax(window)
	alloraFreshMax := llmRetrievalAlloraFreshMax(window)

	appendEvidence := func(source, status string, age int64, reliability, coverage float64, freshnessScale int64, notes ...string) {
		scale := float64(maxInt64(freshnessScale, 30))
		freshness := 1.0 / (1.0 + float64(maxInt64(age, 0))/scale)
		score := upDownClamp(reliability, 0, 1) * upDownClamp(coverage, 0, 1) * upDownClamp(freshness, 0, 1)
		retrieval.Evidence = append(retrieval.Evidence, llmRetrievalEvidence{
			Source:          source,
			Status:          status,
			AgeSeconds:      maxInt64(age, 0),
			Reliability:     upDownClamp(reliability, 0, 1),
			Coverage:        upDownClamp(coverage, 0, 1),
			RetrievalScore:  upDownClamp(score, 0, 1),
			FreshnessWeight: upDownClamp(freshness, 0, 1),
			Notes:           dedupeStrings(notes),
		})
	}

	appendEvidence("market_microstructure", statusForAge(snapshot.MarketAgeSeconds, marketFreshMax), snapshot.MarketAgeSeconds, 0.96, coverageFromValues(snapshot.ExecutableAskUp, snapshot.ExecutableAskDown, snapshot.ExecutableBidUp, snapshot.ExecutableBidDown), marketFreshMax)
	appendEvidence("order_book_depth_buy", statusForDepth(snapshot.UpBuyDepth, snapshot.DownBuyDepth), snapshot.MarketAgeSeconds, 0.92, coverageFromValues(snapshot.UpBuyDepth.FillableSize, snapshot.DownBuyDepth.FillableSize), marketFreshMax)
	appendEvidence("order_book_depth_sell", statusForDepth(snapshot.UpSellDepth, snapshot.DownSellDepth), snapshot.MarketAgeSeconds, 0.88, coverageFromValues(snapshot.UpSellDepth.FillableSize, snapshot.DownSellDepth.FillableSize), marketFreshMax)
	appendEvidence("synth_probabilities", statusForAge(snapshot.SynthAgeSeconds, synthFreshMax), snapshot.SynthAgeSeconds, 0.90, coverageFromPointers(snapshot.PSynthUp, snapshot.PModelUp, snapshot.PLPUp), synthFreshMax)
	appendEvidence("synth_volatility", statusForAge(snapshot.SynthAgeSeconds, synthFreshMax), snapshot.SynthAgeSeconds, 0.94, coverageFromValues(snapshot.VolatilityAverageForecast, snapshot.VolatilityAveragePast), synthFreshMax)
	appendEvidence("synth_reference_prices", statusForReference(snapshot.ReferenceStartPrice, snapshot.ReferenceCurrentPrice), snapshot.SynthAgeSeconds, 0.89, coverageFromPointers(snapshot.ReferenceStartPrice, snapshot.ReferenceCurrentPrice, snapshot.ReferenceEndPrice), synthFreshMax)
	appendEvidence("allora_5m", alloraMeta.ProxyStatus, alloraMeta.AgeSeconds, 0.93, coverageFromValues(alloraMeta.RawP5, alloraMeta.ProxyP15), alloraFreshMax)
	if strings.EqualFold(snapshot.ReferenceSource, "chainlink") {
		refAge := int64(0)
		if snapshot.ReferenceUpdatedAt != nil && !snapshot.ReferenceUpdatedAt.IsZero() {
			refAge = maxInt64(int64(math.Round(now.Sub(snapshot.ReferenceUpdatedAt.UTC()).Seconds())), 0)
		}
		appendEvidence("chainlink_reference", statusForAge(refAge, chainlinkFreshMax), refAge, 0.98, coverageFromPointers(snapshot.ReferenceStartPrice, snapshot.ReferenceCurrentPrice, snapshot.ReferenceEndPrice), chainlinkFreshMax)
	}

	sort.SliceStable(retrieval.Evidence, func(i, j int) bool {
		if retrieval.Evidence[i].RetrievalScore == retrieval.Evidence[j].RetrievalScore {
			return retrieval.Evidence[i].Source < retrieval.Evidence[j].Source
		}
		return retrieval.Evidence[i].RetrievalScore > retrieval.Evidence[j].RetrievalScore
	})

	top := minInt(4, len(retrieval.Evidence))
	if top > 0 {
		sum := 0.0
		for i := 0; i < top; i++ {
			sum += retrieval.Evidence[i].RetrievalScore
		}
		retrieval.QualityScore = upDownClamp(sum/float64(top), 0, 1)
	}

	switch {
	case retrieval.QualityScore < 0.42:
		retrieval.CorrectiveAction = "force_no_trade"
	case retrieval.QualityScore < 0.58:
		retrieval.CorrectiveAction = "degrade_confidence"
	default:
		retrieval.CorrectiveAction = "none"
	}
	return retrieval
}

func llmRetrievalMarketFreshMax(window UpDownWindowType) int64 {
	switch window {
	case Window5m:
		return 45
	case Window15m:
		return 75
	case Window1h:
		return 120
	default:
		return 150
	}
}

func llmRetrievalSynthFreshMax(window UpDownWindowType) int64 {
	switch window {
	case Window5m:
		return 180
	case Window15m:
		return 240
	case Window1h:
		return 420
	default:
		return 600
	}
}

func llmRetrievalChainlinkFreshMax(window UpDownWindowType) int64 {
	switch window {
	case Window5m:
		return 90
	case Window15m:
		return 120
	default:
		return 180
	}
}

func llmRetrievalAlloraFreshMax(window UpDownWindowType) int64 {
	switch window {
	case Window5m:
		return 60
	case Window15m:
		return 380
	default:
		return 380
	}
}

func coverageFromValues(values ...float64) float64 {
	if len(values) == 0 {
		return 0
	}
	valid := 0
	for _, v := range values {
		if !math.IsNaN(v) && !math.IsInf(v, 0) && v > 0 {
			valid++
		}
	}
	return float64(valid) / float64(len(values))
}

func coverageFromPointers(values ...*float64) float64 {
	if len(values) == 0 {
		return 0
	}
	valid := 0
	for _, v := range values {
		if v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) && *v > 0 {
			valid++
		}
	}
	return float64(valid) / float64(len(values))
}

func statusForAge(age int64, freshMax int64) string {
	if age <= freshMax {
		return "fresh"
	}
	if age <= freshMax*2 {
		return "stale_soft"
	}
	return "stale_hard"
}

func statusForDepth(up llmDepthEstimate, down llmDepthEstimate) string {
	if up.FillableSize <= 0 || down.FillableSize <= 0 {
		return "missing"
	}
	if up.InsufficientLiquidity || down.InsufficientLiquidity {
		return "stale_soft"
	}
	return "fresh"
}

func statusForReference(start *float64, current *float64) string {
	if start == nil || current == nil {
		return "missing"
	}
	return "fresh"
}

func sanitizeReasonCode(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return "unknown"
	}
	clean := make([]rune, 0, len(raw))
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
			clean = append(clean, r)
		case r >= '0' && r <= '9':
			clean = append(clean, r)
		default:
			clean = append(clean, '_')
		}
	}
	out := strings.Trim(strings.Join(strings.Fields(strings.ReplaceAll(string(clean), "_", " ")), "_"), "_")
	if out == "" {
		return "unknown"
	}
	return out
}

func (s *UpDownLLMService) buildLLMContext(
	market UpDownMarket,
	snapshot llmIndependentSnapshot,
	alloraInference *allora.PriceInference,
	alloraMeta AlloraProxyMeta,
	stability LLMSnapshotStability,
) (llmStructuredContext, string, string) {
	decimals := maxInt(s.cfg.Services.UpDownLLMContextDecimals, 6)
	fresh5m := int64(maxInt(s.cfg.Services.UpDownLLMAlloraFreshMaxSeconds, 60))
	soft15m := int64(maxInt(s.cfg.Services.UpDownLLMAlloraSoftLagSeconds, 380))
	hard15m := int64(maxInt(s.cfg.Services.UpDownLLMAlloraHardLagSeconds, 440))
	synthFreshMax := llmRetrievalSynthFreshMax(market.WindowType)
	if hard15m <= soft15m {
		hard15m = soft15m + 30
	}
	deterministicMeta := computeDeterministicFormulaMeta(market, snapshot, s.cfg)

	ctxData := llmStructuredContext{
		Version: "updown-llm-v1",
	}
	ctxData.Query.GeneratedUnix = snapshot.Timestamp.UTC().Unix()
	ctxData.DataSemantics.Synth.Role = "native probabilistic and volatility analytics for this market window"
	ctxData.DataSemantics.Synth.FreshMaxSeconds = synthFreshMax
	ctxData.DataSemantics.Synth.StaleSoftSeconds = synthFreshMax * 2
	ctxData.DataSemantics.Synth.WindowNote = "5m and 15m windows must use their matching synth up/down endpoints; cross-window payloads are rejected"
	ctxData.DataSemantics.Synth.VolatilityNote = "volatility features are first-class priors and can override weak directional edges"
	ctxData.DataSemantics.Synth.ProbabilityNote = "p_synth/p_model/p_lp/p_market are calibrated directional estimates, not guaranteed truths"
	ctxData.DataSemantics.Synth.ReliabilityPolicy = "use retrieval evidence age, coverage, and score before trusting any synth field"
	ctxData.DataSemantics.Allora.Role = "short-horizon network inference used as mandatory directional prior"
	ctxData.DataSemantics.Allora.SourceTimeframe = "5m"
	ctxData.DataSemantics.Allora.FreshMaxSeconds5m = fresh5m
	ctxData.DataSemantics.Allora.SoftLagSeconds15m = soft15m
	ctxData.DataSemantics.Allora.HardLagSeconds15m = hard15m
	ctxData.DataSemantics.Allora.ProxyFormula = "p15_proxy = clamp(0.5 + 0.5*((2*p5-1)*0.58*(0.65+0.35*progress)*exp(-max(0,age-30)/180)), 0.01, 0.99)"
	ctxData.DataSemantics.Allora.StatusPolicy = "5m requires fresh; 15m accepts stale_soft with confidence penalty; stale_hard blocks trading"
	ctxData.DataSemantics.Allora.ReliabilityPolicy = "age and proxy_status are hard reliability controls, not soft hints"
	ctxData.DataSemantics.Allora.DecisionBlockRules = "if stale_hard then force NO_TRADE; for 5m non-fresh is invalid for execution"
	ctxData.DataSemantics.Deterministic.Role = "baseline rule engine formulas for EV/confidence/kelly and hard no-trade guards"
	ctxData.DataSemantics.Deterministic.FormulaVersion = "deterministic-v1"
	ctxData.DataSemantics.Deterministic.Description = "included for model calibration parity, drift checks, and optional execution-source toggling"
	ctxData.DataSemantics.Retrieval.Role = "ranks all evidence by freshness, reliability, and coverage"
	ctxData.DataSemantics.Retrieval.RankingPolicy = "freshness_weighted_reliability_rank"
	ctxData.DataSemantics.Retrieval.CorrectiveNote = "degrade_confidence and force_no_trade are hard corrective actions"
	ctxData.Market.Slug = market.Slug
	ctxData.Market.ConditionID = market.ConditionID
	ctxData.Market.Asset = strings.ToUpper(strings.TrimSpace(market.Asset))
	ctxData.Market.WindowType = market.WindowType
	ctxData.Market.ResolutionSourceType = string(market.ResolutionSourceType)
	ctxData.Market.EventStartUnix = market.EventStartTime.UTC().Unix()
	ctxData.Market.EventEndUnix = market.EventEndTime.UTC().Unix()
	ctxData.Market.TimeToEndSeconds = market.TimeToEndSeconds
	ctxData.Market.IsActiveWindow = market.IsActiveWindow
	ctxData.Market.Liquidity = roundFloat(market.Market.Liquidity, decimals)
	ctxData.Market.Volume24h = roundFloat(market.Market.Volume24h, decimals)

	ctxData.Reference.StartPrice = roundFloatPtr(snapshot.ReferenceStartPrice, decimals)
	ctxData.Reference.CurrentPrice = roundFloatPtr(snapshot.ReferenceCurrentPrice, decimals)
	ctxData.Reference.EndPrice = roundFloatPtr(snapshot.ReferenceEndPrice, decimals)
	if snapshot.ReferenceUpdatedAt != nil && !snapshot.ReferenceUpdatedAt.IsZero() {
		ctxData.Reference.UpdatedUnix = snapshot.ReferenceUpdatedAt.UTC().Unix()
	}
	ctxData.Reference.Source = snapshot.ReferenceSource

	ctxData.Microstructure.ExecutableAskUp = roundFloat(snapshot.ExecutableAskUp, decimals)
	ctxData.Microstructure.ExecutableAskDown = roundFloat(snapshot.ExecutableAskDown, decimals)
	ctxData.Microstructure.ExecutableBidUp = roundFloat(snapshot.ExecutableBidUp, decimals)
	ctxData.Microstructure.ExecutableBidDown = roundFloat(snapshot.ExecutableBidDown, decimals)
	ctxData.Microstructure.ExecutableLastUp = roundFloat(snapshot.ExecutableLastUp, decimals)
	ctxData.Microstructure.ExecutableLastDown = roundFloat(snapshot.ExecutableLastDown, decimals)
	ctxData.Microstructure.SpreadUp = roundFloat(snapshot.SpreadUp, decimals)
	ctxData.Microstructure.SpreadDown = roundFloat(snapshot.SpreadDown, decimals)
	ctxData.Microstructure.ExpectedSlippage = roundFloat(snapshot.ExpectedSlippage, decimals)
	ctxData.Microstructure.DepthImbalance = roundFloat(snapshot.DepthImbalance, decimals)
	ctxData.Microstructure.UpBuyDepth = roundDepthEstimate(snapshot.UpBuyDepth, decimals, 5)
	ctxData.Microstructure.DownBuyDepth = roundDepthEstimate(snapshot.DownBuyDepth, decimals, 5)
	ctxData.Microstructure.UpSellDepth = roundDepthEstimate(snapshot.UpSellDepth, decimals, 5)
	ctxData.Microstructure.DownSellDepth = roundDepthEstimate(snapshot.DownSellDepth, decimals, 5)

	ctxData.Synth.PMarketUp = roundFloatPtr(snapshot.PMarketUp, decimals)
	ctxData.Synth.PSynthUp = roundFloatPtr(snapshot.PSynthUp, decimals)
	ctxData.Synth.PModelUp = roundFloatPtr(snapshot.PModelUp, decimals)
	ctxData.Synth.PLPUp = roundFloatPtr(snapshot.PLPUp, decimals)
	ctxData.Synth.SynthDirectProbability = roundFloatPtr(snapshot.SynthDirectProbability, decimals)
	ctxData.Synth.SynthPercentileProxy = roundFloatPtr(snapshot.SynthPercentileProxy, decimals)
	ctxData.Synth.ConsensusUp = roundFloat(snapshot.ConsensusUp, decimals)
	ctxData.Synth.Disagreement = roundFloat(snapshot.Disagreement, decimals)
	ctxData.Synth.EVUp = roundFloat(snapshot.EVUp, decimals)
	ctxData.Synth.EVDown = roundFloat(snapshot.EVDown, decimals)
	ctxData.Synth.EVMinThreshold = roundFloat(snapshot.EVMinThreshold, decimals)
	ctxData.Synth.Confidence = roundFloat(snapshot.Confidence, decimals)
	ctxData.Synth.TimestampUnix = snapshot.Timestamp.UTC().Unix()
	ctxData.Synth.Regime = snapshot.Regime
	ctxData.Synth.SynthClockUnix = snapshot.SynthClockUnix
	ctxData.Synth.SynthWindowProxy = snapshot.SynthWindowProxy
	ctxData.Synth.ModelDiagnosticCode = snapshot.ModelDiagnosticCode
	ctxData.Synth.ModelDiagnosticDetail = snapshot.ModelDiagnosticDetail
	ctxData.Synth.VolatilityAverageForecast = roundFloat(snapshot.VolatilityAverageForecast, decimals)
	ctxData.Synth.VolatilityAveragePast = roundFloat(snapshot.VolatilityAveragePast, decimals)
	ctxData.Synth.VolatilityHeadForecast = roundFloatSlice(snapshot.VolatilityHeadForecast, decimals, 12)
	ctxData.Synth.VolatilityHeadPast = roundFloatSlice(snapshot.VolatilityHeadPast, decimals, 12)
	ctxData.Synth.DeviationRatio = roundFloat(snapshot.DeviationRatio, decimals)
	ctxData.Synth.DeviationZScore = roundFloat(snapshot.DeviationZScore, decimals)

	ctxData.Guards.RiskFlags = snapshot.RiskFlags
	ctxData.Guards.ReasonCodes = dedupeStrings(snapshot.ReasonCodes)

	ctxData.Allora.Asset = strings.ToUpper(strings.TrimSpace(market.Asset))
	ctxData.Allora.Timeframe = string(allora.Timeframe5m)
	ctxData.Allora.RawP5 = roundFloat(alloraMeta.RawP5, decimals)
	ctxData.Allora.SmoothedP5 = roundFloat(alloraMeta.SmoothedP5, decimals)
	ctxData.Allora.ProxyP15 = roundFloat(alloraMeta.ProxyP15, decimals)
	ctxData.Allora.AgeSeconds = alloraMeta.AgeSeconds
	ctxData.Allora.Status = alloraMeta.ProxyStatus
	ctxData.Allora.UsedCached = alloraMeta.UsedCached
	if alloraInference != nil {
		ctxData.Allora.TopicID = strings.TrimSpace(alloraInference.TopicID)
		ctxData.Allora.Timestamp = alloraInference.Timestamp.UTC().Unix()
		v := roundFloat(alloraInference.NetworkInference, decimals)
		ctxData.Allora.NetworkInference = &v
		ctxData.Allora.ConfidenceIntervalPercentiles = roundFloatSlice(alloraInference.ConfidenceIntervalPercentiles, decimals, 16)
		ctxData.Allora.ConfidenceIntervalValues = roundFloatSlice(alloraInference.ConfidenceIntervalValues, decimals, 16)
	}
	ctxData.SnapshotStability = LLMSnapshotStability{
		SampleCount:      stability.SampleCount,
		Stable:           stability.Stable,
		UpVotes:          stability.UpVotes,
		DownVotes:        stability.DownVotes,
		NoTradeVotes:     stability.NoTradeVotes,
		ConsensusDrift:   roundFloat(stability.ConsensusDrift, decimals),
		AskUpDrift:       roundFloat(stability.AskUpDrift, decimals),
		AskDownDrift:     roundFloat(stability.AskDownDrift, decimals),
		BestEVDrift:      roundFloat(stability.BestEVDrift, decimals),
		DirectionSummary: stability.DirectionSummary,
	}
	ctxData.Deterministic = roundDeterministicFormulaMeta(deterministicMeta, decimals)
	ctxData.Retrieval = roundRetrievalBundle(snapshot.Retrieval, decimals)

	blob, _ := json.Marshal(ctxData)
	contextJSON := string(blob)
	contextHash := hashHex(contextJSON)
	return ctxData, contextJSON, contextHash
}

func roundDepthEstimate(in llmDepthEstimate, decimals int, maxLevels int) llmDepthEstimate {
	out := llmDepthEstimate{
		RequestedSize:         roundFloat(in.RequestedSize, decimals),
		FillableSize:          roundFloat(in.FillableSize, decimals),
		EstimatedAveragePrice: roundFloat(in.EstimatedAveragePrice, decimals),
		EstimatedTotalValue:   roundFloat(in.EstimatedTotalValue, decimals),
		InsufficientLiquidity: in.InsufficientLiquidity,
	}
	if maxLevels <= 0 {
		maxLevels = 1
	}
	for i, lvl := range in.Levels {
		if i >= maxLevels {
			break
		}
		out.Levels = append(out.Levels, llmOrderBookLevel{
			Price:           roundFloat(lvl.Price, decimals),
			Available:       roundFloat(lvl.Available, decimals),
			Used:            roundFloat(lvl.Used, decimals),
			CumulativeSize:  roundFloat(lvl.CumulativeSize, decimals),
			CumulativeValue: roundFloat(lvl.CumulativeValue, decimals),
		})
	}
	return out
}

func roundFloatSlice(values []float64, decimals int, maxItems int) []float64 {
	if maxItems <= 0 {
		maxItems = len(values)
	}
	out := make([]float64, 0, minInt(maxItems, len(values)))
	for i, v := range values {
		if i >= maxItems {
			break
		}
		out = append(out, roundFloat(v, decimals))
	}
	return out
}

func roundRetrievalBundle(in llmRetrievalBundle, decimals int) llmRetrievalBundle {
	out := llmRetrievalBundle{
		StrategyVersion:  in.StrategyVersion,
		RankingPolicy:    in.RankingPolicy,
		CorrectiveAction: in.CorrectiveAction,
		QualityScore:     roundFloat(in.QualityScore, decimals),
		Evidence:         make([]llmRetrievalEvidence, 0, len(in.Evidence)),
	}
	for _, ev := range in.Evidence {
		out.Evidence = append(out.Evidence, llmRetrievalEvidence{
			Source:          ev.Source,
			Status:          ev.Status,
			AgeSeconds:      ev.AgeSeconds,
			Reliability:     roundFloat(ev.Reliability, decimals),
			Coverage:        roundFloat(ev.Coverage, decimals),
			RetrievalScore:  roundFloat(ev.RetrievalScore, decimals),
			FreshnessWeight: roundFloat(ev.FreshnessWeight, decimals),
			Notes:           dedupeStrings(ev.Notes),
		})
	}
	return out
}

func roundDeterministicFormulaMeta(in llmDeterministicFormulaMeta, decimals int) llmDeterministicFormulaMeta {
	out := in
	out.BlendWeightMarket = roundFloat(in.BlendWeightMarket, decimals)
	out.BlendWeightSynth = roundFloat(in.BlendWeightSynth, decimals)
	out.BlendWeightModel = roundFloat(in.BlendWeightModel, decimals)
	out.BlendWeightLP = roundFloat(in.BlendWeightLP, decimals)
	out.PFinalUp = roundFloat(in.PFinalUp, decimals)
	out.ConfidenceRaw = roundFloat(in.ConfidenceRaw, decimals)
	out.ConfidenceAdj = roundFloat(in.ConfidenceAdj, decimals)
	out.EdgeUp = roundFloat(in.EdgeUp, decimals)
	out.EdgeDown = roundFloat(in.EdgeDown, decimals)
	out.EVUp = roundFloat(in.EVUp, decimals)
	out.EVDown = roundFloat(in.EVDown, decimals)
	out.EVMinThreshold = roundFloat(in.EVMinThreshold, decimals)
	out.KellyRawUp = roundFloat(in.KellyRawUp, decimals)
	out.KellyRawDown = roundFloat(in.KellyRawDown, decimals)
	out.KellyCappedUp = roundFloat(in.KellyCappedUp, decimals)
	out.KellyCappedDown = roundFloat(in.KellyCappedDown, decimals)
	out.SharpeUp = roundFloat(in.SharpeUp, decimals)
	out.SharpeDown = roundFloat(in.SharpeDown, decimals)
	out.BaselineReasonCodes = dedupeStrings(in.BaselineReasonCodes)
	return out
}

func computeDeterministicFormulaMeta(
	market UpDownMarket,
	snapshot llmIndependentSnapshot,
	cfg *config.Config,
) llmDeterministicFormulaMeta {
	out := llmDeterministicFormulaMeta{
		FormulaVersion: "deterministic-v1",
		BlendModel:     "weighted_mean(p_market,p_synth,p_model,p_lp)",
		EVFormula:      "ev = p_win*(1-ask-fee) - (1-p_win)*ask",
		KellyFormula:   "kelly_raw=(p_win-ask)/(1-ask); kelly_capped=kelly_raw*kelly_fraction*confidence_scalers*caps",
		SharpeFormula:  "sharpe = expected_return / stdev(return), binary payoff with fee-adjusted win leg",
	}

	weightMarket := 0.18
	if snapshot.ExecutableAskUp <= 0 || snapshot.ExecutableAskDown <= 0 {
		weightMarket = 0.08
	}
	weightSynth := 0.34
	if snapshot.RiskFlags.SynthStale {
		weightSynth = 0.18
	}
	out.BlendWeightMarket = weightMarket
	out.BlendWeightSynth = weightSynth
	out.BlendWeightModel = 0.30
	out.BlendWeightLP = 0.18

	pFinal, confidenceRaw := blendProbabilities(
		snapshot.PMarketUp,
		snapshot.PSynthUp,
		snapshot.PModelUp,
		snapshot.PLPUp,
		snapshot.ExecutableAskUp,
		snapshot.ExecutableAskDown,
		snapshot.RiskFlags,
	)
	confidenceAdj := upDownClamp(snapshot.Confidence, 0, 1)
	if confidenceAdj <= 0 {
		confidenceAdj = confidenceRaw
	}

	feeFrac := upDownClamp(snapshot.FeesBps/10_000.0, 0, 0.02)
	evUp := expectedValueBuyBinary(pFinal, snapshot.ExecutableAskUp, feeFrac)
	evDown := expectedValueBuyBinary(1-pFinal, snapshot.ExecutableAskDown, feeFrac)
	edgeUp := pFinal - snapshot.ExecutableAskUp
	edgeDown := (1 - pFinal) - snapshot.ExecutableAskDown

	kellyRawUp := kellyRawForBinary(pFinal, snapshot.ExecutableAskUp)
	kellyRawDown := kellyRawForBinary(1-pFinal, snapshot.ExecutableAskDown)
	kellyCappedUp := kellyCappedForBinary(kellyRawUp, confidenceAdj, cfg)
	kellyCappedDown := kellyCappedForBinary(kellyRawDown, confidenceAdj, cfg)

	sharpeUp := sharpeForBinary(pFinal, snapshot.ExecutableAskUp, feeFrac)
	sharpeDown := sharpeForBinary(1-pFinal, snapshot.ExecutableAskDown, feeFrac)

	out.PFinalUp = pFinal
	out.ConfidenceRaw = confidenceRaw
	out.ConfidenceAdj = confidenceAdj
	out.EdgeUp = edgeUp
	out.EdgeDown = edgeDown
	out.EVUp = evUp
	out.EVDown = evDown
	out.EVMinThreshold = snapshot.EVMinThreshold
	out.KellyRawUp = kellyRawUp
	out.KellyRawDown = kellyRawDown
	out.KellyCappedUp = kellyCappedUp
	out.KellyCappedDown = kellyCappedDown
	out.SharpeUp = sharpeUp
	out.SharpeDown = sharpeDown

	decisionReasons := dedupeStrings(append([]string{}, snapshot.ReasonCodes...))
	rec := buildRecommendation(
		snapshot.Timestamp,
		market,
		pFinal,
		evUp,
		evDown,
		snapshot.ExecutableAskUp,
		snapshot.ExecutableAskDown,
		confidenceAdj,
		snapshot.EVMinThreshold,
		snapshot.RiskFlags,
		decisionReasons,
		cfg,
	)
	out.BaselineDecision = rec.Decision
	out.BaselineSide = rec.RecommendedSide
	out.BaselineReasonCodes = dedupeStrings(rec.ReasonCodes)
	return out
}

func kellyRawForBinary(pWin float64, ask float64) float64 {
	if ask <= 0 || ask >= 1 {
		return 0
	}
	raw := (pWin - ask) / math.Max(1-ask, 1e-6)
	return upDownClamp(raw, 0, 1)
}

func kellyCappedForBinary(kellyRaw float64, confidence float64, cfg *config.Config) float64 {
	if cfg == nil {
		return upDownClamp(kellyRaw*0.35*(0.45+0.55*upDownClamp(confidence, 0, 1)), 0, 0.06)
	}
	kelly := kellyRaw * upDownClamp(cfg.Services.UpDownKellyFraction, 0.01, 1.0)
	kelly *= 0.45 + 0.55*upDownClamp(confidence, 0, 1)
	kelly = math.Min(kelly, upDownClamp(cfg.Services.UpDownMaxFractionPerTrade, 0.001, 1.0))
	kelly = math.Min(kelly, upDownClamp(cfg.Services.UpDownAssetExposureCap, 0.001, 1.0))
	kelly *= upDownClamp(cfg.Services.UpDownDailyDrawdownThrottle, 0.2, 1.0)
	return upDownClamp(kelly, 0, upDownClamp(cfg.Services.UpDownMaxFractionPerTrade, 0.001, 1.0))
}

func sharpeForBinary(pWin float64, ask float64, fee float64) float64 {
	if ask <= 0 || ask >= 1 {
		return 0
	}
	pWin = upDownClamp(pWin, 0.01, 0.99)
	winRet := 1 - ask - fee
	lossRet := -ask
	mean := pWin*winRet + (1-pWin)*lossRet
	variance := pWin*math.Pow(winRet-mean, 2) + (1-pWin)*math.Pow(lossRet-mean, 2)
	std := math.Sqrt(math.Max(variance, 1e-8))
	return upDownClamp(mean/std, -3, 3)
}

func (s *UpDownLLMService) proxyAlloraProbability(
	market *UpDownMarket,
	rawP5 float64,
	alloraAgeSeconds int64,
	now time.Time,
) AlloraProxyMeta {
	out := AlloraProxyMeta{
		RawP5:      upDownClamp(rawP5, 0.01, 0.99),
		ProxyP15:   upDownClamp(rawP5, 0.01, 0.99),
		AgeSeconds: maxInt64(alloraAgeSeconds, 0),
	}
	if market == nil {
		out.ProxyStatus = "stale_hard"
		return out
	}
	softLag := int64(maxInt(s.cfg.Services.UpDownLLMAlloraSoftLagSeconds, 380))
	hardLag := int64(maxInt(s.cfg.Services.UpDownLLMAlloraHardLagSeconds, 440))
	if hardLag <= softLag {
		hardLag = softLag + 30
	}
	if market.WindowType == Window5m {
		if out.AgeSeconds <= int64(maxInt(s.cfg.Services.UpDownLLMAlloraFreshMaxSeconds, 60)) {
			out.ProxyStatus = "fresh"
		} else {
			out.ProxyStatus = "stale_hard"
		}
		return out
	}

	elapsed := now.Sub(market.EventStartTime.UTC()).Seconds()
	progress := upDownClamp(elapsed/900.0, 0, 1)
	s5 := 2*out.RawP5 - 1
	horizonDecay := 0.58
	phaseGain := 0.65 + 0.35*progress
	staleDecay := math.Exp(-math.Max(0, float64(out.AgeSeconds-30)) / 180.0)
	proxyScore := s5 * horizonDecay * phaseGain * staleDecay
	out.ProxyP15 = upDownClamp(0.5+0.5*proxyScore, 0.01, 0.99)

	switch {
	case out.AgeSeconds > hardLag:
		out.ProxyStatus = "stale_hard"
	case out.AgeSeconds > softLag:
		out.ProxyStatus = "stale_soft"
	default:
		out.ProxyStatus = "fresh"
	}
	return out
}

func computeEffectiveGuardBlocks(
	market UpDownMarket,
	flags UpDownRiskFlags,
	alloraMeta AlloraProxyMeta,
	freshness LLMContextFreshness,
	cfg *config.Config,
	retrieval llmRetrievalBundle,
	stability LLMSnapshotStability,
) []string {
	blocks := make([]string, 0, 16)
	if !market.IsActiveWindow {
		blocks = append(blocks, "inactive_window")
	}
	if flags.ReadOnly {
		blocks = append(blocks, "read_only")
	}
	if flags.KillSwitch {
		blocks = append(blocks, "kill_switch")
	}
	if flags.DataIntegrityFailed {
		blocks = append(blocks, "data_integrity_failed")
	}
	if flags.StatusBoundary {
		blocks = append(blocks, "status_boundary")
	}
	if flags.WideSpread {
		blocks = append(blocks, "wide_spread")
	}
	if flags.LowLiquidity {
		blocks = append(blocks, "low_liquidity")
	}
	if flags.SynthStale {
		blocks = append(blocks, "synth_stale")
	}
	if flags.MarketStale {
		blocks = append(blocks, "market_stale")
	}
	if flags.SynthMissing {
		blocks = append(blocks, "synth_missing")
	}
	if flags.SourceMismatch {
		blocks = append(blocks, "synth_source_mismatch")
	}

	freshMax := int64(maxInt(cfg.Services.UpDownLLMAlloraFreshMaxSeconds, 60))
	if market.WindowType == Window5m && freshness.AlloraAgeSeconds > freshMax {
		blocks = append(blocks, "allora_missing_or_stale")
	}
	if alloraMeta.ProxyStatus == "stale_hard" {
		blocks = append(blocks, "allora_stale_hard")
	}
	if retrieval.CorrectiveAction == "force_no_trade" {
		blocks = append(blocks, "retrieval_quality_low")
	}
	return dedupeStrings(blocks)
}

func synthFetchReasonCode(prefix string, err error) string {
	if err == nil {
		return ""
	}
	base := strings.ToLower(strings.TrimSpace(prefix))
	if base == "" {
		base = "synth"
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(msg, "no prediction available"):
		return base + "_no_prediction"
	case strings.Contains(msg, "status 404"):
		return base + "_not_found"
	case strings.Contains(msg, "status 401"), strings.Contains(msg, "status 403"):
		return base + "_auth_failed"
	case strings.Contains(msg, "context deadline exceeded"), strings.Contains(msg, "timeout"):
		return base + "_timeout"
	case strings.Contains(msg, "disabled"):
		return base + "_disabled"
	case strings.Contains(msg, "unsupported"):
		return base + "_unsupported"
	default:
		return base + "_fetch_failed"
	}
}

func compactError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	msg = strings.ReplaceAll(msg, "\n", " ")
	if len(msg) > 220 {
		msg = strings.TrimSpace(msg[:220])
	}
	return msg
}

func shouldMarkLLMMarketStale(
	now time.Time,
	market UpDownMarket,
	marketUpdated time.Time,
	marketStaleThreshold time.Duration,
	upAsk float64,
	downAsk float64,
	referenceUpdatedAt *time.Time,
	chainlinkReference bool,
	referenceCurrentFromChainlink bool,
	referenceCurrentPrice *float64,
	prevSignal *UpDownSignal,
) bool {
	if marketUpdated.IsZero() {
		return false
	}
	staleness := now.Sub(marketUpdated)
	if staleness <= marketStaleThreshold {
		return false
	}

	tolerated := market.IsActiveWindow &&
		upAsk > 0 &&
		downAsk > 0 &&
		staleness <= marketStaleThreshold*2

	if referenceUpdatedAt != nil && !referenceUpdatedAt.IsZero() && now.Sub(*referenceUpdatedAt) <= marketStaleThreshold {
		tolerated = true
	}
	if chainlinkReference &&
		referenceCurrentFromChainlink &&
		referenceCurrentPrice != nil &&
		referenceUpdatedAt != nil &&
		!referenceUpdatedAt.IsZero() &&
		now.Sub(*referenceUpdatedAt) <= marketStaleThreshold*2 {
		tolerated = true
	}
	if prevSignal != nil && now.Sub(prevSignal.Timestamp) <= upDownSignalHistoryTTL && !prevSignal.RiskFlags.MarketStale {
		tolerated = true
	}

	return !tolerated
}

func computeEntryMeta(
	market UpDownMarket,
	snapshot llmIndependentSnapshot,
	packet LLMTradePacket,
	alloraMeta AlloraProxyMeta,
	calibration llmClosedLoopCalibration,
) LLMEntryMeta {
	meta := LLMEntryMeta{
		ReadyToBet: false,
		GateReasons: []string{
			"entry_gate_uninitialized",
		},
	}

	side := strings.ToUpper(strings.TrimSpace(packet.RecommendedSide))
	if packet.Decision == "NO_TRADE" || side == "" || side == "NONE" {
		meta.GateReasons = []string{"decision_no_trade"}
		return meta
	}

	ask := snapshot.ExecutableAskUp
	pSide := upDownClamp(snapshot.ConsensusUp, 0.01, 0.99)
	if side == "DOWN" {
		ask = snapshot.ExecutableAskDown
		pSide = upDownClamp(1-snapshot.ConsensusUp, 0.01, 0.99)
	}
	if ask <= 0 {
		meta.GateReasons = []string{"executable_price_missing"}
		return meta
	}
	meta.ProbabilityChosenSide = pSide
	meta.EdgeChosenSide = pSide - ask
	meta.DeviationRatio = snapshot.DeviationRatio
	meta.DeviationZScore = snapshot.DeviationZScore
	meta.CalibrationSamples = calibration.Samples
	meta.CalibrationLLMBrier = upDownClamp(calibration.LLMBrier, 0, 1)
	meta.CalibrationDetBrier = upDownClamp(calibration.DeterministicBrier, 0, 1)
	meta.CalibrationConfidence = upDownClamp(calibration.ConfidenceScale, 0.5, 1.2)
	meta.CalibrationEdgeBuffer = upDownClamp(calibration.EdgeBuffer, -0.01, 0.03)
	feeFrac := upDownClamp(snapshot.FeesBps/10_000.0, 0, 0.02)
	meta.SharpeUp = sharpeForBinary(upDownClamp(snapshot.ConsensusUp, 0.01, 0.99), snapshot.ExecutableAskUp, feeFrac)
	meta.SharpeDown = sharpeForBinary(upDownClamp(1-snapshot.ConsensusUp, 0.01, 0.99), snapshot.ExecutableAskDown, feeFrac)
	if side == "DOWN" {
		meta.SharpeChosenSide = meta.SharpeDown
	} else {
		meta.SharpeChosenSide = meta.SharpeUp
	}

	now := packet.GeneratedAt.UTC()
	windowSeconds := market.EventEndTime.UTC().Sub(market.EventStartTime.UTC()).Seconds()
	if windowSeconds <= 0 {
		windowSeconds = 1
	}
	elapsed := now.Sub(market.EventStartTime.UTC()).Seconds()
	progress := upDownClamp(elapsed/windowSeconds, 0, 1)

	confThreshold := 0.72
	inTrustZone := true
	switch market.WindowType {
	case Window5m:
		confThreshold = 0.76
		if elapsed < 60 || elapsed > 240 {
			inTrustZone = false
		}
		if elapsed >= 210 {
			confThreshold = 0.82
		}
	case Window15m:
		confThreshold = 0.72
		if elapsed < 120 || elapsed > 780 {
			inTrustZone = false
		}
		if elapsed >= 720 {
			confThreshold = 0.78
		}
	default:
		confThreshold = 0.75
	}
	if calibration.Samples >= 40 {
		confThreshold = upDownClamp(confThreshold+(1-upDownClamp(calibration.ConfidenceScale, 0.78, 1.08))*0.16, 0.62, 0.90)
	}
	meta.ConfidenceThresholdUsed = confThreshold
	confidenceForGate := upDownClamp(packet.Confidence*upDownClamp(calibration.ConfidenceScale, 0.78, 1.08), 0, 1)

	freshnessScore := 1.0
	if packet.Freshness.MarketAgeSeconds > 25 {
		freshnessScore *= 0.82
	}
	if packet.Freshness.SynthAgeSeconds > 90 {
		freshnessScore *= 0.78
	}
	switch alloraMeta.ProxyStatus {
	case "fresh":
	case "stale_soft":
		freshnessScore *= 0.72
	case "stale_hard":
		freshnessScore = 0.05
	}

	maxSpread := upDownClamp(0.2, 0.01, 0.2)
	if market.WindowType == Window5m || market.WindowType == Window15m {
		maxSpread = upDownClamp(market.Market.RewardsMaxSpread, 0.01, 0.2)
		if maxSpread <= 0.01 {
			maxSpread = 0.2
		}
	}
	worstSpread := math.Max(snapshot.SpreadUp, snapshot.SpreadDown)
	spreadScore := 1 - upDownClamp(worstSpread/math.Max(maxSpread, 1e-6), 0, 1)
	slipScore := 1 - upDownClamp(snapshot.ExpectedSlippage/0.05, 0, 1)
	minDepth := math.Min(snapshot.UpBuyDepth.FillableSize, snapshot.DownBuyDepth.FillableSize)
	depthDemand := math.Max(1, packet.SuggestedSizeShares*1.25)
	depthScore := upDownClamp(minDepth/depthDemand, 0, 1)
	liquidityScore := upDownClamp(0.45*spreadScore+0.25*slipScore+0.30*depthScore, 0, 1)

	disagreementScore := 1 - upDownClamp(snapshot.Disagreement/0.30, 0, 1)
	deviationQuality := 0.9
	switch {
	case snapshot.DeviationZScore == 0:
		deviationQuality = 0.82
	case snapshot.DeviationZScore < 0.5:
		deviationQuality = 0.75
	case snapshot.DeviationZScore <= 2.0:
		deviationQuality = 1.0
	case snapshot.DeviationZScore <= 3.0:
		deviationQuality = 0.86
	default:
		deviationQuality = 0.68
	}

	minEdge := 0.008
	if market.WindowType == Window5m {
		minEdge = 0.010
	}
	if snapshot.VolatilityAverageForecast >= 80 || snapshot.DeviationZScore >= 2.0 {
		minEdge += 0.005
	}
	minEdge += upDownClamp(calibration.EdgeBuffer, -0.002, 0.012)
	if progress >= 0.75 {
		minEdge += 0.003
	}
	edgeScore := upDownClamp((meta.EdgeChosenSide-minEdge+0.02)/0.05, 0, 1)
	sharpeThreshold := 0.08
	if snapshot.VolatilityAverageForecast >= 80 {
		sharpeThreshold = 0.12
	}
	if calibration.Samples >= 40 && calibration.LLMBrier > 0.24 {
		sharpeThreshold += 0.03
	}
	sharpeScore := upDownClamp((meta.SharpeChosenSide-sharpeThreshold+0.22)/0.45, 0, 1)

	retrievalScore := upDownClamp(snapshot.Retrieval.QualityScore, 0, 1)
	meta.EntryScore = upDownClamp(
		0.25*confidenceForGate+
			0.16*retrievalScore+
			0.14*freshnessScore+
			0.13*liquidityScore+
			0.12*edgeScore+
			0.08*deviationQuality+
			0.06*disagreementScore+
			0.06*sharpeScore,
		0, 1,
	)

	nEff := 12 + 108*upDownClamp(confidenceForGate, 0.15, 1.0)*upDownClamp(retrievalScore, 0.2, 1.0)*upDownClamp(disagreementScore, 0.2, 1.0)
	sigma := math.Sqrt((pSide * (1 - pSide)) / math.Max(nEff, 1))
	meta.ConfidenceLB90 = upDownClamp(pSide-1.2815515655*sigma, 0, 1)
	meta.ConfidenceLB95 = upDownClamp(pSide-1.6448536269*sigma, 0, 1)

	priceBuffer := 0.002 + 0.5*worstSpread + 0.5*snapshot.ExpectedSlippage
	if snapshot.VolatilityAverageForecast >= 80 {
		priceBuffer += 0.003
	}
	ciPass := meta.ConfidenceLB90 > (ask + priceBuffer)
	if snapshot.VolatilityAverageForecast >= 80 || alloraMeta.ProxyStatus == "stale_soft" {
		ciPass = meta.ConfidenceLB95 > (ask + priceBuffer)
	}

	reasons := make([]string, 0, 12)
	if !inTrustZone {
		reasons = append(reasons, "outside_trust_zone")
	}
	if confidenceForGate < confThreshold {
		reasons = append(reasons, "confidence_below_threshold")
	}
	if meta.EdgeChosenSide < minEdge {
		reasons = append(reasons, "edge_below_threshold")
	}
	if !ciPass {
		reasons = append(reasons, "confidence_interval_edge_fail")
	}
	if packet.ExpectedValue <= snapshot.EVMinThreshold {
		reasons = append(reasons, "ev_below_dynamic_threshold")
	}
	if meta.SharpeChosenSide < sharpeThreshold {
		reasons = append(reasons, "sharpe_below_threshold")
	}
	if len(packet.EffectiveGuardBlocks) > 0 {
		reasons = append(reasons, "effective_guard_blocks_active")
	}
	if market.WindowType == Window5m && alloraMeta.ProxyStatus != "fresh" {
		reasons = append(reasons, "allora_not_fresh_for_5m")
	}
	if market.WindowType == Window15m && alloraMeta.ProxyStatus == "stale_hard" {
		reasons = append(reasons, "allora_stale_hard")
	}
	if snapshot.Retrieval.CorrectiveAction == "force_no_trade" {
		reasons = append(reasons, "retrieval_force_no_trade")
	}
	if snapshot.Retrieval.CorrectiveAction == "degrade_confidence" && meta.EntryScore < 0.82 {
		reasons = append(reasons, "retrieval_quality_degraded")
	}
	if calibration.Samples >= 40 && calibration.LLMBrier > calibration.DeterministicBrier+0.015 {
		reasons = append(reasons, "closed_loop_calibration_penalty")
	}

	entryFloor := 0.78
	if calibration.Samples >= 40 && calibration.LLMBrier > 0.24 {
		entryFloor = 0.82
	}
	meta.ReadyToBet =
		len(reasons) == 0 &&
			meta.EntryScore >= entryFloor &&
			packet.Decision != "NO_TRADE" &&
			packet.RecommendedSide != "NONE" &&
			len(packet.EffectiveGuardBlocks) == 0

	if meta.ReadyToBet {
		meta.GateReasons = []string{"entry_gate_pass"}
	} else {
		meta.GateReasons = dedupeStrings(reasons)
		if len(meta.GateReasons) == 0 {
			meta.GateReasons = []string{"entry_gate_fail"}
		}
	}
	return meta
}

func (s *UpDownLLMService) packetCacheKey(market UpDownMarket, contextHash string) string {
	return fmt.Sprintf("%s%s:%d:%s", upDownLLMCachePrefix, market.Slug, market.EventStartTime.UTC().Unix(), contextHash)
}

func (s *UpDownLLMService) latestPacketCacheKey(market UpDownMarket) string {
	return fmt.Sprintf("%s%s:%d", upDownLLMLatestPref, market.Slug, market.EventStartTime.UTC().Unix())
}

func (s *UpDownLLMService) readPacketCache(ctx context.Context, key string) *LLMTradePacket {
	if s.redis == nil || key == "" {
		return nil
	}
	raw, err := s.redis.Get(ctx, key).Bytes()
	if err != nil || len(raw) == 0 {
		return nil
	}
	var packet LLMTradePacket
	if err := json.Unmarshal(raw, &packet); err != nil {
		return nil
	}
	return &packet
}

func (s *UpDownLLMService) writePacketCache(ctx context.Context, key string, latestKey string, packet LLMTradePacket) {
	if s.redis == nil || key == "" || latestKey == "" {
		return
	}
	ttl := time.Duration(maxInt(s.cfg.Services.UpDownLLMCacheTTLSeconds, 30)) * time.Second
	payload, err := json.Marshal(packet)
	if err != nil {
		return
	}
	pipe := s.redis.Pipeline()
	pipe.Set(ctx, key, payload, ttl)
	pipe.Set(ctx, latestKey, payload, ttl)
	_, _ = pipe.Exec(ctx)
}

func (s *UpDownLLMService) persistPacket(ctx context.Context, market UpDownMarket, contextData llmStructuredContext, packet LLMTradePacket) error {
	if s.db == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()

	packetPayload, err := json.Marshal(packet)
	if err != nil {
		return err
	}
	contextPayload, err := json.Marshal(contextData)
	if err != nil {
		return err
	}

	if err := s.db.WithContext(ctx).Exec(`
		INSERT INTO updown_llm_packets (
			slug, condition_id, asset, window_type, event_start_time,
			context_hash, prompt_hash, model,
			decision, recommended_side, confidence, expected_value,
			suggested_limit_price, suggested_size_shares, suggested_notional,
			effective_guard_blocks, freshness_payload, allora_proxy_payload, trace_payload,
			packet_payload, context_payload, created_at, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, ?::jsonb, ?::jsonb, ?::jsonb, ?::jsonb, NOW(), NOW() + (? || ' seconds')::interval)
	`,
		packet.Slug,
		packet.ConditionID,
		packet.Asset,
		string(packet.WindowType),
		market.EventStartTime.UTC(),
		packet.Freshness.ContextHash,
		packet.Trace.PromptHash,
		packet.Trace.Model,
		packet.Decision,
		packet.RecommendedSide,
		packet.Confidence,
		packet.ExpectedValue,
		packet.SuggestedLimitPrice,
		packet.SuggestedSizeShares,
		packet.SuggestedNotional,
		toJSONString(packet.EffectiveGuardBlocks),
		toJSONString(packet.Freshness),
		toJSONString(packet.AlloraProxy),
		toJSONString(packet.Trace),
		string(packetPayload),
		string(contextPayload),
		fmt.Sprintf("%d", maxInt(s.cfg.Services.UpDownLLMCacheTTLSeconds, 30)),
	).Error; err != nil {
		return err
	}
	_ = s.syncLLMShadowDaily(ctx, market)
	return nil
}

func (s *UpDownLLMService) readLatestPersistedPacket(ctx context.Context, slug string) (*LLMTradePacket, error) {
	if s.db == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
	defer cancel()

	var row struct {
		PacketPayload string `gorm:"column:packet_payload"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT packet_payload
		FROM updown_llm_packets
		WHERE slug = ?
		ORDER BY created_at DESC
		LIMIT 1
	`, slug).Scan(&row).Error; err != nil {
		return nil, err
	}
	if strings.TrimSpace(row.PacketPayload) == "" {
		return nil, nil
	}
	var packet LLMTradePacket
	if err := json.Unmarshal([]byte(row.PacketPayload), &packet); err != nil {
		return nil, err
	}
	return &packet, nil
}

func (s *UpDownLLMService) recordAlloraFetch(err error) {
	s.metaMu.Lock()
	defer s.metaMu.Unlock()
	now := time.Now().UTC()
	s.lastAlloraFetchAt = &now
	if err != nil {
		s.lastAlloraError = err.Error()
		return
	}
	s.lastAlloraError = ""
}

func alloraInferenceCacheKey(asset string, timeframe allora.PriceInferenceTimeframe) string {
	return strings.ToUpper(strings.TrimSpace(asset)) + ":" + strings.ToLower(strings.TrimSpace(string(timeframe)))
}

func (s *UpDownLLMService) fetchAlloraInferenceWithSmoothing(
	ctx context.Context,
	asset string,
	timeframe allora.PriceInferenceTimeframe,
) (*allora.PriceInference, bool, error) {
	asset = strings.ToUpper(strings.TrimSpace(asset))
	key := alloraInferenceCacheKey(asset, timeframe)
	now := time.Now().UTC()

	inf, err := s.allora.GetPriceInference(ctx, asset, timeframe)
	if err == nil && inf != nil {
		s.alloraCacheMu.Lock()
		if cached, ok := s.lastGoodAlloraByKey[key]; ok && cached.Inference.Timestamp.After(inf.Timestamp) {
			copyInference := cached.Inference
			s.alloraCacheMu.Unlock()
			return &copyInference, true, nil
		}
		s.lastGoodAlloraByKey[key] = alloraInferenceCacheValue{
			Inference: *inf,
			FetchedAt: now,
		}
		s.alloraCacheMu.Unlock()
		return inf, false, nil
	}

	s.alloraCacheMu.RLock()
	cached, ok := s.lastGoodAlloraByKey[key]
	s.alloraCacheMu.RUnlock()
	if ok {
		age := now.Sub(cached.Inference.Timestamp.UTC())
		if age <= upDownLLMAlloraLastGoodMaxAge {
			copyInference := cached.Inference
			return &copyInference, true, nil
		}
	}
	return nil, false, err
}

func (s *UpDownLLMService) smoothAlloraProbability(
	asset string,
	window UpDownWindowType,
	rawP5 float64,
	timestamp time.Time,
) float64 {
	key := strings.ToUpper(strings.TrimSpace(asset)) + ":" + strings.ToLower(strings.TrimSpace(string(window)))
	raw := upDownClamp(rawP5, 0.01, 0.99)
	s.alloraCacheMu.Lock()
	defer s.alloraCacheMu.Unlock()
	prev, ok := s.lastAlloraP5ByKey[key]
	if !ok || timestamp.IsZero() {
		s.lastAlloraP5ByKey[key] = alloraProbabilityCacheValue{
			Value:     raw,
			Timestamp: timestamp.UTC(),
		}
		return raw
	}
	if !prev.Timestamp.IsZero() && timestamp.Sub(prev.Timestamp) > 4*time.Minute {
		s.lastAlloraP5ByKey[key] = alloraProbabilityCacheValue{
			Value:     raw,
			Timestamp: timestamp.UTC(),
		}
		return raw
	}
	smoothed := 0.72*raw + 0.28*upDownClamp(prev.Value, 0.01, 0.99)
	smoothed = upDownClamp(smoothed, 0.01, 0.99)
	s.lastAlloraP5ByKey[key] = alloraProbabilityCacheValue{
		Value:     smoothed,
		Timestamp: timestamp.UTC(),
	}
	return smoothed
}

func llmCalibrationKey(asset string, window UpDownWindowType) string {
	return strings.ToUpper(strings.TrimSpace(asset)) + ":" + strings.ToLower(strings.TrimSpace(string(window)))
}

func (s *UpDownLLMService) getClosedLoopCalibration(ctx context.Context, market UpDownMarket) llmClosedLoopCalibration {
	base := llmClosedLoopCalibration{
		ConfidenceScale: 1.0,
		EdgeBuffer:      0,
		UpdatedAt:       time.Now().UTC(),
	}
	if s == nil || s.db == nil {
		return base
	}

	key := llmCalibrationKey(market.Asset, market.WindowType)
	now := time.Now().UTC()
	s.calibrationMu.RLock()
	cached, hasCached := s.calibrationByKey[key]
	fetchedAt := s.calibrationFetchedAt[key]
	s.calibrationMu.RUnlock()
	if hasCached && now.Sub(fetchedAt) <= upDownLLMCalibrationTTL {
		return cached
	}

	queryCtx := ctx
	if queryCtx == nil {
		queryCtx = context.Background()
	}
	queryCtx, cancel := context.WithTimeout(queryCtx, 1600*time.Millisecond)
	defer cancel()

	var row struct {
		Samples            int64   `gorm:"column:samples"`
		DeterministicBrier float64 `gorm:"column:deterministic_brier"`
		LLMBrier           float64 `gorm:"column:llm_brier"`
		EVDelta            float64 `gorm:"column:ev_delta"`
	}
	err := s.db.WithContext(queryCtx).Raw(`
		WITH latest_llm AS (
			SELECT DISTINCT ON (slug, event_start_time)
				slug,
				event_start_time,
				decision,
				recommended_side,
				confidence,
				expected_value
			FROM updown_llm_packets
			WHERE asset = ?
			  AND window_type = ?
			  AND created_at >= NOW() - INTERVAL '21 days'
			ORDER BY slug, event_start_time, created_at DESC
		)
		SELECT
			COALESCE(COUNT(*), 0) AS samples,
			COALESCE(AVG(
				CASE
					WHEN mw.p_final_up IS NULL THEN NULL
					WHEN mw.resolved_outcome = 'UP' THEN POWER(mw.p_final_up - 1.0, 2)
					WHEN mw.resolved_outcome = 'DOWN' THEN POWER(mw.p_final_up - 0.0, 2)
					ELSE POWER(mw.p_final_up - 0.5, 2)
				END
			), 0) AS deterministic_brier,
			COALESCE(AVG(
				CASE
					WHEN llm.slug IS NULL THEN NULL
					ELSE POWER(
						CASE
							WHEN llm.recommended_side = 'UP' THEN llm.confidence
							WHEN llm.recommended_side = 'DOWN' THEN 1.0 - llm.confidence
							ELSE 0.5
						END -
						CASE
							WHEN mw.resolved_outcome = 'UP' THEN 1.0
							WHEN mw.resolved_outcome = 'DOWN' THEN 0.0
							ELSE 0.5
						END
					, 2)
				END
			), 0) AS llm_brier,
			COALESCE(SUM(
				CASE
					WHEN llm.decision IN ('BUY_UP', 'BUY_DOWN') THEN llm.expected_value
					ELSE 0
				END
			), 0) - COALESCE(SUM(
				CASE
					WHEN mw.recommendation_decision IN ('BUY_UP', 'BUY_DOWN') THEN mw.recommendation_expected_value
					ELSE 0
				END
			), 0) AS ev_delta
		FROM updown_market_windows mw
		LEFT JOIN latest_llm llm
			ON llm.slug = mw.slug
		   AND llm.event_start_time = mw.event_start_time
		WHERE mw.asset = ?
		  AND mw.window_type = ?
		  AND mw.event_start_time >= NOW() - INTERVAL '21 days'
		  AND mw.resolved_outcome IN ('UP', 'DOWN', 'FLAT')
	`,
		strings.ToUpper(strings.TrimSpace(market.Asset)),
		string(market.WindowType),
		strings.ToUpper(strings.TrimSpace(market.Asset)),
		string(market.WindowType),
	).Scan(&row).Error
	if err != nil {
		s.calibrationMu.Lock()
		s.calibrationFetchedAt[key] = now
		s.calibrationMu.Unlock()
		return base
	}

	calibration := llmClosedLoopCalibration{
		Samples:            maxInt64(row.Samples, 0),
		LLMBrier:           upDownClamp(row.LLMBrier, 0, 1),
		DeterministicBrier: upDownClamp(row.DeterministicBrier, 0, 1),
		EVDelta:            row.EVDelta,
		UpdatedAt:          now,
		ConfidenceScale:    1.0,
		EdgeBuffer:         0,
	}
	if calibration.Samples >= 40 {
		delta := calibration.LLMBrier - calibration.DeterministicBrier
		switch {
		case delta >= 0.035:
			calibration.ConfidenceScale = 0.82
			calibration.EdgeBuffer = 0.006
		case delta >= 0.02:
			calibration.ConfidenceScale = 0.90
			calibration.EdgeBuffer = 0.003
		case delta <= -0.02:
			calibration.ConfidenceScale = 1.05
			calibration.EdgeBuffer = -0.001
		case delta <= -0.01:
			calibration.ConfidenceScale = 1.02
			calibration.EdgeBuffer = 0
		default:
			calibration.ConfidenceScale = 1.0
			calibration.EdgeBuffer = 0
		}
	}
	calibration.ConfidenceScale = upDownClamp(calibration.ConfidenceScale, 0.78, 1.08)
	calibration.EdgeBuffer = upDownClamp(calibration.EdgeBuffer, -0.002, 0.012)

	s.calibrationMu.Lock()
	s.calibrationByKey[key] = calibration
	s.calibrationFetchedAt[key] = now
	s.calibrationMu.Unlock()
	return calibration
}

func (s *UpDownLLMService) syncLLMShadowDaily(ctx context.Context, market UpDownMarket) error {
	if s == nil || s.db == nil {
		return nil
	}
	day := market.EventStartTime.UTC().Truncate(24 * time.Hour)
	dayEnd := day.Add(24 * time.Hour)
	asset := strings.ToUpper(strings.TrimSpace(market.Asset))
	window := string(market.WindowType)

	queryCtx := ctx
	if queryCtx == nil {
		queryCtx = context.Background()
	}
	queryCtx, cancel := context.WithTimeout(queryCtx, 1600*time.Millisecond)
	defer cancel()

	return s.db.WithContext(queryCtx).Exec(`
		WITH latest_llm AS (
			SELECT DISTINCT ON (slug, event_start_time)
				slug,
				event_start_time,
				decision,
				recommended_side,
				confidence,
				expected_value
			FROM updown_llm_packets
			WHERE asset = ?
			  AND window_type = ?
			  AND event_start_time >= ?
			  AND event_start_time < ?
			ORDER BY slug, event_start_time, created_at DESC
		),
		stats AS (
			SELECT
				COALESCE(COUNT(*), 0) AS windows_count,
				COALESCE(SUM(
					CASE
						WHEN mw.recommendation_decision IN ('BUY_UP', 'BUY_DOWN')
						THEN mw.recommendation_expected_value
						ELSE 0
					END
				), 0) AS deterministic_ev,
				COALESCE(SUM(
					CASE
						WHEN llm.decision IN ('BUY_UP', 'BUY_DOWN')
						THEN llm.expected_value
						ELSE 0
					END
				), 0) AS llm_ev,
				COALESCE(AVG(
					CASE
						WHEN mw.p_final_up IS NULL THEN NULL
						WHEN mw.resolved_outcome = 'UP' THEN POWER(mw.p_final_up - 1.0, 2)
						WHEN mw.resolved_outcome = 'DOWN' THEN POWER(mw.p_final_up - 0.0, 2)
						ELSE POWER(mw.p_final_up - 0.5, 2)
					END
				), 0) AS deterministic_brier,
				COALESCE(AVG(
					CASE
						WHEN llm.slug IS NULL THEN NULL
						ELSE POWER(
							CASE
								WHEN llm.recommended_side = 'UP' THEN llm.confidence
								WHEN llm.recommended_side = 'DOWN' THEN 1.0 - llm.confidence
								ELSE 0.5
							END -
							CASE
								WHEN mw.resolved_outcome = 'UP' THEN 1.0
								WHEN mw.resolved_outcome = 'DOWN' THEN 0.0
								ELSE 0.5
							END
						, 2)
					END
				), 0) AS llm_brier
			FROM updown_market_windows mw
			LEFT JOIN latest_llm llm
				ON llm.slug = mw.slug
			   AND llm.event_start_time = mw.event_start_time
			WHERE mw.event_start_time >= ?
			  AND mw.event_start_time < ?
			  AND mw.asset = ?
			  AND mw.window_type = ?
			  AND mw.resolved_outcome IN ('UP', 'DOWN', 'FLAT')
		)
		INSERT INTO updown_llm_shadow_daily (
			day, asset, window_type, windows_count,
			deterministic_ev, llm_ev, ev_delta,
			deterministic_brier, llm_brier, brier_delta,
			metadata, created_at, updated_at
		)
		SELECT
			DATE(?),
			?,
			?,
			stats.windows_count,
			stats.deterministic_ev,
			stats.llm_ev,
			stats.llm_ev - stats.deterministic_ev,
			stats.deterministic_brier,
			stats.llm_brier,
			stats.llm_brier - stats.deterministic_brier,
			jsonb_build_object('source', 'updown_market_windows+updown_llm_packets'),
			NOW(),
			NOW()
		FROM stats
		ON CONFLICT (day, asset, window_type)
		DO UPDATE SET
			windows_count = EXCLUDED.windows_count,
			deterministic_ev = EXCLUDED.deterministic_ev,
			llm_ev = EXCLUDED.llm_ev,
			ev_delta = EXCLUDED.ev_delta,
			deterministic_brier = EXCLUDED.deterministic_brier,
			llm_brier = EXCLUDED.llm_brier,
			brier_delta = EXCLUDED.brier_delta,
			metadata = EXCLUDED.metadata,
			updated_at = NOW()
	`,
		asset, window, day, dayEnd,
		day, dayEnd, asset, window,
		day, asset, window,
	).Error
}

func probabilityUpFromInference(inf *allora.PriceInference, threshold float64) float64 {
	if inf == nil || threshold <= 0 {
		return 0.5
	}
	median := inf.NetworkInference
	sigma := 0.0

	if len(inf.ConfidenceIntervalPercentiles) == len(inf.ConfidenceIntervalValues) && len(inf.ConfidenceIntervalValues) >= 2 {
		pairs := make([]percentileValuePair, 0, len(inf.ConfidenceIntervalValues))
		for i := 0; i < len(inf.ConfidenceIntervalValues); i++ {
			pairs = append(pairs, percentileValuePair{
				pct: inf.ConfidenceIntervalPercentiles[i],
				val: inf.ConfidenceIntervalValues[i],
			})
		}
		sort.SliceStable(pairs, func(i, j int) bool { return pairs[i].pct < pairs[j].pct })
		median = nearestPercentileValue(pairs, 50, inf.NetworkInference)
		p84 := nearestPercentileValue(pairs, 84.13, median)
		p16 := nearestPercentileValue(pairs, 15.87, median)
		sigma = math.Abs(p84-p16) / 2.0
		if sigma <= 0 {
			p97 := nearestPercentileValue(pairs, 97.72, median)
			p2 := nearestPercentileValue(pairs, 2.28, median)
			sigma = math.Abs(p97-p2) / 4.0
		}
	}

	if sigma <= 1e-9 {
		if median >= threshold {
			return 0.75
		}
		return 0.25
	}

	z := (threshold - median) / sigma
	p := 1 - normalCDF(z)
	return upDownClamp(p, 0.01, 0.99)
}

func nearestPercentileValue(pairs []percentileValuePair, target float64, fallback float64) float64 {
	if len(pairs) == 0 {
		return fallback
	}
	best := fallback
	bestDiff := math.MaxFloat64
	for _, item := range pairs {
		diff := math.Abs(item.pct - target)
		if diff < bestDiff {
			bestDiff = diff
			best = item.val
		}
	}
	return best
}

func decodeStrictLLMResponse(rawContent string) (llmResponseRaw, error) {
	content := strings.TrimSpace(rawContent)
	if content == "" {
		return llmResponseRaw{}, fmt.Errorf("llm output is empty")
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return llmResponseRaw{}, fmt.Errorf("llm output is not valid json: %w", err)
	}
	if len(payload) == 0 {
		return llmResponseRaw{}, fmt.Errorf("llm output is empty object")
	}
	for key := range payload {
		if _, ok := llmResponseSchemaKeys[key]; !ok {
			return llmResponseRaw{}, fmt.Errorf("llm output has unknown field %q", key)
		}
	}
	for key := range llmResponseRequiredKeys {
		if _, ok := payload[key]; !ok {
			return llmResponseRaw{}, fmt.Errorf("llm output missing required field %q", key)
		}
	}
	var raw llmResponseRaw
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return llmResponseRaw{}, fmt.Errorf("llm output decode failed: %w", err)
	}
	return raw, nil
}

func validateLLMResponseRaw(raw llmResponseRaw) error {
	raw.Decision = strings.ToUpper(strings.TrimSpace(raw.Decision))
	switch raw.Decision {
	case "BUY_UP", "BUY_DOWN", "NO_TRADE":
	default:
		return fmt.Errorf("llm decision is invalid")
	}
	raw.RecommendedSide = strings.ToUpper(strings.TrimSpace(raw.RecommendedSide))
	switch raw.RecommendedSide {
	case "UP", "DOWN", "NONE":
	default:
		return fmt.Errorf("llm recommended_side is invalid")
	}
	if (raw.Decision == "BUY_UP" && raw.RecommendedSide != "UP") ||
		(raw.Decision == "BUY_DOWN" && raw.RecommendedSide != "DOWN") ||
		(raw.Decision == "NO_TRADE" && raw.RecommendedSide != "NONE") {
		return fmt.Errorf("llm decision and recommended_side mismatch")
	}
	if math.IsNaN(raw.Confidence) || math.IsInf(raw.Confidence, 0) || raw.Confidence < 0 || raw.Confidence > 1 {
		return fmt.Errorf("llm confidence is invalid")
	}
	if math.IsNaN(raw.ExpectedValue) || math.IsInf(raw.ExpectedValue, 0) {
		return fmt.Errorf("llm expected_value is invalid")
	}
	if math.IsNaN(raw.SuggestedLimitPrice) || math.IsInf(raw.SuggestedLimitPrice, 0) {
		return fmt.Errorf("llm suggested_limit_price is invalid")
	}
	if math.IsNaN(raw.SuggestedSizeShares) || math.IsInf(raw.SuggestedSizeShares, 0) {
		return fmt.Errorf("llm suggested_size_shares is invalid")
	}
	if math.IsNaN(raw.SuggestedNotional) || math.IsInf(raw.SuggestedNotional, 0) {
		return fmt.Errorf("llm suggested_notional is invalid")
	}
	if raw.SuggestedSizeShares < 0 || raw.SuggestedNotional < 0 {
		return fmt.Errorf("llm size fields must be >= 0")
	}
	if raw.Decision != "NO_TRADE" && (raw.SuggestedLimitPrice < 0.01 || raw.SuggestedLimitPrice > 0.99) {
		return fmt.Errorf("llm suggested_limit_price must be in [0.01,0.99] for trade decisions")
	}
	if raw.Decision == "NO_TRADE" && (raw.SuggestedSizeShares > 0 || raw.SuggestedNotional > 0) {
		return fmt.Errorf("llm NO_TRADE must not include non-zero size")
	}
	if len(raw.ReasonCodes) == 0 {
		return fmt.Errorf("llm reason_codes is required")
	}
	minReasons := 2
	if raw.Decision != "NO_TRADE" {
		minReasons = 3
	}
	if len(raw.ReasonCodes) < minReasons {
		return fmt.Errorf("llm reason_codes requires at least %d items", minReasons)
	}
	seenReasons := map[string]struct{}{}
	for _, reason := range raw.ReasonCodes {
		code := sanitizeReasonCode(reason)
		if code == "unknown" {
			return fmt.Errorf("llm reason_codes contains empty/invalid value")
		}
		if len(code) > 64 {
			return fmt.Errorf("llm reason_code exceeds max length")
		}
		if _, exists := seenReasons[code]; exists {
			return fmt.Errorf("llm reason_codes contains duplicates")
		}
		seenReasons[code] = struct{}{}
	}
	for _, condition := range raw.InvalidationConditions {
		trimmed := strings.TrimSpace(condition)
		if trimmed == "" {
			return fmt.Errorf("llm invalidation_conditions contains empty value")
		}
		if len(trimmed) > 220 {
			return fmt.Errorf("llm invalidation_condition exceeds max length")
		}
	}
	return nil
}

func normalizeLLMResponse(raw llmResponseRaw) LLMTradePacket {
	decision := strings.ToUpper(strings.TrimSpace(raw.Decision))
	side := strings.ToUpper(strings.TrimSpace(raw.RecommendedSide))
	if decision == "BUY_UP" && side != "UP" {
		side = "UP"
	}
	if decision == "BUY_DOWN" && side != "DOWN" {
		side = "DOWN"
	}
	if decision == "NO_TRADE" {
		side = "NONE"
	}
	return LLMTradePacket{
		Decision:               decision,
		RecommendedSide:        side,
		Confidence:             upDownClamp(raw.Confidence, 0, 1),
		ExpectedValue:          raw.ExpectedValue,
		SuggestedLimitPrice:    raw.SuggestedLimitPrice,
		SuggestedSizeShares:    raw.SuggestedSizeShares,
		SuggestedNotional:      raw.SuggestedNotional,
		ReasonCodes:            dedupeStrings(raw.ReasonCodes),
		InvalidationConditions: dedupeStrings(raw.InvalidationConditions),
		EffectiveGuardBlocks:   []string{},
	}
}

func ensurePacketInvalidationConditions(packet *LLMTradePacket, market UpDownMarket) {
	if packet == nil {
		return
	}
	defaults := defaultLLMInvalidations(market, packet.RecommendedSide)
	if len(packet.InvalidationConditions) == 0 {
		packet.InvalidationConditions = defaults
		return
	}
	if packet.Decision != "NO_TRADE" && len(packet.InvalidationConditions) < 2 {
		packet.InvalidationConditions = dedupeStrings(append(packet.InvalidationConditions, defaults...))
	}
}

func upDownLLMSystemPrompt() string {
	return strings.TrimSpace(`
You are Bankai UpDown LLM Engine.
You are a volatility-first directional engine for short-horizon binary markets.
Return JSON only. No markdown. No prose.

Schema:
{
  "decision": "BUY_UP|BUY_DOWN|NO_TRADE",
  "recommended_side": "UP|DOWN|NONE",
  "confidence": number,
  "expected_value": number,
  "suggested_limit_price": number,
  "suggested_size_shares": number,
  "suggested_notional": number,
  "reason_codes": [string],
  "invalidation_conditions": [string]
}

Core objective:
- Maximize risk-adjusted directional accuracy for the current market window.
- Default to an actionable side (BUY_UP or BUY_DOWN) when no hard block exists and at least one side has positive net edge after costs.
- Use NO_TRADE only when hard gates fail or both sides are non-positive/invalid after penalties.
- Treat this as a fast-moving relative-price market:
  - Outcome is determined by end-window price versus start price.
  - Use start_price, current_price, event_end_unix, and time_to_end_seconds to infer directional path-to-close.
  - In short windows, momentum and volatility can dominate; reason explicitly about projected move by expiry.

Hard constraints (must obey):
1) Use only provided context JSON.
2) No external facts, no assumptions beyond context.
3) Respect guards/risk flags/retrieval corrective action/allora status as hard execution constraints.
4) If any hard block exists, output NO_TRADE.
5) Keep confidence in [0,1].
6) Do not emit fields outside schema.
7) For BUY decisions, suggested_limit_price must be in [0.01,0.99].
8) For NO_TRADE, set recommended_side=NONE and size/notional to 0.
9) Always provide invalidation_conditions with at least 2 concise items (even for NO_TRADE, phrase as "re-entry conditions").

Data-source contract (explicit trust policy):
- Context is assembled at generation-time from internal connectors:
  market/orderbook microstructure + Synth analytics + Allora consumer API.
- data_semantics.synth explains Synth role and reliability rules.
- data_semantics.allora explains mandatory Allora role, freshness, and 15m proxy behavior.
- data_semantics.retrieval explains ranking and corrective actions.
- deterministic_baseline contains deterministic engine formulas and metrics for parity checks.
- snapshot_stability reports whether generation context passed multi-snapshot drift checks.
- Treat retrieval quality and source ages as first-class confidence controls.
- Never treat a single source as infallible; fuse evidence using freshness + coverage + consistency.

Synth interpretation rules:
- p_synth_up / p_model_up / p_lp_up / p_market_up are directional estimates for this exact window context.
- volatility_average_forecast / volatility_average_past are volatility priors and must be used before selecting size/confidence.
- In high volatility, require stronger edge and better microstructure before trading.
- If synth window mismatch or stale signals appear, abstain.

Allora interpretation rules:
- allora.raw_p5 is a short-horizon prior from 5m topic inference.
- allora.proxy_p15 is deterministic-mapped for 15m; status controls trust:
  - fresh: usable
  - stale_soft: confidence penalty
  - stale_hard: hard NO_TRADE
- For 5m windows, non-fresh Allora is not execution-grade.
- For 5m windows with fresh Allora, treat Allora as a high-importance directional prior.
- Use allora.network_inference and allora.confidence_interval_values/percentiles to sanity-check directional confidence and uncertainty.

Deterministic baseline usage:
- deterministic_baseline is not an override, but a parity anchor.
- Use it to detect drift between baseline EV/edge/sharpe and your final recommendation.
- Large disagreement with weak supporting evidence should push toward NO_TRADE.

Institutional decision protocol:
Step A: Hard gate
- If retrieval.corrective_action = force_no_trade -> NO_TRADE.
- If any critical guard/risk block indicates stale/integrity/liquidity boundary failure -> NO_TRADE.
- If allora.status = stale_hard -> NO_TRADE.
- If key references (start/current prices) are missing for decision quality -> NO_TRADE.

Step B: Build directional edges
- edge_up = p_up_consensus - executable_ask_up
- edge_down = (1 - p_up_consensus) - executable_ask_down
- p_up_consensus should be inferred from p_market/p_synth/p_model/p_lp with volatility-aware weighting:
  - In high volatility: upweight microstructure + lp/market confirmation, downweight weak model signals.
  - In low volatility: balanced use of synth/model/market/lp.

Step C: Microstructure quality
- Penalize wide spread, high slippage, thin depth, and severe bid/ask asymmetry.
- Penalize setups where executable depth cannot support suggested size.
- Prefer NO_TRADE when top-of-book or depth evidence is weak/contradictory.

Step D: Disagreement and uncertainty control
- If p_market, p_synth, p_model, p_lp materially disagree, reduce confidence sharply.
- If evidence is stale, contradictory, or low-coverage, reduce confidence or abstain.
- If volatility regime is high, require stronger net edge to trade.
- Cross-model disagreement alone is not a hard NO_TRADE when one side still has clear positive net edge.
- Snapshot instability is advisory context, not an automatic blocker by itself.

Step E: Trade selection
- Choose BUY_UP only if UP edge remains positive after spread/slippage/liquidity penalties.
- Choose BUY_DOWN only if DOWN edge remains positive after spread/slippage/liquidity penalties.
- If both edges are weak/negative/close, output NO_TRADE.
- For very short remaining time, prefer the side whose probability/edge is supported by both recent direction (start->current) and Synth/Allora priors.

Step F: Output discipline
- reason_codes: concise, traceable to observed evidence; include at least 3 when trading, at least 2 when NO_TRADE.
- invalidation_conditions: explicit cancel conditions tied to spread, staleness, edge decay, and time-to-end.
- suggested_limit_price should align with executable ask of chosen side, bounded for safety.
- suggested_size_shares and suggested_notional must be conservative under high volatility or weak depth.

Preferred reason_code taxonomy (use these whenever applicable):
- volatility_high
- volatility_regime_transition
- microstructure_support_up
- microstructure_support_down
- orderbook_thin
- spread_wide
- slippage_elevated
- cross_model_disagreement
- retrieval_quality_degraded
- retrieval_quality_low
- allora_proxy_fresh
- allora_proxy_stale_soft
- allora_proxy_stale_hard
- edge_up_positive
- edge_down_positive
- edge_below_threshold
- sharpe_below_threshold
- confidence_guard
- staleness_guard
- liquidity_guard
- boundary_guard

Abstention policy:
- NO_TRADE should be rare and reserved for hard blocks or genuinely non-positive edge on both sides.
- If one side has a clear positive edge with acceptable microstructure, choose that side with calibrated confidence/size.
- Do not abstain only because snapshots disagree if market-state, Synth, and Allora still support a positive side edge.
`)
}

func roundFloat(v float64, decimals int) float64 {
	pow := math.Pow(10, float64(maxInt(decimals, 0)))
	if pow == 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	return math.Round(v*pow) / pow
}

func roundFloatPtr(v *float64, decimals int) *float64 {
	if v == nil {
		return nil
	}
	r := roundFloat(*v, decimals)
	return &r
}

func normalCDF(x float64) float64 {
	return 0.5 * (1.0 + math.Erf(x/math.Sqrt2))
}

func hashHex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func toJSONString(value interface{}) string {
	payload, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(payload)
}

func float64Ptr(v float64) *float64 {
	return &v
}

func maxInt64(a int64, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
