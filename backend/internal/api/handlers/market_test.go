package handlers

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/bankai-project/backend/internal/services"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
)

func TestStreamPriceUpdates(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer redisClient.Close()

	service := &services.MarketService{
		Redis: redisClient,
	}

	handler := NewMarketHandler(service)
	app := fiber.New()
	app.Get("/api/v1/markets/stream", handler.StreamPriceUpdates)

	srv := httptest.NewServer(app.Handler())
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	payload := `{"condition_id":"test-market","asset_id":"yes-token","price":0.55}`
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = redisClient.Publish(context.Background(), services.PriceUpdateChannel, payload).Err()
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/markets/stream", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to call SSE endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code: %d", resp.StatusCode)
	}

	reader := bufio.NewReader(resp.Body)

	timeout := time.After(2 * time.Second)
	for {
		select {
		case <-timeout:
			t.Fatal("timed out waiting for SSE data")
		default:
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("failed to read SSE line: %v", err)
			}
			if strings.HasPrefix(line, "data:") {
				if !strings.Contains(line, `"test-market"`) {
					t.Fatalf("unexpected SSE payload: %s", line)
				}
				return
			}
		}
	}
}
