/**
 * @description
 * Profile API Handlers.
 * Handles trader profile, stats, positions, and activity data.
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

// ProfileHandler handles profile-related requests
type ProfileHandler struct {
	profileService *services.ProfileService
	socialService  *services.SocialService
}

// NewProfileHandler creates a new ProfileHandler
func NewProfileHandler(profileService *services.ProfileService, socialService *services.SocialService) *ProfileHandler {
	return &ProfileHandler{
		profileService: profileService,
		socialService:  socialService,
	}
}

// GetTraderProfile returns a trader's profile
// GET /api/v1/profile/:address
func (h *ProfileHandler) GetTraderProfile(c *fiber.Ctx) error {
	address := c.Params("address")
	if address == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Address is required",
		})
	}

	ctx := c.Context()

	profile, err := h.profileService.GetTraderProfile(ctx, address)
	if err != nil {
		logger.Error("ProfileHandler: Failed to get profile: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch trader profile",
		})
	}

	if profile == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "Trader not found",
		})
	}

	// Get follower count
	followerCount, _ := h.socialService.GetFollowerCount(ctx, address)

	return c.JSON(fiber.Map{
		"profile":        profile,
		"follower_count": followerCount,
	})
}

// GetTraderStats returns performance stats for a trader
// GET /api/v1/profile/:address/stats
func (h *ProfileHandler) GetTraderStats(c *fiber.Ctx) error {
	address := c.Params("address")
	if address == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Address is required",
		})
	}

	stats, err := h.profileService.GetTraderStats(c.Context(), address)
	if err != nil {
		logger.Error("ProfileHandler: Failed to get stats: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch trader stats",
		})
	}

	return c.JSON(fiber.Map{
		"stats": stats,
	})
}

// GetTraderPositions returns open positions for a trader
// GET /api/v1/profile/:address/positions
func (h *ProfileHandler) GetTraderPositions(c *fiber.Ctx) error {
	address := c.Params("address")
	if address == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Address is required",
		})
	}

	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))

	positions, err := h.profileService.GetOpenPositions(c.Context(), address, limit, offset)
	if err != nil {
		logger.Error("ProfileHandler: Failed to get positions: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch positions",
		})
	}

	return c.JSON(fiber.Map{
		"positions": positions,
		"count":     len(positions),
	})
}

// GetActivityHeatmap returns trade activity data for heatmap
// GET /api/v1/profile/:address/activity
func (h *ProfileHandler) GetActivityHeatmap(c *fiber.Ctx) error {
	address := c.Params("address")
	if address == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Address is required",
		})
	}

	activity, err := h.profileService.GetActivityHeatmap(c.Context(), address)
	if err != nil {
		logger.Error("ProfileHandler: Failed to get activity: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch activity data",
		})
	}

	return c.JSON(fiber.Map{
		"activity": activity,
	})
}

// GetRecentTrades returns recent trades for a trader
// GET /api/v1/profile/:address/trades
func (h *ProfileHandler) GetRecentTrades(c *fiber.Ctx) error {
	address := c.Params("address")
	if address == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Address is required",
		})
	}

	limit, _ := strconv.Atoi(c.Query("limit", "20"))

	trades, err := h.profileService.GetRecentTrades(c.Context(), address, limit)
	if err != nil {
		logger.Error("ProfileHandler: Failed to get trades: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch trades",
		})
	}

	return c.JSON(fiber.Map{
		"trades": trades,
		"count":  len(trades),
	})
}
