/**
 * @description
 * Social feature database models.
 * Maps to follows, market_bookmarks, and notifications tables.
 * 
 * Note: Trade activity data (for heatmaps) is cached in Redis, not PostgreSQL.
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

// Follow represents a follower relationship for copy-trading
type Follow struct {
	ID            uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id"`
	FollowerID    uuid.UUID `gorm:"type:uuid;not null" json:"follower_id"`
	TargetAddress string    `gorm:"size:42;not null" json:"target_address"`
	CreatedAt     time.Time `json:"created_at"`

	// Relations
	Follower *User `gorm:"foreignKey:FollowerID" json:"follower,omitempty"`
}

func (Follow) TableName() string {
	return "follows"
}

func (f *Follow) BeforeCreate(tx *gorm.DB) (err error) {
	if f.ID == uuid.Nil {
		f.ID = uuid.New()
	}
	return
}

// MarketBookmark represents a user's starred market for watchlist
type MarketBookmark struct {
	ID        uuid.UUID `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id"`
	UserID    uuid.UUID `gorm:"type:uuid;not null" json:"user_id"`
	MarketID  string    `gorm:"size:66;not null" json:"market_id"`
	CreatedAt time.Time `json:"created_at"`

	// Relations
	User   *User   `gorm:"foreignKey:UserID" json:"user,omitempty"`
	Market *Market `gorm:"foreignKey:MarketID;references:ConditionID" json:"market,omitempty"`
}

func (MarketBookmark) TableName() string {
	return "market_bookmarks"
}

func (b *MarketBookmark) BeforeCreate(tx *gorm.DB) (err error) {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	return
}

// NotificationType defines types of notifications
type NotificationType string

const (
	NotificationTypeTradeAlert NotificationType = "TRADE_ALERT"
	NotificationTypeFollowed   NotificationType = "FOLLOWED"
	NotificationTypeSystem     NotificationType = "SYSTEM"
)

// Notification stores user notifications for trade alerts
type Notification struct {
	ID        uuid.UUID        `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id"`
	UserID    uuid.UUID        `gorm:"type:uuid;not null" json:"user_id"`
	Type      NotificationType `gorm:"size:32;default:'TRADE_ALERT'" json:"type"`
	Title     string           `gorm:"size:255;not null" json:"title"`
	Message   string           `json:"message"`
	Data      string           `gorm:"type:jsonb" json:"data"` // JSON string for flexible data
	Read      bool             `gorm:"default:false" json:"read"`
	CreatedAt time.Time        `json:"created_at"`

	// Relations
	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (Notification) TableName() string {
	return "notifications"
}

func (n *Notification) BeforeCreate(tx *gorm.DB) (err error) {
	if n.ID == uuid.Nil {
		n.ID = uuid.New()
	}
	return
}

// FollowWithProfile extends Follow with profile information
type FollowWithProfile struct {
	Follow
	ProfileName    string `json:"profile_name"`
	ProfileImage   string `json:"profile_image"`
	IsVerified     bool   `json:"is_verified"`
}

// WatchlistItem represents a bookmarked market with live price data
type WatchlistItem struct {
	MarketBookmark
	Title          string  `json:"title"`
	ImageURL       string  `json:"image_url"`
	YesPrice       float64 `json:"yes_price"`
	NoPrice        float64 `json:"no_price"`
	Volume24h      float64 `json:"volume_24h"`
	OneDayChange   float64 `json:"one_day_change"`
}

