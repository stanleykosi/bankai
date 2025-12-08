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
	"strconv"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/config"
)

const (
	DefaultTimeout = 10 * time.Second
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
		BaseURL:    cfg.Polymarket.ClobURL,
		APIKey:     cfg.Polymarket.BuilderAPIKey,
		APISecret:  cfg.Polymarket.BuilderSecret,
		Passphrase: cfg.Polymarket.BuilderPass,
		HTTPClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
}

// PostOrder sends a single order to the CLOB
func (c *Client) PostOrder(ctx context.Context, req *PostOrderRequest) (*PostOrderResponse, error) {
	return c.sendRequest(ctx, http.MethodPost, "/order", req)
}

// PostOrders sends a batch of orders to the CLOB
func (c *Client) PostOrders(ctx context.Context, req PostOrdersRequest) (*PostOrderResponse, error) {
	// Note: The response structure for batch orders might differ slightly in practice,
	// but usually follows standard success/error patterns. We'll map to PostOrderResponse for now.
	return c.sendRequest(ctx, http.MethodPost, "/orders", req)
}

// CancelOrder cancels a single order
func (c *Client) CancelOrder(ctx context.Context, req *CancelOrderRequest) (*CancelResponse, error) {
	var resp CancelResponse
	if err := c.sendRequestDecode(ctx, http.MethodDelete, "/order", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CancelOrders cancels multiple orders
func (c *Client) CancelOrders(ctx context.Context, req *CancelOrdersRequest) (*CancelResponse, error) {
	var resp CancelResponse
	if err := c.sendRequestDecode(ctx, http.MethodDelete, "/orders", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetBook fetches the current order book for a token (asset) from the CLOB API.
func (c *Client) GetBook(ctx context.Context, tokenID string) (*BookResponse, error) {
	if strings.TrimSpace(tokenID) == "" {
		return nil, fmt.Errorf("tokenID is required")
	}
	u := fmt.Sprintf("/book?tokenId=%s", tokenID)

	var resp BookResponse
	if err := c.sendRequestDecode(ctx, http.MethodGet, u, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// sendRequest sends a generic request and expects a PostOrderResponse (common for trades)
func (c *Client) sendRequest(ctx context.Context, method, path string, payload interface{}) (*PostOrderResponse, error) {
	var result PostOrderResponse
	if err := c.sendRequestDecode(ctx, method, path, payload, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// sendRequestDecode handles the low-level HTTP construction, signing, and response decoding
func (c *Client) sendRequestDecode(ctx context.Context, method, path string, payload interface{}, result interface{}) error {
	var body []byte
	var err error

	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}
	}

	u := fmt.Sprintf("%s%s", c.BaseURL, path)
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Sign the request for Builder Attribution
	if err := c.setHeaders(req, body); err != nil {
		return fmt.Errorf("failed to sign request: %w", err)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("clob request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errResp ErrorResponse
		if jsonErr := json.Unmarshal(respBody, &errResp); jsonErr == nil && errResp.Error != "" {
			return fmt.Errorf("clob error (%d): %s", resp.StatusCode, errResp.Error)
		}
		// Try parsing as PostOrderResponse errorMsg
		var poResp PostOrderResponse
		if jsonErr := json.Unmarshal(respBody, &poResp); jsonErr == nil && !poResp.Success {
			return fmt.Errorf("clob error (%d): %s", resp.StatusCode, poResp.ErrorMsg)
		}
		return fmt.Errorf("clob error (%d): %s", resp.StatusCode, string(respBody))
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

func (c *Client) setHeaders(req *http.Request, body []byte) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Bankai-Terminal/1.0")

	// If no builder credentials, we can't sign.
	// However, we shouldn't fail if they aren't configured, just skip attribution.
	// But the project requirements specifically ask for Builder Attribution.
	if c.APIKey == "" || c.APISecret == "" || c.Passphrase == "" {
		return fmt.Errorf("missing builder credentials: POLY_BUILDER_API_KEY, SECRET, and PASSPHRASE are required for CLOB requests")
	}

	path := req.URL.Path
	if path == "" {
		path = "/"
	}

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

	// Try URL-safe base64 decoding first
	decodedSecret, err = base64.URLEncoding.DecodeString(normalizedSecret)
	if err != nil {
		// Fallback to standard base64
		decodedSecret, err = base64.StdEncoding.DecodeString(normalizedSecret)
		if err != nil {
			// Fallback to treating secret as raw bytes (some environments might inject it raw)
			decodedSecret = []byte(normalizedSecret)
		}
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
