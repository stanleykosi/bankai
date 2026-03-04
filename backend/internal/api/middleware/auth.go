/**
 * @description
 * Authentication middleware using Bankai-issued JWTs (wallet-only auth).
 * Validates JWTs from Authorization headers or httpOnly cookies.
 *
 * @dependencies
 * - github.com/gofiber/fiber/v2: HTTP Context
 * - github.com/golang-jwt/jwt/v5: JWT parsing
 *
 * @notes
 * - Requires AUTH_JWT_SECRET to be set in configuration.
 * - Tokens are issued by the /auth/verify endpoint.
 */

package middleware

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bankai-project/backend/internal/config"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

// AuthMiddlewareConfig holds the JWKS function
type AuthMiddlewareConfig struct {
	JWTSecret  []byte
	CookieName string
	Issuer     string
}

var mwConfig *AuthMiddlewareConfig

const (
	AuthTokenTypeSession         = "session"
	AuthTokenTypeSignerAssertion = "signer_assertion"
)

// AuthClaims defines JWT claims for wallet auth.
type AuthClaims struct {
	jwt.RegisteredClaims
	Wallet    string `json:"wallet"`
	TokenType string `json:"token_type,omitempty"`
}

// InitAuthMiddleware initializes the JWT config. Should be called at startup.
func InitAuthMiddleware(cfg *config.Config) error {
	if cfg.Auth.JWTSecret == "" {
		return nil
	}

	mwConfig = &AuthMiddlewareConfig{
		JWTSecret:  []byte(cfg.Auth.JWTSecret),
		CookieName: cfg.Auth.CookieName,
		Issuer:     cfg.Auth.JWTIssuer,
	}
	return nil
}

// Protected protects routes requiring authentication
func Protected() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if mwConfig == nil || len(mwConfig.JWTSecret) == 0 {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Auth configuration not initialized",
			})
		}

		// 1. Get token from Authorization header or cookie
		tokenString := ""
		authHeader := c.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			tokenString = strings.TrimPrefix(authHeader, "Bearer ")
		}

		if tokenString == "" {
			tokenString = strings.TrimSpace(c.Cookies(mwConfig.CookieName))
		}

		if tokenString == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Missing auth token"})
		}

		// 2. Parse and Validate Token
		token, err := jwt.ParseWithClaims(tokenString, &AuthClaims{}, func(token *jwt.Token) (interface{}, error) {
			if token.Method != jwt.SigningMethodHS512 {
				return nil, fmt.Errorf("unexpected signing method: %s", token.Header["alg"])
			}
			return mwConfig.JWTSecret, nil
		})
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid token: " + err.Error()})
		}

		// 3. Validate Claims
		claims, ok := token.Claims.(*AuthClaims)
		if !ok || !token.Valid {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid token"})
		}

		if mwConfig.Issuer != "" && claims.Issuer != "" && claims.Issuer != mwConfig.Issuer {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid token issuer"})
		}
		if strings.EqualFold(strings.TrimSpace(claims.TokenType), AuthTokenTypeSignerAssertion) {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid token scope"})
		}

		// 4. Extract User ID (sub)
		if claims.Subject == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Token missing subject"})
		}

		// 5. Set User ID in Context
		c.Locals("user_id", claims.Subject)
		if claims.Wallet != "" {
			c.Locals("wallet_address", claims.Wallet)
		}

		return c.Next()
	}
}

// GetUserID returns the authenticated user's ID from context
func GetUserID(c *fiber.Ctx) (string, error) {
	id, ok := c.Locals("user_id").(string)
	if !ok {
		return "", errors.New("user id not found in context")
	}
	return id, nil
}

// GetWalletAddress returns the authenticated wallet address if present.
func GetWalletAddress(c *fiber.Ctx) (string, error) {
	addr, ok := c.Locals("wallet_address").(string)
	if !ok {
		return "", errors.New("wallet address not found in context")
	}
	return addr, nil
}
