/**
 * @description
 * User Settings database model.
 * Maps to the 'user_settings' table in PostgreSQL.
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

// UserSettings stores per-user preferences for trading + notifications.
type UserSettings struct {
	ID     uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;uniqueIndex;not null" json:"user_id"`

	// Trading
	DefaultOrderType         string `gorm:"column:default_order_type;type:varchar(10);default:'GTC'" json:"default_order_type"`
	SlippageToleranceBps     int    `gorm:"column:slippage_tolerance_bps;default:100" json:"slippage_tolerance_bps"`
	TradeConfirmationEnabled bool   `gorm:"column:trade_confirmation_enabled;default:true" json:"trade_confirmation_enabled"`

	// Notifications
	NotificationChannel    string  `gorm:"column:notification_channel;type:varchar(20);default:'IN_APP'" json:"notification_channel"`
	WhaleAlertThresholdUSD float64 `gorm:"column:whale_alert_threshold_usd;type:decimal(12,2);default:5000" json:"whale_alert_threshold_usd"`
	OrderFillNotifications bool    `gorm:"column:order_fill_notifications;default:true" json:"order_fill_notifications"`
	ResolutionAlerts       bool    `gorm:"column:resolution_alerts;default:true" json:"resolution_alerts"`
	FollowedTraderAlerts   string  `gorm:"column:followed_trader_alerts;type:varchar(20);default:'ALL'" json:"followed_trader_alerts"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (UserSettings) TableName() string {
	return "user_settings"
}

func (s *UserSettings) BeforeCreate(tx *gorm.DB) (err error) {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}
