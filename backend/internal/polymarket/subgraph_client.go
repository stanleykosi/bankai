package polymarket

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
	defaultSubgraphTimeout   = 20 * time.Second
	defaultSubgraphPageSize  = 1000
	defaultSubgraphMaxPages  = 120
	DefaultSubgraphEndpoint  = "https://api.goldsky.com/api/public/project_cl6mb8i9h0003e201j6li0diw/subgraphs/orderbook-subgraph/0.0.1/gn"
	defaultCollateralAssetID = "0x2791bca1f2de4661ed88a30c99a7a9449aa84174" // USDC on Polygon per Polymarket docs
)

var orderFilledQuery = `
query ($timestamp: BigInt!) {
  orderFilledEvents(
    where: { timestamp_gt: $timestamp }
    first: 1000
    orderBy: timestamp
    orderDirection: asc
  ) {
    id
    timestamp
    transactionHash
    maker
    taker
    makerAmountFilled
    takerAmountFilled
    makerAssetId
    takerAssetId
  }
}
`

// OrderFilledEvent represents a single fill returned by the orderbook subgraph.
type OrderFilledEvent struct {
	ID                string
	Timestamp         int64
	TransactionHash   string
	Maker             string
	Taker             string
	MakerAmountFilled string
	TakerAmountFilled string
	MakerAssetID      string
	TakerAssetID      string
}

// SubgraphClient queries the Goldsky Orderbook subgraph for historical trades.
type SubgraphClient struct {
	Endpoint          string
	HTTPClient        *http.Client
	CollateralAssetID string
	RetryMax          int
	RetryBase         time.Duration
}

// NewSubgraphClient constructs a client using configuration defaults and sane fallbacks.
func NewSubgraphClient(cfg *config.Config) *SubgraphClient {
	endpoint := DefaultSubgraphEndpoint
	collateral := defaultCollateralAssetID
	if cfg != nil {
		if strings.TrimSpace(cfg.Polymarket.OrderbookSubgraphURL) != "" {
			endpoint = strings.TrimSpace(cfg.Polymarket.OrderbookSubgraphURL)
		}
		if strings.TrimSpace(cfg.Polymarket.CollateralAssetID) != "" {
			collateral = strings.ToLower(strings.TrimSpace(cfg.Polymarket.CollateralAssetID))
		}
	}

	return &SubgraphClient{
		Endpoint:          endpoint,
		CollateralAssetID: collateral,
		HTTPClient: &http.Client{
			Timeout: defaultSubgraphTimeout,
		},
		RetryMax:  3,
		RetryBase: 250 * time.Millisecond,
	}
}

// FetchOrderFilledEvents returns order fills after the provided timestamp, paginated by timestamp cursor.
func (c *SubgraphClient) FetchOrderFilledEvents(ctx context.Context, since time.Time, maxPages int) ([]OrderFilledEvent, error) {
	if c == nil {
		return nil, fmt.Errorf("subgraph client is not configured")
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: defaultSubgraphTimeout}
	}
	if maxPages <= 0 {
		maxPages = defaultSubgraphMaxPages
	}

	cursor := since.Unix()
	if cursor < 0 {
		cursor = 0
	}

	results := make([]OrderFilledEvent, 0)

	for page := 0; page < maxPages; page++ {
		pageEvents, err := c.queryOrderFills(ctx, cursor)
		if err != nil {
			return results, err
		}
		if len(pageEvents) == 0 {
			break
		}

		results = append(results, pageEvents...)

		lastTS := pageEvents[len(pageEvents)-1].Timestamp
		if lastTS <= cursor {
			// Prevent infinite loops if the subgraph returns unexpected ordering.
			break
		}
		cursor = lastTS

		if len(pageEvents) < defaultSubgraphPageSize {
			break
		}
	}

	return results, nil
}

func (c *SubgraphClient) queryOrderFills(ctx context.Context, timestamp int64) ([]OrderFilledEvent, error) {
	payload := map[string]interface{}{
		"query": orderFilledQuery,
		"variables": map[string]interface{}{
			"timestamp": timestamp,
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.doRequestWithRetry(ctx, req, data)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var parsed struct {
		Data struct {
			Events []struct {
				ID                string `json:"id"`
				Timestamp         string `json:"timestamp"`
				TransactionHash   string `json:"transactionHash"`
				Maker             string `json:"maker"`
				Taker             string `json:"taker"`
				MakerAmountFilled string `json:"makerAmountFilled"`
				TakerAmountFilled string `json:"takerAmountFilled"`
				MakerAssetID      string `json:"makerAssetId"`
				TakerAssetID      string `json:"takerAssetId"`
			} `json:"orderFilledEvents"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}

	if len(parsed.Errors) > 0 {
		msgs := make([]string, 0, len(parsed.Errors))
		for _, e := range parsed.Errors {
			msgs = append(msgs, e.Message)
		}
		return nil, fmt.Errorf("subgraph query failed: %s", strings.Join(msgs, "; "))
	}

	events := make([]OrderFilledEvent, 0, len(parsed.Data.Events))
	for _, raw := range parsed.Data.Events {
		ts, err := strconv.ParseInt(strings.TrimSpace(raw.Timestamp), 10, 64)
		if err != nil {
			continue
		}
		events = append(events, OrderFilledEvent{
			ID:                raw.ID,
			Timestamp:         ts,
			TransactionHash:   raw.TransactionHash,
			Maker:             strings.ToLower(strings.TrimSpace(raw.Maker)),
			Taker:             strings.ToLower(strings.TrimSpace(raw.Taker)),
			MakerAmountFilled: strings.TrimSpace(raw.MakerAmountFilled),
			TakerAmountFilled: strings.TrimSpace(raw.TakerAmountFilled),
			MakerAssetID:      strings.ToLower(strings.TrimSpace(raw.MakerAssetID)),
			TakerAssetID:      strings.ToLower(strings.TrimSpace(raw.TakerAssetID)),
		})
	}

	return events, nil
}

func (c *SubgraphClient) doRequestWithRetry(ctx context.Context, req *http.Request, body []byte) (*http.Response, error) {
	attempts := c.RetryMax
	if attempts <= 0 {
		attempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		cloned := req.Clone(ctx)
		if len(body) > 0 {
			cloned.Body = io.NopCloser(bytes.NewReader(body))
		}

		resp, err := c.HTTPClient.Do(cloned)
		if err != nil {
			lastErr = err
		} else if shouldRetrySubgraph(resp.StatusCode) {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("subgraph retryable status %d", resp.StatusCode)
		} else if resp.StatusCode != http.StatusOK {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("subgraph error: status %d", resp.StatusCode)
		} else {
			return resp, nil
		}

		if attempt < attempts-1 {
			sleepWithContextSubgraph(ctx, c.retryDelay(attempt))
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("subgraph request failed")
	}
	return nil, lastErr
}

func shouldRetrySubgraph(status int) bool {
	if status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout {
		return true
	}
	return status >= 500
}

func (c *SubgraphClient) retryDelay(attempt int) time.Duration {
	base := c.RetryBase
	if base <= 0 {
		base = 250 * time.Millisecond
	}
	d := base * time.Duration(1<<attempt)
	if d > 5*time.Second {
		return 5 * time.Second
	}
	return d
}

func sleepWithContextSubgraph(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
