/**
 * @description
 * Holders API Handler.
 * Fetches top holders (whales) for a market.
 *
 * @dependencies
 * - github.com/gofiber/fiber/v2
 * - backend/internal/services
 */

package handlers

import (
	"strconv"

	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/services"
	"github.com/gofiber/fiber/v2"
)

// HoldersHandler handles holder-related requests
type HoldersHandler struct {
	profileService *services.ProfileService
}

// NewHoldersHandler creates a new HoldersHandler
func NewHoldersHandler(profileService *services.ProfileService) *HoldersHandler {
	return &HoldersHandler{
		profileService: profileService,
	}
}

// GetMarketHolders returns top holders for a market (whale table)
// GET /api/v1/markets/:condition_id/holders
func (h *HoldersHandler) GetMarketHolders(c *fiber.Ctx) error {
	conditionID := c.Params("condition_id")
	if conditionID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Condition ID is required",
		})
	}

	tokenID := c.Query("token_id", "")
	limit, _ := strconv.Atoi(c.Query("limit", "10"))

	holders, err := h.profileService.GetMarketHolders(c.Context(), conditionID, tokenID, limit)
	if err != nil {
		logger.Error("HoldersHandler: Failed to get holders: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch market holders",
		})
	}

	return c.JSON(fiber.Map{
		"holders":      holders,
		"count":        len(holders),
		"condition_id": conditionID,
		"token_id":     tokenID,
	})
}
