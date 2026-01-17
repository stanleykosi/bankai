package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/bankai-project/backend/internal/api/middleware"
	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/models"
	"github.com/bankai-project/backend/internal/services"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
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

func signTestJWT(t *testing.T, secret []byte, userID uuid.UUID) string {
	t.Helper()
	claims := &middleware.AuthClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: userID.String(),
			Issuer:  "bankai-test",
		},
		Wallet: "0x0000000000000000000000000000000000000000",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS512, claims)
	signed, err := token.SignedString(secret)
	if err != nil {
		t.Fatalf("failed to sign jwt: %v", err)
	}
	return signed
}

func TestSettingsRequiresAuth(t *testing.T) {
	db := newTestDB(t)
	settingsService := services.NewSettingsService(db)
	handler := NewSettingsHandler(db, settingsService)

	secret := []byte("test-secret-512")
	_ = middleware.InitAuthMiddleware(&config.Config{
		Server: config.ServerConfig{Env: "test"},
		Auth: config.AuthConfig{
			JWTSecret:  string(secret),
			CookieName: "bankai_auth",
			JWTIssuer:  "bankai-test",
		},
	})

	app := fiber.New()
	v1 := app.Group("/api/v1")
	settings := v1.Group("/settings", middleware.Protected())
	settings.Get("/", handler.GetSettings)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.StatusCode)
	}
}

func TestGetSettingsEndpoint(t *testing.T) {
	db := newTestDB(t)
	settingsService := services.NewSettingsService(db)
	handler := NewSettingsHandler(db, settingsService)

	secret := []byte("test-secret-512")
	_ = middleware.InitAuthMiddleware(&config.Config{
		Server: config.ServerConfig{Env: "test"},
		Auth: config.AuthConfig{
			JWTSecret:  string(secret),
			CookieName: "bankai_auth",
			JWTIssuer:  "bankai-test",
		},
	})

	userID := uuid.New()
	if err := db.Create(&models.User{
		ID:         userID,
		EOAAddress: "0x1111111111111111111111111111111111111111",
	}).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	app := fiber.New()
	v1 := app.Group("/api/v1")
	settings := v1.Group("/settings", middleware.Protected())
	settings.Get("/", handler.GetSettings)

	jwtCookie := signTestJWT(t, secret, userID)
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	req.Header.Set("Cookie", "bankai_auth="+jwtCookie)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var settingsResp models.UserSettings
	if err := json.NewDecoder(resp.Body).Decode(&settingsResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if settingsResp.UserID != userID {
		t.Fatalf("expected user_id=%s, got %s", userID, settingsResp.UserID)
	}
	if settingsResp.DefaultOrderType != services.DefaultOrderTypeGTC {
		t.Fatalf("expected default_order_type=%s, got %s", services.DefaultOrderTypeGTC, settingsResp.DefaultOrderType)
	}
}

func TestUpdateSettingsEndpoint(t *testing.T) {
	db := newTestDB(t)
	settingsService := services.NewSettingsService(db)
	handler := NewSettingsHandler(db, settingsService)

	secret := []byte("test-secret-512")
	_ = middleware.InitAuthMiddleware(&config.Config{
		Server: config.ServerConfig{Env: "test"},
		Auth: config.AuthConfig{
			JWTSecret:  string(secret),
			CookieName: "bankai_auth",
			JWTIssuer:  "bankai-test",
		},
	})

	userID := uuid.New()
	if err := db.Create(&models.User{
		ID:         userID,
		EOAAddress: "0x1111111111111111111111111111111111111111",
	}).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	app := fiber.New()
	v1 := app.Group("/api/v1")
	settings := v1.Group("/settings", middleware.Protected())
	settings.Patch("/", handler.UpdateSettings)

	body, _ := json.Marshal(map[string]any{
		"slippage_tolerance_bps":       200,
		"trade_confirmation_enabled":   false,
		"default_order_type":           "GTD",
		"notification_channel":         "NONE",
		"followed_trader_alerts":       "LARGE_ONLY",
		"whale_alert_threshold_usd":    10000,
		"order_fill_notifications":     false,
		"resolution_alerts":            false,
	})

	jwtCookie := signTestJWT(t, secret, userID)
	req, _ := http.NewRequest(http.MethodPatch, "/api/v1/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "bankai_auth="+jwtCookie)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var settingsResp models.UserSettings
	if err := json.NewDecoder(resp.Body).Decode(&settingsResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if settingsResp.SlippageToleranceBps != 200 {
		t.Fatalf("expected slippage_tolerance_bps=200, got %d", settingsResp.SlippageToleranceBps)
	}
	if settingsResp.TradeConfirmationEnabled != false {
		t.Fatalf("expected trade_confirmation_enabled=false, got %v", settingsResp.TradeConfirmationEnabled)
	}
	if settingsResp.DefaultOrderType != services.DefaultOrderTypeGTD {
		t.Fatalf("expected default_order_type=%s, got %s", services.DefaultOrderTypeGTD, settingsResp.DefaultOrderType)
	}
	if settingsResp.NotificationChannel != services.NotificationChannelNone {
		t.Fatalf("expected notification_channel=%s, got %s", services.NotificationChannelNone, settingsResp.NotificationChannel)
	}
}
