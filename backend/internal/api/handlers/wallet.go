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
	Manager *services.WalletManager
}

func NewWalletHandler(manager *services.WalletManager) *WalletHandler {
	return &WalletHandler{Manager: manager}
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

	wType := models.WalletTypeSafe
	if req.WalletType == "PROXY" {
		wType = models.WalletTypeProxy
	}

	if err := h.Manager.UpdateVaultAddress(c.Context(), clerkID, req.VaultAddress, wType); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update wallet: " + err.Error()})
	}

	return c.JSON(fiber.Map{"status": "success"})
}




