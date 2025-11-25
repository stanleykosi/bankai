/**
 * @description
 * Market and Event database models.
 * Maps to the 'markets' table in PostgreSQL.
 *
 * @dependencies
 * - gorm.io/gorm
 * - github.com/lib/pq (for string array support if needed, though we use string for simplicity or specialized types)
 */

package models

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"time"
)

// StringArray is a helper type to handle string arrays in Postgres (TEXT[])
type StringArray []string

// Scan implements the sql.Scanner interface
func (a *StringArray) Scan(src interface{}) error {
	if src == nil {
		*a = nil
		return nil
	}
	switch v := src.(type) {
	case []byte:
		// PostgreSQL returns arrays as strings like "{value1,value2,value3}"
		return a.parsePostgresArray(string(v))
	case string:
		return a.parsePostgresArray(v)
	default:
		return errors.New("type assertion failed for StringArray")
	}
}

// parsePostgresArray parses PostgreSQL array format: {value1,value2,value3}
func (a *StringArray) parsePostgresArray(s string) error {
	if s == "{}" || s == "" {
		*a = []string{}
		return nil
	}

	// Remove curly braces
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")

	if s == "" {
		*a = []string{}
		return nil
	}

	// Split by comma, handling quoted values
	// Simple approach: split by comma (works for most cases)
	// For production, might need more sophisticated parsing for quoted values with commas
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		// Remove quotes if present
		if len(part) >= 2 && part[0] == '"' && part[len(part)-1] == '"' {
			part = part[1 : len(part)-1]
		}
		result = append(result, part)
	}
	*a = result
	return nil
}

// Value implements the driver.Valuer interface
// Returns PostgreSQL array format: {value1,value2,value3}
func (a StringArray) Value() (driver.Value, error) {
	if a == nil || len(a) == 0 {
		return "{}", nil
	}

	// Format as PostgreSQL array: {value1,value2,value3}
	// Escape and quote values that contain special characters
	quoted := make([]string, len(a))
	for i, v := range a {
		// Quote values that contain commas, quotes, backslashes, or spaces
		if strings.ContainsAny(v, `,"\{} `) {
			// Escape backslashes and quotes
			escaped := strings.ReplaceAll(v, `\`, `\\`)
			escaped = strings.ReplaceAll(escaped, `"`, `\"`)
			quoted[i] = fmt.Sprintf(`"%s"`, escaped)
		} else {
			quoted[i] = v
		}
	}
	return fmt.Sprintf("{%s}", strings.Join(quoted, ",")), nil
}

// Market represents a Polymarket market (contract)
// Maps to the 'markets' table
type Market struct {
	ConditionID           string      `gorm:"primaryKey;column:condition_id" json:"condition_id"`
	GammaMarketID         string      `gorm:"column:gamma_market_id" json:"gamma_market_id"`
	QuestionID            string      `gorm:"column:question_id" json:"question_id"`
	Slug                  string      `gorm:"column:slug;index" json:"slug"`
	Title                 string      `gorm:"column:title" json:"title"`
	Description           string      `gorm:"column:description" json:"description"`
	ResolutionRules       string      `gorm:"column:resolution_rules" json:"resolution_rules"`
	ImageURL              string      `gorm:"column:image_url" json:"image_url"`
	IconURL               string      `gorm:"column:icon_url" json:"icon_url"`
	Category              string      `gorm:"column:category" json:"category"`
	Tags                  StringArray `gorm:"column:tags;type:text[]" json:"tags"` // Requires handling for postgres array if using raw SQL, or JSON for simplicity
	Active                bool        `gorm:"column:active;default:true" json:"active"`
	Closed                bool        `gorm:"column:closed;default:false" json:"closed"`
	Archived              bool        `gorm:"column:archived;default:false" json:"archived"`
	Featured              bool        `gorm:"column:featured;default:false" json:"featured"`
	IsNew                 bool        `gorm:"column:is_new;default:false" json:"is_new"`
	Restricted            bool        `gorm:"column:restricted;default:false" json:"restricted"`
	EnableOrderBook       bool        `gorm:"column:enable_order_book;default:false" json:"enable_order_book"`
	TokenIDYes            string      `gorm:"column:token_id_yes" json:"token_id_yes"`
	TokenIDNo             string      `gorm:"column:token_id_no" json:"token_id_no"`
	MarketMakerAddr       string      `gorm:"column:market_maker_address" json:"market_maker_address"`
	StartDate             *time.Time  `gorm:"column:start_date" json:"start_date"`
	EndDate               *time.Time  `gorm:"column:end_date" json:"end_date"`
	EventStartTime        *time.Time  `gorm:"column:event_start_time" json:"event_start_time"`
	AcceptingOrders       bool        `gorm:"column:accepting_orders" json:"accepting_orders"`
	AcceptingOrdersAt     *time.Time  `gorm:"column:accepting_orders_at" json:"accepting_orders_at"`
	Ready                 bool        `gorm:"column:ready" json:"ready"`
	Funded                bool        `gorm:"column:funded" json:"funded"`
	PendingDeployment     bool        `gorm:"column:pending_deployment" json:"pending_deployment"`
	Deploying             bool        `gorm:"column:deploying" json:"deploying"`
	RFQEnabled            bool        `gorm:"column:rfq_enabled" json:"rfq_enabled"`
	HoldingRewardsEnabled bool        `gorm:"column:holding_rewards_enabled" json:"holding_rewards_enabled"`
	FeesEnabled           bool        `gorm:"column:fees_enabled" json:"fees_enabled"`
	NegRisk               bool        `gorm:"column:neg_risk" json:"neg_risk"`
	NegRiskOther          bool        `gorm:"column:neg_risk_other" json:"neg_risk_other"`
	AutomaticallyActive   bool        `gorm:"column:automatically_active" json:"automatically_active"`
	ManualActivation      bool        `gorm:"column:manual_activation" json:"manual_activation"`

	VolumeAllTime    float64 `gorm:"column:volume_all_time" json:"volume_all_time"`
	Volume24h        float64 `gorm:"column:volume_24h" json:"volume_24h"`
	Volume24hAmm     float64 `gorm:"column:volume_24h_amm" json:"volume_24h_amm"`
	Volume24hClob    float64 `gorm:"column:volume_24h_clob" json:"volume_24h_clob"`
	Volume1Week      float64 `gorm:"column:volume_1w" json:"volume_1w"`
	Volume1WeekAmm   float64 `gorm:"column:volume_1w_amm" json:"volume_1w_amm"`
	Volume1WeekClob  float64 `gorm:"column:volume_1w_clob" json:"volume_1w_clob"`
	Volume1Month     float64 `gorm:"column:volume_1m" json:"volume_1m"`
	Volume1MonthAmm  float64 `gorm:"column:volume_1m_amm" json:"volume_1m_amm"`
	Volume1MonthClob float64 `gorm:"column:volume_1m_clob" json:"volume_1m_clob"`
	Volume1Year      float64 `gorm:"column:volume_1y" json:"volume_1y"`
	Volume1YearAmm   float64 `gorm:"column:volume_1y_amm" json:"volume_1y_amm"`
	Volume1YearClob  float64 `gorm:"column:volume_1y_clob" json:"volume_1y_clob"`
	VolumeAmm        float64 `gorm:"column:volume_amm" json:"volume_amm"`
	VolumeClob       float64 `gorm:"column:volume_clob" json:"volume_clob"`
	VolumeNum        float64 `gorm:"column:volume_num" json:"volume_num"`

	Liquidity     float64 `gorm:"column:liquidity" json:"liquidity"`
	LiquidityNum  float64 `gorm:"column:liquidity_num" json:"liquidity_num"`
	LiquidityClob float64 `gorm:"column:liquidity_clob" json:"liquidity_clob"`
	LiquidityAmm  float64 `gorm:"column:liquidity_amm" json:"liquidity_amm"`

	OrderMinSize          float64 `gorm:"column:order_min_size" json:"order_min_size"`
	OrderPriceMinTickSize float64 `gorm:"column:order_price_min_tick" json:"order_price_min_tick"`
	BestBid               float64 `gorm:"column:best_bid" json:"best_bid"`
	BestAsk               float64 `gorm:"column:best_ask" json:"best_ask"`
	Spread                float64 `gorm:"column:spread" json:"spread"`
	LastTradePrice        float64 `gorm:"column:last_trade_price" json:"last_trade_price"`
	OneHourPriceChange    float64 `gorm:"column:one_hour_price_change" json:"one_hour_price_change"`
	OneDayPriceChange     float64 `gorm:"column:one_day_price_change" json:"one_day_price_change"`
	OneWeekPriceChange    float64 `gorm:"column:one_week_price_change" json:"one_week_price_change"`
	OneMonthPriceChange   float64 `gorm:"column:one_month_price_change" json:"one_month_price_change"`
	OneYearPriceChange    float64 `gorm:"column:one_year_price_change" json:"one_year_price_change"`

	Competitive      float64 `gorm:"column:competitive" json:"competitive"`
	RewardsMinSize   float64 `gorm:"column:rewards_min_size" json:"rewards_min_size"`
	RewardsMaxSpread float64 `gorm:"column:rewards_max_spread" json:"rewards_max_spread"`

	Outcomes      string `gorm:"column:outcomes" json:"outcomes"`
	OutcomePrices string `gorm:"column:outcome_prices" json:"outcome_prices"`

	MarketCreatedAt *time.Time `gorm:"column:market_created_at" json:"market_created_at"`
	MarketUpdatedAt *time.Time `gorm:"column:market_updated_at" json:"market_updated_at"`

	YesPrice        float64    `gorm:"-" json:"yes_price"`
	YesBestBid      float64    `gorm:"-" json:"yes_best_bid"`
	YesBestAsk      float64    `gorm:"-" json:"yes_best_ask"`
	YesPriceUpdated *time.Time `gorm:"-" json:"yes_price_updated"`

	NoPrice        float64    `gorm:"-" json:"no_price"`
	NoBestBid      float64    `gorm:"-" json:"no_best_bid"`
	NoBestAsk      float64    `gorm:"-" json:"no_best_ask"`
	NoPriceUpdated *time.Time `gorm:"-" json:"no_price_updated"`

	TrendingScore float64 `gorm:"-" json:"trending_score,omitempty"`

	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

// TableName overrides the table name used by Market to `markets`
func (Market) TableName() string {
	return "markets"
}
