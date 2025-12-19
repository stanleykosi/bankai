/**
 * @description
 * User API Handlers.
 * Handles user synchronization and profile retrieval.
 *
 * @dependencies
 * - github.com/gofiber/fiber/v2
 * - gorm.io/gorm
 */

package handlers

import (
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/api/middleware"
	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/models"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type UserHandler struct {
	DB *gorm.DB
}

func NewUserHandler(db *gorm.DB) *UserHandler {
	return &UserHandler{DB: db}
}

// SyncUserRequest defines payload for syncing user
type SyncUserRequest struct {
	Email       string `json:"email"`
	EOAAddress  string `json:"eoa_address"`  // The wallet address (Metamask or Embedded)
	ClearWallet bool   `json:"clear_wallet"` // Explicit disconnect request
}

// SyncUser ensures the user exists in the database
// POST /api/user/sync
func (h *UserHandler) SyncUser(c *fiber.Ctx) error {
	// 1. Get Clerk ID from context
	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		logger.Error("SyncUser: Failed to get user ID from context: %v", err)
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	// 2. Parse Body
	var req SyncUserRequest
	if err := c.BodyParser(&req); err != nil {
		logger.Error("SyncUser: Failed to parse request body: %v", err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body", "details": err.Error()})
	}

	// 3. Fetch existing user to detect EOA changes
	var existingUser models.User
	var hasExisting bool
	if err := h.DB.Where("clerk_id = ?", clerkID).First(&existingUser).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			logger.Error("SyncUser: Failed to fetch existing user: %v", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error":   "Failed to sync user",
				"details": err.Error(),
			})
		}
	} else {
		hasExisting = true
	}

	// 4. Upsert User
	now := time.Now()
	requestedEOA := strings.TrimSpace(req.EOAAddress)
	shouldUpdateEOA := req.ClearWallet || requestedEOA != ""
	nextEOA := requestedEOA
	if req.ClearWallet {
		nextEOA = ""
	}
	user := models.User{
		ClerkID:    clerkID,
		Email:      req.Email,
		EOAAddress: nextEOA, // May be empty on first insert or explicit clear
		UpdatedAt:  now,
	}

	updates := map[string]interface{}{
		"email":      req.Email,
		"updated_at": now,
	}
	if shouldUpdateEOA {
		updates["eoa_address"] = nextEOA
	}

	// Use Postgres ON CONFLICT to update email/eoa if changed, or do nothing
	result := h.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "clerk_id"}},
		DoUpdates: clause.Assignments(updates),
	}).Create(&user)

	if result.Error != nil {
		logger.Error("SyncUser: Database error during upsert: %v", result.Error)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to sync user",
			"details": result.Error.Error(),
		})
	}

	// 5. Clear vault metadata if the connected EOA changed (or explicit disconnect)
	if hasExisting {
		oldEOA := strings.ToLower(strings.TrimSpace(existingUser.EOAAddress))
		newEOA := strings.ToLower(strings.TrimSpace(nextEOA))
		switch {
		case req.ClearWallet:
			logger.Info("SyncUser: Explicit wallet clear for user %s. Clearing cached vault state.", clerkID)
		case !shouldUpdateEOA:
			// Skip vault reset if we didn't update the EOA (e.g., refresh with empty address).
			newEOA = oldEOA
		case oldEOA != newEOA:
			logger.Info("SyncUser: EOA changed for user %s. Clearing cached vault state.", clerkID)
		}

		if oldEOA != newEOA || req.ClearWallet {
			reset := map[string]interface{}{
				"vault_address": "",
				"wallet_type":   gorm.Expr("NULL"),
				"updated_at":    now,
			}
			if err := h.DB.Model(&models.User{}).Where("clerk_id = ?", clerkID).Updates(reset).Error; err != nil {
				logger.Error("SyncUser: Failed to clear vault state for user %s: %v", clerkID, err)
			}
		}
	}

	// 6. Fetch full user to return (including ID and Vault Address)
	var updatedUser models.User
	if err := h.DB.Where("clerk_id = ?", clerkID).First(&updatedUser).Error; err != nil {
		logger.Error("SyncUser: Failed to fetch user after upsert: %v", err)
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found after sync"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to fetch synced user",
			"details": err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(updatedUser)
}

// GetMe returns the current authenticated user
// GET /api/user/me
func (h *UserHandler) GetMe(c *fiber.Ctx) error {
	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var user models.User
	if err := h.DB.Where("clerk_id = ?", clerkID).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Database error"})
	}

	return c.Status(fiber.StatusOK).JSON(user)
}
