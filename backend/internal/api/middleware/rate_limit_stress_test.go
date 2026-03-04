package middleware

import (
	"net/http/httptest"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
)

func TestRateLimitStressAndFailureInjection(t *testing.T) {
	t.Run("enforces_429_under_load", func(t *testing.T) {
		mr, err := miniredis.Run()
		if err != nil {
			t.Fatalf("failed to start miniredis: %v", err)
		}
		defer mr.Close()

		rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		app := fiber.New()
		app.Use(RateLimit(RateLimitConfig{
			Redis:  rdb,
			Prefix: "test-load",
			Limit:  25,
			Window: time.Minute,
			KeyFunc: func(c *fiber.Ctx) string {
				return "ip:test-client"
			},
		}))
		app.Get("/ping", func(c *fiber.Ctx) error {
			return c.SendStatus(fiber.StatusOK)
		})

		okCount := 0
		limitedCount := 0
		for i := 0; i < 80; i++ {
			req := httptest.NewRequest("GET", "/ping", nil)
			resp, err := app.Test(req, -1)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			switch resp.StatusCode {
			case fiber.StatusOK:
				okCount++
			case fiber.StatusTooManyRequests:
				limitedCount++
			}
		}
		t.Logf("rate-limit stress result: ok=%d limited=%d total=%d", okCount, limitedCount, okCount+limitedCount)

		if okCount == 0 {
			t.Fatalf("expected at least some successful requests")
		}
		if limitedCount == 0 {
			t.Fatalf("expected some 429 responses, got none")
		}
	})

	t.Run("redis_outage_fail_open", func(t *testing.T) {
		// Failure injection: point limiter to unavailable Redis and ensure API stays up.
		rdb := redis.NewClient(&redis.Options{
			Addr:         "127.0.0.1:6399",
			DialTimeout:  25 * time.Millisecond,
			ReadTimeout:  25 * time.Millisecond,
			WriteTimeout: 25 * time.Millisecond,
			PoolTimeout:  25 * time.Millisecond,
			MaxRetries:   0,
		})

		app := fiber.New()
		app.Use(RateLimit(RateLimitConfig{
			Redis:  rdb,
			Prefix: "test-failure",
			Limit:  1,
			Window: time.Minute,
		}))
		app.Get("/health", func(c *fiber.Ctx) error {
			return c.SendStatus(fiber.StatusOK)
		})

		for i := 0; i < 10; i++ {
			req := httptest.NewRequest("GET", "/health", nil)
			resp, err := app.Test(req, -1)
			if err != nil {
				t.Fatalf("request failed during redis outage: %v", err)
			}
			if resp.StatusCode != fiber.StatusOK {
				t.Fatalf("expected fail-open 200, got %d", resp.StatusCode)
			}
		}
		t.Logf("redis outage fail-open result: successful_requests=%d", 10)
	})
}

func BenchmarkRateLimitParallel(b *testing.B) {
	mr, err := miniredis.Run()
	if err != nil {
		b.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	app := fiber.New()
	app.Use(RateLimit(RateLimitConfig{
		Redis:  rdb,
		Prefix: "bench-load",
		Limit:  1000000000, // effectively disable limiting to measure middleware overhead
		Window: time.Minute,
		KeyFunc: func(c *fiber.Ctx) string {
			return "bench"
		},
	}))
	app.Get("/ping", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest("GET", "/ping", nil)
			resp, err := app.Test(req, -1)
			if err != nil {
				b.Fatalf("request failed: %v", err)
			}
			if resp.StatusCode != fiber.StatusOK {
				b.Fatalf("unexpected status: %d", resp.StatusCode)
			}
		}
	})
}
