/**
 * @description
 * Settings API Handlers.
 * Handles per-user settings retrieval and updates.
 *
 * @dependencies
 * - github.com/gofiber/fiber/v2
 * - backend/internal/services
 * - backend/internal/api/middleware
 */

package handlers

import (
	"errors"

	"github.com/bankai-project/backend/internal/api/middleware"
	"github.com/bankai-project/backend/internal/models"
	"github.com/bankai-project/backend/internal/services"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SettingsHandler struct {
	db              *gorm.DB
	settingsService *services.SettingsService
}

func NewSettingsHandler(db *gorm.DB, settingsService *services.SettingsService) *SettingsHandler {
	return &SettingsHandler{
		db:              db,
		settingsService: settingsService,
	}
}

// GetSettings returns current user's settings (creates defaults if missing).
// GET /api/v1/settings
func (h *SettingsHandler) GetSettings(c *fiber.Ctx) error {
	userIDRaw, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	userID, err := uuid.Parse(userIDRaw)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user id"})
	}

	var user models.User
	if err := h.db.WithContext(c.Context()).Where("id = ?", userID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to load user"})
	}

	settings, err := h.settingsService.GetSettings(c.Context(), user.ID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to load settings"})
	}
	return c.JSON(settings)
}

// UpdateSettings updates specific settings fields.
// PATCH /api/v1/settings
func (h *SettingsHandler) UpdateSettings(c *fiber.Ctx) error {
	userIDRaw, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	userID, err := uuid.Parse(userIDRaw)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user id"})
	}

	var user models.User
	if err := h.db.WithContext(c.Context()).Where("id = ?", userID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to load user"})
	}

	var req services.UpdateUserSettings
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	settings, err := h.settingsService.UpdateSettings(c.Context(), user.ID, req)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(settings)
}

// ResetSettings resets settings to defaults.
// POST /api/v1/settings/reset
func (h *SettingsHandler) ResetSettings(c *fiber.Ctx) error {
	userIDRaw, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	userID, err := uuid.Parse(userIDRaw)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user id"})
	}

	var user models.User
	if err := h.db.WithContext(c.Context()).Where("id = ?", userID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to load user"})
	}

	settings, err := h.settingsService.ResetSettings(c.Context(), user.ID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to reset settings"})
	}
	return c.JSON(settings)
}
