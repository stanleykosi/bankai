package handlers

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/services"
	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUpDownStreamDisabledReturnsNotFound(t *testing.T) {
	svc := services.NewUpDownService(
		nil,
		nil,
		&config.Config{Services: config.ServicesConfig{UpDownEnabled: false}},
		nil,
		nil,
	)
	handler := NewUpDownHandler(svc, nil)

	app := fiber.New()
	app.Get("/api/v1/updown/stream", handler.Stream)

	req, _ := http.NewRequest(http.MethodGet, "/api/v1/updown/stream", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404 when feature is disabled, got %d", resp.StatusCode)
	}
}

func TestUpDownLogDecisionDatabaseErrorReturnsServerError(t *testing.T) {
	svc := services.NewUpDownService(
		nil,
		nil,
		&config.Config{Services: config.ServicesConfig{UpDownEnabled: true}},
		nil,
		nil,
	)
	handler := NewUpDownHandler(svc, nil)

	app := fiber.New()
	app.Post("/api/v1/updown/decisions", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return handler.LogDecision(c)
	})

	body := []byte(`{"slug":"btc-updown-5m-1","action":"accepted"}`)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/updown/decisions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected status 500 for database failure, got %d", resp.StatusCode)
	}
}

func TestUpDownLogDecisionValidationErrorReturnsBadRequest(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	svc := services.NewUpDownService(
		db,
		nil,
		&config.Config{Services: config.ServicesConfig{UpDownEnabled: true}},
		nil,
		nil,
	)
	handler := NewUpDownHandler(svc, nil)

	app := fiber.New()
	app.Post("/api/v1/updown/decisions", func(c *fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return handler.LogDecision(c)
	})

	body := []byte(`{"slug":"btc-updown-5m-1","action":"not-valid"}`)
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/updown/decisions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400 for validation error, got %d", resp.StatusCode)
	}
}
