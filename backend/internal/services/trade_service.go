/**
 * @description
 * Service for managing trade execution.
 * Bridges the API layer with the CLOB client.
 *
 * @dependencies
 * - backend/internal/polymarket/clob
 */

package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bankai-project/backend/internal/polymarket/clob"
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

// RelayTrade relays a signed order from the user to the Polymarket CLOB.
// It attaches Builder Attribution headers via the CLOB client.
func (s *TradeService) RelayTrade(ctx context.Context, req *clob.PostOrderRequest) (*clob.PostOrderResponse, error) {
	if req == nil {
		return nil, errors.New("post order request is required")
	}
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid order payload: %w", err)
	}

	resp, err := s.Clob.PostOrder(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to relay trade: %w", err)
	}

	if !resp.Success {
		return resp, fmt.Errorf("clob rejected order: %s", resp.ErrorMsg)
	}

	// In Step 12, we will add logic here to save the OrderID to our DB

	return resp, nil
}

// RelayBatchTrades relays multiple orders in a single request.
func (s *TradeService) RelayBatchTrades(ctx context.Context, req clob.PostOrdersRequest) (*clob.PostOrderResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid batch payload: %w", err)
	}

	resp, err := s.Clob.PostOrders(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to relay batch trades: %w", err)
	}

	if !resp.Success {
		return resp, fmt.Errorf("clob rejected batch: %s", resp.ErrorMsg)
	}

	return resp, nil
}

// CancelOrder cancels an existing order on the CLOB
func (s *TradeService) CancelOrder(ctx context.Context, orderID string) (*clob.CancelResponse, error) {
	if strings.TrimSpace(orderID) == "" {
		return nil, errors.New("orderID is required")
	}
	return s.Clob.CancelOrder(ctx, &clob.CancelOrderRequest{OrderID: orderID})
}
