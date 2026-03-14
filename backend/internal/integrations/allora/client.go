package allora

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/config"
)

const (
	defaultBaseURL = "https://api.allora.network"
	defaultTimeout = 8 * time.Second
	maxFetchTries  = 3
	retryBaseDelay = 180 * time.Millisecond
)

var (
	ErrClientDisabled = errors.New("allora client disabled")
)

type PriceInferenceTimeframe string

const (
	Timeframe5m PriceInferenceTimeframe = "5m"
	Timeframe8h PriceInferenceTimeframe = "8h"
)

type Client struct {
	apiKey          string
	baseURL         string
	signatureFormat string
	httpClient      *http.Client
}

type PriceInference struct {
	RequestID                     string    `json:"request_id"`
	Asset                         string    `json:"asset"`
	Timeframe                     string    `json:"timeframe"`
	SignatureFormat               string    `json:"signature_format"`
	Signature                     string    `json:"signature,omitempty"`
	TopicID                       string    `json:"topic_id"`
	Timestamp                     time.Time `json:"timestamp"`
	NetworkInference              float64   `json:"network_inference"`
	ConfidenceIntervalPercentiles []float64 `json:"confidence_interval_percentiles"`
	ConfidenceIntervalValues      []float64 `json:"confidence_interval_values"`
}

type priceInferenceAPIResponse struct {
	RequestID string `json:"request_id"`
	Status    bool   `json:"status"`
	Data      struct {
		Signature     string `json:"signature"`
		InferenceData struct {
			NetworkInference              string   `json:"network_inference"`
			ConfidenceIntervalPercentiles []string `json:"confidence_interval_percentiles"`
			ConfidenceIntervalValues      []string `json:"confidence_interval_values"`
			TopicID                       string   `json:"topic_id"`
			Timestamp                     string   `json:"timestamp"`
		} `json:"inference_data"`
	} `json:"data"`
}

func NewClient(cfg *config.Config) *Client {
	baseURL := strings.TrimSpace(cfg.Services.AlloraBaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	signatureFormat := strings.TrimSpace(cfg.Services.AlloraSignatureFormat)
	if signatureFormat == "" {
		signatureFormat = "ethereum-11155111"
	}

	return &Client{
		apiKey:          strings.TrimSpace(cfg.Services.AlloraAPIKey),
		baseURL:         strings.TrimRight(baseURL, "/"),
		signatureFormat: signatureFormat,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.apiKey != ""
}

func (c *Client) GetPriceInference(ctx context.Context, asset string, timeframe PriceInferenceTimeframe) (*PriceInference, error) {
	if !c.Enabled() {
		return nil, ErrClientDisabled
	}

	asset = strings.ToUpper(strings.TrimSpace(asset))
	if asset == "" {
		return nil, fmt.Errorf("asset is required")
	}
	if timeframe != Timeframe5m && timeframe != Timeframe8h {
		return nil, fmt.Errorf("unsupported timeframe %q", timeframe)
	}

	url := fmt.Sprintf("%s/v2/allora/consumer/price/%s/%s/%s", c.baseURL, c.signatureFormat, asset, string(timeframe))

	var body []byte
	var lastErr error
	for attempt := 1; attempt <= maxFetchTries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("x-api-key", c.apiKey)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if attempt < maxFetchTries && isRetryableTransportErr(err) {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(retryBaseDelay * time.Duration(attempt)):
				}
				lastErr = err
				continue
			}
			return nil, fmt.Errorf("allora request failed: %w", err)
		}

		readBody, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			if attempt < maxFetchTries {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(retryBaseDelay * time.Duration(attempt)):
				}
				lastErr = readErr
				continue
			}
			return nil, fmt.Errorf("allora response read failed: %w", readErr)
		}
		if resp.StatusCode != http.StatusOK {
			if attempt < maxFetchTries && isRetryableHTTPStatus(resp.StatusCode) {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(retryBaseDelay * time.Duration(attempt)):
				}
				lastErr = fmt.Errorf("status %d", resp.StatusCode)
				continue
			}
			return nil, fmt.Errorf("allora api status %d: %s", resp.StatusCode, strings.TrimSpace(string(readBody)))
		}
		body = readBody
		lastErr = nil
		break
	}
	if len(body) == 0 {
		if lastErr != nil {
			return nil, fmt.Errorf("allora request failed after retries: %w", lastErr)
		}
		return nil, fmt.Errorf("allora response is empty")
	}

	var payload priceInferenceAPIResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("allora payload decode failed: %w", err)
	}
	if !payload.Status {
		return nil, fmt.Errorf("allora response status=false")
	}

	inferenceValue, err := parseScaledDecimal(payload.Data.InferenceData.NetworkInference)
	if err != nil {
		return nil, fmt.Errorf("invalid network_inference: %w", err)
	}

	ts, err := parseUnixSeconds(payload.Data.InferenceData.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("invalid inference timestamp: %w", err)
	}

	percentiles := make([]float64, 0, len(payload.Data.InferenceData.ConfidenceIntervalPercentiles))
	for _, raw := range payload.Data.InferenceData.ConfidenceIntervalPercentiles {
		v, parseErr := parseScaledDecimal(raw)
		if parseErr != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		percentiles = append(percentiles, v)
	}

	values := make([]float64, 0, len(payload.Data.InferenceData.ConfidenceIntervalValues))
	for _, raw := range payload.Data.InferenceData.ConfidenceIntervalValues {
		v, parseErr := parseScaledDecimal(raw)
		if parseErr != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		values = append(values, v)
	}

	out := &PriceInference{
		RequestID:                     strings.TrimSpace(payload.RequestID),
		Asset:                         asset,
		Timeframe:                     string(timeframe),
		SignatureFormat:               c.signatureFormat,
		Signature:                     strings.TrimSpace(payload.Data.Signature),
		TopicID:                       strings.TrimSpace(payload.Data.InferenceData.TopicID),
		Timestamp:                     ts,
		NetworkInference:              inferenceValue,
		ConfidenceIntervalPercentiles: percentiles,
		ConfidenceIntervalValues:      values,
	}
	return out, nil
}

func isRetryableHTTPStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

func isRetryableTransportErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func parseScaledDecimal(raw string) (float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("empty numeric string")
	}
	value, ok := new(big.Float).SetString(raw)
	if !ok {
		return 0, fmt.Errorf("invalid numeric string %q", raw)
	}

	// Allora consumer payloads usually encode fixed-point integers with 1e18 scale.
	// When values are huge integers, normalize to human units.
	if !strings.Contains(raw, ".") && len(raw) > 12 {
		value.Quo(value, big.NewFloat(1e18))
	}

	f, _ := value.Float64()
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, fmt.Errorf("invalid float conversion")
	}
	return f, nil
}

func parseUnixSeconds(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	sec := new(big.Int)
	if _, ok := sec.SetString(raw, 10); !ok {
		return time.Time{}, fmt.Errorf("invalid timestamp %q", raw)
	}
	return time.Unix(sec.Int64(), 0).UTC(), nil
}
