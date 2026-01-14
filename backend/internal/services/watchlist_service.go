/**
 * @description
 * Watchlist Service for market bookmark operations.
 * Manages user's starred markets in the database and hydrates metadata via Redis-backed market cache.
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
	db           *gorm.DB
	marketService *MarketService
}

// NewWatchlistService creates a new WatchlistService
func NewWatchlistService(db *gorm.DB, marketService *MarketService) *WatchlistService {
	return &WatchlistService{
		db:            db,
		marketService: marketService,
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

	marketIDs := make([]string, 0, len(bookmarks))
	for _, b := range bookmarks {
		if b.MarketID != "" {
			marketIDs = append(marketIDs, b.MarketID)
		}
	}

	var marketMap map[string]*models.Market
	if s.marketService != nil {
		marketMap, _ = s.marketService.GetMarketsByConditionIDsWithGammaFallback(ctx, marketIDs)
	}
	if marketMap == nil {
		marketMap = make(map[string]*models.Market)
	}

	// Build watchlist response with cached market metadata (Redis-backed)
	items := make([]models.WatchlistItem, 0, len(bookmarks))
	for _, b := range bookmarks {
		market := marketMap[b.MarketID]

		var yesPrice, noPrice float64
		title := "Market unavailable"
		slug := ""
		imageURL := ""
		volume24h := 0.0
		oneDayChange := 0.0

		if market != nil {
			title = market.Title
			slug = market.Slug
			imageURL = market.ImageURL
			volume24h = market.Volume24h
			oneDayChange = market.OneDayPriceChange

			if price, ok := calculateDisplayPrice(market.YesBestBid, market.YesBestAsk, market.YesPrice); ok {
				yesPrice = price
			}
			if price, ok := calculateDisplayPrice(market.NoBestBid, market.NoBestAsk, market.NoPrice); ok {
				noPrice = price
			}
			if yesPrice > 0 && noPrice == 0 {
				noPrice = 1 - yesPrice
			} else if noPrice > 0 && yesPrice == 0 {
				yesPrice = 1 - noPrice
			}
		}

		items = append(items, models.WatchlistItem{
			MarketBookmark: b,
			Title:          title,
			Slug:           slug,
			ImageURL:       imageURL,
			YesPrice:       yesPrice,
			NoPrice:        noPrice,
			Volume24h:      volume24h,
			OneDayChange:   oneDayChange,
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
