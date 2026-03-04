/**
 * @description
 * Service for backend-assisted trade actions.
 *
 * Order creation and order lifecycle reads are handled directly in the frontend
 * through the official Polymarket SDK.
 */

package services

import (
	"context"
	"errors"
	"strings"

	"github.com/bankai-project/backend/internal/models"
	"github.com/bankai-project/backend/internal/polymarket/clob"
	"gorm.io/gorm"
)

var ErrBackendCancellationDisabled = errors.New("backend order cancellation is disabled; cancel with user-scoped CLOB credentials")

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

// CancelOrder cancels an existing order on the CLOB.
func (s *TradeService) CancelOrder(ctx context.Context, user *models.User, orderID string) (*clob.CancelResponse, error) {
	if user == nil {
		return nil, errors.New("user context is required")
	}
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return nil, errors.New("orderID is required")
	}
	return nil, ErrBackendCancellationDisabled
}

// CancelOrders cancels multiple orders on the CLOB.
func (s *TradeService) CancelOrders(ctx context.Context, user *models.User, orderIDs []string) (*clob.CancelResponse, error) {
	if user == nil {
		return nil, errors.New("user context is required")
	}
	if len(orderIDs) == 0 {
		return nil, errors.New("at least one orderID is required")
	}
	return nil, ErrBackendCancellationDisabled
}
