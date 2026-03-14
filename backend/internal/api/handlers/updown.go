package handlers

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/api/middleware"
	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/services"
	"github.com/gofiber/fiber/v2"
)

type UpDownHandler struct {
	Service    *services.UpDownService
	LLMService *services.UpDownLLMService
}

func NewUpDownHandler(service *services.UpDownService, llmService *services.UpDownLLMService) *UpDownHandler {
	return &UpDownHandler{
		Service:    service,
		LLMService: llmService,
	}
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
	c.Set("X-Accel-Buffering", "no")

	requestCtx := c.Context()
	ctx, cancel := context.WithCancel(context.Background())
	msgCh, unsubscribe := h.Service.StreamHub().Subscribe()

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer func() {
			cancel()
			unsubscribe()
		}()
		requestDone := requestCtx.Done()
		keepalive := time.NewTicker(15 * time.Second)
		defer keepalive.Stop()
		for {
			select {
			case <-requestDone:
				return
			case <-ctx.Done():
				return
			case <-keepalive.C:
				fmt.Fprint(w, ": ping\n\n")
				if err := w.Flush(); err != nil {
					return
				}
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

// POST /api/v1/updown/llm/generate
func (h *UpDownHandler) GenerateLLMPacket(c *fiber.Ctx) error {
	if h.LLMService == nil || !h.LLMService.Enabled() {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "updown llm service unavailable"})
	}
	var req services.UpDownLLMGenerateRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
	}
	packet, err := h.LLMService.Generate(c.Context(), req)
	if err != nil {
		logger.Error("updown llm generate failed slug=%s force_refresh=%t err=%v", strings.TrimSpace(req.Slug), req.ForceRefresh, err)
		status := fiber.StatusBadRequest
		if isGatewayTimeoutErr(err) {
			status = fiber.StatusGatewayTimeout
		}
		return c.Status(status).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(packet)
}

// GET /api/v1/updown/llm/packet/:slug
func (h *UpDownHandler) GetLLMPacket(c *fiber.Ctx) error {
	if h.LLMService == nil || !h.LLMService.Enabled() {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "updown llm service unavailable"})
	}
	slug := strings.TrimSpace(c.Params("slug"))
	packet, err := h.LLMService.GetPacket(c.Context(), slug)
	if err != nil {
		if errors.Is(err, services.ErrUpDownLLMNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "llm packet not found"})
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(packet)
}

// GET /api/v1/updown/llm/health
func (h *UpDownHandler) LLMHealth(c *fiber.Ctx) error {
	if h.LLMService == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "updown llm service unavailable"})
	}
	return c.JSON(h.LLMService.Health())
}

func isGatewayTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
