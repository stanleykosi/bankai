/**
 * @description
 * Price History database model.
 * Maps to the 'price_history' table in PostgreSQL.
 *
 * @dependencies
 * - gorm.io/gorm
 */

package models

import (
	"time"
)

// PriceHistory represents a historical price point for a market outcome
type PriceHistory struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	MarketID  string    `gorm:"column:market_id;index:idx_price_history_market_time" json:"market_id"`
	Outcome   string    `gorm:"column:outcome" json:"outcome"` // "YES" or "NO"
	Price     float64   `gorm:"column:price;type:decimal(10,4)" json:"price"`
	Volume    float64   `gorm:"column:volume;type:decimal(20,4)" json:"volume"`
	Timestamp time.Time `gorm:"column:timestamp;index:idx_price_history_market_time" json:"timestamp"`
}

// TableName overrides the table name used by PriceHistory to `price_history`
func (PriceHistory) TableName() string {
	return "price_history"
}

