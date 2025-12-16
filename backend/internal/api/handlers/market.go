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
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/bankai-project/backend/internal/polymarket/clob"
	"github.com/bankai-project/backend/internal/services"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
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

	category := c.Query("category")
	tag := c.Query("tag")
	sortParam := c.Query("sort")
	limit := c.QueryInt("limit", 0)
	offset := c.QueryInt("offset", 0)

	if limit <= 0 {
		limit = 500
	}
	if limit > 1000 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}

	useCache := category == "" && tag == "" && (sortParam == "" || sortParam == "all")
	if useCache {
		markets, total, err := h.Service.GetActiveMarketsPaged(ctx, limit, offset)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to fetch active markets",
			})
		}
		c.Set("X-Total-Count", strconv.Itoa(total))
		return c.JSON(markets)
	}

	sort := sortParam
	if sort == "all" {
		sort = ""
	}

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

// GetPriceHistory returns historical price data for the market's YES token.
// GET /api/v1/markets/:condition_id/history?range=1d
func (h *MarketHandler) GetPriceHistory(c *fiber.Ctx) error {
	conditionID := strings.TrimSpace(c.Params("condition_id"))
	if conditionID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "condition_id param is required",
		})
	}

	rangeParam := c.Query("range")
	history, err := h.Service.GetPriceHistory(c.Context(), conditionID, rangeParam)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "market not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	if history == nil {
		history = []clob.HistoryPoint{}
	}

	return c.JSON(history)
}

// StreamPriceUpdates streams live price updates over SSE
func (h *MarketHandler) StreamPriceUpdates(c *fiber.Ctx) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")

	requestCtx := c.Context()

	ctx, cancel := context.WithCancel(context.Background())

	streamHub := h.Service.StreamHub()
	msgCh, unsubscribe := streamHub.Subscribe()

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer func() {
			cancel()
			unsubscribe()
		}()

		requestDone := requestCtx.Done()

		for {
			select {
			case <-requestDone:
				return
			case <-ctx.Done():
				return
			case msg, ok := <-msgCh:
				if !ok {
					return
				}
				fmt.Fprintf(w, "data: %s\n\n", msg)
				if err := w.Flush(); err != nil {
					return
				}
			}
		}
	})

	return nil
}

func (h *MarketHandler) GetActiveMarketsMeta(c *fiber.Ctx) error {
	ctx := c.Context()
	meta, err := h.Service.GetActiveMarketsMeta(ctx)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to compute market metadata",
		})
	}
	return c.JSON(meta)
}

func (h *MarketHandler) GetMarketLanes(c *fiber.Ctx) error {
	ctx := c.Context()

	params := services.MarketLaneParams{
		Category: c.Query("category"),
		Tag:      c.Query("tag"),
		PoolSort: c.Query("sort"),
	}

	lanes, err := h.Service.GetMarketLanes(ctx, params)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to compute market lanes",
		})
	}

	return c.JSON(lanes)
}

// GetMarketBySlug returns a single market by slug.
// GET /api/v1/markets/:slug
func (h *MarketHandler) GetMarketBySlug(c *fiber.Ctx) error {
	slug := c.Params("slug")
	if slug == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Market slug is required",
		})
	}

	ctx := c.Context()
	market, err := h.Service.GetMarketBySlug(ctx, slug)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch market: " + err.Error(),
		})
	}

	if market == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "Market not found",
		})
	}

	return c.JSON(market)
}

// GetDepthEstimate returns an estimated execution summary for a market/token pair.
func (h *MarketHandler) GetDepthEstimate(c *fiber.Ctx) error {
	marketID := c.Params("condition_id")
	tokenID := c.Query("tokenId")
	side := strings.ToUpper(c.Query("side"))
	size := c.QueryFloat("size", 0)

	if strings.TrimSpace(marketID) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "condition_id param is required"})
	}
	if strings.TrimSpace(tokenID) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "tokenId query param is required"})
	}
	if side != "BUY" && side != "SELL" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "side must be BUY or SELL"})
	}
	if size <= 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "size must be greater than zero"})
	}

	estimate, err := h.Service.GetDepthEstimate(c.Context(), marketID, tokenID, side, size)
	if err != nil {
		if errors.Is(err, services.ErrOrderBookUnavailable) {
			return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"error": "Order book snapshot unavailable"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(estimate)
}

// RequestMarketStream allows clients to request live streaming for a specific market.
func (h *MarketHandler) RequestMarketStream(c *fiber.Ctx) error {
	conditionID := strings.TrimSpace(c.Params("condition_id"))
	if conditionID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "condition_id param is required"})
	}

	if err := h.Service.RequestMarketStream(c.Context(), conditionID); err != nil {
		if errors.Is(err, services.ErrMarketHasNoTokens) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "market has no tradable tokens"})
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "market not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"status":       "queued",
		"condition_id": conditionID,
	})
}
