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
	Email      string `json:"email"`
	EOAAddress string `json:"eoa_address"` // The wallet address (Metamask or Embedded)
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

	// 3. Upsert User
	now := time.Now()
	user := models.User{
		ClerkID:    clerkID,
		Email:      req.Email,
		EOAAddress: req.EOAAddress, // Can be empty string if no wallet connected yet
		UpdatedAt:  now,
	}

	// Use Postgres ON CONFLICT to update email/eoa if changed, or do nothing
	result := h.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "clerk_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"email", "eoa_address", "updated_at"}),
	}).Create(&user)

	if result.Error != nil {
		logger.Error("SyncUser: Database error during upsert: %v", result.Error)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error":   "Failed to sync user",
			"details": result.Error.Error(),
		})
	}

	// 4. Fetch full user to return (including ID and Vault Address)
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

