package middleware

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

func TestProtectedRejectsSignerAssertionTokens(t *testing.T) {
	previous := mwConfig
	mwConfig = &AuthMiddlewareConfig{
		JWTSecret:  []byte("test-secret"),
		CookieName: "bankai_auth",
		Issuer:     "",
	}
	defer func() {
		mwConfig = previous
	}()

	app := fiber.New()
	app.Use(Protected())
	app.Get("/protected", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	t.Run("rejects signer assertion token type", func(t *testing.T) {
		token := signAuthTokenForTest(t, AuthClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "user-1",
				IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
				ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(time.Hour)),
			},
			Wallet:    "0x1111111111111111111111111111111111111111",
			TokenType: AuthTokenTypeSignerAssertion,
		})

		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("expected 401 for signer assertion token, got %d", resp.StatusCode)
		}
	})

	t.Run("accepts session token type", func(t *testing.T) {
		token := signAuthTokenForTest(t, AuthClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "user-2",
				IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
				ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(time.Hour)),
			},
			Wallet:    "0x2222222222222222222222222222222222222222",
			TokenType: AuthTokenTypeSession,
		})

		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("expected 200 for session token, got %d", resp.StatusCode)
		}
	})

	t.Run("accepts legacy token without explicit type", func(t *testing.T) {
		token := signAuthTokenForTest(t, AuthClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "user-3",
				IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
				ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(time.Hour)),
			},
			Wallet: "0x3333333333333333333333333333333333333333",
		})

		req := httptest.NewRequest("GET", "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("expected 200 for legacy token, got %d", resp.StatusCode)
		}
	})
}

func signAuthTokenForTest(t *testing.T, claims AuthClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS512, claims)
	signed, err := token.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	return signed
}
