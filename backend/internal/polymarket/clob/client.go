/**
 * @description
 * HTTP Client for the Polymarket CLOB API.
 * Handles order placement, cancellation, and retrieval.
 * Implements Builder Attribution logic by signing requests with the Builder API Key.
 *
 * @dependencies
 * - backend/internal/config
 * - crypto/hmac, crypto/sha256: For request signing
 */

package clob

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/logger"
)

const (
	DefaultTimeout        = 10 * time.Second
	pricesHistoryEndpoint = "/prices-history"
	lastTradeEndpoint     = "/last-trade-price"
	midpointEndpoint      = "/midpoint"
	spreadsEndpoint       = "/spreads"
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	APIKey     string
	APISecret  string
	Passphrase string
	RetryMax   int
	RetryBase  time.Duration
}

func NewClient(cfg *config.Config) *Client {
	retryMax := 4
	retryBase := 200 * time.Millisecond
	if cfg != nil {
		if cfg.Services.RetryMaxAttempts > 0 {
			retryMax = cfg.Services.RetryMaxAttempts
		}
		if cfg.Services.RetryBaseDelayMs > 0 {
			retryBase = time.Duration(cfg.Services.RetryBaseDelayMs) * time.Millisecond
		}
	}

	return &Client{
		BaseURL:    cfg.Polymarket.ClobURL,
		APIKey:     cfg.Polymarket.BuilderAPIKey,
		APISecret:  cfg.Polymarket.BuilderSecret,
		Passphrase: cfg.Polymarket.BuilderPass,
		HTTPClient: &http.Client{
			Timeout: DefaultTimeout,
		},
		RetryMax:  retryMax,
		RetryBase: retryBase,
	}
}

// PostOrder sends a single order to the CLOB with both builder and user credentials.
func (c *Client) PostOrder(ctx context.Context, req *PostOrderRequest, userCreds *APIKeyCredentials) (*PostOrderResponse, error) {
	return c.sendRequest(ctx, http.MethodPost, "/order", req, userCreds)
}

// PostOrders sends a batch of orders to the CLOB
func (c *Client) PostOrders(ctx context.Context, req PostOrdersRequest, userCreds *APIKeyCredentials) (*PostOrderResponse, error) {
	// Note: The response structure for batch orders might differ slightly in practice,
	// but usually follows standard success/error patterns. We'll map to PostOrderResponse for now.
	return c.sendRequest(ctx, http.MethodPost, "/orders", req, userCreds)
}

// CancelOrder cancels a single order
func (c *Client) CancelOrder(ctx context.Context, req *CancelOrderRequest, userCreds *APIKeyCredentials) (*CancelResponse, error) {
	if err := requireUserCredentials(userCreds); err != nil {
		return nil, err
	}
	var resp CancelResponse
	if err := c.sendRequestDecode(ctx, http.MethodDelete, "/order", req, &resp, userCreds); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CancelOrders cancels multiple orders
func (c *Client) CancelOrders(ctx context.Context, req *CancelOrdersRequest, userCreds *APIKeyCredentials) (*CancelResponse, error) {
	if err := requireUserCredentials(userCreds); err != nil {
		return nil, err
	}
	var resp CancelResponse
	if err := c.sendRequestDecode(ctx, http.MethodDelete, "/orders", req, &resp, userCreds); err != nil {
		return nil, err
	}
	return &resp, nil
}

func requireUserCredentials(userCreds *APIKeyCredentials) error {
	if userCreds == nil {
		return fmt.Errorf("user api credentials are required")
	}
	if strings.TrimSpace(userCreds.Key) == "" || strings.TrimSpace(userCreds.Secret) == "" || strings.TrimSpace(userCreds.Passphrase) == "" {
		return fmt.Errorf("incomplete user api credentials")
	}
	return nil
}

// GetBook fetches the current order book for a token (asset) from the CLOB API.
func (c *Client) GetBook(ctx context.Context, tokenID string) (*BookResponse, error) {
	if strings.TrimSpace(tokenID) == "" {
		return nil, fmt.Errorf("tokenID is required")
	}
	// CLOB expects snake_case token_id for the book endpoint
	u := fmt.Sprintf("/book?token_id=%s", tokenID)

	var resp BookResponse
	if err := c.sendRequestDecode(ctx, http.MethodGet, u, nil, &resp, nil); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetPriceHistory fetches historical prices for a token (asset) from the CLOB API.
// The "market" parameter in the API refers to the token/asset ID.
func (c *Client) GetPriceHistory(ctx context.Context, params PriceHistoryParams) ([]HistoryPoint, error) {
	market := strings.TrimSpace(params.Market)
	if market == "" {
		return nil, fmt.Errorf("market token id is required")
	}

	values := url.Values{}
	values.Set("market", market)

	if params.Interval != "" {
		values.Set("interval", string(params.Interval))
	}
	if params.StartTs > 0 {
		values.Set("startTs", strconv.FormatInt(params.StartTs, 10))
	}
	if params.EndTs > 0 {
		values.Set("endTs", strconv.FormatInt(params.EndTs, 10))
	}
	if params.Fidelity > 0 {
		values.Set("fidelity", strconv.Itoa(params.Fidelity))
	}

	path := pricesHistoryEndpoint
	if encoded := values.Encode(); encoded != "" {
		path = fmt.Sprintf("%s?%s", pricesHistoryEndpoint, encoded)
	}

	var raw json.RawMessage
	if err := c.sendRequestDecode(ctx, http.MethodGet, path, nil, &raw, nil); err != nil {
		return nil, err
	}

	var history []HistoryPoint
	if err := json.Unmarshal(raw, &history); err == nil {
		return history, nil
	}

	// Some responses wrap the array (e.g., {"history":[...]} or {"data":[...]}).
	var wrapper struct {
		History []HistoryPoint `json:"history"`
		Data    []HistoryPoint `json:"data"`
		Result  []HistoryPoint `json:"result"`
		Prices  []HistoryPoint `json:"prices"`
	}
	if err := json.Unmarshal(raw, &wrapper); err == nil {
		switch {
		case len(wrapper.History) > 0:
			return wrapper.History, nil
		case len(wrapper.Data) > 0:
			return wrapper.Data, nil
		case len(wrapper.Result) > 0:
			return wrapper.Result, nil
		case len(wrapper.Prices) > 0:
			return wrapper.Prices, nil
		default:
			// If the wrapper keys exist but are empty arrays, treat as empty history instead of error.
			var keyed map[string]json.RawMessage
			if mapErr := json.Unmarshal(raw, &keyed); mapErr == nil {
				if _, ok := keyed["history"]; ok {
					return []HistoryPoint{}, nil
				}
				if _, ok := keyed["data"]; ok {
					return []HistoryPoint{}, nil
				}
				if _, ok := keyed["result"]; ok {
					return []HistoryPoint{}, nil
				}
				if _, ok := keyed["prices"]; ok {
					return []HistoryPoint{}, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("unexpected prices-history response shape: %s", string(raw))
}

// GetLastTradePrice fetches the last traded price for a token from the CLOB API.
func (c *Client) GetLastTradePrice(ctx context.Context, tokenID string) (float64, string, error) {
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return 0, "", fmt.Errorf("tokenID is required")
	}

	path := fmt.Sprintf("%s?token_id=%s", lastTradeEndpoint, tokenID)
	var raw json.RawMessage
	if err := c.sendRequestDecode(ctx, http.MethodGet, path, nil, &raw, nil); err != nil {
		return 0, "", err
	}

	return parseLastTradePriceResponse(raw)
}

// GetMidpoint fetches the midpoint price for a token from the CLOB API.
// GET /midpoint?token_id={tokenId}
func (c *Client) GetMidpoint(ctx context.Context, tokenID string) (float64, error) {
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return 0, fmt.Errorf("tokenID is required")
	}

	values := url.Values{}
	values.Set("token_id", tokenID)
	path := fmt.Sprintf("%s?%s", midpointEndpoint, values.Encode())

	var raw json.RawMessage
	if err := c.sendRequestDecode(ctx, http.MethodGet, path, nil, &raw, nil); err != nil {
		return 0, err
	}

	return parseMidpointResponse(raw)
}

// GetSpread fetches the bid-ask spread for a token from the CLOB API.
// POST /spreads
func (c *Client) GetSpread(ctx context.Context, tokenID string) (float64, error) {
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return 0, fmt.Errorf("tokenID is required")
	}

	req := []map[string]string{{"token_id": tokenID}}

	var raw json.RawMessage
	if err := c.sendRequestDecode(ctx, http.MethodPost, spreadsEndpoint, req, &raw, nil); err != nil {
		return 0, err
	}

	return parseSpreadResponse(raw, tokenID)
}

// GetTrades fetches trades from the CLOB /data/trades endpoint.
// It supports filtering by market, maker, taker, and time window.
func (c *Client) GetTrades(ctx context.Context, params TradesQuery) ([]TradeEvent, error) {
	values := url.Values{}
	if strings.TrimSpace(params.Market) != "" {
		values.Set("market", strings.TrimSpace(params.Market))
	}
	if strings.TrimSpace(params.Maker) != "" {
		values.Set("maker", strings.TrimSpace(params.Maker))
	}
	if strings.TrimSpace(params.Taker) != "" {
		values.Set("taker", strings.TrimSpace(params.Taker))
	}
	if params.After > 0 {
		values.Set("after", strconv.FormatInt(params.After, 10))
	}
	if params.Before > 0 {
		values.Set("before", strconv.FormatInt(params.Before, 10))
	}
	if params.Limit > 0 {
		values.Set("limit", strconv.Itoa(params.Limit))
	}

	path := "/data/trades"
	if encoded := values.Encode(); encoded != "" {
		path = fmt.Sprintf("%s?%s", path, encoded)
	}

	var raw []map[string]interface{}
	if err := c.sendRequestDecode(ctx, http.MethodGet, path, nil, &raw, nil); err != nil {
		return nil, err
	}

	trades := make([]TradeEvent, 0, len(raw))
	for _, entry := range raw {
		trade := TradeEvent{
			ID:        stringValue(entry["id"]),
			Market:    stringValue(entry["market"]),
			TokenID:   stringValue(entry["token_id"]),
			Side:      normalizeSide(stringValue(entry["side"])),
			Price:     floatValue(entry["price"], entry["price_num"]),
			Size:      floatValue(entry["size"], entry["size_num"]),
			Value:     floatValue(entry["value"]),
			Taker:     stringValue(entry["taker"]),
			Maker:     stringValue(entry["maker"]),
			MatchTime: int64Value(entry["match_time"], entry["timestamp"]),
		}
		if trade.Value == 0 && trade.Price > 0 && trade.Size > 0 {
			trade.Value = trade.Price * trade.Size
		}
		trades = append(trades, trade)
	}
	return trades, nil
}

func parseLastTradePriceResponse(raw json.RawMessage) (float64, string, error) {
	var num float64
	if err := json.Unmarshal(raw, &num); err == nil && num > 0 {
		return num, "", nil
	}

	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		if parsed, perr := strconv.ParseFloat(str, 64); perr == nil && parsed > 0 {
			return parsed, "", nil
		}
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return 0, "", fmt.Errorf("unexpected last-trade-price response: %s", string(raw))
	}

	parseNumeric := func(value interface{}) (float64, bool) {
		switch v := value.(type) {
		case float64:
			return v, v > 0
		case json.Number:
			if parsed, err := v.Float64(); err == nil {
				return parsed, parsed > 0
			}
		case string:
			if parsed, err := strconv.ParseFloat(v, 64); err == nil {
				return parsed, parsed > 0
			}
		}
		return 0, false
	}

	price, ok := parseNumeric(obj["price"])
	if !ok {
		price, ok = parseNumeric(obj["last_trade_price"])
	}

	timestamp := ""
	if value, ok := obj["timestamp"]; ok {
		if str, ok := value.(string); ok {
			timestamp = str
		}
	}

	if price > 0 {
		return price, timestamp, nil
	}

	return 0, "", fmt.Errorf("unexpected last-trade-price response: %s", string(raw))
}

func parseMidpointResponse(raw json.RawMessage) (float64, error) {
	var num float64
	if err := json.Unmarshal(raw, &num); err == nil && num > 0 {
		return num, nil
	}

	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		if parsed, perr := strconv.ParseFloat(str, 64); perr == nil && parsed > 0 {
			return parsed, nil
		}
	}

	var obj struct {
		Mid string `json:"mid"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.Mid != "" {
		if parsed, perr := strconv.ParseFloat(obj.Mid, 64); perr == nil && parsed > 0 {
			return parsed, nil
		}
	}

	return 0, fmt.Errorf("unexpected midpoint response: %s", string(raw))
}

func parseSpreadResponse(raw json.RawMessage, tokenID string) (float64, error) {
	var strMap map[string]string
	if err := json.Unmarshal(raw, &strMap); err == nil {
		if value, ok := strMap[tokenID]; ok {
			if parsed, perr := strconv.ParseFloat(value, 64); perr == nil {
				return parsed, nil
			}
		}
	}

	var ifaceMap map[string]interface{}
	if err := json.Unmarshal(raw, &ifaceMap); err == nil {
		if value, ok := ifaceMap[tokenID]; ok {
			switch v := value.(type) {
			case float64:
				return v, nil
			case json.Number:
				if parsed, perr := v.Float64(); perr == nil {
					return parsed, nil
				}
			case string:
				if parsed, perr := strconv.ParseFloat(v, 64); perr == nil {
					return parsed, nil
				}
			}
		}
	}

	return 0, fmt.Errorf("unexpected spreads response: %s", string(raw))
}

// Helper to normalize BUY/SELL sides and tolerate numeric encodings.
func normalizeSide(side string) OrderSide {
	switch strings.ToUpper(strings.TrimSpace(side)) {
	case "BUY", "B", "1", "TRUE":
		return BUY
	case "SELL", "S", "0", "FALSE":
		return SELL
	default:
		return OrderSide(strings.ToUpper(strings.TrimSpace(side)))
	}
}

// stringValue safely extracts a string from an interface{}.
func stringValue(val interface{}) string {
	switch v := val.(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return strings.TrimSpace(v.String())
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(v, 'f', -1, 64))
	default:
		return ""
	}
}

// floatValue attempts to parse the first non-zero numeric value from a list of candidates.
func floatValue(candidates ...interface{}) float64 {
	for _, c := range candidates {
		switch v := c.(type) {
		case float64:
			if v != 0 {
				return v
			}
		case json.Number:
			if parsed, err := v.Float64(); err == nil && parsed != 0 {
				return parsed
			}
		case string:
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && parsed != 0 {
				return parsed
			}
		}
	}
	return 0
}

// int64Value attempts to parse the first non-zero integer value from a list of candidates.
func int64Value(candidates ...interface{}) int64 {
	for _, c := range candidates {
		switch v := c.(type) {
		case int64:
			if v != 0 {
				return v
			}
		case float64:
			if v != 0 {
				return int64(v)
			}
		case json.Number:
			if parsed, err := v.Int64(); err == nil && parsed != 0 {
				return parsed
			}
		case string:
			if parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil && parsed != 0 {
				return parsed
			}
		}
	}
	return 0
}

// DeriveAPIKey requests (or creates) the user API credentials using the L1 ClobAuth signature.
func (c *Client) DeriveAPIKey(ctx context.Context, proof *ClobAuthProof) (*APIKeyCredentials, error) {
	if proof == nil {
		return nil, fmt.Errorf("auth proof is required")
	}

	// Prefer derive endpoint to avoid creating multiples.
	u := fmt.Sprintf("%s%s", c.BaseURL, "/auth/derive-api-key")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create derive request: %w", err)
	}
	if err := setL1Headers(req, proof); err != nil {
		return nil, err
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadRequest {
		// Fallback to create if derive is not available for the address.
		createURL := fmt.Sprintf("%s%s", c.BaseURL, "/auth/api-key")
		req, cerr := http.NewRequestWithContext(ctx, http.MethodPost, createURL, nil)
		if cerr != nil {
			return nil, fmt.Errorf("failed to create api-key request: %w", cerr)
		}
		if err := setL1Headers(req, proof); err != nil {
			return nil, err
		}
		resp, err = c.HTTPClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("api-key request failed: %w", err)
		}
		defer resp.Body.Close()
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read auth response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("auth endpoint error (%d): %s", resp.StatusCode, string(body))
	}

	creds, err := parseAPIKeyCredentials(body)
	if err != nil {
		return nil, err
	}
	creds.Address = proof.Address
	fmt.Printf("Derived user API creds for %s (key prefix: %s...)\n", proof.Address, shortKey(creds.Key))
	return creds, nil
}

// sendRequest sends a generic request and expects a PostOrderResponse (common for trades)
func (c *Client) sendRequest(ctx context.Context, method, path string, payload interface{}, userCreds *APIKeyCredentials) (*PostOrderResponse, error) {
	var result PostOrderResponse
	if err := c.sendRequestDecode(ctx, method, path, payload, &result, userCreds); err != nil {
		return nil, err
	}
	return &result, nil
}

// sendRequestDecode handles the low-level HTTP construction, signing, and response decoding
func (c *Client) sendRequestDecode(ctx context.Context, method, path string, payload interface{}, result interface{}, userCreds *APIKeyCredentials) error {
	var body []byte
	var err error

	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}
	}

	u := fmt.Sprintf("%s%s", c.BaseURL, path)
	maxAttempts := c.RetryMax
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	var respBody []byte
	var respStatus int
	var lastErr error
	var reqRef *http.Request

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, reqErr := http.NewRequestWithContext(ctx, method, u, bytes.NewBuffer(body))
		if reqErr != nil {
			return fmt.Errorf("failed to create request: %w", reqErr)
		}
		reqRef = req

		if signErr := c.setHeaders(req, body, userCreds); signErr != nil {
			return fmt.Errorf("failed to sign request: %w", signErr)
		}

		resp, doErr := c.HTTPClient.Do(req)
		if doErr != nil {
			lastErr = fmt.Errorf("clob request failed: %w", doErr)
			if attempt < maxAttempts && shouldRetryTransport(method) {
				sleepWithContext(ctx, c.retryDelay(attempt))
				continue
			}
			return lastErr
		}

		respStatus = resp.StatusCode
		respBody, err = io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("failed to read response body: %w", err)
			if attempt < maxAttempts && shouldRetryTransport(method) {
				sleepWithContext(ctx, c.retryDelay(attempt))
				continue
			}
			return lastErr
		}

		if shouldRetryRequest(method, respStatus) && attempt < maxAttempts {
			sleepWithContext(ctx, c.retryDelay(attempt))
			continue
		}

		break
	}

	// Debug: surface request context when a 400 occurs to diagnose payload issues.
	if respStatus >= 400 {
		// Build a safe request descriptor
		methodForLog := ""
		if reqRef != nil {
			methodForLog = reqRef.Method
		}

		// Avoid logging secrets; only include owner key for correlation when present.
		var ownerKey string
		if payload != nil {
			if po, ok := payload.(*PostOrderRequest); ok {
				ownerKey = po.Owner
			}
		}

		// For URL clarity, decode path+query only.
		pathForLog := path
		if reqRef != nil {
			pathForLog = reqRef.URL.Path
			if reqRef.URL.RawQuery != "" {
				pathForLog = fmt.Sprintf("%s?%s", pathForLog, reqRef.URL.RawQuery)
			}
			if decoded, derr := url.QueryUnescape(pathForLog); derr == nil {
				pathForLog = decoded
			}
		}

		logger.Error(
			"CLOB 4xx: status=%d method=%s path=%s owner=%s body=%s",
			respStatus, methodForLog, pathForLog, shortKey(ownerKey), string(respBody),
		)
	}

	if respStatus >= 400 {
		if looksLikeHTML(respBody) {
			return fmt.Errorf("clob waf blocked request (status %d)", respStatus)
		}
		var errResp ErrorResponse
		if jsonErr := json.Unmarshal(respBody, &errResp); jsonErr == nil && errResp.Error != "" {
			return fmt.Errorf("clob error (%d): %s | body: %s", respStatus, errResp.Error, string(respBody))
		}
		// Try parsing as PostOrderResponse errorMsg
		var poResp PostOrderResponse
		if jsonErr := json.Unmarshal(respBody, &poResp); jsonErr == nil && !poResp.Success {
			return fmt.Errorf("clob error (%d): %s | body: %s", respStatus, poResp.ErrorMsg, string(respBody))
		}
		return fmt.Errorf("clob error (%d): %s", respStatus, string(respBody))
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

func shouldRetryHTTPStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusRequestTimeout, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return status >= 500
	}
}

func shouldRetryRequest(method string, status int) bool {
	method = strings.ToUpper(strings.TrimSpace(method))
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return shouldRetryHTTPStatus(status)
	default:
		// Avoid replaying mutations (e.g., POST order submission) on transient transport errors.
		return false
	}
}

func shouldRetryTransport(method string) bool {
	method = strings.ToUpper(strings.TrimSpace(method))
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		// Never replay non-idempotent methods on transport ambiguity.
		return false
	}
}

func (c *Client) retryDelay(attempt int) time.Duration {
	base := c.RetryBase
	if base <= 0 {
		base = 200 * time.Millisecond
	}
	if attempt <= 0 {
		attempt = 1
	}
	// simple capped exponential backoff
	delay := base * time.Duration(1<<(attempt-1))
	if delay > 2*time.Second {
		delay = 2 * time.Second
	}
	return delay
}

func sleepWithContext(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func (c *Client) setHeaders(req *http.Request, body []byte, userCreds *APIKeyCredentials) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	// Use a browser-like UA to avoid aggressive WAF heuristics.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; BankaiTerminal/1.0; +https://polymarket.com)")

	path := req.URL.Path
	if path == "" {
		path = "/"
	}
	if req.URL.RawQuery != "" {
		path = fmt.Sprintf("%s?%s", path, req.URL.RawQuery)
	}

	// If no builder credentials, we can't sign.
	if c.APIKey == "" || c.APISecret == "" || c.Passphrase == "" {
		return fmt.Errorf("missing builder credentials: POLY_BUILDER_API_KEY, SECRET, and PASSPHRASE are required for CLOB requests")
	}

	// Always include the key header; some CLOB endpoints (like /data/trades) require X-API-KEY even when using builder HMAC.
	req.Header.Set("X-API-KEY", c.APIKey)

	// Docs: POLY_BUILDER_SIGNATURE = base64url( HMAC_SHA256( base64Decode(secret), timestamp + method + path + body ) )
	method := strings.ToUpper(req.Method)
	timestamp := time.Now().Unix()

	sig, err := c.buildBuilderSignature(c.APISecret, timestamp, method, path, body)
	if err != nil {
		return err
	}

	req.Header.Set("POLY_BUILDER_API_KEY", c.APIKey)
	req.Header.Set("POLY_BUILDER_PASSPHRASE", c.Passphrase)
	req.Header.Set("POLY_BUILDER_SIGNATURE", sig)
	req.Header.Set("POLY_BUILDER_TIMESTAMP", strconv.FormatInt(timestamp, 10))

	if userCreds != nil {
		userSig, err := c.buildBuilderSignature(userCreds.Secret, timestamp, method, path, body)
		if err != nil {
			return fmt.Errorf("failed to compute user signature: %w", err)
		}
		if userCreds.Address != "" {
			req.Header.Set("POLY_ADDRESS", userCreds.Address)
		}
		req.Header.Set("POLY_API_KEY", userCreds.Key)
		req.Header.Set("POLY_PASSPHRASE", userCreds.Passphrase)
		req.Header.Set("POLY_SIGNATURE", userSig)
		req.Header.Set("POLY_TIMESTAMP", strconv.FormatInt(timestamp, 10))
	}

	return nil
}

// buildBuilderSignature implements the HMAC signing logic
func (c *Client) buildBuilderSignature(secret string, timestamp int64, method, requestPath string, body []byte) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("builder secret missing")
	}

	normalizedSecret := strings.TrimSpace(secret)

	var decodedSecret []byte
	var err error

	// Try URL-safe base64 decoding first (with and without padding)
	decodedSecret, err = base64.RawURLEncoding.DecodeString(normalizedSecret)
	if err != nil {
		decodedSecret, err = base64.URLEncoding.DecodeString(normalizedSecret)
	}
	if err != nil {
		// Fallback to standard base64 variants
		decodedSecret, err = base64.RawStdEncoding.DecodeString(normalizedSecret)
		if err != nil {
			decodedSecret, err = base64.StdEncoding.DecodeString(normalizedSecret)
		}
	}
	if err != nil {
		// Fallback to treating secret as raw bytes (some environments might inject it raw)
		decodedSecret = []byte(normalizedSecret)
	}

	payload := fmt.Sprintf("%d%s%s", timestamp, strings.ToUpper(method), requestPath)
	if len(body) > 0 {
		payload += string(body)
	}

	mac := hmac.New(sha256.New, decodedSecret)
	if _, err := mac.Write([]byte(payload)); err != nil {
		return "", fmt.Errorf("failed to compute signature: %w", err)
	}

	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	// Make URL-safe while preserving padding (Polymarket requirement)
	sig = strings.ReplaceAll(sig, "+", "-")
	sig = strings.ReplaceAll(sig, "/", "_")

	return sig, nil
}

func looksLikeHTML(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return false
	}
	// Common markers of an HTML error page
	if bytes.HasPrefix(trimmed, []byte("<!DOCTYPE html")) || bytes.HasPrefix(trimmed, []byte("<html")) {
		return true
	}
	if bytes.Contains(trimmed, []byte("Cloudflare")) {
		return true
	}
	return false
}

// shortKey returns a truncated version of a key for safe logging.
func shortKey(key string) string {
	if len(key) <= 6 {
		return key
	}
	return key[:6]
}

// parseAPIKeyCredentials attempts to decode various response shapes from /auth/api-key or /auth/derive-api-key.
// Some deployments return flat fields, others nest under "apiKey" or use camelCase keys.
// If the response is not valid JSON, we return a parse error to aid debugging.
func parseAPIKeyCredentials(body []byte) (*APIKeyCredentials, error) {
	// First attempt: parse into a generic map to catch JSON syntax errors early.
	var generic map[string]interface{}
	if err := json.Unmarshal(body, &generic); err != nil {
		return nil, fmt.Errorf("failed to parse auth response: %w", err)
	}

	// Helper to extract a string from the generic map.
	get := func(m map[string]interface{}, keys ...string) string {
		for _, k := range keys {
			if v, ok := m[k]; ok {
				if s, ok := v.(string); ok {
					return strings.TrimSpace(s)
				}
			}
		}
		return ""
	}

	// 1) Direct fields on the root object.
	rootCreds := APIKeyCredentials{
		Key:        get(generic, "key", "apiKey", "apikey", "api_key", "key_id", "apiKeyId"),
		Secret:     get(generic, "secret", "apiSecret", "api_secret"),
		Passphrase: get(generic, "passphrase", "apiPassphrase", "api_passphrase"),
	}
	if rootCreds.Key != "" && rootCreds.Secret != "" && rootCreds.Passphrase != "" {
		return &rootCreds, nil
	}

	// 2) Nested "apiKey" object if present.
	if nestedRaw, ok := generic["apiKey"]; ok {
		if nestedMap, ok := nestedRaw.(map[string]interface{}); ok {
			creds := APIKeyCredentials{
				Key:        get(nestedMap, "key", "apiKey", "apikey", "api_key", "key_id", "apiKeyId"),
				Secret:     get(nestedMap, "secret", "apiSecret", "api_secret"),
				Passphrase: get(nestedMap, "passphrase", "apiPassphrase", "api_passphrase"),
			}
			if creds.Key != "" && creds.Secret != "" && creds.Passphrase != "" {
				return &creds, nil
			}
		}
	}

	// 3) As a fallback, attempt strict struct decoding in case types were clearer there.
	var flat APIKeyCredentials
	if err := json.Unmarshal(body, &flat); err == nil {
		if flat.Key != "" && flat.Secret != "" && flat.Passphrase != "" {
			return &flat, nil
		}
	}

	return nil, fmt.Errorf("auth response missing credentials")
}
