package middleware

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

func TestClientIdentifierUsesAuthenticatedUserFromCookie(t *testing.T) {
	cleanup := setTestAuthConfig()
	defer cleanup()

	token := mustSignTestToken(t, "user-123")

	app := fiber.New()
	app.Get("/id", func(c *fiber.Ctx) error {
		return c.SendString(ClientIdentifier(c))
	})

	req := httptest.NewRequest("GET", "/id", nil)
	req.Header.Set("Cookie", "bankai_auth="+token)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if got := string(raw); got != "u:user-123" {
		t.Fatalf("expected user-scoped identifier, got %q", got)
	}
}

func TestClientIdentifierFallsBackToIPWhenTokenInvalid(t *testing.T) {
	cleanup := setTestAuthConfig()
	defer cleanup()

	app := fiber.New()
	app.Get("/id", func(c *fiber.Ctx) error {
		return c.SendString(ClientIdentifier(c))
	})

	req := httptest.NewRequest("GET", "/id", nil)
	req.Header.Set("Cookie", "bankai_auth=invalid-token")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if !strings.HasPrefix(string(raw), "ip:") {
		t.Fatalf("expected ip-scoped identifier, got %q", string(raw))
	}
}

func setTestAuthConfig() func() {
	original := mwConfig
	mwConfig = &AuthMiddlewareConfig{
		JWTSecret:  []byte("test-secret"),
		CookieName: "bankai_auth",
		Issuer:     "",
	}
	return func() {
		mwConfig = original
	}
}

func mustSignTestToken(t *testing.T, subject string) string {
	t.Helper()
	now := time.Now().UTC()
	claims := AuthClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	}
	jwtToken := jwt.NewWithClaims(jwt.SigningMethodHS512, claims)
	signed, err := jwtToken.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatalf("failed to sign test token: %v", err)
	}
	return signed
}
