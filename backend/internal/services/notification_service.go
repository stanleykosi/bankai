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
	"strings"
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
	settings      *SettingsService
}

// NewNotificationService creates a new NotificationService
func NewNotificationService(db *gorm.DB, socialService *SocialService, settings *SettingsService) *NotificationService {
	return &NotificationService{
		db:            db,
		socialService: socialService,
		settings:      settings,
	}
}

// TradeAlertData contains data for a trade alert notification
type TradeAlertData struct {
	OrderID       string  `json:"order_id,omitempty"`
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
	if s == nil || s.db == nil || s.socialService == nil {
		return fmt.Errorf("notification service unavailable")
	}
	data.TraderAddress = strings.ToLower(strings.TrimSpace(data.TraderAddress))
	data.OrderID = strings.TrimSpace(data.OrderID)

	// Get all users following this trader
	followerIDs, err := s.socialService.GetFollowerUserIDs(ctx, data.TraderAddress)
	if err != nil {
		logger.Error("NotificationService: Failed to get followers: %v", err)
		return err
	}

	if len(followerIDs) == 0 {
		return nil // No followers, nothing to do
	}

	existingRecipients, err := s.listExistingTradeAlertRecipients(ctx, followerIDs, data)
	if err != nil {
		logger.Error("NotificationService: Failed to dedupe trade alerts: %v", err)
		return err
	}

	settingsMap := map[uuid.UUID]models.UserSettings{}
	if s.settings != nil {
		m, err := s.settings.GetSettingsForUsers(ctx, followerIDs)
		if err != nil {
			logger.Error("NotificationService: Failed to load settings: %v", err)
			return err
		}
		settingsMap = m
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

	notifications := make([]models.Notification, 0, len(followerIDs))
	now := time.Now()
	for _, userID := range followerIDs {
		if _, alreadyNotified := existingRecipients[userID]; alreadyNotified {
			continue
		}
		if s.settings != nil {
			userSettings, ok := settingsMap[userID]
			if ok {
				if strings.EqualFold(userSettings.NotificationChannel, NotificationChannelNone) {
					continue
				}
				switch strings.ToUpper(strings.TrimSpace(userSettings.FollowedTraderAlerts)) {
				case FollowedTraderAlertsNone:
					continue
				case FollowedTraderAlertsLargeOnly:
					if data.Value < userSettings.WhaleAlertThresholdUSD {
						continue
					}
				}
			}
		}

		notifications = append(notifications, models.Notification{
			ID:        uuid.New(),
			UserID:    userID,
			Type:      models.NotificationTypeTradeAlert,
			Title:     title,
			Message:   message,
			Data:      string(dataJSON),
			Read:      false,
			CreatedAt: now,
		})
	}

	if len(notifications) == 0 {
		return nil
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

func (s *NotificationService) listExistingTradeAlertRecipients(
	ctx context.Context,
	followerIDs []uuid.UUID,
	data TradeAlertData,
) (map[uuid.UUID]struct{}, error) {
	out := make(map[uuid.UUID]struct{})
	if len(followerIDs) == 0 || strings.TrimSpace(data.OrderID) == "" || strings.TrimSpace(data.TraderAddress) == "" {
		return out, nil
	}

	query := s.db.WithContext(ctx).
		Model(&models.Notification{}).
		Select("user_id").
		Where("user_id IN ? AND type = ?", followerIDs, models.NotificationTypeTradeAlert)

	switch s.db.Dialector.Name() {
	case "postgres":
		query = query.
			Where("data::jsonb ->> 'order_id' = ?", data.OrderID).
			Where("data::jsonb ->> 'trader_address' = ?", data.TraderAddress)
	default:
		// Fallback for non-Postgres tests/environments.
		query = query.
			Where("data LIKE ?", `%"order_id":"`+data.OrderID+`"%`).
			Where("data LIKE ?", `%"trader_address":"`+data.TraderAddress+`"%`)
	}

	rows := make([]struct {
		UserID uuid.UUID `gorm:"column:user_id"`
	}, 0, len(followerIDs))
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}

	for _, row := range rows {
		if row.UserID != uuid.Nil {
			out[row.UserID] = struct{}{}
		}
	}
	return out, nil
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

// CreateSystemNotification creates a single system notification for one user.
func (s *NotificationService) CreateSystemNotification(
	ctx context.Context,
	userID uuid.UUID,
	title string,
	message string,
	data map[string]interface{},
) error {
	if userID == uuid.Nil {
		return fmt.Errorf("user id is required")
	}
	_, err := s.CreateSystemNotifications(ctx, []uuid.UUID{userID}, title, message, data)
	return err
}

// CreateSystemNotifications inserts system notifications in bulk.
func (s *NotificationService) CreateSystemNotifications(
	ctx context.Context,
	userIDs []uuid.UUID,
	title string,
	message string,
	data map[string]interface{},
) (int, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}

	payload := "{}"
	if data != nil {
		if marshaled, err := json.Marshal(data); err == nil {
			payload = string(marshaled)
		}
	}

	trimmedTitle := strings.TrimSpace(title)
	trimmedMessage := strings.TrimSpace(message)
	if trimmedTitle == "" {
		trimmedTitle = "System Notification"
	}

	now := time.Now().UTC()
	notifications := make([]models.Notification, 0, len(userIDs))
	for _, userID := range userIDs {
		if userID == uuid.Nil {
			continue
		}
		notifications = append(notifications, models.Notification{
			ID:        uuid.New(),
			UserID:    userID,
			Type:      models.NotificationTypeSystem,
			Title:     trimmedTitle,
			Message:   trimmedMessage,
			Data:      payload,
			Read:      false,
			CreatedAt: now,
		})
	}
	if len(notifications) == 0 {
		return 0, nil
	}

	if err := s.db.WithContext(ctx).CreateInBatches(notifications, 500).Error; err != nil {
		return 0, err
	}
	return len(notifications), nil
}

// Helper to truncate address for display
func truncateAddress(address string) string {
	if len(address) <= 10 {
		return address
	}
	return address[:6] + "..." + address[len(address)-4:]
}
