/**
 * @description
 * Settings Service for user preferences.
 * Provides CRUD with validation + lazy initialization defaults.
 *
 * @dependencies
 * - gorm.io/gorm
 * - backend/internal/models
 */

package services

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/bankai-project/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	DefaultOrderTypeGTC = "GTC"
	DefaultOrderTypeGTD = "GTD"
	DefaultOrderTypeFOK = "FOK"
	DefaultOrderTypeFAK = "FAK"

	NotificationChannelInApp = "IN_APP"
	NotificationChannelNone  = "NONE"

	FollowedTraderAlertsAll       = "ALL"
	FollowedTraderAlertsLargeOnly = "LARGE_ONLY"
	FollowedTraderAlertsNone      = "NONE"
)

const (
	MinSlippageToleranceBps = 10
	MaxSlippageToleranceBps = 500
)

// UpdateUserSettings supports partial updates via PATCH.
type UpdateUserSettings struct {
	DefaultOrderType         *string  `json:"default_order_type"`
	SlippageToleranceBps     *int     `json:"slippage_tolerance_bps"`
	TradeConfirmationEnabled *bool    `json:"trade_confirmation_enabled"`
	NotificationChannel      *string  `json:"notification_channel"`
	WhaleAlertThresholdUSD   *float64 `json:"whale_alert_threshold_usd"`
	OrderFillNotifications   *bool    `json:"order_fill_notifications"`
	ResolutionAlerts         *bool    `json:"resolution_alerts"`
	FollowedTraderAlerts     *string  `json:"followed_trader_alerts"`
}

// SettingsService handles user setting operations.
type SettingsService struct {
	db *gorm.DB
}

func NewSettingsService(db *gorm.DB) *SettingsService {
	return &SettingsService{db: db}
}

func (s *SettingsService) GetDefaultSettings(userID uuid.UUID) models.UserSettings {
	return models.UserSettings{
		UserID:                   userID,
		DefaultOrderType:         DefaultOrderTypeGTC,
		SlippageToleranceBps:     100,
		TradeConfirmationEnabled: true,
		NotificationChannel:      NotificationChannelInApp,
		WhaleAlertThresholdUSD:   5000.00,
		OrderFillNotifications:   true,
		ResolutionAlerts:         true,
		FollowedTraderAlerts:     FollowedTraderAlertsAll,
	}
}

func (s *SettingsService) ValidateSlippageTolerance(bps int) error {
	if bps < MinSlippageToleranceBps || bps > MaxSlippageToleranceBps {
		return fmt.Errorf("slippage_tolerance_bps must be between %d and %d", MinSlippageToleranceBps, MaxSlippageToleranceBps)
	}
	return nil
}

func (s *SettingsService) ValidateOrderType(orderType string) error {
	switch strings.ToUpper(strings.TrimSpace(orderType)) {
	case DefaultOrderTypeGTC, DefaultOrderTypeGTD, DefaultOrderTypeFOK, DefaultOrderTypeFAK:
		return nil
	default:
		return fmt.Errorf("default_order_type must be one of %s, %s, %s, %s", DefaultOrderTypeGTC, DefaultOrderTypeGTD, DefaultOrderTypeFOK, DefaultOrderTypeFAK)
	}
}

func (s *SettingsService) ValidateNotificationChannel(channel string) error {
	switch strings.ToUpper(strings.TrimSpace(channel)) {
	case NotificationChannelInApp, NotificationChannelNone:
		return nil
	default:
		return fmt.Errorf("notification_channel must be one of %s, %s", NotificationChannelInApp, NotificationChannelNone)
	}
}

func (s *SettingsService) ValidateWhaleAlertThresholdUSD(value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return fmt.Errorf("whale_alert_threshold_usd must be a non-negative number")
	}
	if value > 1_000_000_000 {
		return fmt.Errorf("whale_alert_threshold_usd is too large")
	}
	return nil
}

func (s *SettingsService) ValidateFollowedTraderAlerts(value string) error {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case FollowedTraderAlertsAll, FollowedTraderAlertsLargeOnly, FollowedTraderAlertsNone:
		return nil
	default:
		return fmt.Errorf("followed_trader_alerts must be one of %s, %s, %s", FollowedTraderAlertsAll, FollowedTraderAlertsLargeOnly, FollowedTraderAlertsNone)
	}
}

func (s *SettingsService) GetSettings(ctx context.Context, userID uuid.UUID) (*models.UserSettings, error) {
	var settings models.UserSettings
	err := s.db.WithContext(ctx).Where("user_id = ?", userID).First(&settings).Error
	if err == nil {
		return &settings, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// Lazily initialize defaults (race-safe via ON CONFLICT DO NOTHING).
	defaults := s.GetDefaultSettings(userID)
	if err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}},
			DoNothing: true,
		}).
		Create(&defaults).Error; err != nil {
		return nil, err
	}

	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).First(&settings).Error; err != nil {
		return nil, err
	}
	return &settings, nil
}

func (s *SettingsService) GetSettingsForUsers(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]models.UserSettings, error) {
	result := make(map[uuid.UUID]models.UserSettings)
	if len(userIDs) == 0 {
		return result, nil
	}

	var rows []models.UserSettings
	if err := s.db.WithContext(ctx).Where("user_id IN ?", userIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.UserID] = row
	}

	missing := make([]uuid.UUID, 0)
	for _, userID := range userIDs {
		if _, ok := result[userID]; !ok {
			missing = append(missing, userID)
		}
	}
	if len(missing) == 0 {
		return result, nil
	}

	defaults := make([]models.UserSettings, 0, len(missing))
	for _, userID := range missing {
		defaults = append(defaults, s.GetDefaultSettings(userID))
	}

	if err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}},
			DoNothing: true,
		}).
		Create(&defaults).Error; err != nil {
		return nil, err
	}

	var created []models.UserSettings
	if err := s.db.WithContext(ctx).Where("user_id IN ?", missing).Find(&created).Error; err != nil {
		return nil, err
	}
	for _, row := range created {
		result[row.UserID] = row
	}

	return result, nil
}

func (s *SettingsService) UpdateSettings(ctx context.Context, userID uuid.UUID, updates UpdateUserSettings) (*models.UserSettings, error) {
	normalized := updates

	if normalized.DefaultOrderType != nil {
		value := strings.ToUpper(strings.TrimSpace(*normalized.DefaultOrderType))
		if err := s.ValidateOrderType(value); err != nil {
			return nil, err
		}
		normalized.DefaultOrderType = &value
	}
	if normalized.SlippageToleranceBps != nil {
		if err := s.ValidateSlippageTolerance(*normalized.SlippageToleranceBps); err != nil {
			return nil, err
		}
	}
	if normalized.NotificationChannel != nil {
		value := strings.ToUpper(strings.TrimSpace(*normalized.NotificationChannel))
		if err := s.ValidateNotificationChannel(value); err != nil {
			return nil, err
		}
		normalized.NotificationChannel = &value
	}
	if normalized.WhaleAlertThresholdUSD != nil {
		if err := s.ValidateWhaleAlertThresholdUSD(*normalized.WhaleAlertThresholdUSD); err != nil {
			return nil, err
		}
	}
	if normalized.FollowedTraderAlerts != nil {
		value := strings.ToUpper(strings.TrimSpace(*normalized.FollowedTraderAlerts))
		if err := s.ValidateFollowedTraderAlerts(value); err != nil {
			return nil, err
		}
		normalized.FollowedTraderAlerts = &value
	}

	patch := make(map[string]interface{})
	if normalized.DefaultOrderType != nil {
		patch["default_order_type"] = *normalized.DefaultOrderType
	}
	if normalized.SlippageToleranceBps != nil {
		patch["slippage_tolerance_bps"] = *normalized.SlippageToleranceBps
	}
	if normalized.TradeConfirmationEnabled != nil {
		patch["trade_confirmation_enabled"] = *normalized.TradeConfirmationEnabled
	}
	if normalized.NotificationChannel != nil {
		patch["notification_channel"] = *normalized.NotificationChannel
	}
	if normalized.WhaleAlertThresholdUSD != nil {
		patch["whale_alert_threshold_usd"] = *normalized.WhaleAlertThresholdUSD
	}
	if normalized.OrderFillNotifications != nil {
		patch["order_fill_notifications"] = *normalized.OrderFillNotifications
	}
	if normalized.ResolutionAlerts != nil {
		patch["resolution_alerts"] = *normalized.ResolutionAlerts
	}
	if normalized.FollowedTraderAlerts != nil {
		patch["followed_trader_alerts"] = *normalized.FollowedTraderAlerts
	}

	// No-op update: return current settings (still lazy initializes).
	if len(patch) == 0 {
		return s.GetSettings(ctx, userID)
	}

	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	defer func() {
		_ = tx.Rollback()
	}()

	defaults := s.GetDefaultSettings(userID)
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoNothing: true,
	}).Create(&defaults).Error; err != nil {
		return nil, err
	}

	if err := tx.Model(&models.UserSettings{}).Where("user_id = ?", userID).Updates(patch).Error; err != nil {
		return nil, err
	}

	var out models.UserSettings
	if err := tx.Where("user_id = ?", userID).First(&out).Error; err != nil {
		return nil, err
	}

	if err := tx.Commit().Error; err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *SettingsService) ResetSettings(ctx context.Context, userID uuid.UUID) (*models.UserSettings, error) {
	defaults := s.GetDefaultSettings(userID)
	patch := map[string]interface{}{
		"default_order_type":           defaults.DefaultOrderType,
		"slippage_tolerance_bps":       defaults.SlippageToleranceBps,
		"trade_confirmation_enabled":   defaults.TradeConfirmationEnabled,
		"notification_channel":         defaults.NotificationChannel,
		"whale_alert_threshold_usd":    defaults.WhaleAlertThresholdUSD,
		"order_fill_notifications":     defaults.OrderFillNotifications,
		"resolution_alerts":            defaults.ResolutionAlerts,
		"followed_trader_alerts":       defaults.FollowedTraderAlerts,
	}

	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoNothing: true,
	}).Create(&defaults).Error; err != nil {
		return nil, err
	}

	if err := tx.Model(&models.UserSettings{}).Where("user_id = ?", userID).Updates(patch).Error; err != nil {
		return nil, err
	}

	var out models.UserSettings
	if err := tx.Where("user_id = ?", userID).First(&out).Error; err != nil {
		return nil, err
	}

	if err := tx.Commit().Error; err != nil {
		return nil, err
	}
	return &out, nil
}
