/**
 * @description
 * HTTP Handlers for Wallet management.
 * Exposes endpoints to get wallet status and trigger deployment.
 *
 * @dependencies
 * - github.com/gofiber/fiber/v2
 * - backend/internal/services
 * - backend/internal/api/middleware
 * - backend/internal/models
 */

package handlers

import (
	"github.com/bankai-project/backend/internal/api/middleware"
	"github.com/bankai-project/backend/internal/models"
	"github.com/bankai-project/backend/internal/services"
	"github.com/gofiber/fiber/v2"
)

type WalletHandler struct {
	Manager    *services.WalletManager
	Blockchain *services.BlockchainService
}

func NewWalletHandler(manager *services.WalletManager, blockchain *services.BlockchainService) *WalletHandler {
	return &WalletHandler{
		Manager:    manager,
		Blockchain: blockchain,
	}
}

// GetWallet returns the wallet status for the authenticated user
// GET /api/v1/wallet
func (h *WalletHandler) GetWallet(c *fiber.Ctx) error {
	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	// We use EnsureWallet here to opportunistically check/deploy if missing.
	// This effectively "Auto-Onboards" the user when they visit the app.
	user, err := h.Manager.EnsureWallet(c.Context(), clerkID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Wallet check failed: " + err.Error(),
		})
	}

	return c.JSON(user)
}

// DeployWallet manually triggers the deployment process
// POST /api/v1/wallet/deploy
func (h *WalletHandler) DeployWallet(c *fiber.Ctx) error {
	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	user, err := h.Manager.EnsureWallet(c.Context(), clerkID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Deployment failed: " + err.Error(),
		})
	}

	return c.JSON(user)
}

// UpdateWallet allows the frontend to report a discovered wallet address
// (Useful if the frontend detects the proxy via other means/libraries)
// POST /api/v1/wallet/update
type UpdateWalletRequest struct {
	VaultAddress string `json:"vault_address"`
	WalletType   string `json:"wallet_type"` // "PROXY" or "SAFE"
}

func (h *WalletHandler) UpdateWallet(c *fiber.Ctx) error {
	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var req UpdateWalletRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid body"})
	}

	if req.VaultAddress == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "vault_address is required"})
	}

	var wType *models.WalletType
	if req.WalletType == "PROXY" {
		wt := models.WalletTypeProxy
		wType = &wt
	} else if req.WalletType == "SAFE" {
		wt := models.WalletTypeSafe
		wType = &wt
	}

	if err := h.Manager.UpdateVaultAddress(c.Context(), clerkID, req.VaultAddress, wType); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update wallet: " + err.Error()})
	}

	return c.JSON(fiber.Map{"status": "success"})
}

// GetDepositAddress returns the vault address for deposits
// GET /api/v1/wallet/deposit
func (h *WalletHandler) GetDepositAddress(c *fiber.Ctx) error {
	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	user, err := h.Manager.GetUserWallet(c.Context(), clerkID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get user wallet: " + err.Error(),
		})
	}

	if user.VaultAddress == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Vault address not found. Please connect a wallet first.",
		})
	}

	return c.JSON(fiber.Map{
		"vault_address": user.VaultAddress,
		"network":       "polygon",
		"token":         "USDC",
		"token_address": "0x3c499c542cEF5E3811e1192ce70d8cC03d5c3359", // Native USDC (USDC.e deprecated as of 2024/2025)
	})
}

// GetBalance returns the USDC balance of the user's vault
// GET /api/v1/wallet/balance
func (h *WalletHandler) GetBalance(c *fiber.Ctx) error {
	if h.Blockchain == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "Blockchain service unavailable",
		})
	}

	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	user, err := h.Manager.GetUserWallet(c.Context(), clerkID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get user wallet: " + err.Error(),
		})
	}

	if user.VaultAddress == "" {
		return c.JSON(fiber.Map{
			"balance":           "0",
			"balance_formatted": "0.00",
			"vault_address":     "",
		})
	}

	balance, err := h.Blockchain.GetUSDCBalance(c.Context(), user.VaultAddress)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get balance: " + err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"balance":           balance.String(),
		"balance_formatted": h.Blockchain.FormatUSDCBalance(balance),
		"vault_address":     user.VaultAddress,
		"token":             "USDC",
	})
}

// WithdrawRequest represents a withdrawal request
type WithdrawRequest struct {
	ToAddress string `json:"to_address"` // Destination address (EOA)
	Amount    string `json:"amount"`     // Amount in USDC (with 6 decimals, e.g., "1000000" for 1 USDC)
}

// Withdraw initiates a withdrawal from the vault to the specified address
// POST /api/v1/wallet/withdraw
// Note: This is a placeholder - actual withdrawal requires signing a Safe transaction
// In production, this would use the relayer to submit a Safe transaction
func (h *WalletHandler) Withdraw(c *fiber.Ctx) error {
	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var req WithdrawRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid body"})
	}

	if req.ToAddress == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "to_address is required"})
	}

	if req.Amount == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "amount is required"})
	}

	user, err := h.Manager.GetUserWallet(c.Context(), clerkID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get user wallet: " + err.Error(),
		})
	}

	if user.VaultAddress == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Vault address not found. Please connect a wallet first.",
		})
	}

	// TODO: Implement actual withdrawal via Safe transaction
	// This would require:
	// 1. Encoding a Safe transaction (execTransaction) to transfer USDC
	// 2. Signing the transaction with the user's EOA
	// 3. Submitting to the relayer
	// For now, return a placeholder response

	return c.Status(fiber.StatusNotImplemented).JSON(fiber.Map{
		"error": "Withdrawal functionality is not yet implemented. This requires Safe transaction signing.",
		"note":  "In production, this would submit a Safe transaction via the relayer to transfer USDC from the vault to the specified address.",
	})
}
