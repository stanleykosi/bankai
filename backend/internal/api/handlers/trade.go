/**
 * @description
 * HTTP Handlers for Trade execution.
 * Handles order placement and relay to Polymarket CLOB.
 * Includes validation that the authenticated user owns the signing address.
 *
 * @dependencies
 * - github.com/gofiber/fiber/v2
 * - backend/internal/services
 * - backend/internal/api/middleware
 * - backend/internal/polymarket/clob
 * - backend/internal/models
 * - gorm.io/gorm
 */

package handlers

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/bankai-project/backend/internal/api/middleware"
	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/models"
	"github.com/bankai-project/backend/internal/polymarket/clob"
	"github.com/bankai-project/backend/internal/services"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type TradeHandler struct {
	Service  *services.TradeService
	Verifier *services.SignatureVerifier
	Config   *config.Config
	DB       *gorm.DB
}

func NewTradeHandler(service *services.TradeService, cfg *config.Config, db *gorm.DB) *TradeHandler {
	return &TradeHandler{
		Service:  service,
		Verifier: services.NewSignatureVerifier(),
		Config:   cfg,
		DB:       db,
	}
}

// PostTradeRequest represents the frontend request payload
// The frontend sends the signed order and orderType
// The backend adds the owner (Builder API Key) before relaying to CLOB
type PostTradeRequest struct {
	Order     clob.Order     `json:"order"`
	OrderType clob.OrderType `json:"orderType"`
}

type BatchTradeRequest struct {
	Orders []PostTradeRequest `json:"orders"`
}

type CancelTradeRequest struct {
	OrderID string `json:"orderId"`
}

type CancelTradesRequest struct {
	OrderIDs []string `json:"orderIds"`
}

var validOrderTypes = map[clob.OrderType]struct{}{
	clob.OrderTypeGTC: {},
	clob.OrderTypeGTD: {},
	clob.OrderTypeFOK: {},
	clob.OrderTypeFAK: {},
}

// PostTrade handles POST /api/v1/trade
// Accepts a signed order from the frontend, verifies ownership, and relays to CLOB.
func (h *TradeHandler) PostTrade(c *fiber.Ctx) error {
	// 1. Authenticate User
	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	// 2. Fetch User from DB to get EOA
	user, err := h.fetchUserRecord(c.Context(), clerkID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User profile not found. Please sync user first."})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Database error"})
	}

	// 3. Parse Request
	var req PostTradeRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body: " + err.Error(),
		})
	}

	// 4. Validate Order Structure
	if err := req.Order.Validate(); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid order: " + err.Error(),
		})
	}

	// 5. Verify ownership: signer must match EOA and maker must match vault
	if err := h.Verifier.VerifyOrderOwnership(user, &req.Order); err != nil {
		logger.Error("Trade signature verification failed for user %s: %v", clerkID, err)
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Signature verification failed: " + err.Error(),
		})
	}

	// 6. Normalize Order Type
	normalizedType, err := normalizeOrderType(req.OrderType)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	// 7. Construct CLOB Request
	clobReq := &clob.PostOrderRequest{
		Order:     req.Order,
		Owner:     h.Config.Polymarket.BuilderAPIKey, // Builder Attribution
		OrderType: normalizedType,
	}

	// 8. Relay Trade & Persist
	resp, err := h.Service.RelayTrade(c.Context(), user, clobReq)
	if err != nil {
		logger.Error("Failed to relay trade for user %s: %v", clerkID, err)
		// Distinguish between CLOB rejection (400) and Server error (500) based on error string?
		// For simplicity, we use 502 Bad Gateway as it failed upstream.
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
			"error": "Order placement failed: " + err.Error(),
		})
	}

	// 9. Return Success
	return c.JSON(resp)
}

// PostBatchTrade handles POST /api/v1/trade/batch
func (h *TradeHandler) PostBatchTrade(c *fiber.Ctx) error {
	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	user, err := h.fetchUserRecord(c.Context(), clerkID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User profile not found. Please sync user first."})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Database error"})
	}

	var req BatchTradeRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body: " + err.Error()})
	}
	if len(req.Orders) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "At least one order is required"})
	}
	if len(req.Orders) > 15 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Batch limit is 15 orders per request"})
	}

	var batch []*clob.PostOrderRequest
	for idx, entry := range req.Orders {
		if err := entry.Order.Validate(); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fmt.Sprintf("Order %d invalid: %s", idx, err.Error()),
			})
		}
		if err := h.Verifier.VerifyOrderOwnership(user, &entry.Order); err != nil {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": fmt.Sprintf("Order %d signature verification failed: %s", idx, err.Error()),
			})
		}
		normalizedType, err := normalizeOrderType(entry.OrderType)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fmt.Sprintf("Order %d: %s", idx, err.Error()),
			})
		}
		batch = append(batch, &clob.PostOrderRequest{
			Order:     entry.Order,
			Owner:     h.Config.Polymarket.BuilderAPIKey,
			OrderType: normalizedType,
		})
	}

	responses, err := h.Service.RelayBatchTrades(c.Context(), user, batch)
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "Batch placement failed: " + err.Error()})
	}

	return c.JSON(fiber.Map{
		"count":     len(responses),
		"responses": responses,
	})
}

// GetOrders returns the authenticated user's order history
func (h *TradeHandler) GetOrders(c *fiber.Ctx) error {
	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	user, err := h.fetchUserRecord(c.Context(), clerkID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User profile not found. Please sync user first."})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Database error"})
	}

	limit, offset, parseErr := parsePagination(c.Query("limit"), c.Query("offset"))
	if parseErr != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": parseErr.Error()})
	}

	orders, total, svcErr := h.Service.ListOrders(c.Context(), user.ID, limit, offset)
	if svcErr != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": svcErr.Error()})
	}

	return c.JSON(fiber.Map{
		"data":   orders,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (h *TradeHandler) CancelOrder(c *fiber.Ctx) error {
	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	user, err := h.fetchUserRecord(c.Context(), clerkID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User profile not found. Please sync user first."})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Database error"})
	}

	var req CancelTradeRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	if strings.TrimSpace(req.OrderID) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "orderId is required"})
	}

	resp, svcErr := h.Service.CancelOrder(c.Context(), user, req.OrderID)
	if svcErr != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": svcErr.Error()})
	}

	return c.JSON(resp)
}

func (h *TradeHandler) CancelOrders(c *fiber.Ctx) error {
	clerkID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	user, err := h.fetchUserRecord(c.Context(), clerkID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User profile not found. Please sync user first."})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Database error"})
	}

	var req CancelTradesRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	if len(req.OrderIDs) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "orderIds must include at least one id"})
	}

	resp, svcErr := h.Service.CancelOrders(c.Context(), user, req.OrderIDs)
	if svcErr != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": svcErr.Error()})
	}

	return c.JSON(resp)
}

func (h *TradeHandler) fetchUserRecord(ctx context.Context, clerkID string) (*models.User, error) {
	var user models.User
	if err := h.DB.WithContext(ctx).Where("clerk_id = ?", clerkID).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func parsePagination(limitRaw, offsetRaw string) (int, int, error) {
	limit := 50
	offset := 0
	if limitRaw != "" {
		val, err := strconv.Atoi(limitRaw)
		if err != nil || val <= 0 {
			return 0, 0, fmt.Errorf("invalid limit")
		}
		limit = val
	}
	if offsetRaw != "" {
		val, err := strconv.Atoi(offsetRaw)
		if err != nil || val < 0 {
			return 0, 0, fmt.Errorf("invalid offset")
		}
		offset = val
	}
	return limit, offset, nil
}

func normalizeOrderType(raw clob.OrderType) (clob.OrderType, error) {
	normalized := clob.OrderType(strings.ToUpper(string(raw)))
	if _, ok := validOrderTypes[normalized]; !ok {
		return "", fmt.Errorf("invalid orderType: %s", raw)
	}
	return normalized, nil
}
