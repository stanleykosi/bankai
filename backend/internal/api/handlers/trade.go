/**
 * @description
 * HTTP handlers for trade actions that still require backend access.
 *
 * Order placement/history persistence are handled directly in the frontend via
 * the official Polymarket SDK and CLOB API.
 */

package handlers

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/api/middleware"
	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/models"
	"github.com/bankai-project/backend/internal/services"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type TradeHandler struct {
	Service       *services.TradeService
	Notifications *services.NotificationService
	Config        *config.Config
	DB            *gorm.DB
	Redis         *redis.Client
}

func NewTradeHandler(service *services.TradeService, notifications *services.NotificationService, cfg *config.Config, db *gorm.DB, rdb *redis.Client) *TradeHandler {
	return &TradeHandler{
		Service:       service,
		Notifications: notifications,
		Config:        cfg,
		DB:            db,
		Redis:         rdb,
	}
}

type CancelTradeRequest struct {
	OrderID string `json:"orderId"`
}

type CancelTradesRequest struct {
	OrderIDs []string `json:"orderIds"`
}

type SyncOrdersRequest struct {
	Orders []SyncedOrder `json:"orders"`
}

type SyncedOrder struct {
	OrderID        string    `json:"orderId"`
	MarketID       string    `json:"marketId"`
	Outcome        string    `json:"outcome"`
	OutcomeTokenID string    `json:"outcomeTokenId"`
	MakerAddress   string    `json:"makerAddress"`
	Side           string    `json:"side"`
	Price          float64   `json:"price"`
	Size           float64   `json:"size"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

const seenTradeAlertTTL = 30 * 24 * time.Hour
const pendingTradeAlertTTL = 2 * time.Minute

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
		if errors.Is(svcErr, services.ErrBackendCancellationDisabled) {
			return c.Status(fiber.StatusGone).JSON(fiber.Map{"error": svcErr.Error()})
		}
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
		if errors.Is(svcErr, services.ErrBackendCancellationDisabled) {
			return c.Status(fiber.StatusGone).JSON(fiber.Map{"error": svcErr.Error()})
		}
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": svcErr.Error()})
	}

	return c.JSON(resp)
}

// SyncOrders emits follower trade alerts for SDK-fetched order lifecycle snapshots.
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
	req.Orders = normalizeSyncedOrderMakers(user, req.Orders)

	h.notifyFollowers(c.Context(), user, req.Orders)
	return c.JSON(fiber.Map{"status": "ok"})
}

func (h *TradeHandler) fetchUserRecord(ctx context.Context, userID string) (*models.User, error) {
	var user models.User
	if err := h.DB.WithContext(ctx).Where("id = ?", userID).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func normalizeAddress(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func normalizeSyncedOrderMakers(user *models.User, orders []SyncedOrder) []SyncedOrder {
	if user == nil || len(orders) == 0 {
		return orders
	}

	vault := normalizeAddress(user.VaultAddress)
	eoa := normalizeAddress(user.EOAAddress)
	preferred := vault
	if preferred == "" {
		preferred = eoa
	}

	allowed := map[string]struct{}{}
	if vault != "" {
		allowed[vault] = struct{}{}
	}
	if eoa != "" {
		allowed[eoa] = struct{}{}
	}

	normalized := make([]SyncedOrder, len(orders))
	for i, order := range orders {
		maker := normalizeAddress(order.MakerAddress)
		if maker == "" {
			order.MakerAddress = preferred
			normalized[i] = order
			continue
		}

		if _, ok := allowed[maker]; ok {
			order.MakerAddress = maker
		} else {
			// `maker_address` can be a counterparty on taker-side fills.
			order.MakerAddress = preferred
		}
		normalized[i] = order
	}

	return normalized
}

func (h *TradeHandler) notifyFollowers(ctx context.Context, user *models.User, orders []SyncedOrder) {
	if h == nil || h.Notifications == nil || len(orders) == 0 {
		return
	}

	marketMap := h.fetchMarketMap(ctx, orders)
	seenInBatch := make(map[string]struct{}, len(orders))
	userScope := ""
	vaultAddress := ""
	eoaAddress := ""
	if user != nil {
		userScope = strings.TrimSpace(user.ID.String())
		vaultAddress = strings.ToLower(strings.TrimSpace(user.VaultAddress))
		eoaAddress = strings.ToLower(strings.TrimSpace(user.EOAAddress))
	}

	for _, order := range orders {
		orderID := strings.TrimSpace(order.OrderID)
		if orderID == "" {
			continue
		}
		if !shouldNotifyForStatus(order.Status) {
			continue
		}
		if _, ok := seenInBatch[orderID]; ok {
			continue
		}

		preferredTraderAddress := vaultAddress
		if preferredTraderAddress == "" {
			preferredTraderAddress = eoaAddress
		}
		traderAddress := preferredTraderAddress
		candidate := normalizeAddress(order.MakerAddress)
		if candidate != "" {
			if candidate == vaultAddress || candidate == eoaAddress {
				traderAddress = candidate
			}
		}
		if traderAddress == "" {
			continue
		}

		reserved, reserveErr := h.reserveOrderAlert(ctx, userScope, orderID)
		if reserveErr != nil {
			logger.Error("TradeHandler: Failed to reserve dedupe key for order %s: %v", orderID, reserveErr)
			continue
		}
		if !reserved {
			continue
		}

		data := buildTradeAlertData(order, traderAddress, marketMap)
		if err := h.Notifications.CreateTradeAlert(ctx, data); err != nil {
			logger.Error("TradeHandler: Failed to create trade alert for order %s: %v", orderID, err)
			h.releaseOrderAlert(ctx, userScope, orderID)
			continue
		}

		seenInBatch[orderID] = struct{}{}
		if err := h.confirmOrderAlert(ctx, userScope, orderID); err != nil {
			logger.Error("TradeHandler: Failed to confirm dedupe key for order %s: %v", orderID, err)
		}
	}
}

func (h *TradeHandler) fetchMarketMap(ctx context.Context, orders []SyncedOrder) map[string]models.Market {
	marketMap := make(map[string]models.Market)
	if h == nil || h.DB == nil || len(orders) == 0 {
		return marketMap
	}

	seen := make(map[string]struct{}, len(orders))
	ids := make([]string, 0, len(orders))
	for _, order := range orders {
		id := strings.TrimSpace(order.MarketID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return marketMap
	}

	var markets []models.Market
	if err := h.DB.WithContext(ctx).Where("condition_id IN ?", ids).Find(&markets).Error; err != nil {
		logger.Error("TradeHandler: Failed to load markets for alerts: %v", err)
		return marketMap
	}
	for _, market := range markets {
		if strings.TrimSpace(market.ConditionID) != "" {
			marketMap[market.ConditionID] = market
		}
	}
	return marketMap
}

func normalizeTradeAlertScope(userScope string) string {
	userScope = strings.TrimSpace(userScope)
	if userScope == "" {
		return "anon"
	}
	return strings.ToLower(userScope)
}

func tradeAlertSeenKey(userScope, orderID string) string {
	return "trade:alert:seen:" + normalizeTradeAlertScope(userScope) + ":" + strings.ToLower(strings.TrimSpace(orderID))
}

func (h *TradeHandler) reserveOrderAlert(ctx context.Context, userScope, orderID string) (bool, error) {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return false, nil
	}
	if h == nil || h.Redis == nil {
		// Fail-open; durable dedupe happens in Postgres notifications.
		return true, nil
	}

	ok, err := h.Redis.SetNX(ctx, tradeAlertSeenKey(userScope, orderID), "pending", pendingTradeAlertTTL).Result()
	if err != nil {
		// Redis errors should not suppress alerts.
		return true, nil
	}
	return ok, nil
}

func (h *TradeHandler) confirmOrderAlert(ctx context.Context, userScope, orderID string) error {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return nil
	}
	if h == nil || h.Redis == nil {
		return nil
	}

	return h.Redis.Set(ctx, tradeAlertSeenKey(userScope, orderID), "sent", seenTradeAlertTTL).Err()
}

func (h *TradeHandler) releaseOrderAlert(ctx context.Context, userScope, orderID string) {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" || h == nil || h.Redis == nil {
		return
	}

	if err := h.Redis.Del(ctx, tradeAlertSeenKey(userScope, orderID)).Err(); err != nil {
		logger.Error("TradeHandler: Failed to release dedupe key for order %s: %v", orderID, err)
	}
}

func shouldNotifyForStatus(raw string) bool {
	status := strings.ToLower(strings.TrimSpace(raw))
	if status == "" {
		return true
	}
	if strings.Contains(status, "cancel") ||
		strings.Contains(status, "fail") ||
		strings.Contains(status, "reject") ||
		strings.Contains(status, "expire") {
		return false
	}
	return true
}

func buildTradeAlertData(order SyncedOrder, traderAddress string, marketMap map[string]models.Market) services.TradeAlertData {
	side := strings.ToUpper(strings.TrimSpace(order.Side))
	if side == "" {
		side = "BUY"
	}
	outcome := strings.TrimSpace(order.Outcome)
	if outcome == "" {
		outcome = "Outcome"
	}

	marketTitle := strings.TrimSpace(order.MarketID)
	marketSlug := strings.TrimSpace(order.MarketID)
	if market, ok := marketMap[strings.TrimSpace(order.MarketID)]; ok {
		if strings.TrimSpace(market.Title) != "" {
			marketTitle = strings.TrimSpace(market.Title)
		}
		if strings.TrimSpace(market.Slug) != "" {
			marketSlug = strings.TrimSpace(market.Slug)
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
		OrderID:       strings.TrimSpace(order.OrderID),
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
