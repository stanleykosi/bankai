/**
 * @description
 * Notification Service for trade alerts.
 * Creates and manages notifications for followed traders' trades.
 *
 * @dependencies
 * - gorm.io/gorm
 * - backend/internal/models
 */

package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// NotificationService handles notification operations
type NotificationService struct {
	db            *gorm.DB
	socialService *SocialService
}

// NewNotificationService creates a new NotificationService
func NewNotificationService(db *gorm.DB, socialService *SocialService) *NotificationService {
	return &NotificationService{
		db:            db,
		socialService: socialService,
	}
}

// TradeAlertData contains data for a trade alert notification
type TradeAlertData struct {
	TraderAddress string  `json:"trader_address"`
	TraderName    string  `json:"trader_name,omitempty"`
	MarketSlug    string  `json:"market_slug"`
	MarketTitle   string  `json:"market_title"`
	Side          string  `json:"side"` // BUY or SELL
	Outcome       string  `json:"outcome"`
	Price         float64 `json:"price"`
	Size          float64 `json:"size"`
	Value         float64 `json:"value"`
	Timestamp     string  `json:"timestamp"`
}

// CreateTradeAlert creates trade alert notifications for all followers of a trader
func (s *NotificationService) CreateTradeAlert(ctx context.Context, data TradeAlertData) error {
	// Get all users following this trader
	followerIDs, err := s.socialService.GetFollowerUserIDs(ctx, data.TraderAddress)
	if err != nil {
		logger.Error("NotificationService: Failed to get followers: %v", err)
		return err
	}

	if len(followerIDs) == 0 {
		return nil // No followers, nothing to do
	}

	// Marshal the data to JSON
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return err
	}

	// Create notification for each follower
	traderName := data.TraderName
	if traderName == "" {
		traderName = truncateAddress(data.TraderAddress)
	}

	title := fmt.Sprintf("%s %s %s", traderName, data.Side, data.Outcome)
	message := fmt.Sprintf("%s placed a %s order for %.2f shares at $%.2f on %s",
		traderName, data.Side, data.Size, data.Price, data.MarketTitle)

	notifications := make([]models.Notification, len(followerIDs))
	now := time.Now()
	for i, userID := range followerIDs {
		notifications[i] = models.Notification{
			ID:        uuid.New(),
			UserID:    userID,
			Type:      models.NotificationTypeTradeAlert,
			Title:     title,
			Message:   message,
			Data:      string(dataJSON),
			Read:      false,
			CreatedAt: now,
		}
	}

	// Batch insert notifications
	result := s.db.WithContext(ctx).Create(&notifications)
	if result.Error != nil {
		logger.Error("NotificationService: Failed to create notifications: %v", result.Error)
		return result.Error
	}

	logger.Info("NotificationService: Created %d trade alert notifications for trader %s",
		len(notifications), data.TraderAddress)

	return nil
}

// GetNotifications returns notifications for a user
func (s *NotificationService) GetNotifications(ctx context.Context, userID uuid.UUID, limit, offset int) ([]models.Notification, error) {
	if limit <= 0 {
		limit = 50
	}

	var notifications []models.Notification

	result := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&notifications)

	if result.Error != nil {
		return nil, result.Error
	}

	return notifications, nil
}

// GetUnreadNotifications returns unread notifications for a user
func (s *NotificationService) GetUnreadNotifications(ctx context.Context, userID uuid.UUID) ([]models.Notification, error) {
	var notifications []models.Notification

	result := s.db.WithContext(ctx).
		Where("user_id = ? AND read = ?", userID, false).
		Order("created_at DESC").
		Limit(50).
		Find(&notifications)

	if result.Error != nil {
		return nil, result.Error
	}

	return notifications, nil
}

// GetUnreadCount returns the count of unread notifications
func (s *NotificationService) GetUnreadCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	var count int64
	result := s.db.WithContext(ctx).
		Model(&models.Notification{}).
		Where("user_id = ? AND read = ?", userID, false).
		Count(&count)

	if result.Error != nil {
		return 0, result.Error
	}

	return count, nil
}

// MarkAsRead marks a specific notification as read
func (s *NotificationService) MarkAsRead(ctx context.Context, userID uuid.UUID, notificationID uuid.UUID) error {
	result := s.db.WithContext(ctx).
		Model(&models.Notification{}).
		Where("id = ? AND user_id = ?", notificationID, userID).
		Update("read", true)

	if result.Error != nil {
		return result.Error
	}

	return nil
}

// MarkAllAsRead marks all notifications as read for a user
func (s *NotificationService) MarkAllAsRead(ctx context.Context, userID uuid.UUID) error {
	result := s.db.WithContext(ctx).
		Model(&models.Notification{}).
		Where("user_id = ? AND read = ?", userID, false).
		Update("read", true)

	if result.Error != nil {
		return result.Error
	}

	return nil
}

// DeleteNotification deletes a specific notification
func (s *NotificationService) DeleteNotification(ctx context.Context, userID uuid.UUID, notificationID uuid.UUID) error {
	result := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", notificationID, userID).
		Delete(&models.Notification{})

	if result.Error != nil {
		return result.Error
	}

	return nil
}

// DeleteOldNotifications deletes notifications older than the specified duration
func (s *NotificationService) DeleteOldNotifications(ctx context.Context, olderThan time.Duration) error {
	cutoff := time.Now().Add(-olderThan)

	result := s.db.WithContext(ctx).
		Where("created_at < ?", cutoff).
		Delete(&models.Notification{})

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected > 0 {
		logger.Info("NotificationService: Deleted %d old notifications", result.RowsAffected)
	}

	return nil
}

// Helper to truncate address for display
func truncateAddress(address string) string {
	if len(address) <= 10 {
		return address
	}
	return address[:6] + "..." + address[len(address)-4:]
}
