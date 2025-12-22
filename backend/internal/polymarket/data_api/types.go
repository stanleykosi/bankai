/**
 * @description
 * Type definitions for Polymarket Data API responses.
 * These structs map to the JSON returned by endpoints like /positions, /holders, /trades.
 *
 * API Base URL: https://data-api.polymarket.com/
 */

package data_api

import (
	"encoding/json"
	"strconv"
	"time"
)

// Position represents a user's current open position
type Position struct {
	Asset            string  `json:"asset"`
	ConditionID      string  `json:"conditionId"`
	TokenID          string  `json:"tokenId"`
	Outcome          string  `json:"outcome"`
	Size             float64 `json:"size"`
	AveragePrice     float64 `json:"avgPrice"`
	CurrentPrice     float64 `json:"curPrice"`
	InitialValue     float64 `json:"initialValue"`
	CurrentValue     float64 `json:"currentValue"`
	CashPnL          float64 `json:"cashPnl"`
	PercentPnL       float64 `json:"percentPnl"`
	TotalBought      float64 `json:"totalBought"`
	TotalSold        float64 `json:"totalSold"`
	RealizedPnL      float64 `json:"realizedPnl"`
	UnrealizedPnL    float64 `json:"unrealizedPnl"`
	PctUnrealizedPnL float64 `json:"pctUnrealizedPnl"`
	PctRealizedPnL   float64 `json:"pctRealizedPnl"`
	MarketSlug       string  `json:"slug"`
	MarketQuestion   string  `json:"question"`
	ProxyWallet      string  `json:"proxyWallet"`
	Owner            string  `json:"owner"`
}

// ClosedPosition represents a user's closed/resolved position
type ClosedPosition struct {
	Asset         string    `json:"asset"`
	ConditionID   string    `json:"conditionId"`
	TokenID       string    `json:"tokenId"`
	Outcome       string    `json:"outcome"`
	Size          float64   `json:"size"`
	AveragePrice  float64   `json:"avgPrice"`
	ExitPrice     float64   `json:"exitPrice"`
	InitialValue  float64   `json:"initialValue"`
	ExitValue     float64   `json:"exitValue"`
	RealizedPnL   float64   `json:"realizedPnl"`
	PctPnL        float64   `json:"pctPnl"`
	MarketSlug    string    `json:"slug"`
	MarketQuestion string   `json:"question"`
	ClosedAt      time.Time `json:"closedAt"`
	Resolved      bool      `json:"resolved"`
	Winner        bool      `json:"winner"`
}

// Holder represents a token holder from the /holders endpoint
type Holder struct {
	Address      string  `json:"address"`
	ProxyAddress string  `json:"proxyAddress"`
	Size         float64 `json:"size"`
	Value        float64 `json:"value"`
	Percentage   float64 `json:"percentage"`
	// Profile info (if available)
	ProfileName  string `json:"profileName,omitempty"`
	ProfileImage string `json:"profileImage,omitempty"`
}

// Trade represents a single trade from the /trades endpoint
type Trade struct {
	ID            string    `json:"id"`
	ConditionID   string    `json:"conditionId"`
	TokenID       string    `json:"tokenId"`
	Outcome       string    `json:"outcome"`
	Side          string    `json:"side"` // BUY or SELL
	Price         float64   `json:"price"`
	Size          float64   `json:"size"`
	Value         float64   `json:"value"`
	Maker         string    `json:"maker"`
	Taker         string    `json:"taker"`
	MakerIsBuyer  bool      `json:"makerIsBuyer"`
	TradeOwner    string    `json:"tradeOwner"`
	MarketSlug    string    `json:"slug"`
	MarketQuestion string   `json:"question"`
	Timestamp     time.Time `json:"timestamp"`
	TxHash        string    `json:"transactionHash"`
}

// TradedStats represents trading volume stats from /traded endpoint
type TradedStats struct {
	TotalVolume    float64 `json:"totalVolume"`
	TotalTrades    int     `json:"totalTrades"`
	AvgTradeSize   float64 `json:"avgTradeSize"`
	BuyVolume      float64 `json:"buyVolume"`
	SellVolume     float64 `json:"sellVolume"`
	FirstTradeDate string  `json:"firstTradeDate"`
	LastTradeDate  string  `json:"lastTradeDate"`
}

// PnLData represents profit/loss data from /{user}/pnl endpoint
type PnLData struct {
	TotalPnL       float64 `json:"totalPnl"`
	RealizedPnL    float64 `json:"realizedPnl"`
	UnrealizedPnL  float64 `json:"unrealizedPnl"`
	PercentPnL     float64 `json:"percentPnl"`
	WinningTrades  int     `json:"winningTrades"`
	LosingTrades   int     `json:"losingTrades"`
	WinRate        float64 `json:"winRate"`
	TotalPositions int     `json:"totalPositions"`
}

// TraderProfile aggregates all profile data for a trader
type TraderProfile struct {
	Address        string      `json:"address"`
	ProxyWallet    string      `json:"proxy_wallet"`
	ProfileName    string      `json:"profile_name"`
	ProfileImage   string      `json:"profile_image"`
	Bio            string      `json:"bio"`
	IsVerified     bool        `json:"is_verified"`
	ENSName        string      `json:"ens_name,omitempty"`
	LensHandle     string      `json:"lens_handle,omitempty"`
	JoinedAt       string      `json:"joined_at"`
	Stats          *TraderStats `json:"stats,omitempty"`
}

// TraderStats contains calculated performance metrics
type TraderStats struct {
	WinRate         float64 `json:"win_rate"`
	TotalVolume     float64 `json:"total_volume"`
	RealizedPnL     float64 `json:"realized_pnl"`
	TotalTrades     int     `json:"total_trades"`
	WinningTrades   int     `json:"winning_trades"`
	LosingTrades    int     `json:"losing_trades"`
	OpenPositions   int     `json:"open_positions"`
	ClosedPositions int     `json:"closed_positions"`
	AvgTradeSize    float64 `json:"avg_trade_size"`
}

// ActivityDataPoint represents a single day's trading activity for heatmap
type ActivityDataPoint struct {
	Date       string  `json:"date"`
	TradeCount int     `json:"trade_count"`
	Volume     float64 `json:"volume"`
	Level      int     `json:"level"` // 0-4 intensity level for heatmap
}

// PositionsParams query parameters for /positions endpoint
type PositionsParams struct {
	Limit         int    `json:"limit,omitempty"`
	Offset        int    `json:"offset,omitempty"`
	SortBy        string `json:"sortBy,omitempty"`        // SIZE, CASHPNL, PERCENTPNL
	SortDirection string `json:"sortDirection,omitempty"` // ASC, DESC
	Market        string `json:"market,omitempty"`        // Filter by conditionId
}

// TradesParams query parameters for /trades endpoint
type TradesParams struct {
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
	Before string `json:"before,omitempty"` // ISO timestamp
	After  string `json:"after,omitempty"`  // ISO timestamp
}

// parseFloatSafe converts interface to float64 safely
func parseFloatSafe(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case json.Number:
		f, _ := val.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	case int:
		return float64(val)
	case int64:
		return float64(val)
	default:
		return 0
	}
}

// parseIntSafe converts interface to int safely
func parseIntSafe(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	case json.Number:
		i, _ := val.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(val)
		return i
	default:
		return 0
	}
}
