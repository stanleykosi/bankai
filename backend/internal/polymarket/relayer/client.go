/**
 * @description
 * HTTP Client for the Polymarket Relayer API.
 * Handles interactions with the Gas Station Network (GSN) Relayer for
 * gasless transactions and Safe wallet deployment.
 *
 * @dependencies
 * - net/http
 * - backend/internal/config
 * - backend/internal/logger
 *
 * @notes
 * - Relayer URL: https://relayer-v2.polymarket.com/ (from docs)
 * - Endpoint: POST /submit (from "Other API Rate Limits" docs)
 * - Auth: Builder API Headers (POLY_BUILDER_API_KEY)
 * - Deployment: Involves sending a transaction to the Gnosis Proxy Factory.
 */

package relayer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/logger"
)

const (
	DefaultTimeout = 30 * time.Second
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
		BaseURL:    cfg.Polymarket.RelayerURL,
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

// RelayerResponse is the response from /submit
type RelayerResponse struct {
	TransactionHash string `json:"transactionHash"`
	TaskID          string `json:"taskId"`
	State           string `json:"state"`                  // PENDING, MINED, etc.
	ProxyAddress    string `json:"proxyAddress,omitempty"` // Safe address after deployment (may not be in initial response)
}

// RelayerError represents an error response from the relayer
type RelayerError struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

type deployedResponse struct {
	Deployed bool `json:"deployed"`
}

type nonceResponse struct {
	Nonce string `json:"nonce"`
}

// DeploySafe submits a SAFE-CREATE TransactionRequest to the relayer.
func (c *Client) DeploySafe(ctx context.Context, request *TransactionRequest) (*RelayerResponse, error) {
	if request == nil {
		return nil, fmt.Errorf("transaction request cannot be nil")
	}
	return c.submitTransaction(ctx, request)
}

// SubmitSafeTransaction submits a pre-signed SAFE transaction request.
func (c *Client) SubmitSafeTransaction(ctx context.Context, request *TransactionRequest) (*RelayerResponse, error) {
	if request == nil {
		return nil, fmt.Errorf("transaction request cannot be nil")
	}
	if request.Type != TransactionTypeSafe {
		return nil, fmt.Errorf("transaction type must be SAFE")
	}
	return c.submitTransaction(ctx, request)
}

// GetDeployed checks whether a Safe has already been deployed for the derived proxy address.
func (c *Client) GetDeployed(ctx context.Context, safeAddress string) (bool, error) {
	if safeAddress == "" {
		return false, fmt.Errorf("safe address cannot be empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/deployed?address=%s", c.BaseURL, safeAddress), nil)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req, nil); err != nil {
		return false, fmt.Errorf("failed to sign relayer request: %w", err)
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return false, fmt.Errorf("relayer request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		logger.Error("Relayer GET /deployed status=%d address=%s body=%s", resp.StatusCode, safeAddress, truncate(string(body), 600))
		return false, fmt.Errorf("relayer returned status %d: %s", resp.StatusCode, string(body))
	}

	var result deployedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Deployed, nil
}

// GetNonce returns the next relayer nonce for a signer address and transaction type.
func (c *Client) GetNonce(ctx context.Context, signerAddress string, txType TransactionType) (string, error) {
	signerAddress = strings.TrimSpace(signerAddress)
	if signerAddress == "" {
		return "", fmt.Errorf("signer address is required")
	}
	if txType == "" {
		txType = TransactionTypeSafe
	}

	u := fmt.Sprintf("%s/nonce?address=%s&type=%s", c.BaseURL, signerAddress, string(txType))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	if err := c.setHeaders(req, nil); err != nil {
		return "", fmt.Errorf("failed to sign relayer request: %w", err)
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return "", fmt.Errorf("relayer request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("relayer returned status %d: %s", resp.StatusCode, string(body))
	}

	var out nonceResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("failed to decode nonce response: %w", err)
	}
	if strings.TrimSpace(out.Nonce) == "" {
		return "", fmt.Errorf("relayer returned empty nonce")
	}
	return out.Nonce, nil
}

// submitTransaction sends a transaction to the relayer
func (c *Client) submitTransaction(ctx context.Context, payload interface{}) (*RelayerResponse, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Endpoint from "Other API Rate Limits": RELAYER /submit
	u := fmt.Sprintf("%s/submit", c.BaseURL)

	req, err := http.NewRequestWithContext(ctx, "POST", u, bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.setHeaders(req, data); err != nil {
		return nil, fmt.Errorf("failed to sign relayer request: %w", err)
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("relayer request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		// Read error body for better error messages
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("relayer returned status %d (failed to read error body: %v)", resp.StatusCode, readErr)
		}

		logger.Error("Relayer /submit status=%d body=%s", resp.StatusCode, truncate(string(body), 800))

		// Try to parse as JSON error
		var relayerErr RelayerError
		if jsonErr := json.Unmarshal(body, &relayerErr); jsonErr == nil && relayerErr.Message != "" {
			return nil, fmt.Errorf("relayer error (status %d): %s", resp.StatusCode, relayerErr.Message)
		}

		// Fallback to raw body if not JSON
		return nil, fmt.Errorf("relayer returned status %d: %s", resp.StatusCode, string(body))
	}

	var result RelayerResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

func (c *Client) setHeaders(req *http.Request, body []byte) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Bankai-Terminal/1.0")

	if c.APIKey == "" || c.APISecret == "" || c.Passphrase == "" {
		return fmt.Errorf("builder credentials are not configured")
	}

	// Always include the key header for correlation/allowlist checks.
	req.Header.Set("POLY_BUILDER_API_KEY", c.APIKey)

	// Ensure we have just the path portion (plus query) for signing (e.g., /submit)
	path := req.URL.Path
	if path == "" {
		path = "/"
	}
	if req.URL.RawQuery != "" {
		path = fmt.Sprintf("%s?%s", path, req.URL.RawQuery)
	}

	// Relayer docs require uppercase method in the signature payload.
	method := strings.ToUpper(req.Method)
	timestamp := time.Now().Unix()

	sig, err := buildBuilderSignature(c.APISecret, timestamp, method, path, body)
	if err != nil {
		return err
	}

	req.Header.Set("POLY_BUILDER_PASSPHRASE", c.Passphrase)
	req.Header.Set("POLY_BUILDER_SIGNATURE", sig)
	req.Header.Set("POLY_BUILDER_TIMESTAMP", strconv.FormatInt(timestamp, 10))

	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func (c *Client) doRequestWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	maxAttempts := c.RetryMax
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		cloned := req.Clone(ctx)
		if req.GetBody != nil {
			body, getErr := req.GetBody()
			if getErr != nil {
				return nil, getErr
			}
			cloned.Body = body
		}
		resp, err := c.HTTPClient.Do(cloned)
		if err != nil {
			lastErr = err
			if attempt < maxAttempts && shouldRetryTransport(cloned.Method) {
				sleepWithContext(ctx, c.retryDelay(attempt))
				continue
			}
			return nil, lastErr
		}

		if shouldRetryRequest(cloned.Method, resp.StatusCode) && attempt < maxAttempts {
			resp.Body.Close()
			sleepWithContext(ctx, c.retryDelay(attempt))
			continue
		}

		return resp, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("request failed")
}

func shouldRetryStatus(status int) bool {
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
		return shouldRetryStatus(status)
	default:
		// Avoid replaying non-idempotent requests such as POST /submit.
		return false
	}
}

func shouldRetryTransport(method string) bool {
	method = strings.ToUpper(strings.TrimSpace(method))
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
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

// CheckAuth performs a lightweight POST /submit with a no-op transaction to verify credentials.
// The relayer will reject the payload, but we only care that we pass authentication (i.e., avoid 401).
func (c *Client) CheckAuth(ctx context.Context) error {
	dummy := TransactionRequest{
		Type:      TransactionTypeSafeCreate,
		From:      ZeroAddress,
		To:        SafeFactoryAddress,
		Data:      "0x",
		Signature: "0x",
		SignatureParams: SignatureParams{
			PaymentToken:    ZeroAddress,
			Payment:         paymentValue,
			PaymentReceiver: ZeroAddress,
		},
	}

	_, err := c.submitTransaction(ctx, dummy)
	return err
}
