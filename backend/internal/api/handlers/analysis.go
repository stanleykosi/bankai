/**
 * @description
 * HTTP handlers for the Alpha Hub (/analysis) endpoints.
 *
 * @dependencies
 * - backend/internal/services
 * - github.com/gofiber/fiber/v2
 */

package handlers

import (
	"strconv"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/services"
	"github.com/gofiber/fiber/v2"
)

type AnalysisHandler struct {
	Service *services.AlphaHubService
}

func NewAnalysisHandler(service *services.AlphaHubService) *AnalysisHandler {
	return &AnalysisHandler{Service: service}
}

// GetSmartMoney returns the smart-money + whale signals for the last hour (default) or provided window.
// GET /api/v1/analysis/smart-money?window=60
func (h *AnalysisHandler) GetSmartMoney(c *fiber.Ctx) error {
	window := time.Duration(60) * time.Minute
	if raw := strings.TrimSpace(c.Query("window")); raw != "" {
		if mins, err := strconv.Atoi(raw); err == nil && mins > 0 && mins <= 180 {
			window = time.Duration(mins) * time.Minute
		}
	}

	resp, err := h.Service.GetSmartMoneySignals(c.Context(), window)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(resp)
}

// GetAIPicks runs the Alpha Hub LLM against the latest smart-money payload.
// GET /api/v1/analysis/ai-picks?window=60
func (h *AnalysisHandler) GetAIPicks(c *fiber.Ctx) error {
	window := time.Duration(60) * time.Minute
	if raw := strings.TrimSpace(c.Query("window")); raw != "" {
		if mins, err := strconv.Atoi(raw); err == nil && mins > 0 && mins <= 180 {
			window = time.Duration(mins) * time.Minute
		}
	}

	signals, err := h.Service.GetSmartMoneySignals(c.Context(), window)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}

	ai, err := h.Service.GenerateAIPicks(c.Context(), signals)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(ai)
}
