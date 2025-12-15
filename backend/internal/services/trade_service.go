/**
 * @description
 * Service for managing trade execution.
 * Bridges the API layer with the CLOB client and Database persistence.
 *
 * @dependencies
 * - backend/internal/polymarket/clob
 * - backend/internal/models
 * - gorm.io/gorm
 */

package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/models"
	"github.com/bankai-project/backend/internal/polymarket/clob"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TradeService struct {
	DB   *gorm.DB
	Clob *clob.Client
}

func NewTradeService(db *gorm.DB, clobClient *clob.Client) *TradeService {
	return &TradeService{
		DB:   db,
		Clob: clobClient,
	}
}

// RelayTrade function has been removed.
// The frontend now uses the official Polymarket SDK directly for order creation, signing, and submission.
// This eliminates the need for backend order relaying.

// SyncOrdersFromSDK upserts orders fetched via the JS SDK into Postgres for history/audit.
func (s *TradeService) SyncOrdersFromSDK(ctx context.Context, user *models.User, synced []SyncedOrder) error {
	if user == nil {
		return errors.New("user context is required")
	}
	if len(synced) == 0 {
		return nil
	}

	orders := make([]models.Order, 0, len(synced))
	for _, src := range synced {
		status := mapSDKStatus(src.Status)
		if status == "" {
			status = models.OrderStatusPending
		}
		side := models.OrderSideBuy
		if strings.ToUpper(src.Side) == "SELL" {
			side = models.OrderSideSell
		}
		source := src.Source
		if source == "" {
			source = models.OrderSourceUnknown
		}

		o := models.Order{
			UserID:         user.ID,
			CLOBOrderID:    src.OrderID,
			MarketID:       src.MarketID,
			Side:           side,
			Outcome:        src.Outcome,
			OutcomeTokenID: src.OutcomeTokenID,
			Price:          src.Price,
			Size:           src.Size,
			OrderType:      strings.ToUpper(strings.TrimSpace(src.OrderType)),
			Status:         status,
			StatusDetail:   src.StatusDetail,
			OrderHashes:    models.StringArray(src.OrderHashes),
			Source:         source,
		}
		// Preserve client timestamps if provided
		if !src.CreatedAt.IsZero() {
			o.CreatedAt = src.CreatedAt
		}
		if !src.UpdatedAt.IsZero() {
			o.UpdatedAt = src.UpdatedAt
		}
		orders = append(orders, o)
	}

	// Upsert on (user_id, clob_order_id)
	return s.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "clob_order_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"status", "status_detail", "price", "size", "order_type", "outcome", "outcome_token_id", "order_hashes", "market_id", "updated_at", "source"}),
	}).Create(&orders).Error
}

// SyncOrdersByAddress upserts orders and associates them to users by makerAddress (vault or EOA).
func (s *TradeService) SyncOrdersByAddress(ctx context.Context, synced []SyncedOrder) error {
	if len(synced) == 0 {
		return nil
	}

	for _, src := range synced {
		addr := strings.ToLower(strings.TrimSpace(src.MakerAddress))
		if addr == "" {
			continue
		}

		var user models.User
		if err := s.DB.WithContext(ctx).
			Where("LOWER(vault_address) = ? OR LOWER(eoa_address) = ?", addr, addr).
			First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return err
		}

		if err := s.SyncOrdersFromSDK(ctx, &user, []SyncedOrder{src}); err != nil {
			return err
		}
	}

	return nil
}

func mapSDKStatus(raw string) models.OrderStatus {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "open", "live", "unmatched":
		return models.OrderStatusOpen
	case "pending", "delayed":
		return models.OrderStatusPending
	case "matched", "filled":
		return models.OrderStatusFilled
	case "canceled", "cancelled":
		return models.OrderStatusCanceled
	case "failed", "error":
		return models.OrderStatusFailed
	default:
		return ""
	}
}

// SyncedOrder represents the payload used to persist SDK-fetched orders.
type SyncedOrder struct {
	OrderID        string             `json:"orderId"`
	MarketID       string             `json:"marketId"`
	Outcome        string             `json:"outcome"`
	OutcomeTokenID string             `json:"outcomeTokenId"`
	MakerAddress   string             `json:"makerAddress"`
	Side           string             `json:"side"`
	Price          float64            `json:"price"`
	Size           float64            `json:"size"`
	OrderType      string             `json:"orderType"`
	Status         string             `json:"status"`
	StatusDetail   string             `json:"statusDetail"`
	OrderHashes    []string           `json:"orderHashes"`
	Source         models.OrderSource `json:"source"`
	CreatedAt      time.Time          `json:"createdAt"`
	UpdatedAt      time.Time          `json:"updatedAt"`
}

func (s *TradeService) persistOrder(ctx context.Context, user *models.User, req *clob.PostOrderRequest, resp *clob.PostOrderResponse) error {
	if resp == nil {
		return errors.New("clob response cannot be nil")
	}

	orderID := resp.OrderID
	if orderID == "" && len(resp.OrderIDs) == 1 {
		orderID = resp.OrderIDs[0]
	}

	// Parse numeric fields
	makerAmt, err := strconv.ParseFloat(req.Order.MakerAmount, 64)
	if err != nil {
		return fmt.Errorf("invalid makerAmount: %w", err)
	}
	takerAmt, err := strconv.ParseFloat(req.Order.TakerAmount, 64)
	if err != nil {
		return fmt.Errorf("invalid takerAmount: %w", err)
	}

	// Try to find market by token ID to determine outcome label
	var market models.Market
	var marketFound bool
	err = s.DB.WithContext(ctx).
		Where("token_id_yes = ? OR token_id_no = ?", req.Order.TokenID, req.Order.TokenID).
		First(&market).Error
	if err == nil {
		marketFound = true
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		logger.Error("Failed to lookup market for token %s: %v", req.Order.TokenID, err)
	}

	outcomeLabel := deriveOutcomeLabel(func() *models.Market {
		if marketFound {
			return &market
		}
		return nil
	}(), req.Order.TokenID, req.Order.Side)

	// Calculate price and size from atomic units
	var dbPrice, dbSize float64
	if req.Order.Side == clob.BUY {
		if takerAmt > 0 {
			dbPrice = makerAmt / takerAmt
		}
		dbSize = takerAmt / 1e6
	} else {
		if makerAmt > 0 {
			dbPrice = takerAmt / makerAmt
		}
		dbSize = makerAmt / 1e6
	}

	order := models.Order{
		UserID:         user.ID,
		CLOBOrderID:    orderID,
		Side:           models.OrderSide(req.Order.Side),
		Outcome:        outcomeLabel,
		OutcomeTokenID: req.Order.TokenID,
		Price:          dbPrice,
		Size:           dbSize,
		OrderType:      string(req.OrderType),
		Status:         mapClobStatus(resp, req.OrderType),
		StatusDetail:   strings.ToLower(resp.Status),
		OrderHashes:    models.StringArray(resp.OrderHashes),
		ErrorMessage:   resp.ErrorMsg,
	}

	if marketFound {
		order.MarketID = market.ConditionID
	}

	return s.DB.WithContext(ctx).Create(&order).Error
}

// validateOrderAmounts checks price/size alignment to market tick & min-size rules to catch payload issues pre-flight.
func (s *TradeService) validateOrderAmounts(ctx context.Context, order *clob.Order) error {
	if order == nil {
		return errors.New("order is nil")
	}

	// Fetch minimal market metadata for tick/min size and neg-risk context.
	var market models.Market
	var hasMarket bool
	if err := s.DB.WithContext(ctx).
		Where("token_id_yes = ? OR token_id_no = ?", order.TokenID, order.TokenID).
		First(&market).Error; err == nil {
		hasMarket = true
	}

	// Parse amounts (all are integer strings representing 1e6 base units).
	makerAmt, err := strconv.ParseFloat(order.MakerAmount, 64)
	if err != nil {
		return fmt.Errorf("makerAmount parse: %w", err)
	}
	takerAmt, err := strconv.ParseFloat(order.TakerAmount, 64)
	if err != nil {
		return fmt.Errorf("takerAmount parse: %w", err)
	}
	if makerAmt <= 0 || takerAmt <= 0 {
		return fmt.Errorf("maker/taker amounts must be > 0")
	}

	// Compute price from amounts (shares priced in USDC, 1e6 decimals).
	var price float64
	switch order.Side {
	case clob.BUY:
		price = makerAmt / takerAmt
	case clob.SELL:
		price = takerAmt / makerAmt
	default:
		return fmt.Errorf("unsupported side %s", order.Side)
	}

	// Default tick sizes and min-size if we lack metadata.
	tickSize := 0.01
	minSize := 1.0
	if hasMarket {
		if market.OrderPriceMinTickSize > 0 {
			tickSize = market.OrderPriceMinTickSize
		}
		if market.OrderMinSize > 0 {
			minSize = market.OrderMinSize
		}
	}

	// Validate min size (shares are taker for BUY, maker for SELL, measured in full units not atomic).
	shares := takerAmt / 1e6
	if order.Side == clob.SELL {
		shares = makerAmt / 1e6
	}
	if shares < minSize {
		return fmt.Errorf("size %.4f below min size %.4f", shares, minSize)
	}

	// Validate price range and tick.
	if price <= 0 || price >= 1 {
		return fmt.Errorf("price %.6f out of bounds (0,1)", price)
	}
	if !isOnTick(price, tickSize) {
		return fmt.Errorf("price %.6f not aligned to tick %.4f", price, tickSize)
	}

	return nil
}

// isOnTick returns true if value is within a tiny epsilon of tick grid.
func isOnTick(val, tick float64) bool {
	if tick <= 0 {
		return true
	}
	steps := val / tick
	nearest := math.Round(steps)
	return math.Abs(steps-nearest) < 1e-6
}

// recoverOrderSigner and related helper functions have been removed.
// The frontend now uses the official Polymarket SDK which handles all signing internally.

func mapClobStatus(resp *clob.PostOrderResponse, orderType clob.OrderType) models.OrderStatus {
	if resp == nil {
		return models.OrderStatusFailed
	}
	if !resp.Success {
		return models.OrderStatusFailed
	}

	switch strings.ToLower(resp.Status) {
	case "matched":
		return models.OrderStatusFilled
	case "live":
		return models.OrderStatusOpen
	case "delayed":
		return models.OrderStatusPending
	case "unmatched":
		return models.OrderStatusOpen
	}

	// FOK orders that succeed without status default to filled, FAK to open
	if orderType == clob.OrderTypeFOK {
		return models.OrderStatusFilled
	}
	return models.OrderStatusOpen
}

type outcomeMeta struct {
	Index   int
	Label   string
	TokenID string
}

func deriveOutcomeLabel(market *models.Market, tokenID string, side clob.OrderSide) string {
	rawTokenID := strings.TrimSpace(tokenID)
	tokenID = strings.ToLower(rawTokenID)

	var metas []outcomeMeta
	if market != nil {
		metas = parseMarketOutcomeMetadata(market.Outcomes)

		// Direct tokenID match from metadata
		for _, meta := range metas {
			if meta.TokenID != "" && tokenID != "" && strings.EqualFold(meta.TokenID, tokenID) {
				if lbl := meta.LabelOrFallback(); lbl != "" {
					return lbl
				}
			}
		}

		if tokenID != "" {
			if market.TokenIDYes != "" && strings.EqualFold(strings.ToLower(market.TokenIDYes), tokenID) {
				if lbl := labelForOutcomeIndex(metas, 0); lbl != "" {
					return lbl
				}
				return "YES"
			}
			if market.TokenIDNo != "" && strings.EqualFold(strings.ToLower(market.TokenIDNo), tokenID) {
				if lbl := labelForOutcomeIndex(metas, 1); lbl != "" {
					return lbl
				}
				return "NO"
			}
		}
	}

	if rawTokenID != "" {
		return rawTokenID
	}
	if side != "" {
		return strings.ToUpper(string(side))
	}
	return "UNKNOWN"
}

func parseMarketOutcomeMetadata(raw string) []outcomeMeta {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var entries []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil
	}

	metas := make([]outcomeMeta, 0, len(entries))
	for idx, entry := range entries {
		var str string
		if err := json.Unmarshal(entry, &str); err == nil {
			if trimmed := strings.TrimSpace(str); trimmed != "" {
				metas = append(metas, outcomeMeta{
					Index: idx,
					Label: trimmed,
				})
				continue
			}
		}

		var obj map[string]interface{}
		if err := json.Unmarshal(entry, &obj); err == nil {
			meta := outcomeMeta{
				Index: idx,
				TokenID: firstNonEmptyString(
					interfaceToString(obj["tokenId"]),
					interfaceToString(obj["tokenID"]),
					interfaceToString(obj["token_id"]),
					interfaceToString(obj["id"]),
				),
				Label: firstNonEmptyString(
					interfaceToString(obj["label"]),
					interfaceToString(obj["name"]),
					interfaceToString(obj["shortName"]),
					interfaceToString(obj["title"]),
					interfaceToString(obj["outcome"]),
					interfaceToString(obj["value"]),
				),
			}
			if meta.Label == "" && meta.TokenID != "" {
				meta.Label = meta.TokenID
			}
			if meta.Label != "" || meta.TokenID != "" {
				metas = append(metas, meta)
			}
		}
	}

	return metas
}

func (o outcomeMeta) LabelOrFallback() string {
	if o.Label != "" {
		return o.Label
	}
	return o.TokenID
}

func labelForOutcomeIndex(metas []outcomeMeta, idx int) string {
	for _, meta := range metas {
		if meta.Index == idx && meta.Label != "" {
			return meta.Label
		}
	}
	return ""
}

func interfaceToString(val interface{}) string {
	switch v := val.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(v, 'f', -1, 64))
	case int:
		return strings.TrimSpace(strconv.Itoa(int(v)))
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// RelayBatchTrades function has been removed.
// The frontend now uses the official Polymarket SDK directly for batch order submission.

// CancelOrder cancels an existing order on the CLOB and updates persistence.
func (s *TradeService) CancelOrder(ctx context.Context, user *models.User, orderID string) (*clob.CancelResponse, error) {
	if user == nil {
		return nil, errors.New("user context is required")
	}
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return nil, errors.New("orderID is required")
	}

	var order models.Order
	if err := s.DB.WithContext(ctx).
		Where("user_id = ? AND clob_order_id = ?", user.ID, orderID).
		First(&order).Error; err != nil {
		return nil, fmt.Errorf("order not found or not owned by user: %w", err)
	}

	resp, err := s.Clob.CancelOrder(ctx, &clob.CancelOrderRequest{OrderID: orderID}, nil)
	if err != nil {
		return nil, err
	}

	if len(resp.Canceled) > 0 {
		if err := s.DB.WithContext(ctx).Model(&order).Updates(map[string]interface{}{
			"status":        models.OrderStatusCanceled,
			"status_detail": "canceled",
		}).Error; err != nil {
			logger.Error("Failed to update order %s cancellation status: %v", orderID, err)
		}
	}

	return resp, nil
}

// CancelOrders cancels multiple orders for the user and updates their status.
func (s *TradeService) CancelOrders(ctx context.Context, user *models.User, orderIDs []string) (*clob.CancelResponse, error) {
	if user == nil {
		return nil, errors.New("user context is required")
	}
	if len(orderIDs) == 0 {
		return nil, errors.New("at least one orderID is required")
	}

	// Ensure all orders belong to the user
	var count int64
	if err := s.DB.WithContext(ctx).Model(&models.Order{}).
		Where("user_id = ? AND clob_order_id IN ?", user.ID, orderIDs).
		Count(&count).Error; err != nil {
		return nil, fmt.Errorf("failed to verify order ownership: %w", err)
	}
	if count != int64(len(orderIDs)) {
		return nil, fmt.Errorf("one or more orders do not belong to the user")
	}

	resp, err := s.Clob.CancelOrders(ctx, &clob.CancelOrdersRequest{OrderIDs: orderIDs}, nil)
	if err != nil {
		return nil, err
	}

	if len(resp.Canceled) > 0 {
		if err := s.DB.WithContext(ctx).Model(&models.Order{}).
			Where("user_id = ? AND clob_order_id IN ?", user.ID, resp.Canceled).
			Updates(map[string]interface{}{
				"status":        models.OrderStatusCanceled,
				"status_detail": "canceled",
			}).Error; err != nil {
			logger.Error("Failed to update canceled orders for user %s: %v", user.ID, err)
		}
	}

	return resp, nil
}

// ListOrders returns paginated order history for a user.
func (s *TradeService) ListOrders(ctx context.Context, userID uuid.UUID, limit, offset int) ([]models.Order, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}

	var total int64
	query := s.DB.WithContext(ctx).Model(&models.Order{}).
		Where("user_id = ?", userID)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count orders: %w", err)
	}

	var orders []models.Order
	if err := query.Order("created_at DESC").
		Limit(limit).Offset(offset).
		Find(&orders).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to fetch orders: %w", err)
	}

	return orders, total, nil
}
