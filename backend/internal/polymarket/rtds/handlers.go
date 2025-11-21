/**
 * @description
 * Handlers for Polymarket WebSocket messages.
 * Defines the data structures for the Market Channel events (Price Change, Book, etc.)
 * and implements the logic to process/persist them.
 *
 * Key features:
 * - Handles the "Sept 2025" Price Change schema (breaking change support).
 * - Processes Orderbook Snapshots (`book`).
 * - Processes Trades (`last_trade_price`).
 * - Updates Redis with latest prices/velocity metrics.
 *
 * @dependencies
 * - encoding/json
 * - github.com/redis/go-redis/v9
 * - gorm.io/gorm
 */

package rtds

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/models"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Event Types
const (
	EventTypePriceChange    = "price_change"
	EventTypeBook           = "book"
	EventTypeLastTradePrice = "last_trade_price"
	EventTypeTickSizeChange = "tick_size_change"
)

// BaseMessage is used to peek at the event type before full unmarshalling
type BaseMessage struct {
	EventType string `json:"event_type"`
}

// PriceChange represents a single update in the new Sept 2025 schema
type PriceChange struct {
	AssetID string `json:"asset_id"`
	Price   string `json:"price"`
	Size    string `json:"size"`
	Side    string `json:"side"` // "BUY" or "SELL"
	Hash    string `json:"hash"`
	BestBid string `json:"best_bid"`
	BestAsk string `json:"best_ask"`
}

// PriceChangeMessage represents the batched price update message
type PriceChangeMessage struct {
	EventType    string        `json:"event_type"`
	Market       string        `json:"market"` // Condition ID
	Timestamp    string        `json:"timestamp"`
	PriceChanges []PriceChange `json:"price_changes"`
}

// OrderSummary represents a level in the order book snapshot
type OrderSummary struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

// BookMessage represents the initial order book snapshot
type BookMessage struct {
	EventType string         `json:"event_type"`
	AssetID   string         `json:"asset_id"`
	Market    string         `json:"market"`
	Timestamp string         `json:"timestamp"`
	Hash      string         `json:"hash"`
	Bids      []OrderSummary `json:"bids"`
	Asks      []OrderSummary `json:"asks"`
}

// LastTradeMessage represents a trade execution event
type LastTradeMessage struct {
	EventType  string `json:"event_type"`
	AssetID    string `json:"asset_id"`
	Market     string `json:"market"`
	Price      string `json:"price"`
	Size       string `json:"size"`
	Side       string `json:"side"`
	Timestamp  string `json:"timestamp"`
	FeeRateBps string `json:"fee_rate_bps"`
}

// MessageHandler processes incoming WS messages
type MessageHandler struct {
	DB    *gorm.DB
	Redis *redis.Client
}

func NewMessageHandler(db *gorm.DB, r *redis.Client) *MessageHandler {
	return &MessageHandler{
		DB:    db,
		Redis: r,
	}
}

// HandleMessage routes the raw JSON message to the specific handler
func (h *MessageHandler) HandleMessage(ctx context.Context, msg []byte) error {
	msg = bytes.TrimSpace(msg)
	if len(msg) == 0 {
		return nil
	}

	switch msg[0] {
	case '{', '[':
		// valid JSON starts - continue
	default:
		text := strings.ToUpper(string(msg))
		switch text {
		case "PING", "PONG":
			return nil
		default:
			log.Printf("RTDS ignoring non-JSON frame: %s", text)
			return nil
		}
	}

	// The RTDS stream often batches multiple events inside a JSON array.
	// Detect that case and fan each payload back into HandleMessage.
	if msg[0] == '[' {
		var batch []json.RawMessage
		if err := json.Unmarshal(msg, &batch); err != nil {
			return fmt.Errorf("failed to parse batched events: %w", err)
		}

		for _, raw := range batch {
			if err := h.HandleMessage(ctx, raw); err != nil {
				log.Printf("RTDS batch item failed: %v", err)
			}
		}
		return nil
	}

	var base BaseMessage
	if err := json.Unmarshal(msg, &base); err != nil {
		return fmt.Errorf("failed to parse event type: %w", err)
	}

	switch base.EventType {
	case EventTypePriceChange:
		var m PriceChangeMessage
		if err := json.Unmarshal(msg, &m); err != nil {
			return err
		}
		return h.handlePriceChange(ctx, &m)

	case EventTypeBook:
		var m BookMessage
		if err := json.Unmarshal(msg, &m); err != nil {
			return err
		}
		return h.handleBook(ctx, &m)

	case EventTypeLastTradePrice:
		var m LastTradeMessage
		if err := json.Unmarshal(msg, &m); err != nil {
			return err
		}
		return h.handleLastTrade(ctx, &m)

	default:
		// Ignore unknown events (like tick_size_change for now)
		return nil
	}
}

// handlePriceChange updates the "High Velocity" metrics and caches current prices
func (h *MessageHandler) handlePriceChange(ctx context.Context, m *PriceChangeMessage) error {
	// For velocity, we might want to count updates per minute per market.
	// Use Redis HyperLogLog or simple counters.

	// 1. Update Velocity Counter (Expires in 1 hour)
	// Key: velocity:{market_id}:{minute_bucket}
	// This allows us to calculate acceleration later

	// For simplicity in MVP, just increment a score in a Sorted Set "market:velocity"
	// Score = number of updates (proxy for activity)
	err := h.Redis.ZIncrBy(ctx, "market:velocity", 1, m.Market).Err()
	if err != nil {
		log.Printf("Redis error updating velocity: %v", err)
	}

	// 2. Cache latest prices for immediate frontend retrieval
	// We process each change in the batch
	pipe := h.Redis.Pipeline()
	for _, change := range m.PriceChanges {
		// Store latest price: market:{market_id}:{asset_id}:price
		key := fmt.Sprintf("price:%s:%s", m.Market, change.AssetID)

		// Store a hash with details
		pipe.HSet(ctx, key, map[string]interface{}{
			"price":    change.Price,
			"side":     change.Side,
			"size":     change.Size,
			"best_bid": change.BestBid,
			"best_ask": change.BestAsk,
			"updated":  m.Timestamp,
		})

		// Also persist to Postgres for historical charting?
		// Doing this synchronously here might be too slow for high frequency.
		// Better to push to a channel/queue for the History Worker (Step 13).
		// For now, we skip DB insert here to prioritize ingestion speed.
	}

	_, err = pipe.Exec(ctx)
	return err
}

// handleBook processes the initial snapshot
func (h *MessageHandler) handleBook(ctx context.Context, m *BookMessage) error {
	// Store the full book snapshot in Redis if needed for the UI "Depth" view
	// Key: book:{market_id}:{asset_id}
	key := fmt.Sprintf("book:%s:%s", m.Market, m.AssetID)

	// Serialize bids/asks
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}

	// Set with a TTL (refresh happens on next snapshot or via updates)
	return h.Redis.Set(ctx, key, data, 24*time.Hour).Err()
}

// handleLastTrade records actual trades, important for volume tracking
func (h *MessageHandler) handleLastTrade(ctx context.Context, m *LastTradeMessage) error {
	// 1. Parse numeric values
	price, _ := strconv.ParseFloat(m.Price, 64)
	size, _ := strconv.ParseFloat(m.Size, 64)
	volume := price * size

	// 2. Persist Trade to DB (Audit Log / History)
	// We map this to a simplified model if needed, or just the PriceHistory table
	// Converting timestamp string (ms) to time.Time
	tsInt, _ := strconv.ParseInt(m.Timestamp, 10, 64)
	ts := time.UnixMilli(tsInt)

	// Map side to outcome for the price history table
	// NOTE: CLOB side BUY/SELL is regarding the token.
	// If asset_id matches TokenIDYes, then Side=BUY -> YES Price Up.
	// We need to know if asset_id is YES or NO.
	// For now, we'll defer the exact YES/NO mapping logic to the service layer
	// or assume the caller knows the asset_id mapping.
	// We will just insert assuming the asset ID is sufficient for now.

	// We use a lightweight struct here just for the insert to avoid circular deps if possible,
	// or use the models.PriceHistory if available.

	// NOTE: We skip DB insert for PriceHistory here and rely on the specialized "History Worker" (Step 13)
	// which polls the API for definitive history, or we enrich this data downstream.
	// The history variable is intentionally unused as per the design decision.
	_ = models.PriceHistory{
		MarketID:  m.Market,
		Price:     price,
		Volume:    volume,
		Timestamp: ts,
	}

	// 3. Update Redis Volume Stats
	// Increment 24h volume for the market
	// Key: market:{id}:volume
	return h.Redis.IncrByFloat(ctx, fmt.Sprintf("market:%s:volume", m.Market), volume).Err()
}
