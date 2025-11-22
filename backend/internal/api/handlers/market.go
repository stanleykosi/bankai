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
	"bufio"
	"context"
	"fmt"

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

	var (
		category = c.Query("category")
		tag      = c.Query("tag")
		sort     = c.Query("sort", "volume") // volume | liquidity | created
		limit    = c.QueryInt("limit", 50)
		offset   = c.QueryInt("offset", 0)
	)

	markets, err := h.Service.QueryActiveMarkets(ctx, services.QueryActiveMarketsParams{
		Category: category,
		Tag:      tag,
		Sort:     sort,
		Limit:    limit,
		Offset:   offset,
	})
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

// StreamPriceUpdates streams live price updates over SSE
func (h *MarketHandler) StreamPriceUpdates(c *fiber.Ctx) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pubsub := h.Service.Redis.Subscribe(ctx, services.PriceUpdateChannel)
	defer pubsub.Close()

	ch := pubsub.Channel()

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer cancel()
		for {
			select {
			case <-c.Context().Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				fmt.Fprintf(w, "data: %s\n\n", msg.Payload)
				if err := w.Flush(); err != nil {
					return
				}
			}
		}
	})

	return nil
}

