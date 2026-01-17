package services

import (
	"context"
	"testing"

	"github.com/bankai-project/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + uuid.NewString() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}

	if err := db.Exec(`
CREATE TABLE users (
  id TEXT PRIMARY KEY,
  email TEXT,
  eoa_address TEXT NOT NULL,
  vault_address TEXT,
  wallet_type TEXT,
  created_at DATETIME,
  updated_at DATETIME
);
`).Error; err != nil {
		t.Fatalf("failed to create users table: %v", err)
	}

	if err := db.Exec(`
CREATE TABLE user_settings (
  id TEXT PRIMARY KEY,
  user_id TEXT UNIQUE NOT NULL,
  default_order_type TEXT,
  slippage_tolerance_bps INTEGER,
  trade_confirmation_enabled BOOLEAN,
  notification_channel TEXT,
  whale_alert_threshold_usd REAL,
  order_fill_notifications BOOLEAN,
  resolution_alerts BOOLEAN,
  followed_trader_alerts TEXT,
  created_at DATETIME,
  updated_at DATETIME
);
`).Error; err != nil {
		t.Fatalf("failed to create user_settings table: %v", err)
	}

	return db
}

func TestSettings_GetSettingsCreatesDefaults(t *testing.T) {
	db := newTestDB(t)
	service := NewSettingsService(db)

	userID := uuid.New()

	settings, err := service.GetSettings(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetSettings returned error: %v", err)
	}
	if settings == nil {
		t.Fatalf("expected settings, got nil")
	}
	if settings.UserID != userID {
		t.Fatalf("expected user_id=%s, got %s", userID, settings.UserID)
	}
	if settings.DefaultOrderType != DefaultOrderTypeGTC {
		t.Fatalf("expected default_order_type=%s, got %s", DefaultOrderTypeGTC, settings.DefaultOrderType)
	}
	if settings.SlippageToleranceBps != 100 {
		t.Fatalf("expected slippage_tolerance_bps=100, got %d", settings.SlippageToleranceBps)
	}
	if settings.NotificationChannel != NotificationChannelInApp {
		t.Fatalf("expected notification_channel=%s, got %s", NotificationChannelInApp, settings.NotificationChannel)
	}

	var count int64
	if err := db.Model(&models.UserSettings{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		t.Fatalf("failed counting settings rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 settings row, got %d", count)
	}
}

func TestSettings_UpdateSettingsValidation(t *testing.T) {
	db := newTestDB(t)
	service := NewSettingsService(db)

	userID := uuid.New()

	_, err := service.UpdateSettings(context.Background(), userID, UpdateUserSettings{
		SlippageToleranceBps: intPtrTest(5),
	})
	if err == nil {
		t.Fatalf("expected validation error for slippage, got nil")
	}
}

func TestSettings_UpdateSettingsPartialUpdate(t *testing.T) {
	db := newTestDB(t)
	service := NewSettingsService(db)

	userID := uuid.New()
	_, err := service.GetSettings(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetSettings returned error: %v", err)
	}

	out, err := service.UpdateSettings(context.Background(), userID, UpdateUserSettings{
		SlippageToleranceBps:     intPtrTest(250),
		TradeConfirmationEnabled: boolPtrTest(false),
	})
	if err != nil {
		t.Fatalf("UpdateSettings returned error: %v", err)
	}
	if out.SlippageToleranceBps != 250 {
		t.Fatalf("expected slippage_tolerance_bps=250, got %d", out.SlippageToleranceBps)
	}
	if out.TradeConfirmationEnabled != false {
		t.Fatalf("expected trade_confirmation_enabled=false, got %v", out.TradeConfirmationEnabled)
	}
	if out.DefaultOrderType != DefaultOrderTypeGTC {
		t.Fatalf("expected default_order_type unchanged (%s), got %s", DefaultOrderTypeGTC, out.DefaultOrderType)
	}
}

func TestSettings_ResetToDefaults(t *testing.T) {
	db := newTestDB(t)
	service := NewSettingsService(db)

	userID := uuid.New()
	_, err := service.UpdateSettings(context.Background(), userID, UpdateUserSettings{
		DefaultOrderType:       strPtrTest(DefaultOrderTypeFOK),
		NotificationChannel:    strPtrTest(NotificationChannelNone),
		WhaleAlertThresholdUSD: floatPtrTest(12345.67),
	})
	if err != nil {
		t.Fatalf("UpdateSettings returned error: %v", err)
	}

	reset, err := service.ResetSettings(context.Background(), userID)
	if err != nil {
		t.Fatalf("ResetSettings returned error: %v", err)
	}

	if reset.DefaultOrderType != DefaultOrderTypeGTC {
		t.Fatalf("expected default_order_type=%s, got %s", DefaultOrderTypeGTC, reset.DefaultOrderType)
	}
	if reset.NotificationChannel != NotificationChannelInApp {
		t.Fatalf("expected notification_channel=%s, got %s", NotificationChannelInApp, reset.NotificationChannel)
	}
	if reset.WhaleAlertThresholdUSD != 5000.00 {
		t.Fatalf("expected whale_alert_threshold_usd=5000.00, got %v", reset.WhaleAlertThresholdUSD)
	}
}

func intPtrTest(v int) *int             { return &v }
func boolPtrTest(v bool) *bool          { return &v }
func floatPtrTest(v float64) *float64   { return &v }
func strPtrTest(v string) *string       { return &v }
