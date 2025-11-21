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
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/bankai-project/backend/internal/config"
)

const (
	DefaultTimeout = 10 * time.Second
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewClient(cfg *config.Config) *Client {
	return &Client{
		BaseURL: cfg.Polymarket.GammaURL,
		HTTPClient: &http.Client{
			Timeout: DefaultTimeout,
		},
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

	resp, err := c.HTTPClient.Do(req)
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

	resp, err := c.HTTPClient.Do(req)
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

