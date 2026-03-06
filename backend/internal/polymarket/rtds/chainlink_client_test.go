package rtds

import (
	"context"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/bankai-project/backend/internal/services"
	"github.com/redis/go-redis/v9"
)

func TestChainlinkHandlerStoresPrimaryTopicPayload(t *testing.T) {
	t.Parallel()

	mini := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	handler := NewChainlinkHandler(rdb)

	msg := []byte(`{"topic":"crypto_prices_chainlink","type":"update","timestamp":1741269965000,"payload":{"symbol":"btc/usd","timestamp":1741269965000,"value":67234.5}}`)
	if err := handler.HandleMessage(context.Background(), msg); err != nil {
		t.Fatalf("handle message: %v", err)
	}

	point := services.GetChainlinkLatest(context.Background(), rdb, "BTC")
	if point == nil {
		t.Fatal("expected stored chainlink point")
	}
	if point.Asset != "BTC" {
		t.Fatalf("asset = %q, want BTC", point.Asset)
	}
	if point.Price != 67234.5 {
		t.Fatalf("price = %v, want 67234.5", point.Price)
	}
	if point.UpdatedAt.UnixMilli() != 1741269965000 {
		t.Fatalf("updated_at = %d, want %d", point.UpdatedAt.UnixMilli(), int64(1741269965000))
	}
}

func TestChainlinkHandlerStoresCryptoPricesBatchPayload(t *testing.T) {
	t.Parallel()

	mini := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	handler := NewChainlinkHandler(rdb)

	msg := []byte(`{"topic":"crypto_prices","type":"update","timestamp":1741269965000,"payload":{"symbol":"eth/usd","data":[{"timestamp":1741269964000,"value":3910.12},{"timestamp":1741269965000,"value":3911.44}]}}`)
	if err := handler.HandleMessage(context.Background(), msg); err != nil {
		t.Fatalf("handle message: %v", err)
	}

	point := services.GetChainlinkLatest(context.Background(), rdb, "ETH")
	if point == nil {
		t.Fatal("expected stored chainlink point")
	}
	if point.Price != 3911.44 {
		t.Fatalf("price = %v, want 3911.44", point.Price)
	}
	if point.UpdatedAt.UnixMilli() != 1741269965000 {
		t.Fatalf("updated_at = %d, want %d", point.UpdatedAt.UnixMilli(), int64(1741269965000))
	}
}

func TestChainlinkHandlerNormalizesSecondTimestamps(t *testing.T) {
	t.Parallel()

	mini := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	handler := NewChainlinkHandler(rdb)

	msg := []byte(`{"topic":"unexpected_topic","type":"update","timestamp":1741269965,"payload":{"symbol":"sol/usd","timestamp":1741269965,"value":155.25}}`)
	if err := handler.HandleMessage(context.Background(), msg); err != nil {
		t.Fatalf("handle message: %v", err)
	}

	point := services.GetChainlinkLatest(context.Background(), rdb, "SOL")
	if point == nil {
		t.Fatal("expected stored chainlink point")
	}
	if point.Price != 155.25 {
		t.Fatalf("price = %v, want 155.25", point.Price)
	}
	want := time.Unix(1741269965, 0).UTC()
	if !point.UpdatedAt.Equal(want) {
		t.Fatalf("updated_at = %s, want %s", point.UpdatedAt.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}
