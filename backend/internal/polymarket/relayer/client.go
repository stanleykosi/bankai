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
}

func NewClient(cfg *config.Config) *Client {
	return &Client{
		BaseURL:    cfg.Polymarket.RelayerURL,
		APIKey:     cfg.Polymarket.BuilderAPIKey,
		APISecret:  cfg.Polymarket.BuilderSecret,
		Passphrase: cfg.Polymarket.BuilderPass,
		HTTPClient: &http.Client{
			Timeout: DefaultTimeout,
		},
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

// DeploySafe submits a SAFE-CREATE TransactionRequest to the relayer.
func (c *Client) DeploySafe(ctx context.Context, request *TransactionRequest) (*RelayerResponse, error) {
	if request == nil {
		return nil, fmt.Errorf("transaction request cannot be nil")
	}
	return c.submitTransaction(ctx, request)
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

	resp, err := c.HTTPClient.Do(req)
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

	// Ensure we have just the path portion for signing (e.g., /submit)
	path := req.URL.Path
	if path == "" {
		path = "/"
	}

	// Relayer docs require uppercase method in the signature payload.
	method := strings.ToUpper(req.Method)
	timestamp := time.Now().Unix()

	sig, err := buildBuilderSignature(c.APISecret, timestamp, method, path, body)
	if err != nil {
		return err
	}

	req.Header.Set("POLY_BUILDER_API_KEY", c.APIKey)
	req.Header.Set("POLY_BUILDER_PASSPHRASE", c.Passphrase)
	req.Header.Set("POLY_BUILDER_SIGNATURE", sig)
	req.Header.Set("POLY_BUILDER_TIMESTAMP", strconv.FormatInt(timestamp, 10))

	return nil
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
