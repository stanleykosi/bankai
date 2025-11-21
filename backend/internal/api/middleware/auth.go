/**
 * @description
 * Authentication middleware using Clerk JWTs.
 * Validates Bearer tokens against Clerk's JWKS.
 *
 * @dependencies
 * - github.com/gofiber/fiber/v2: HTTP Context
 * - github.com/golang-jwt/jwt/v5: JWT parsing
 * - github.com/MicahParks/keyfunc/v2: JWKS fetching and caching
 *
 * @notes
 * - Requires CLERK_JWKS_URL to be set in configuration.
 * - Caches JWKS keys to prevent excessive network calls.
 */

package middleware

import (
	"errors"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v2"
	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/logger"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

// AuthMiddlewareConfig holds the JWKS function
type AuthMiddlewareConfig struct {
	JWKS *keyfunc.JWKS
}

var mwConfig *AuthMiddlewareConfig

// InitAuthMiddleware initializes the JWKS cache. Should be called at startup.
func InitAuthMiddleware(cfg *config.Config) error {
	if cfg.Services.ClerkJWKSURL == "" {
		// In dev/test, we might not have this, but it's required for real auth
		logger.Info("⚠️ Warning: CLERK_JWKS_URL is empty. Auth validation will fail if not mocked.")
		return nil
	}

	// Create the JWKS from the resource at the given URL.
	// Refresh the JWKS every hour.
	jwks, err := keyfunc.Get(cfg.Services.ClerkJWKSURL, keyfunc.Options{
		RefreshInterval: time.Hour,
		RefreshErrorHandler: func(err error) {
			logger.Error("There was an error with the JWKS refresh: %v", err)
		},
	})
	if err != nil {
		return err
	}

	mwConfig = &AuthMiddlewareConfig{
		JWKS: jwks,
	}
	logger.Info("✅ Auth Middleware Initialized with JWKS")
	return nil
}

// Protected protects routes requiring authentication
func Protected() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if mwConfig == nil || mwConfig.JWKS == nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Auth configuration not initialized",
			})
		}

		// 1. Get Token from Header
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Missing authorization header"})
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString == authHeader {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid token format"})
		}

		// 2. Parse and Validate Token
		token, err := jwt.Parse(tokenString, mwConfig.JWKS.Keyfunc)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid token: " + err.Error()})
		}

		// 3. Validate Claims
		if !token.Valid {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid token"})
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid token claims"})
		}

		// 4. Extract User ID (sub)
		sub, ok := claims["sub"].(string)
		if !ok || sub == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Token missing subject"})
		}

		// 5. Set User ID in Context
		c.Locals("clerk_id", sub)

		return c.Next()
	}
}

// GetUserID returns the authenticated user's Clerk ID from context
func GetUserID(c *fiber.Ctx) (string, error) {
	id, ok := c.Locals("clerk_id").(string)
	if !ok {
		return "", errors.New("user id not found in context")
	}
	return id, nil
}

