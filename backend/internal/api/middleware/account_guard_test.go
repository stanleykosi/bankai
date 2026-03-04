package middleware

import (
	"net/http/httptest"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAccountGuardBlocksVaultAddressEvenWhenSessionWalletIsEOA(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE users (
			id TEXT PRIMARY KEY,
			eoa_address TEXT,
			vault_address TEXT
		)
	`).Error; err != nil {
		t.Fatalf("failed to create users table: %v", err)
	}

	userID := uuid.New().String()
	eoaAddress := "0x1111111111111111111111111111111111111111"
	vaultAddress := "0x2222222222222222222222222222222222222222"
	if err := db.Exec(
		`INSERT INTO users (id, eoa_address, vault_address) VALUES (?, ?, ?)`,
		userID, eoaAddress, vaultAddress,
	).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	if err := rdb.Set(
		t.Context(),
		blockedWalletKeyPrefix+vaultAddress,
		"1",
		0,
	).Err(); err != nil {
		t.Fatalf("failed to seed blocked wallet key: %v", err)
	}

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		c.Locals("wallet_address", eoaAddress) // SIWE signer wallet in session
		return c.Next()
	})
	app.Use(AccountGuard(rdb, db))
	app.Get("/protected", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest("GET", "/protected", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("expected 403 for blocked vault wallet, got %d", resp.StatusCode)
	}
}
