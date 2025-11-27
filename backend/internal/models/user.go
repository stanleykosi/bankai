/**
 * @description
 * User database model.
 * Maps to the 'users' table in PostgreSQL.
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

type WalletType string

const (
	WalletTypeProxy WalletType = "PROXY"
	WalletTypeSafe  WalletType = "SAFE"
)

// User represents a registered user in the system
type User struct {
	ID           uuid.UUID   `gorm:"type:uuid;primary_key;default:uuid_generate_v4()" json:"id"`
	ClerkID      string       `gorm:"uniqueIndex;not null" json:"clerk_id"`
	Email        string       `json:"email"`
	EOAAddress   string       `gorm:"column:eoa_address" json:"eoa_address"` // Optional - can be set when wallet is connected
	VaultAddress string       `gorm:"column:vault_address" json:"vault_address"` // Proxy or Gnosis Safe
	WalletType   *WalletType  `gorm:"column:wallet_type" json:"wallet_type"` // Pointer to allow NULL

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName overrides the table name used by User to `users`
func (User) TableName() string {
	return "users"
}

// BeforeCreate ensures UUID is generated if not present (though DB usually handles this)
func (u *User) BeforeCreate(tx *gorm.DB) (err error) {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	return
}

