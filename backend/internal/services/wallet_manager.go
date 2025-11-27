/**
 * @description
 * Wallet Manager Service.
 * Handles the business logic for:
 * 1. Detecting if a user has a wallet.
 * 2. Triggering deployment via Relayer if not.
 * 3. Updating the local User database with the Vault (Safe/Proxy) address.
 *
 * @dependencies
 * - gorm.io/gorm
 * - backend/internal/models
 * - backend/internal/polymarket/relayer
 * - backend/internal/logger
 */

package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/models"
	"github.com/bankai-project/backend/internal/polymarket/relayer"
	"gorm.io/gorm"
)

type WalletManager struct {
	DB      *gorm.DB
	Relayer *relayer.Client
}

func NewWalletManager(db *gorm.DB, relayer *relayer.Client) *WalletManager {
	return &WalletManager{
		DB:      db,
		Relayer: relayer,
	}
}

// GetUserWallet returns the wallet info for a user
func (s *WalletManager) GetUserWallet(ctx context.Context, userID string) (*models.User, error) {
	var user models.User
	if err := s.DB.WithContext(ctx).Where("clerk_id = ?", userID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("user not found: %w", err)
		}
		return nil, fmt.Errorf("failed to fetch user: %w", err)
	}
	return &user, nil
}

// EnsureWallet checks if a user has a Vault address. If not, it attempts to find or deploy one.
func (s *WalletManager) EnsureWallet(ctx context.Context, clerkID string) (*models.User, error) {
	var user models.User
	if err := s.DB.WithContext(ctx).Where("clerk_id = ?", clerkID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("user not found: %w", err)
		}
		return nil, fmt.Errorf("failed to fetch user: %w", err)
	}

	// 1. If Vault Address exists, we are good.
	if user.VaultAddress != "" {
		return &user, nil
	}

	// 2. If not, we try to deploy (or discover).
	// For Metamask users, the Vault is usually a Gnosis Safe.
	// The Relayer handles the logic of "deploy if not exists" usually.

	logger.Info("🧐 User %s (EOA: %s) has no vault. Triggering deployment check...", user.ClerkID, user.EOAAddress)

	// 3. Call Relayer to Deploy
	// This will hit POST /submit on the Relayer with properly ABI-encoded transaction data.
	// The DeploySafe method handles full ABI encoding of the Safe deployment transaction.
	resp, err := s.Relayer.DeploySafe(ctx, user.EOAAddress)
	if err != nil {
		logger.Error("Failed to request safe deployment via Relayer: %v", err)
		// We don't block the user flow completely, but return error metadata if possible
		// For now, just return user with empty vault
		return &user, nil
	}

	// 4. Check Response
	if resp.TransactionHash != "" {
		logger.Info("✅ Deployment Transaction Sent: %s", resp.TransactionHash)
		// In a full system, we'd poll this TX hash to get the event logs and find the Safe Address.
		// Since we don't have the address immediately from an async /submit call,
		// we might need to wait or let the frontend poll 'GET /wallet' which could check the graph/chain.

		// For now, we update a status flag if we had one, or just log it.
		// The frontend can call UpdateWallet once it detects the deployed address on-chain.
	}

	return &user, nil
}

// UpdateVaultAddress manually updates the vault address (e.g. after frontend detects it on-chain)
func (s *WalletManager) UpdateVaultAddress(ctx context.Context, clerkID, vaultAddr string, wType *models.WalletType) error {
	if vaultAddr == "" {
		return errors.New("vault address cannot be empty")
	}

	updates := map[string]interface{}{
		"vault_address": vaultAddr,
		"updated_at":    time.Now(),
	}
	if wType != nil {
		updates["wallet_type"] = *wType
	}

	return s.DB.WithContext(ctx).Model(&models.User{}).
		Where("clerk_id = ?", clerkID).
		Updates(updates).Error
}



