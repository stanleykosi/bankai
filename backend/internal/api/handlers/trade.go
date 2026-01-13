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
	"time"

	"github.com/bankai-project/backend/internal/api/middleware"
	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/models"
	"github.com/bankai-project/backend/internal/polymarket/clob"
	"github.com/bankai-project/backend/internal/services"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type TradeHandler struct {
	Service       *services.TradeService
	Notifications *services.NotificationService
	Config        *config.Config
	DB            *gorm.DB
}

func NewTradeHandler(service *services.TradeService, notifications *services.NotificationService, cfg *config.Config, db *gorm.DB) *TradeHandler {
	return &TradeHandler{
		Service:       service,
		Notifications: notifications,
		Config:        cfg,
		DB:            db,
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
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	user, err := h.fetchUserRecord(c.Context(), userID)
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
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	user, err := h.fetchUserRecord(c.Context(), userID)
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
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	user, err := h.fetchUserRecord(c.Context(), userID)
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
	userID, err := middleware.GetUserID(c)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	user, err := h.fetchUserRecord(c.Context(), userID)
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

	existingOrders := h.getExistingOrderIDs(c.Context(), user.ID, req.Orders)

	if err := h.Service.SyncOrdersFromSDK(c.Context(), user, req.Orders); err != nil {
		logger.Error("SyncOrders failed: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to sync orders"})
	}

	h.notifyFollowers(c.Context(), user, req.Orders, existingOrders)

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

	existingOrders := h.getExistingOrderIDsByAddress(c.Context(), req.Orders)

	if err := h.Service.SyncOrdersByAddress(c.Context(), req.Orders); err != nil {
		logger.Error("SyncOrdersInternal failed: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to sync orders"})
	}

	h.notifyFollowersByAddress(c.Context(), req.Orders, existingOrders)

	return c.JSON(fiber.Map{"status": "ok"})
}

func (h *TradeHandler) fetchUserRecord(ctx context.Context, userID string) (*models.User, error) {
	var user models.User
	if err := h.DB.WithContext(ctx).Where("id = ?", userID).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (h *TradeHandler) getExistingOrderIDs(ctx context.Context, userID uuid.UUID, orders []services.SyncedOrder) map[string]struct{} {
	existing := make(map[string]struct{})
	if len(orders) == 0 {
		return existing
	}

	orderIDs := uniqueOrderIDs(orders)
	if len(orderIDs) == 0 {
		return existing
	}

	var rows []models.Order
	if err := h.DB.WithContext(ctx).
		Select("clob_order_id").
		Where("user_id = ? AND clob_order_id IN ?", userID, orderIDs).
		Find(&rows).Error; err != nil {
		logger.Error("TradeHandler: Failed to check existing orders: %v", err)
		return existing
	}

	for _, row := range rows {
		if row.CLOBOrderID != "" {
			existing[row.CLOBOrderID] = struct{}{}
		}
	}

	return existing
}

func (h *TradeHandler) getExistingOrderIDsByAddress(ctx context.Context, orders []services.SyncedOrder) map[string]struct{} {
	existing := make(map[string]struct{})
	if len(orders) == 0 {
		return existing
	}

	orderIDs := uniqueOrderIDs(orders)
	if len(orderIDs) == 0 {
		return existing
	}

	var rows []models.Order
	if err := h.DB.WithContext(ctx).
		Select("clob_order_id").
		Where("clob_order_id IN ?", orderIDs).
		Find(&rows).Error; err != nil {
		logger.Error("TradeHandler: Failed to check existing orders: %v", err)
		return existing
	}

	for _, row := range rows {
		if row.CLOBOrderID != "" {
			existing[row.CLOBOrderID] = struct{}{}
		}
	}

	return existing
}

func (h *TradeHandler) notifyFollowers(ctx context.Context, user *models.User, orders []services.SyncedOrder, existing map[string]struct{}) {
	if h.Notifications == nil || len(orders) == 0 {
		return
	}

	marketMap := fetchMarketMap(ctx, h.DB, orders)
	sent := make(map[string]struct{})

	for _, order := range orders {
		if shouldSkipNotification(order, existing) {
			continue
		}
		if _, ok := sent[order.OrderID]; ok {
			continue
		}

		traderAddress := strings.ToLower(strings.TrimSpace(order.MakerAddress))
		if traderAddress == "" && user != nil {
			if user.VaultAddress != "" {
				traderAddress = strings.ToLower(user.VaultAddress)
			} else if user.EOAAddress != "" {
				traderAddress = strings.ToLower(user.EOAAddress)
			}
		}
		if traderAddress == "" {
			continue
		}

		data := buildTradeAlertData(order, traderAddress, marketMap)
		if err := h.Notifications.CreateTradeAlert(ctx, data); err != nil {
			logger.Error("TradeHandler: Failed to create trade alert: %v", err)
		} else {
			sent[order.OrderID] = struct{}{}
		}
	}
}

func (h *TradeHandler) notifyFollowersByAddress(ctx context.Context, orders []services.SyncedOrder, existing map[string]struct{}) {
	if h.Notifications == nil || len(orders) == 0 {
		return
	}

	marketMap := fetchMarketMap(ctx, h.DB, orders)
	sent := make(map[string]struct{})

	for _, order := range orders {
		if shouldSkipNotification(order, existing) {
			continue
		}
		if _, ok := sent[order.OrderID]; ok {
			continue
		}

		traderAddress := strings.ToLower(strings.TrimSpace(order.MakerAddress))
		if traderAddress == "" {
			continue
		}

		data := buildTradeAlertData(order, traderAddress, marketMap)
		if err := h.Notifications.CreateTradeAlert(ctx, data); err != nil {
			logger.Error("TradeHandler: Failed to create trade alert: %v", err)
		} else {
			sent[order.OrderID] = struct{}{}
		}
	}
}

func shouldSkipNotification(order services.SyncedOrder, existing map[string]struct{}) bool {
	if order.OrderID == "" {
		return true
	}
	if _, ok := existing[order.OrderID]; ok {
		return true
	}
	status := strings.ToLower(strings.TrimSpace(order.Status))
	if strings.Contains(status, "cancel") || strings.Contains(status, "fail") {
		return true
	}
	return false
}

func uniqueOrderIDs(orders []services.SyncedOrder) []string {
	seen := make(map[string]struct{})
	ids := make([]string, 0, len(orders))
	for _, order := range orders {
		id := strings.TrimSpace(order.OrderID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func fetchMarketMap(ctx context.Context, db *gorm.DB, orders []services.SyncedOrder) map[string]models.Market {
	marketMap := make(map[string]models.Market)
	if db == nil || len(orders) == 0 {
		return marketMap
	}

	seen := make(map[string]struct{})
	ids := make([]string, 0, len(orders))
	for _, order := range orders {
		if order.MarketID == "" {
			continue
		}
		if _, ok := seen[order.MarketID]; ok {
			continue
		}
		seen[order.MarketID] = struct{}{}
		ids = append(ids, order.MarketID)
	}
	if len(ids) == 0 {
		return marketMap
	}

	var markets []models.Market
	if err := db.WithContext(ctx).
		Where("condition_id IN ?", ids).
		Find(&markets).Error; err != nil {
		logger.Error("TradeHandler: Failed to load markets for notifications: %v", err)
		return marketMap
	}

	for _, market := range markets {
		if market.ConditionID != "" {
			marketMap[market.ConditionID] = market
		}
	}

	return marketMap
}

func buildTradeAlertData(order services.SyncedOrder, traderAddress string, marketMap map[string]models.Market) services.TradeAlertData {
	side := strings.ToUpper(strings.TrimSpace(order.Side))
	if side == "" {
		side = "BUY"
	}
	outcome := order.Outcome
	if outcome == "" {
		outcome = "Outcome"
	}

	marketTitle := order.MarketID
	marketSlug := order.MarketID
	if market, ok := marketMap[order.MarketID]; ok {
		if market.Title != "" {
			marketTitle = market.Title
		}
		if market.Slug != "" {
			marketSlug = market.Slug
		}
	}
	if marketTitle == "" {
		marketTitle = "Unknown market"
	}
	if marketSlug == "" {
		marketSlug = "unknown"
	}

	ts := order.UpdatedAt
	if ts.IsZero() {
		ts = order.CreatedAt
	}
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	return services.TradeAlertData{
		TraderAddress: traderAddress,
		MarketSlug:    marketSlug,
		MarketTitle:   marketTitle,
		Side:          side,
		Outcome:       outcome,
		Price:         order.Price,
		Size:          order.Size,
		Value:         order.Price * order.Size,
		Timestamp:     ts.UTC().Format(time.RFC3339),
	}
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
