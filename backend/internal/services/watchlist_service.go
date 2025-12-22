/**
 * @description
 * Watchlist Service for market bookmark operations.
 * Manages user's starred markets in the database.
 *
 * @dependencies
 * - gorm.io/gorm
 * - backend/internal/models
 */

package services

import (
	"context"
	"time"

	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// WatchlistService handles market bookmark operations
type WatchlistService struct {
	db *gorm.DB
}

// NewWatchlistService creates a new WatchlistService
func NewWatchlistService(db *gorm.DB) *WatchlistService {
	return &WatchlistService{
		db: db,
	}
}

// BookmarkMarket adds a market to user's watchlist
func (s *WatchlistService) BookmarkMarket(ctx context.Context, userID uuid.UUID, marketID string) error {
	if marketID == "" {
		return nil
	}

	bookmark := &models.MarketBookmark{
		UserID:    userID,
		MarketID:  marketID,
		CreatedAt: time.Now(),
	}

	// Use FirstOrCreate to avoid duplicates
	result := s.db.WithContext(ctx).
		Where("user_id = ? AND market_id = ?", userID, marketID).
		FirstOrCreate(bookmark)

	if result.Error != nil {
		logger.Error("WatchlistService: Failed to bookmark market: %v", result.Error)
		return result.Error
	}

	return nil
}

// RemoveBookmark removes a market from user's watchlist
func (s *WatchlistService) RemoveBookmark(ctx context.Context, userID uuid.UUID, marketID string) error {
	result := s.db.WithContext(ctx).
		Where("user_id = ? AND market_id = ?", userID, marketID).
		Delete(&models.MarketBookmark{})

	if result.Error != nil {
		logger.Error("WatchlistService: Failed to remove bookmark: %v", result.Error)
		return result.Error
	}

	return nil
}

// GetWatchlist returns user's bookmarked markets with live price data
func (s *WatchlistService) GetWatchlist(ctx context.Context, userID uuid.UUID) ([]models.WatchlistItem, error) {
	var bookmarks []models.MarketBookmark

	result := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&bookmarks)

	if result.Error != nil {
		return nil, result.Error
	}

	// Get market details for each bookmark
	items := make([]models.WatchlistItem, 0, len(bookmarks))
	for _, b := range bookmarks {
		var market models.Market
		if err := s.db.WithContext(ctx).
			Where("condition_id = ?", b.MarketID).
			First(&market).Error; err != nil {
			// Skip markets that no longer exist
			continue
		}

		// Parse outcome prices for YES/NO prices
		var yesPrice, noPrice float64
		if market.OutcomePrices != "" {
			// OutcomePrices is typically stored as JSON array like "[0.65, 0.35]"
			// For simplicity, we'll use the best bid/ask if available
			yesPrice = market.BestBid
			noPrice = 1 - market.BestBid
		}

		items = append(items, models.WatchlistItem{
			MarketBookmark: b,
			Title:          market.Title,
			ImageURL:       market.ImageURL,
			YesPrice:       yesPrice,
			NoPrice:        noPrice,
			Volume24h:      market.Volume24h,
			OneDayChange:   market.OneDayPriceChange,
		})
	}

	return items, nil
}

// GetWatchlistMarketIDs returns just the market IDs in user's watchlist
func (s *WatchlistService) GetWatchlistMarketIDs(ctx context.Context, userID uuid.UUID) ([]string, error) {
	var marketIDs []string

	result := s.db.WithContext(ctx).
		Model(&models.MarketBookmark{}).
		Where("user_id = ?", userID).
		Pluck("market_id", &marketIDs)

	if result.Error != nil {
		return nil, result.Error
	}

	return marketIDs, nil
}

// IsBookmarked checks if user has bookmarked a specific market
func (s *WatchlistService) IsBookmarked(ctx context.Context, userID uuid.UUID, marketID string) (bool, error) {
	var count int64
	result := s.db.WithContext(ctx).
		Model(&models.MarketBookmark{}).
		Where("user_id = ? AND market_id = ?", userID, marketID).
		Count(&count)

	if result.Error != nil {
		return false, result.Error
	}

	return count > 0, nil
}

// GetBookmarkCount returns the number of markets user has bookmarked
func (s *WatchlistService) GetBookmarkCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	var count int64
	result := s.db.WithContext(ctx).
		Model(&models.MarketBookmark{}).
		Where("user_id = ?", userID).
		Count(&count)

	if result.Error != nil {
		return 0, result.Error
	}

	return count, nil
}

// ToggleBookmark toggles bookmark status and returns the new state
func (s *WatchlistService) ToggleBookmark(ctx context.Context, userID uuid.UUID, marketID string) (bool, error) {
	isBookmarked, err := s.IsBookmarked(ctx, userID, marketID)
	if err != nil {
		return false, err
	}

	if isBookmarked {
		err = s.RemoveBookmark(ctx, userID, marketID)
		return false, err
	}

	err = s.BookmarkMarket(ctx, userID, marketID)
	return true, err
}
