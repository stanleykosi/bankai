package middleware

import (
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

type RateLimitConfig struct {
	Redis   *redis.Client
	Prefix  string
	Limit   int
	Window  time.Duration
	KeyFunc func(*fiber.Ctx) string
}

func RateLimit(cfg RateLimitConfig) fiber.Handler {
	limit := cfg.Limit
	if limit <= 0 {
		limit = 60
	}
	window := cfg.Window
	if window <= 0 {
		window = time.Minute
	}
	prefix := strings.TrimSpace(cfg.Prefix)
	if prefix == "" {
		prefix = "rl"
	}

	return func(c *fiber.Ctx) error {
		if cfg.Redis == nil {
			return c.Next()
		}

		keyPart := "anon"
		if cfg.KeyFunc != nil {
			if candidate := strings.TrimSpace(cfg.KeyFunc(c)); candidate != "" {
				keyPart = candidate
			}
		}

		bucket := time.Now().UTC().Unix() / int64(window.Seconds())
		key := fmt.Sprintf("%s:%d:%s", prefix, bucket, keyPart)

		pipe := cfg.Redis.Pipeline()
		countCmd := pipe.Incr(c.Context(), key)
		pipe.Expire(c.Context(), key, window+5*time.Second)
		_, err := pipe.Exec(c.Context())
		if err != nil {
			return c.Next()
		}

		count := countCmd.Val()
		remaining := int64(limit) - count
		if remaining < 0 {
			remaining = 0
		}
		c.Set("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
		c.Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))

		if count > int64(limit) {
			retryAfter := int(window.Seconds())
			if retryAfter <= 0 {
				retryAfter = 60
			}
			c.Set("Retry-After", fmt.Sprintf("%d", retryAfter))
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "rate limit exceeded",
			})
		}

		return c.Next()
	}
}

func ClientIdentifier(c *fiber.Ctx) string {
	if uid, ok := c.Locals("user_id").(string); ok && strings.TrimSpace(uid) != "" {
		return "u:" + strings.TrimSpace(uid)
	}
	if uid := userIDFromAuthToken(c); uid != "" {
		return "u:" + uid
	}
	return "ip:" + clientIP(c)
}

func userIDFromAuthToken(c *fiber.Ctx) string {
	if c == nil || mwConfig == nil || len(mwConfig.JWTSecret) == 0 {
		return ""
	}

	tokenString := ""
	authHeader := strings.TrimSpace(c.Get("Authorization"))
	if strings.HasPrefix(authHeader, "Bearer ") {
		tokenString = strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	}
	if tokenString == "" {
		tokenString = strings.TrimSpace(c.Cookies(mwConfig.CookieName))
	}
	if tokenString == "" {
		return ""
	}

	token, err := jwt.ParseWithClaims(tokenString, &AuthClaims{}, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS512 {
			return nil, fmt.Errorf("unexpected signing method: %s", token.Header["alg"])
		}
		return mwConfig.JWTSecret, nil
	})
	if err != nil || !token.Valid {
		return ""
	}

	claims, ok := token.Claims.(*AuthClaims)
	if !ok {
		return ""
	}
	return strings.TrimSpace(claims.Subject)
}

func clientIP(c *fiber.Ctx) string {
	ip := strings.TrimSpace(c.IP())
	if ip != "" {
		return ip
	}
	return "unknown"
}
