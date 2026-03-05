package synthdata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/config"
)

const (
	defaultBaseURL = "https://api.synthdata.co"
	defaultTimeout = 12 * time.Second
)

var (
	ErrClientDisabled = errors.New("synthdata client disabled")
)

type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

type UpDownWindow string

const (
	UpDownWindow5m  UpDownWindow = "5min"
	UpDownWindow15m UpDownWindow = "15min"
	UpDownWindow1h  UpDownWindow = "hourly"
	UpDownWindow1d  UpDownWindow = "daily"
)

type PolymarketUpDownResponse struct {
	Slug                       string  `json:"slug"`
	StartPrice                 float64 `json:"start_price"`
	CurrentTime                string  `json:"current_time"`
	CurrentPrice               float64 `json:"current_price"`
	CurrentOutcome             string  `json:"current_outcome"`
	SynthProbabilityUp         float64 `json:"synth_probability_up"`
	SynthOutcome               string  `json:"synth_outcome"`
	PolymarketProbabilityUp    float64 `json:"polymarket_probability_up"`
	PolymarketOutcome          string  `json:"polymarket_outcome"`
	EventStartTime             string  `json:"event_start_time"`
	EventEndTime               string  `json:"event_end_time"`
	EventCreationTime          string  `json:"event_creation_time"`
	BestBidPrice               float64 `json:"best_bid_price"`
	BestAskPrice               float64 `json:"best_ask_price"`
	BestBidSize                float64 `json:"best_bid_size"`
	BestAskSize                float64 `json:"best_ask_size"`
	PolymarketLastTradeTime    string  `json:"polymarket_last_trade_time"`
	PolymarketLastTradePrice   float64 `json:"polymarket_last_trade_price"`
	PolymarketLastTradeOutcome string  `json:"polymarket_last_trade_outcome"`
}

type PredictionPercentilesResponse struct {
	CurrentPrice   float64                `json:"current_price"`
	ForecastFuture ForecastPercentileData `json:"forecast_future"`
}

type ForecastPercentileData struct {
	Percentiles []PercentilePoint `json:"percentiles"`
}

type PercentilePoint struct {
	P005 float64 `json:"0.005"`
	P05  float64 `json:"0.05"`
	P20  float64 `json:"0.2"`
	P35  float64 `json:"0.35"`
	P50  float64 `json:"0.5"`
	P65  float64 `json:"0.65"`
	P80  float64 `json:"0.8"`
	P95  float64 `json:"0.95"`
	P995 float64 `json:"0.995"`
}

type VolatilityResponse struct {
	CurrentPrice   float64            `json:"current_price"`
	ForecastFuture ForecastVolatility `json:"forecast_future"`
	ForecastPast   ForecastVolatility `json:"forecast_past"`
}

type ForecastVolatility struct {
	AverageVolatility float64   `json:"average_volatility"`
	Volatility        []float64 `json:"volatility"`
}

type LPProbabilitiesResponse struct {
	CurrentPrice float64                         `json:"current_price"`
	Data         map[string]LPProbabilityHorizon `json:"data"`
}

type LPProbabilityHorizon struct {
	ProbabilityAbove map[string]float64 `json:"probability_above"`
	ProbabilityBelow map[string]float64 `json:"probability_below"`
}

type EnterpriseProbability struct {
	ProbabilityUp float64
	Samples       int
	Source        string
}

type HistoricalCalibrationStats struct {
	Asset               string
	Samples             int
	DirectionalAccuracy float64
	BrierScore          float64
	ConfidenceScale     float64
	EdgeBuffer          float64
	Source              string
}

func NewClient(cfg *config.Config) *Client {
	baseURL := strings.TrimSpace(cfg.Services.SynthDataBaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	return &Client{
		apiKey:  strings.TrimSpace(cfg.Services.SynthDataAPIKey),
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.apiKey != ""
}

func (c *Client) GetPolymarketUpDown(ctx context.Context, asset string, window UpDownWindow, horizon string, days, limit int) (*PolymarketUpDownResponse, error) {
	if !c.Enabled() {
		return nil, ErrClientDisabled
	}

	asset = strings.ToUpper(strings.TrimSpace(asset))
	if asset == "" {
		return nil, fmt.Errorf("asset is required")
	}

	path := fmt.Sprintf("/insights/polymarket/up-down/%s", string(window))
	params := url.Values{}
	params.Set("asset", asset)
	if h := strings.TrimSpace(horizon); h != "" {
		params.Set("horizon", h)
	}
	if days > 0 {
		params.Set("days", strconv.Itoa(days))
	}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}

	var out PolymarketUpDownResponse
	if err := c.getJSON(ctx, path, params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetPredictionPercentiles(ctx context.Context, asset, horizon string, days, limit int) (*PredictionPercentilesResponse, error) {
	if !c.Enabled() {
		return nil, ErrClientDisabled
	}

	params := url.Values{}
	params.Set("asset", strings.ToUpper(strings.TrimSpace(asset)))
	if h := strings.TrimSpace(horizon); h != "" {
		params.Set("horizon", h)
	}
	if days > 0 {
		params.Set("days", strconv.Itoa(days))
	}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}

	var out PredictionPercentilesResponse
	if err := c.getJSON(ctx, "/insights/prediction-percentiles", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetVolatility(ctx context.Context, asset, horizon string, days, limit int) (*VolatilityResponse, error) {
	if !c.Enabled() {
		return nil, ErrClientDisabled
	}

	params := url.Values{}
	params.Set("asset", strings.ToUpper(strings.TrimSpace(asset)))
	if h := strings.TrimSpace(horizon); h != "" {
		params.Set("horizon", h)
	}
	if days > 0 {
		params.Set("days", strconv.Itoa(days))
	}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}

	var out VolatilityResponse
	if err := c.getJSON(ctx, "/insights/volatility", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetLPProbabilities(ctx context.Context, asset, horizon string, days, limit int) (*LPProbabilitiesResponse, error) {
	if !c.Enabled() {
		return nil, ErrClientDisabled
	}

	params := url.Values{}
	params.Set("asset", strings.ToUpper(strings.TrimSpace(asset)))
	if h := strings.TrimSpace(horizon); h != "" {
		params.Set("horizon", h)
	}
	if days > 0 {
		params.Set("days", strconv.Itoa(days))
	}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}

	var out LPProbabilitiesResponse
	if err := c.getJSON(ctx, "/insights/lp-probabilities", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetEnterpriseProbabilityUp attempts to infer P(price_t >= thresholdPrice)
// from the enterprise /v2/prediction/best endpoint. It gracefully returns
// an error when the shape is not available for parsing.
func (c *Client) GetEnterpriseProbabilityUp(ctx context.Context, asset string, timeIncrement, timeLength, targetStep int, thresholdPrice float64) (*EnterpriseProbability, error) {
	if !c.Enabled() {
		return nil, ErrClientDisabled
	}

	if timeIncrement <= 0 {
		timeIncrement = 300
	}
	if timeLength <= 0 {
		timeLength = 86400
	}
	if targetStep < 0 {
		targetStep = 0
	}
	if thresholdPrice <= 0 || math.IsNaN(thresholdPrice) {
		return nil, fmt.Errorf("threshold price must be positive")
	}

	params := url.Values{}
	params.Set("asset", strings.ToUpper(strings.TrimSpace(asset)))
	params.Set("time_increment", strconv.Itoa(timeIncrement))
	params.Set("time_length", strconv.Itoa(timeLength))

	raw := make(map[string]interface{})
	if err := c.getJSON(ctx, "/v2/prediction/best", params, &raw); err != nil {
		return nil, err
	}

	paths := collectPredictionPaths(raw)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no parseable enterprise prediction paths")
	}

	wins := 0
	samples := 0
	for _, path := range paths {
		if targetStep >= len(path) {
			continue
		}
		v := path[targetStep]
		if math.IsNaN(v) || v <= 0 {
			continue
		}
		samples++
		if v >= thresholdPrice {
			wins++
		}
	}
	if samples == 0 {
		return nil, fmt.Errorf("enterprise paths do not cover target step")
	}

	return &EnterpriseProbability{
		ProbabilityUp: float64(wins) / float64(samples),
		Samples:       samples,
		Source:        "v2/prediction/best",
	}, nil
}

// GetLatestProbabilityUp provides a fallback enterprise probability estimate
// when /v2/prediction/best is unavailable or sparse.
func (c *Client) GetLatestProbabilityUp(ctx context.Context, asset string, timeIncrement, timeLength, targetStep int, thresholdPrice float64) (*EnterpriseProbability, error) {
	if !c.Enabled() {
		return nil, ErrClientDisabled
	}
	if thresholdPrice <= 0 || math.IsNaN(thresholdPrice) {
		return nil, fmt.Errorf("threshold price must be positive")
	}
	if timeIncrement <= 0 {
		timeIncrement = 300
	}
	if timeLength <= 0 {
		timeLength = 86400
	}
	if targetStep < 0 {
		targetStep = 0
	}

	params := url.Values{}
	params.Set("asset", strings.ToUpper(strings.TrimSpace(asset)))
	params.Set("time_increment", strconv.Itoa(timeIncrement))
	params.Set("time_length", strconv.Itoa(timeLength))

	var raw interface{}
	if err := c.getJSON(ctx, "/v2/prediction/latest", params, &raw); err != nil {
		return nil, err
	}
	paths := collectPredictionPaths(raw)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no parseable latest prediction paths")
	}

	wins := 0
	samples := 0
	for _, path := range paths {
		if targetStep >= len(path) {
			continue
		}
		v := path[targetStep]
		if math.IsNaN(v) || v <= 0 {
			continue
		}
		samples++
		if v >= thresholdPrice {
			wins++
		}
	}
	if samples == 0 {
		return nil, fmt.Errorf("latest paths do not cover target step")
	}

	return &EnterpriseProbability{
		ProbabilityUp: float64(wins) / float64(samples),
		Samples:       samples,
		Source:        "v2/prediction/latest",
	}, nil
}

// GetHistoricalCalibrationStats computes directional calibration metrics from
// /v2/prediction/historical for rolling confidence/edge adjustments.
func (c *Client) GetHistoricalCalibrationStats(ctx context.Context, asset string, startTime, endTime time.Time) (*HistoricalCalibrationStats, error) {
	if !c.Enabled() {
		return nil, ErrClientDisabled
	}
	asset = strings.ToUpper(strings.TrimSpace(asset))
	if asset == "" {
		return nil, fmt.Errorf("asset is required")
	}
	if startTime.IsZero() || endTime.IsZero() || !endTime.After(startTime) {
		return nil, fmt.Errorf("invalid historical time range")
	}

	params := url.Values{}
	params.Set("asset", asset)
	params.Set("start_time", strconv.FormatInt(startTime.UTC().Unix(), 10))
	params.Set("end_time", strconv.FormatInt(endTime.UTC().Unix(), 10))

	var raw interface{}
	if err := c.getJSON(ctx, "/v2/prediction/historical", params, &raw); err != nil {
		return nil, err
	}

	pairs := collectHistoricalPairs(raw)
	if len(pairs) == 0 {
		return nil, fmt.Errorf("no parseable historical prediction/realized pairs")
	}

	var total int
	var correct int
	var brier float64
	for _, pair := range pairs {
		if pair.pUp < 0 || pair.pUp > 1 {
			continue
		}
		total++
		y := 0.0
		if pair.realizedUp {
			y = 1.0
		}
		if (pair.pUp >= 0.5) == pair.realizedUp {
			correct++
		}
		d := pair.pUp - y
		brier += d * d
	}
	if total == 0 {
		return nil, fmt.Errorf("historical pairs did not contain valid samples")
	}

	accuracy := float64(correct) / float64(total)
	brier /= float64(total)
	confScale := upDownClamp(1.12-1.65*brier, 0.55, 1.10)
	edgeBuffer := upDownClamp((brier-0.17)*0.12, 0, 0.05)

	return &HistoricalCalibrationStats{
		Asset:               asset,
		Samples:             total,
		DirectionalAccuracy: accuracy,
		BrierScore:          brier,
		ConfidenceScale:     confScale,
		EdgeBuffer:          edgeBuffer,
		Source:              "v2/prediction/historical",
	}, nil
}

func EstimateProbabilityUpFromPercentiles(points []PercentilePoint, targetStep int, thresholdPrice float64) (float64, error) {
	if len(points) == 0 {
		return 0, fmt.Errorf("percentile points empty")
	}
	if targetStep < 0 {
		targetStep = 0
	}
	if targetStep >= len(points) {
		targetStep = len(points) - 1
	}
	if thresholdPrice <= 0 || math.IsNaN(thresholdPrice) {
		return 0, fmt.Errorf("threshold price must be positive")
	}

	pt := points[targetStep]
	mu := pt.P50
	if mu <= 0 || math.IsNaN(mu) {
		return 0, fmt.Errorf("invalid median percentile")
	}

	// Approximate sigma via normal quantiles using P95/P05 span.
	sigma := (pt.P95 - pt.P05) / 3.289706
	if sigma <= 0 || math.IsNaN(sigma) {
		if thresholdPrice <= mu {
			return 0.5, nil
		}
		return 0.25, nil
	}

	z := (thresholdPrice - mu) / sigma
	p := 1.0 - normalCDF(z)
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	return p, nil
}

func (c *Client) getJSON(ctx context.Context, path string, params url.Values, out interface{}) error {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return err
	}
	if params != nil {
		u.RawQuery = params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Apikey "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("synthdata request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("synthdata api status %d: %s", resp.StatusCode, msg)
	}

	if strings.TrimSpace(string(body)) == "" {
		return fmt.Errorf("synthdata empty response")
	}
	if strings.EqualFold(strings.Trim(strings.TrimSpace(string(body)), `"`), "No prediction available") {
		return fmt.Errorf("synthdata: no prediction available")
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("failed to decode synthdata payload: %w", err)
	}

	return nil
}

func collectPredictionPaths(node interface{}) [][]float64 {
	paths := make([][]float64, 0)
	walkForNumericArrays(node, &paths)

	// Remove tiny arrays and sort by size descending, which heuristically keeps
	// actual price paths ahead of metadata vectors.
	filtered := make([][]float64, 0, len(paths))
	for _, p := range paths {
		if len(p) >= 4 {
			filtered = append(filtered, p)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return len(filtered[i]) > len(filtered[j])
	})

	return filtered
}

func walkForNumericArrays(node interface{}, out *[][]float64) {
	switch v := node.(type) {
	case []interface{}:
		if path, ok := asFloatArray(v); ok {
			*out = append(*out, path)
		}
		for _, item := range v {
			walkForNumericArrays(item, out)
		}
	case map[string]interface{}:
		for _, item := range v {
			walkForNumericArrays(item, out)
		}
	}
}

func asFloatArray(arr []interface{}) ([]float64, bool) {
	if len(arr) == 0 {
		return nil, false
	}
	out := make([]float64, 0, len(arr))
	for _, item := range arr {
		switch n := item.(type) {
		case float64:
			out = append(out, n)
		case json.Number:
			f, err := n.Float64()
			if err != nil {
				return nil, false
			}
			out = append(out, f)
		default:
			return nil, false
		}
	}
	return out, true
}

func normalCDF(x float64) float64 {
	return 0.5 * (1.0 + math.Erf(x/math.Sqrt2))
}

type historicalPair struct {
	pUp        float64
	realizedUp bool
}

func collectHistoricalPairs(node interface{}) []historicalPair {
	pairs := make([]historicalPair, 0, 64)
	walkHistorical(node, &pairs)
	return pairs
}

func walkHistorical(node interface{}, out *[]historicalPair) {
	switch v := node.(type) {
	case []interface{}:
		for _, item := range v {
			walkHistorical(item, out)
		}
	case map[string]interface{}:
		if p, ok := inferHistoricalPair(v); ok {
			*out = append(*out, p)
		}
		for _, item := range v {
			walkHistorical(item, out)
		}
	}
}

func inferHistoricalPair(m map[string]interface{}) (historicalPair, bool) {
	paths := collectPredictionPaths(m)
	if len(paths) == 0 {
		return historicalPair{}, false
	}

	realized := extractHistoricalRealizedPath(m)
	if len(realized) < 2 {
		return historicalPair{}, false
	}
	realizedUp := realized[len(realized)-1] >= realized[0]

	upCount := 0
	total := 0
	for _, path := range paths {
		if len(path) < 2 {
			continue
		}
		total++
		if path[len(path)-1] >= path[0] {
			upCount++
		}
	}
	if total == 0 {
		return historicalPair{}, false
	}
	return historicalPair{
		pUp:        float64(upCount) / float64(total),
		realizedUp: realizedUp,
	}, true
}

func extractHistoricalRealizedPath(m map[string]interface{}) []float64 {
	candidates := []string{
		"realized",
		"actual",
		"actual_prices",
		"realized_prices",
		"prices",
		"ground_truth",
		"target",
	}
	for _, key := range candidates {
		if raw, ok := m[key]; ok {
			switch value := raw.(type) {
			case []interface{}:
				if arr, ok := asFloatArray(value); ok && len(arr) >= 2 {
					return arr
				}
			case map[string]interface{}:
				for _, nested := range []string{"prices", "path", "values", "series"} {
					if x, exists := value[nested]; exists {
						if list, ok := x.([]interface{}); ok {
							if arr, ok := asFloatArray(list); ok && len(arr) >= 2 {
								return arr
							}
						}
					}
				}
			}
		}
	}
	return nil
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
