/**
 * @description
 * Market API Handlers.
 * Exposes endpoints to fetch market data (Active, Fresh, etc.)
 *
 * @dependencies
 * - github.com/gofiber/fiber/v2
 * - backend/internal/services
 */

package handlers

import (
	"github.com/bankai-project/backend/internal/services"
	"github.com/gofiber/fiber/v2"
)

type MarketHandler struct {
	Service *services.MarketService
}

func NewMarketHandler(service *services.MarketService) *MarketHandler {
	return &MarketHandler{Service: service}
}

// GetActiveMarkets returns the top active markets by volume
// GET /api/v1/markets/active
func (h *MarketHandler) GetActiveMarkets(c *fiber.Ctx) error {
	ctx := c.Context()
	markets, err := h.Service.GetActiveMarkets(ctx)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch active markets",
		})
	}
	return c.JSON(markets)
}

// GetFreshDrops returns the most recently created markets
// GET /api/v1/markets/fresh
func (h *MarketHandler) GetFreshDrops(c *fiber.Ctx) error {
	ctx := c.Context()
	markets, err := h.Service.GetFreshDrops(ctx)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch fresh drops",
		})
	}
	return c.JSON(markets)
}

