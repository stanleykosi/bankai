/**
 * @description
 * Order database model.
 * Maps to the 'orders' table in PostgreSQL.
 * Used for auditing and tracking user trade history within Bankai.
 *
 * @dependencies
 * - gorm.io/gorm
 * - github.com/google/uuid
 */

package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// OrderStatus defines the state of an order in our system
type OrderStatus string

const (
	OrderStatusPending  OrderStatus = "PENDING"
	OrderStatusOpen     OrderStatus = "OPEN"
	OrderStatusFilled   OrderStatus = "FILLED"
	OrderStatusCanceled OrderStatus = "CANCELED"
	OrderStatusFailed   OrderStatus = "FAILED"
)

// OrderSide defines the side of the trade
type OrderSide string

const (
	OrderSideBuy  OrderSide = "BUY"
	OrderSideSell OrderSide = "SELL"
)

// OrderSource tracks where an order originated (Bankai vs external)
type OrderSource string

const (
	OrderSourceBankai  OrderSource = "BANKAI"
	OrderSourceExternal OrderSource = "EXTERNAL"
	OrderSourceUnknown  OrderSource = "UNKNOWN"
)

// Order represents a trade order placed through the system
type Order struct {
	ID             uuid.UUID   `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id"`
	UserID         uuid.UUID   `gorm:"type:uuid;not null;index:idx_orders_user" json:"user_id"`
	CLOBOrderID    string      `gorm:"column:clob_order_id" json:"clob_order_id"`
	MarketID       string      `gorm:"column:market_id" json:"market_id"` // Condition ID
	Side           OrderSide   `gorm:"column:side;type:varchar(4)" json:"side"`
	Outcome        string      `gorm:"column:outcome;type:varchar(64)" json:"outcome"` // Outcome label (e.g., "YES", "NO", "CANDIDATE A")
	OutcomeTokenID string      `gorm:"column:outcome_token_id;type:varchar(255)" json:"outcome_token_id"`
	Price          float64     `gorm:"column:price;type:decimal" json:"price"`
	Size           float64     `gorm:"column:size;type:decimal" json:"size"`
	OrderType      string      `gorm:"column:order_type;type:varchar(10)" json:"order_type"` // LIMIT, MARKET, FOK, FAK, GTC, GTD
	Status         OrderStatus `gorm:"column:status;type:varchar(20);default:'PENDING';index:idx_orders_status" json:"status"`
	StatusDetail   string      `gorm:"column:status_detail;type:varchar(32)" json:"status_detail"`
	OrderHashes    StringArray `gorm:"column:order_hashes;type:text[]" json:"order_hashes"`
	ErrorMessage   string      `gorm:"column:error_msg" json:"error_msg"`
	TxHash         string      `gorm:"column:tx_hash;type:varchar(66)" json:"tx_hash"` // Optional, if executed on-chain
	Source         OrderSource `gorm:"column:source;type:varchar(16);default:'UNKNOWN'" json:"source"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`

	// Associations
	User User `gorm:"foreignKey:UserID" json:"-"`
}

// TableName overrides the table name used by Order to `orders`
func (Order) TableName() string {
	return "orders"
}

// BeforeCreate ensures UUID is generated if not present
func (o *Order) BeforeCreate(tx *gorm.DB) (err error) {
	if o.ID == uuid.Nil {
		o.ID = uuid.New()
	}
	return
}
