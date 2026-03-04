/**
 * @description
 * HTTP Client for the Polymarket Gamma API.
 * Fetches markets, events, and metadata.
 *
 * @dependencies
 * - net/http
 * - encoding/json
 * - backend/internal/config
 */

package gamma

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	RetryMax   int
	RetryBase  time.Duration
}

func NewClient(cfg *config.Config) *Client {
	retryMax := 3
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
		BaseURL: cfg.Polymarket.GammaURL,
		HTTPClient: &http.Client{
			Timeout: DefaultTimeout,
		},
		RetryMax:  retryMax,
		RetryBase: retryBase,
	}
}

// GetEventsParams holds query parameters for fetching events
type GetEventsParams struct {
	Limit     int
	Offset    int
	Active    *bool
	Closed    *bool
	Order     string // "volume", "liquidity", "createdAt"
	Ascending *bool
	Slug      string
}

// GetEvents fetches a list of events from Gamma
func (c *Client) GetEvents(ctx context.Context, params GetEventsParams) ([]GammaEvent, error) {
	u, err := url.Parse(fmt.Sprintf("%s/events", c.BaseURL))
	if err != nil {
		return nil, err
	}

	q := u.Query()
	if params.Limit > 0 {
		q.Set("limit", strconv.Itoa(params.Limit))
	}
	if params.Offset > 0 {
		q.Set("offset", strconv.Itoa(params.Offset))
	}
	if params.Active != nil {
		q.Set("active", strconv.FormatBool(*params.Active))
	}
	if params.Closed != nil {
		q.Set("closed", strconv.FormatBool(*params.Closed))
	}
	if params.Order != "" {
		q.Set("order", params.Order)
	}
	if params.Ascending != nil {
		q.Set("ascending", strconv.FormatBool(*params.Ascending))
	}
	if params.Slug != "" {
		q.Set("slug", params.Slug)
	}

	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gamma api error: status %d", resp.StatusCode)
	}

	var events []GammaEvent
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		return nil, err
	}

	return events, nil
}

// GetMarketParams holds query parameters for fetching markets directly
type GetMarketParams struct {
	ID string
}

// GetMarket fetches a single market by ID
func (c *Client) GetMarket(ctx context.Context, id string) (*GammaMarket, error) {
	u := fmt.Sprintf("%s/markets/%s", c.BaseURL, id)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gamma api error: status %d", resp.StatusCode)
	}

	var market GammaMarket
	if err := json.NewDecoder(resp.Body).Decode(&market); err != nil {
		return nil, err
	}

	return &market, nil
}

// SearchProfiles queries Gamma's /public-search endpoint focusing on user profiles.
// Documentation reference: polymarket_documentation.md -> "Search markets, events, and profiles"
func (c *Client) SearchProfiles(ctx context.Context, query string, limit int) ([]Profile, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}

	if limit <= 0 {
		limit = 1
	}

	u, err := url.Parse(fmt.Sprintf("%s/public-search", c.BaseURL))
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("q", query)
	q.Set("search_profiles", "true")
	q.Set("limit_per_type", strconv.Itoa(limit))
	q.Set("cache", "false")
	q.Set("optimized", "false")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequestWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gamma search error: status %d", resp.StatusCode)
	}

	var searchResp SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, err
	}

	return searchResp.Profiles, nil
}

func (c *Client) doRequestWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	attempts := c.RetryMax
	if attempts <= 0 {
		attempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		cloned := req.Clone(ctx)
		resp, err := c.HTTPClient.Do(cloned)
		if err != nil {
			lastErr = err
		} else if shouldRetryGammaStatus(resp.StatusCode) {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("gamma api retryable status %d", resp.StatusCode)
		} else {
			return resp, nil
		}

		if attempt < attempts-1 {
			sleepWithContext(ctx, c.retryDelay(attempt))
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("gamma api request failed")
	}
	return nil, lastErr
}

func shouldRetryGammaStatus(status int) bool {
	if status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout {
		return true
	}
	return status >= 500
}

func (c *Client) retryDelay(attempt int) time.Duration {
	base := c.RetryBase
	if base <= 0 {
		base = 200 * time.Millisecond
	}
	delay := base * time.Duration(1<<attempt)
	if delay > 5*time.Second {
		return 5 * time.Second
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
