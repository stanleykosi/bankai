/**
 * @description
 * HTTP Handlers for Trade execution.
 * Handles order placement and relay to Polymarket CLOB.
 * 
 * @dependencies
 * - github.com/gofiber/fiber/v2
 * - backend/internal/services
 * - backend/internal/api/middleware
 * - backend/internal/polymarket/clob
 */

package handlers

import (
	"strings"

	"github.com/bankai-project/backend/internal/api/middleware"
	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/polymarket/clob"
	"github.com/bankai-project/backend/internal/services"
	"github.com/gofiber/fiber/v2"
)

type TradeHandler struct {
	Service *services.TradeService
	Config  *config.Config
}

func NewTradeHandler(service *services.TradeService, cfg *config.Config) *TradeHandler {
	return &TradeHandler{
		Service: service,
		Config:  cfg,
	}
}

// PostTradeRequest represents the frontend request payload
// The frontend sends the signed order and orderType
// The backend adds the owner (Builder API Key) before relaying to CLOB
type PostTradeRequest struct {
	Order     clob.Order     `json:"order"`
	OrderType clob.OrderType `json:"orderType"`
}

// PostTrade handles POST /api/v1/trade
// Accepts a signed order from the frontend and relays it to Polymarket CLOB
// with Builder Attribution headers
func (h *TradeHandler) PostTrade(c *fiber.Ctx) error {
	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var req PostTradeRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body: " + err.Error(),
		})
	}

	// Validate the order structure
	if err := req.Order.Validate(); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid order: " + err.Error(),
		})
	}

	// Normalize orderType
	normalizedType := clob.OrderType(strings.ToUpper(string(req.OrderType)))
	validTypes := map[clob.OrderType]struct{}{
		clob.OrderTypeGTC: {},
		clob.OrderTypeGTD: {},
		clob.OrderTypeFOK: {},
		clob.OrderTypeFAK: {},
	}
	if _, ok := validTypes[normalizedType]; !ok {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid orderType: " + string(req.OrderType),
		})
	}

	// Build the CLOB request
	// The owner field should be the Builder API Key for attribution
	clobReq := &clob.PostOrderRequest{
		Order:     req.Order,
		Owner:     h.Config.Polymarket.BuilderAPIKey, // Builder API Key for attribution
		OrderType: normalizedType,
	}

	// Validate the complete request
	if err := clobReq.Validate(); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid CLOB request: " + err.Error(),
		})
	}

	// Relay to CLOB
	resp, err := h.Service.RelayTrade(c.Context(), clobReq)
	if err != nil {
		logger.Error("Failed to relay trade for user %s: %v", clerkID, err)
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
			"error": "Failed to place order: " + err.Error(),
		})
	}

	// Return the CLOB response
	return c.JSON(resp)
}

