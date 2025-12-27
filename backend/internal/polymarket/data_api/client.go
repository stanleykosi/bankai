/**
 * @description
 * HTTP Client for the Polymarket Data API.
 * Fetches trader positions, trades, holders, and PnL data.
 *
 * API Base URL: https://data-api.polymarket.com/
 *
 * @dependencies
 * - net/http
 * - encoding/json
 * - backend/internal/config
 */

package data_api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/config"
)

const (
	DefaultTimeout     = 15 * time.Second
	DefaultDataAPIURL  = "https://data-api.polymarket.com"
)

// Client for Polymarket Data API
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewClient creates a new Data API client
func NewClient(cfg *config.Config) *Client {
	baseURL := DefaultDataAPIURL
	if cfg.Polymarket.DataAPIURL != "" {
		baseURL = cfg.Polymarket.DataAPIURL
	}

	return &Client{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
}

// GetPositions fetches open positions for an address
// GET /positions?user={address}
func (c *Client) GetPositions(ctx context.Context, address string, params *PositionsParams) ([]Position, error) {
	if address == "" {
		return nil, fmt.Errorf("address is required")
	}

	u, err := url.Parse(fmt.Sprintf("%s/positions", c.BaseURL))
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("user", strings.ToLower(address))
	
	if params != nil {
		if params.Limit > 0 {
			q.Set("limit", strconv.Itoa(params.Limit))
		}
		if params.Offset > 0 {
			q.Set("offset", strconv.Itoa(params.Offset))
		}
		if params.SortBy != "" {
			q.Set("sortBy", params.SortBy)
		}
		if params.SortDirection != "" {
			q.Set("sortDirection", params.SortDirection)
		}
		if params.Market != "" {
			q.Set("market", params.Market)
		}
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("data api error: status %d", resp.StatusCode)
	}

	var positions []Position
	if err := json.NewDecoder(resp.Body).Decode(&positions); err != nil {
		return nil, err
	}

	return positions, nil
}

// GetClosedPositions fetches closed/resolved positions for an address
// GET /closed-positions?user={address}
func (c *Client) GetClosedPositions(ctx context.Context, address string, limit, offset int) ([]ClosedPosition, error) {
	if address == "" {
		return nil, fmt.Errorf("address is required")
	}

	u, err := url.Parse(fmt.Sprintf("%s/closed-positions", c.BaseURL))
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("user", strings.ToLower(address))
	q.Set("sortBy", "REALIZEDPNL")
	q.Set("sortDirection", "DESC")
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("data api error: status %d", resp.StatusCode)
	}

	var positions []ClosedPosition
	if err := json.NewDecoder(resp.Body).Decode(&positions); err != nil {
		return nil, err
	}

	return positions, nil
}

// GetHolders fetches top holders for a market token
// GET /holders?market={conditionId}
func (c *Client) GetHolders(ctx context.Context, conditionID, tokenID string, limit int) ([]Holder, error) {
	if conditionID == "" {
		return nil, fmt.Errorf("conditionID is required")
	}

	u, err := url.Parse(fmt.Sprintf("%s/holders", c.BaseURL))
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("market", conditionID)
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	} else {
		q.Set("limit", "10") // Default to top 10
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("data api error: status %d", resp.StatusCode)
	}

	var metaHolders []MetaHolder
	if err := json.NewDecoder(resp.Body).Decode(&metaHolders); err != nil {
		return nil, err
	}

	return normalizeHolders(metaHolders, tokenID), nil
}

func normalizeHolders(meta []MetaHolder, tokenID string) []Holder {
	if len(meta) == 0 {
		return []Holder{}
	}

	var selected *MetaHolder
	if tokenID != "" {
		for i := range meta {
			if strings.EqualFold(meta[i].Token, tokenID) {
				selected = &meta[i]
				break
			}
		}
		if selected == nil {
			return []Holder{}
		}
	} else {
		selected = &meta[0]
	}

	holders := make([]Holder, 0, len(selected.Holders))
	total := 0.0
	for _, raw := range selected.Holders {
		size := parseFloatSafe(raw.Amount)
		total += size

		holders = append(holders, Holder{
			Address:      strings.ToLower(raw.ProxyWallet),
			ProxyAddress: raw.ProxyWallet,
			Size:         size,
			Value:        0,
			ProfileName:  resolveHolderName(raw),
			ProfileImage: resolveHolderImage(raw),
		})
	}

	if total > 0 {
		for i := range holders {
			holders[i].Percentage = (holders[i].Size / total) * 100
		}
	}

	return holders
}

func resolveHolderName(raw RawHolder) string {
	if raw.DisplayUsernamePublic && raw.Name != "" {
		return raw.Name
	}
	if raw.Pseudonym != "" {
		return raw.Pseudonym
	}
	return raw.Name
}

func resolveHolderImage(raw RawHolder) string {
	if raw.ProfileImageOptimized != "" {
		return raw.ProfileImageOptimized
	}
	return raw.ProfileImage
}

// GetTrades fetches trades for an address
// GET /trades?user={address}
func (c *Client) GetTrades(ctx context.Context, address string, params *TradesParams) ([]Trade, error) {
	if address == "" {
		return nil, fmt.Errorf("address is required")
	}

	u, err := url.Parse(fmt.Sprintf("%s/trades", c.BaseURL))
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("user", strings.ToLower(address))
	
	if params != nil {
		if params.Limit > 0 {
			q.Set("limit", strconv.Itoa(params.Limit))
		}
		if params.Offset > 0 {
			q.Set("offset", strconv.Itoa(params.Offset))
		}
		if params.Before != "" {
			q.Set("before", params.Before)
		}
		if params.After != "" {
			q.Set("after", params.After)
		}
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("data api error: status %d", resp.StatusCode)
	}

	var trades []Trade
	if err := json.NewDecoder(resp.Body).Decode(&trades); err != nil {
		return nil, err
	}

	return trades, nil
}

// GetTradedCount fetches total markets traded for an address
// GET /traded?user={address}
func (c *Client) GetTradedCount(ctx context.Context, address string) (*TradedCount, error) {
	if address == "" {
		return nil, fmt.Errorf("address is required")
	}

	u, err := url.Parse(fmt.Sprintf("%s/traded", c.BaseURL))
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("user", strings.ToLower(address))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("data api error: status %d", resp.StatusCode)
	}

	var stats TradedCount
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return nil, err
	}

	return &stats, nil
}

// GetPnL fetches profit/loss data for an address
// This aggregates data from positions and closed-positions
func (c *Client) GetPnL(ctx context.Context, address string) (*PnLData, error) {
	if address == "" {
		return nil, fmt.Errorf("address is required")
	}

	// Get open positions for unrealized PnL
	positions, err := c.GetPositions(ctx, address, &PositionsParams{Limit: 100})
	if err != nil {
		return nil, fmt.Errorf("failed to get positions: %w", err)
	}

	// Get closed positions for realized PnL
	closedPositions, err := c.GetClosedPositions(ctx, address, 200, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to get closed positions: %w", err)
	}

	// Calculate aggregated PnL
	var unrealizedPnL, realizedPnL float64
	var winningTrades, losingTrades int

	for _, pos := range positions {
		unrealizedPnL += pos.CashPnL
	}

	for _, pos := range closedPositions {
		realizedPnL += pos.RealizedPnL
		if pos.RealizedPnL > 0 {
			winningTrades++
		} else if pos.RealizedPnL < 0 {
			losingTrades++
		}
	}

	totalTrades := winningTrades + losingTrades
	var winRate float64
	if totalTrades > 0 {
		winRate = float64(winningTrades) / float64(totalTrades) * 100
	}

	totalPnL := realizedPnL + unrealizedPnL
	var percentPnL float64
	// Calculate percent based on total invested (sum of initial values)
	var totalInvested float64
	for _, pos := range positions {
		totalInvested += pos.InitialValue
	}
	for _, pos := range closedPositions {
		totalInvested += pos.InitialValue
	}
	if totalInvested > 0 {
		percentPnL = (totalPnL / totalInvested) * 100
	}

	return &PnLData{
		TotalPnL:       totalPnL,
		RealizedPnL:    realizedPnL,
		UnrealizedPnL:  unrealizedPnL,
		PercentPnL:     percentPnL,
		WinningTrades:  winningTrades,
		LosingTrades:   losingTrades,
		WinRate:        winRate,
		TotalPositions: len(positions) + len(closedPositions),
	}, nil
}

// GetActivityHeatmap fetches trading activity for the past year, grouped by day
func (c *Client) GetActivityHeatmap(ctx context.Context, address string) ([]ActivityDataPoint, error) {
	if address == "" {
		return nil, fmt.Errorf("address is required")
	}

	// Get trades for the past year
	oneYearAgo := time.Now().AddDate(-1, 0, 0).Format(time.RFC3339)
	trades, err := c.GetTrades(ctx, address, &TradesParams{
		Limit: 5000,
		After: oneYearAgo,
	})
	if err != nil {
		return nil, err
	}

	// Group trades by day
	activityMap := make(map[string]*ActivityDataPoint)

	resolveTradeTime := func(timestamp int64) time.Time {
		if timestamp <= 0 {
			return time.Time{}
		}
		if timestamp > 1_000_000_000_000 {
			return time.Unix(0, timestamp*int64(time.Millisecond))
		}
		return time.Unix(timestamp, 0)
	}

	for _, trade := range trades {
		tradeTime := resolveTradeTime(trade.Timestamp)
		if tradeTime.IsZero() {
			continue
		}
		dateKey := tradeTime.Format("2006-01-02")
		if _, exists := activityMap[dateKey]; !exists {
			activityMap[dateKey] = &ActivityDataPoint{
				Date:       dateKey,
				TradeCount: 0,
				Volume:     0,
			}
		}
		activityMap[dateKey].TradeCount++
		if trade.Value > 0 {
			activityMap[dateKey].Volume += trade.Value
		} else if trade.Price > 0 && trade.Size > 0 {
			activityMap[dateKey].Volume += trade.Price * trade.Size
		}
	}

	// Convert map to slice and calculate levels
	var result []ActivityDataPoint
	var maxTrades int
	for _, dp := range activityMap {
		if dp.TradeCount > maxTrades {
			maxTrades = dp.TradeCount
		}
		result = append(result, *dp)
	}

	// Assign levels (0-4) based on relative activity
	for i := range result {
		if maxTrades == 0 {
			result[i].Level = 0
		} else {
			ratio := float64(result[i].TradeCount) / float64(maxTrades)
			switch {
			case ratio == 0:
				result[i].Level = 0
			case ratio < 0.25:
				result[i].Level = 1
			case ratio < 0.50:
				result[i].Level = 2
			case ratio < 0.75:
				result[i].Level = 3
			default:
				result[i].Level = 4
			}
		}
	}

	return result, nil
}
