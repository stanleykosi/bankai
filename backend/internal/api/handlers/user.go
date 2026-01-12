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
	"github.com/bankai-project/backend/internal/api/middleware"
	"github.com/bankai-project/backend/internal/models"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type UserHandler struct {
	DB *gorm.DB
}

func NewUserHandler(db *gorm.DB) *UserHandler {
	return &UserHandler{DB: db}
}

// GetMe returns the current authenticated user
// GET /api/user/me
func (h *UserHandler) GetMe(c *fiber.Ctx) error {
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var user models.User
	if err := h.DB.Where("id = ?", userID).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Database error"})
	}

	return c.Status(fiber.StatusOK).JSON(user)
}
