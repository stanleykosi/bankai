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
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/models"
	"github.com/bankai-project/backend/internal/polymarket/gamma"
	"github.com/bankai-project/backend/internal/polymarket/relayer"
	"gorm.io/gorm"
)

type WalletManager struct {
	DB      *gorm.DB
	Relayer *relayer.Client
	Gamma   *gamma.Client
}

const (
	gammaRetryAttempts  = 3
	gammaRetryDelay     = 400 * time.Millisecond
	registrationChecks  = 5
	registrationBackoff = 600 * time.Millisecond
)

func NewWalletManager(db *gorm.DB, relayer *relayer.Client, gammaClient *gamma.Client) *WalletManager {
	return &WalletManager{
		DB:      db,
		Relayer: relayer,
		Gamma:   gammaClient,
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

	// 2. If user has no EOA address, they can't deploy a vault yet
	// Return the user as-is - they need to connect a wallet first
	if user.EOAAddress == "" {
		logger.Info("User %s has no EOA address yet. Vault deployment requires a connected wallet.", user.ClerkID)
		return &user, nil
	}

	// 3. If not, we try to deploy (or discover).
	// For Metamask users, the Vault is usually a Gnosis Safe.
	// The Relayer handles the logic of "deploy if not exists" usually.

	// Attempt discovery via Polymarket public-search before deploying.
	if vaultAddr, walletType, err := s.lookupVaultAddress(ctx, user.EOAAddress); err == nil && vaultAddr != "" {
		logger.Info("🔎 Located existing vault %s for user %s", vaultAddr, user.ClerkID)
		if err := s.UpdateVaultAddress(ctx, user.ClerkID, vaultAddr, walletType); err != nil {
			logger.Error("Failed to persist discovered vault address: %v", err)
		} else {
			user.VaultAddress = vaultAddr
			user.WalletType = walletType
			return &user, nil
		}
	} else if err != nil {
		logger.Error("Vault discovery failed for user %s: %v", user.ClerkID, err)
	}

	logger.Info("🧐 User %s (EOA: %s) has no vault. Awaiting SAFE-CREATE signature.", user.ClerkID, user.EOAAddress)
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

// lookupVaultAddress attempts to find an existing vault by querying public Polymarket data (proxy wallets).
// References polymarket_documentation.md section "Search markets, events, and profiles".
func (s *WalletManager) lookupVaultAddress(ctx context.Context, eoa string) (string, *models.WalletType, error) {
	if eoa == "" {
		return "", nil, nil
	}

	if addr, err := s.lookupProxyVault(ctx, eoa); err == nil && addr != "" {
		wType := models.WalletTypeProxy
		return addr, &wType, nil
	} else if err != nil {
		logger.Info("[WARN] Proxy vault lookup failed for %s: %v", eoa, err)
	}

	return "", nil, nil
}

func (s *WalletManager) lookupProxyVault(ctx context.Context, eoa string) (string, error) {
	if s.Gamma == nil {
		return "", nil
	}

	lower := strings.ToLower(eoa)
	upper := strings.ToUpper(eoa)
	var queries []string
	queries = append(queries, eoa, lower, upper)
	if len(lower) >= 8 {
		prefix := lower[:8]
		queries = append(queries, prefix, strings.ToUpper(prefix))
	}

	for _, q := range queries {
		for attempt := 0; attempt < gammaRetryAttempts; attempt++ {
			profiles, err := s.Gamma.SearchProfiles(ctx, q, 10)
			if err != nil {
				logger.Info("[WARN] Gamma search attempt %d failed for %s: %v", attempt+1, q, err)
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(gammaRetryDelay):
				}
				continue
			}

			for _, profile := range profiles {
				if profile.ProxyWallet == "" {
					continue
				}

				switch {
				case profile.BaseAddress != "" && strings.EqualFold(profile.BaseAddress, eoa):
					return profile.ProxyWallet, nil
				case profile.BaseAddress == "" && strings.Contains(strings.ToLower(profile.Name), lower):
					return profile.ProxyWallet, nil
				}
			}

			// No profile matched for this query; break retries to move to next query string.
			break
		}
	}

	return "", nil
}

// awaitVaultRegistration polls Gamma for a short duration after a successful relayer call.
// This covers the short delay between relayer deployment and Gamma profile propagation.
func (s *WalletManager) awaitVaultRegistration(ctx context.Context, eoa string) (string, error) {
	for i := 0; i < registrationChecks; i++ {
		addr, err := s.lookupProxyVault(ctx, eoa)
		if err != nil {
			return "", err
		}
		if addr != "" {
			return addr, nil
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(registrationBackoff):
		}
	}

	return "", nil
}
