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
	"strconv"
	"strings"

	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/models"
	"github.com/bankai-project/backend/internal/polymarket/clob"
	"github.com/google/uuid"
	"gorm.io/gorm"
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

// RelayTrade relays a signed order from the user to the Polymarket CLOB
// and persists the order record to the database.
func (s *TradeService) RelayTrade(ctx context.Context, user *models.User, req *clob.PostOrderRequest) (*clob.PostOrderResponse, error) {
	if req == nil {
		return nil, errors.New("post order request is required")
	}
	if user == nil {
		return nil, errors.New("user context is required")
	}
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid order payload: %w", err)
	}

	if strings.TrimSpace(user.VaultAddress) == "" {
		return nil, fmt.Errorf("user has no deployed vault on file")
	}

	if !strings.EqualFold(strings.TrimSpace(req.Order.Maker), strings.TrimSpace(user.VaultAddress)) {
		return nil, fmt.Errorf("order maker %s does not match vault %s", req.Order.Maker, user.VaultAddress)
	}

	// 1. Post to CLOB
	resp, err := s.Clob.PostOrder(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to relay trade: %w", err)
	}

	if err := s.persistOrder(ctx, user, req, resp); err != nil {
		logger.Error("Failed to persist order for user %s: %v", user.ID, err)
	}

	if !resp.Success {
		return resp, fmt.Errorf("clob rejected order: %s", resp.ErrorMsg)
	}
	return resp, nil
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

// RelayBatchTrades iterates through the provided PostOrderRequests and relays each one sequentially.
// Using single-order submission ensures we receive deterministic order IDs for persistence.
func (s *TradeService) RelayBatchTrades(ctx context.Context, user *models.User, requests []*clob.PostOrderRequest) ([]*clob.PostOrderResponse, error) {
	if user == nil {
		return nil, errors.New("user context is required")
	}
	if len(requests) == 0 {
		return nil, errors.New("at least one order is required")
	}

	responses := make([]*clob.PostOrderResponse, 0, len(requests))
	for idx, req := range requests {
		if err := req.Validate(); err != nil {
			return nil, fmt.Errorf("order %d invalid: %w", idx, err)
		}
		resp, err := s.RelayTrade(ctx, user, req)
		if err != nil {
			return nil, fmt.Errorf("batch order %d failed: %w", idx, err)
		}
		responses = append(responses, resp)
	}

	return responses, nil
}

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

	resp, err := s.Clob.CancelOrder(ctx, &clob.CancelOrderRequest{OrderID: orderID})
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

	resp, err := s.Clob.CancelOrders(ctx, &clob.CancelOrdersRequest{OrderIDs: orderIDs})
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
