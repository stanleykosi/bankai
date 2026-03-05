package handlers

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/api/middleware"
	"github.com/bankai-project/backend/internal/services"
	"github.com/gofiber/fiber/v2"
)

type UpDownHandler struct {
	Service *services.UpDownService
}

func NewUpDownHandler(service *services.UpDownService) *UpDownHandler {
	return &UpDownHandler{Service: service}
}

// GET /api/v1/updown/markets?asset=BTC&window=5m|15m|1h|4h
func (h *UpDownHandler) GetMarkets(c *fiber.Ctx) error {
	if h.Service == nil || !h.Service.Enabled() {
		return c.JSON([]services.UpDownMarket{})
	}
	markets, err := h.Service.ListMarkets(c.Context(), c.Query("asset"), c.Query("window"))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(markets)
}

// GET /api/v1/updown/market/:slug
func (h *UpDownHandler) GetMarket(c *fiber.Ctx) error {
	if h.Service == nil || !h.Service.Enabled() {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "updown service unavailable"})
	}
	slug := strings.TrimSpace(c.Params("slug"))
	market, err := h.Service.GetMarket(c.Context(), slug)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if market == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "market not found"})
	}
	return c.JSON(market)
}

// GET /api/v1/updown/signal/:slug
func (h *UpDownHandler) GetSignal(c *fiber.Ctx) error {
	if h.Service == nil || !h.Service.Enabled() {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "updown service unavailable"})
	}
	slug := strings.TrimSpace(c.Params("slug"))
	signal, err := h.Service.GetSignal(c.Context(), slug)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	if signal == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "signal not found"})
	}
	return c.JSON(signal)
}

// GET /api/v1/updown/recommendations?asset=BTC&limit=50
func (h *UpDownHandler) GetRecommendations(c *fiber.Ctx) error {
	if h.Service == nil || !h.Service.Enabled() {
		return c.JSON([]services.UpDownRecommendation{})
	}
	limit := c.QueryInt("limit", 40)
	recs, err := h.Service.ListRecommendations(c.Context(), c.Query("asset"), limit)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(recs)
}

// GET /api/v1/updown/stream
func (h *UpDownHandler) Stream(c *fiber.Ctx) error {
	if h.Service == nil || !h.Service.Enabled() {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "updown service unavailable"})
	}
	if h.Service.StreamHub() == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "updown stream unavailable"})
	}
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")

	requestCtx := c.Context()
	ctx, cancel := context.WithCancel(context.Background())
	msgCh, unsubscribe := h.Service.StreamHub().Subscribe()

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

// POST /api/v1/updown/decisions
func (h *UpDownHandler) LogDecision(c *fiber.Ctx) error {
	if h.Service == nil || !h.Service.Enabled() {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "updown service unavailable"})
	}
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
	var req services.UpDownDecisionLogRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}
	row, err := h.Service.LogDecision(c.Context(), userID, req)
	if err != nil {
		if errors.Is(err, services.ErrInvalidUpDownDecisionRequest) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to persist decision"})
	}
	return c.JSON(row)
}

// GET /api/v1/updown/performance?from=YYYY-MM-DD&to=YYYY-MM-DD
func (h *UpDownHandler) GetPerformance(c *fiber.Ctx) error {
	if h.Service == nil || !h.Service.Enabled() {
		return c.JSON(&services.UpDownPerformanceSummary{
			From:      time.Now().UTC().AddDate(0, 0, -7).Format("2006-01-02"),
			To:        time.Now().UTC().Format("2006-01-02"),
			ByAsset:   []services.PerformanceSlice{},
			ByWindow:  []services.PerformanceSlice{},
			UpdatedAt: time.Now().UTC(),
		})
	}
	now := time.Now().UTC()
	to := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	from := to.AddDate(0, 0, -14)
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		if parsed, err := time.Parse("2006-01-02", raw); err == nil {
			from = parsed.UTC()
		}
	}
	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		if parsed, err := time.Parse("2006-01-02", raw); err == nil {
			to = parsed.UTC()
		}
	}
	summary, err := h.Service.GetPerformance(c.Context(), from, to)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(summary)
}
