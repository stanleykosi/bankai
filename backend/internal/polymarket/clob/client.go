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
	var resp CancelResponse
	if err := c.sendRequestDecode(ctx, http.MethodDelete, "/order", req, &resp, userCreds); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CancelOrders cancels multiple orders
func (c *Client) CancelOrders(ctx context.Context, req *CancelOrdersRequest, userCreds *APIKeyCredentials) (*CancelResponse, error) {
	var resp CancelResponse
	if err := c.sendRequestDecode(ctx, http.MethodDelete, "/orders", req, &resp, userCreds); err != nil {
		return nil, err
	}
	return &resp, nil
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
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Sign the request for Builder Attribution
	if err := c.setHeaders(req, body, userCreds); err != nil {
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

	// Debug: surface request context when a 400 occurs to diagnose payload issues.
	if resp.StatusCode >= 400 {
		// Build a safe request descriptor
		method := ""
		if req != nil {
			method = req.Method
		}

		// Avoid logging secrets; only include owner key for correlation when present.
		var ownerKey string
		if payload != nil {
			if po, ok := payload.(*PostOrderRequest); ok {
				ownerKey = po.Owner
			}
		}

		// For URL clarity, decode path+query only.
		path := req.URL.Path
		if req.URL.RawQuery != "" {
			path = fmt.Sprintf("%s?%s", path, req.URL.RawQuery)
		}
		if decoded, derr := url.QueryUnescape(path); derr == nil {
			path = decoded
		}

		logger.Error(
			"CLOB 4xx: status=%d method=%s path=%s owner=%s body=%s",
			resp.StatusCode, method, path, shortKey(ownerKey), string(respBody),
		)
	}

	if resp.StatusCode >= 400 {
		if looksLikeHTML(respBody) {
			return fmt.Errorf("clob waf blocked request (status %d)", resp.StatusCode)
		}
		var errResp ErrorResponse
		if jsonErr := json.Unmarshal(respBody, &errResp); jsonErr == nil && errResp.Error != "" {
			return fmt.Errorf("clob error (%d): %s | body: %s", resp.StatusCode, errResp.Error, string(respBody))
		}
		// Try parsing as PostOrderResponse errorMsg
		var poResp PostOrderResponse
		if jsonErr := json.Unmarshal(respBody, &poResp); jsonErr == nil && !poResp.Success {
			return fmt.Errorf("clob error (%d): %s | body: %s", resp.StatusCode, poResp.ErrorMsg, string(respBody))
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

func (c *Client) setHeaders(req *http.Request, body []byte, userCreds *APIKeyCredentials) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	// Use a browser-like UA to avoid aggressive WAF heuristics.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; BankaiTerminal/1.0; +https://polymarket.com)")

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
	if req.URL.RawQuery != "" {
		path = fmt.Sprintf("%s?%s", path, req.URL.RawQuery)
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
