/**
 * @description
 * HTTP Handlers for the AI Oracle.
 *
 * @dependencies
 * - backend/internal/services
 * - github.com/gofiber/fiber/v2
 */

package handlers

import (
	"errors"
	"strings"

	"github.com/bankai-project/backend/internal/services"
	"github.com/gofiber/fiber/v2"
)

type OracleHandler struct {
	Service *services.OracleService
}

func NewOracleHandler(service *services.OracleService) *OracleHandler {
	return &OracleHandler{Service: service}
}

// AnalyzeMarket triggers an AI analysis of a specific market.
// GET /api/v1/oracle/analyze/:condition_id
func (h *OracleHandler) AnalyzeMarket(c *fiber.Ctx) error {
	conditionID := strings.TrimSpace(c.Params("condition_id"))
	if conditionID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "condition_id is required"})
	}

	analysis, err := h.Service.AnalyzeMarket(c.Context(), conditionID)
	if err != nil {
		if errors.Is(err, services.ErrMarketNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "market not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Analysis failed: " + err.Error(),
		})
	}

	return c.JSON(analysis)
}
