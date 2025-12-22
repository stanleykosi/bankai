/**
 * @description
 * Watchlist API Handlers.
 * Handles market bookmark operations.
 *
 * @dependencies
 * - github.com/gofiber/fiber/v2
 * - backend/internal/services
 * - backend/internal/api/middleware
 */

package handlers

import (
	"github.com/bankai-project/backend/internal/api/middleware"
	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/models"
	"github.com/bankai-project/backend/internal/services"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// WatchlistHandler handles watchlist-related requests
type WatchlistHandler struct {
	db               *gorm.DB
	watchlistService *services.WatchlistService
}

// NewWatchlistHandler creates a new WatchlistHandler
func NewWatchlistHandler(db *gorm.DB, watchlistService *services.WatchlistService) *WatchlistHandler {
	return &WatchlistHandler{
		db:               db,
		watchlistService: watchlistService,
	}
}

// BookmarkRequest represents a bookmark request body
type BookmarkRequest struct {
	MarketID string `json:"market_id"`
}

// BookmarkMarket adds a market to watchlist
// POST /api/v1/watchlist/bookmark
func (h *WatchlistHandler) BookmarkMarket(c *fiber.Ctx) error {
	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var user models.User
	if err := h.db.Where("clerk_id = ?", clerkID).First(&user).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
	}

	var req BookmarkRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	if req.MarketID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Market ID is required",
		})
	}

	err = h.watchlistService.BookmarkMarket(c.Context(), user.ID, req.MarketID)
	if err != nil {
		logger.Error("WatchlistHandler: Failed to bookmark: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to bookmark market",
		})
	}

	return c.JSON(fiber.Map{
		"success":    true,
		"bookmarked": true,
		"market_id":  req.MarketID,
	})
}

// RemoveBookmark removes a market from watchlist
// DELETE /api/v1/watchlist/:market_id
func (h *WatchlistHandler) RemoveBookmark(c *fiber.Ctx) error {
	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var user models.User
	if err := h.db.Where("clerk_id = ?", clerkID).First(&user).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
	}

	marketID := c.Params("market_id")
	if marketID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Market ID is required",
		})
	}

	err = h.watchlistService.RemoveBookmark(c.Context(), user.ID, marketID)
	if err != nil {
		logger.Error("WatchlistHandler: Failed to remove bookmark: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to remove bookmark",
		})
	}

	return c.JSON(fiber.Map{
		"success":    true,
		"bookmarked": false,
		"market_id":  marketID,
	})
}

// ToggleBookmark toggles bookmark status
// POST /api/v1/watchlist/toggle
func (h *WatchlistHandler) ToggleBookmark(c *fiber.Ctx) error {
	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var user models.User
	if err := h.db.Where("clerk_id = ?", clerkID).First(&user).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
	}

	var req BookmarkRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	if req.MarketID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Market ID is required",
		})
	}

	isBookmarked, err := h.watchlistService.ToggleBookmark(c.Context(), user.ID, req.MarketID)
	if err != nil {
		logger.Error("WatchlistHandler: Failed to toggle bookmark: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to toggle bookmark",
		})
	}

	return c.JSON(fiber.Map{
		"success":    true,
		"bookmarked": isBookmarked,
		"market_id":  req.MarketID,
	})
}

// GetWatchlist returns user's watchlist
// GET /api/v1/watchlist
func (h *WatchlistHandler) GetWatchlist(c *fiber.Ctx) error {
	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var user models.User
	if err := h.db.Where("clerk_id = ?", clerkID).First(&user).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
	}

	watchlist, err := h.watchlistService.GetWatchlist(c.Context(), user.ID)
	if err != nil {
		logger.Error("WatchlistHandler: Failed to get watchlist: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch watchlist",
		})
	}

	return c.JSON(fiber.Map{
		"watchlist": watchlist,
		"count":     len(watchlist),
	})
}

// CheckIsBookmarked checks if a market is bookmarked
// GET /api/v1/watchlist/check/:market_id
func (h *WatchlistHandler) CheckIsBookmarked(c *fiber.Ctx) error {
	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var user models.User
	if err := h.db.Where("clerk_id = ?", clerkID).First(&user).Error; err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
	}

	marketID := c.Params("market_id")
	isBookmarked, err := h.watchlistService.IsBookmarked(c.Context(), user.ID, marketID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to check bookmark status",
		})
	}

	return c.JSON(fiber.Map{
		"is_bookmarked": isBookmarked,
		"market_id":     marketID,
	})
}
