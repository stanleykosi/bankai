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
	Config   *config.Config
	DB       *gorm.DB
}

func NewTradeHandler(service *services.TradeService, cfg *config.Config, db *gorm.DB) *TradeHandler {
	return &TradeHandler{
		Service: service,
		Config:  cfg,
		DB:      db,
	}
}

// PostTradeRequest and BatchTradeRequest types have been removed.
// The frontend now uses the official Polymarket SDK directly, so these request types are no longer needed.

type CancelTradeRequest struct {
	OrderID string `json:"orderId"`
}

type CancelTradesRequest struct {
	OrderIDs []string `json:"orderIds"`
}

// SyncOrdersRequest is used by the frontend (after fetching via the SDK) to persist orders.
type SyncOrdersRequest struct {
	Orders []services.SyncedOrder `json:"orders"`
}

var validOrderTypes = map[clob.OrderType]struct{}{
	clob.OrderTypeGTC: {},
	clob.OrderTypeGTD: {},
	clob.OrderTypeFOK: {},
	clob.OrderTypeFAK: {},
}

// GetAuthTypedData endpoint has been removed.
// The frontend now uses the official Polymarket SDK's deriveApiKey/createApiKey methods,
// which handle EIP-712 signing internally.

// PostTrade and PostBatchTrade endpoints have been removed.
// The frontend now uses the official Polymarket SDK directly for order creation, signing, and submission.
// This eliminates the need for backend order relaying and ensures compatibility with the official SDK.

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

// SyncOrders persists Polymarket orders fetched via the SDK into Postgres for history/audit.
func (h *TradeHandler) SyncOrders(c *fiber.Ctx) error {
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

	var req SyncOrdersRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	if len(req.Orders) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "orders array is required"})
	}

	if err := h.Service.SyncOrdersFromSDK(c.Context(), user, req.Orders); err != nil {
		logger.Error("SyncOrders failed: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to sync orders"})
	}

	return c.JSON(fiber.Map{"status": "ok"})
}

// SyncOrdersInternal allows background workers to persist orders by maker address using JOB_SYNC_SECRET.
func (h *TradeHandler) SyncOrdersInternal(c *fiber.Ctx) error {
	secret := c.Get("X-Job-Secret")
	if secret == "" || secret != h.Config.Services.SyncJobSecret {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	var req SyncOrdersRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	if len(req.Orders) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "orders array is required"})
	}

	if err := h.Service.SyncOrdersByAddress(c.Context(), req.Orders); err != nil {
		logger.Error("SyncOrdersInternal failed: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to sync orders"})
	}

	return c.JSON(fiber.Map{"status": "ok"})
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

// normalizeSignatureType function has been removed.
// The frontend now uses the official Polymarket SDK which handles signature types internally.
