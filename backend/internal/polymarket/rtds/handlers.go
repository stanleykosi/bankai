/**
 * @description
 * Handlers for Polymarket WebSocket messages.
 * Defines the data structures for the Market Channel events (Price Change, Book, etc.)
 * and implements the logic to process/persist them.
 *
 * Key features:
 * - Handles the "Sept 2025" Price Change schema (breaking change support).
 * - Processes Orderbook Snapshots (`book`).
 * - Processes Trades (`last_trade_price` / `last_trade`).
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
	"strconv"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/polymarket/clob"
	"github.com/bankai-project/backend/internal/services"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Event Types
const (
	EventTypePriceChange    = "price_change"
	EventTypeBook           = "book"
	EventTypeLastTradePrice = "last_trade_price"
	EventTypeLastTrade      = "last_trade"
	EventTypeTickSizeChange = "tick_size_change"
)

// BaseMessage is used to peek at the event type before full unmarshalling
type BaseMessage struct {
	EventType string `json:"event_type"`
}

const (
	tradeBufferTTL       = 6 * time.Hour
	defaultPriceCacheTTL = 10 * time.Minute
	defaultBookCacheTTL  = 0 * time.Minute
)

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

	cacheAllowlist *CacheAllowlist
	priceCacheTTL  time.Duration
	bookCacheTTL   time.Duration
}

func NewMessageHandler(db *gorm.DB, r *redis.Client) *MessageHandler {
	return &MessageHandler{
		DB:            db,
		Redis:         r,
		priceCacheTTL: defaultPriceCacheTTL,
		bookCacheTTL:  defaultBookCacheTTL,
	}
}

func (h *MessageHandler) SetCacheAllowlist(allowlist *CacheAllowlist) {
	h.cacheAllowlist = allowlist
}

func (h *MessageHandler) SetCacheTTLs(priceTTL, bookTTL time.Duration) {
	if priceTTL >= 0 {
		h.priceCacheTTL = priceTTL
	}
	if bookTTL >= 0 {
		h.bookCacheTTL = bookTTL
	}
}

func (h *MessageHandler) shouldCache(assetID string) bool {
	if h.cacheAllowlist == nil {
		return true
	}
	return h.cacheAllowlist.IsAllowed(assetID)
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
				logger.Warn("market RTDS batch item failed: %v", err)
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

	case EventTypeLastTradePrice, EventTypeLastTrade:
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
	// Cache/publish only allowlisted assets to keep RTDS throughput stable.
	// If this batch has no allowlisted assets, skip Redis work entirely.
	allowlisted := make([]PriceChange, 0, len(m.PriceChanges))
	for _, change := range m.PriceChanges {
		if h.shouldCache(change.AssetID) {
			allowlisted = append(allowlisted, change)
		}
	}
	if len(allowlisted) == 0 {
		return nil
	}

	// Use one pipeline for velocity + latest price writes.
	pipe := h.Redis.Pipeline()
	pipe.ZIncrBy(ctx, "market:velocity", 1, m.Market)
	didCache := false
	for _, change := range allowlisted {
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
		if h.priceCacheTTL > 0 {
			pipe.Expire(ctx, key, h.priceCacheTTL)
		}
		didCache = true

		// Also persist to Postgres for historical charting?
		// Doing this synchronously here might be too slow for high frequency.
		// Better to push to a channel/queue for the History Worker (Step 13).
		// For now, we skip DB insert here to prioritize ingestion speed.
	}

	if didCache {
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
	}

	h.publishPriceUpdates(ctx, m)
	return nil
}

// handleBook processes the initial snapshot
func (h *MessageHandler) handleBook(ctx context.Context, m *BookMessage) error {
	if h.bookCacheTTL <= 0 {
		return nil
	}
	if !h.shouldCache(m.AssetID) {
		return nil
	}
	// Store the full book snapshot in Redis if needed for the UI "Depth" view
	// Key: book:{market_id}:{asset_id}
	key := fmt.Sprintf("book:%s:%s", m.Market, m.AssetID)

	// Serialize bids/asks
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}

	// Set with a TTL (refresh happens on next snapshot or via updates)
	return h.Redis.Set(ctx, key, data, h.bookCacheTTL).Err()
}

// handleLastTrade records actual trades, important for volume tracking
func (h *MessageHandler) handleLastTrade(ctx context.Context, m *LastTradeMessage) error {
	// 1. Parse numeric values
	price, _ := strconv.ParseFloat(m.Price, 64)
	size, _ := strconv.ParseFloat(m.Size, 64)
	volume := price * size
	matchTime := parseTimestampToUnix(m.Timestamp)
	tradeID := fmt.Sprintf("%s-%s-%s", m.Market, m.AssetID, m.Timestamp)

	pipe := h.Redis.Pipeline()

	// 2. Update Redis Volume Stats
	// Increment 24h volume for the market
	// Key: market:{id}:volume
	pipe.IncrByFloat(ctx, fmt.Sprintf("market:%s:volume", m.Market), volume)

	// 3. Cache latest last trade price for UI fallback when spread > $0.10
	key := fmt.Sprintf("price:%s:%s", m.Market, m.AssetID)
	pipe.HSet(ctx, key, map[string]interface{}{
		"last_trade_price":   m.Price,
		"last_trade_updated": m.Timestamp,
	})

	// 4. Buffer trade event for analysis (rolling window)
	if matchTime > 0 {
		bucket := matchTime / 60
		trade := clob.TradeEvent{
			ID:        tradeID,
			Market:    m.Market,
			TokenID:   m.AssetID,
			Side:      clob.OrderSide(strings.ToUpper(m.Side)),
			Price:     price,
			Size:      size,
			Value:     volume,
			Taker:     "", // Not provided in RTDS; left empty
			Maker:     "",
			MatchTime: matchTime,
		}
		data, err := json.Marshal(trade)
		if err == nil {
			listKey := fmt.Sprintf("rtds:trades:%d", bucket)
			pipe.LPush(ctx, listKey, data)
			pipe.Expire(ctx, listKey, tradeBufferTTL)
		}
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}

	// Whale updates are sourced from RTDS activity; avoid duplicating via CLOB.

	h.publishLastTradeUpdate(ctx, m)
	return nil
}

type priceUpdatePayload struct {
	ConditionID        string   `json:"condition_id"`
	AssetID            string   `json:"asset_id"`
	Price              *float64 `json:"price,omitempty"`
	BestBid            *float64 `json:"best_bid,omitempty"`
	BestAsk            *float64 `json:"best_ask,omitempty"`
	Timestamp          *string  `json:"timestamp,omitempty"`
	LastTradePrice     *float64 `json:"last_trade_price,omitempty"`
	LastTradeTimestamp *string  `json:"last_trade_timestamp,omitempty"`
}

func (h *MessageHandler) publishPriceUpdates(ctx context.Context, m *PriceChangeMessage) {
	if h.Redis == nil {
		return
	}

	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	pipe := h.Redis.Pipeline()
	published := 0

	for _, change := range m.PriceChanges {
		// Keep downstream pub/sub aligned with allowlist to avoid flooding
		// consumers with irrelevant asset updates under high RTDS throughput.
		if !h.shouldCache(change.AssetID) {
			continue
		}

		price := parseFloat(change.Price)
		bestBid := parseFloat(change.BestBid)
		bestAsk := parseFloat(change.BestAsk)
		ts := timestamp

		payload := priceUpdatePayload{
			ConditionID: m.Market,
			AssetID:     change.AssetID,
			Price:       &price,
			BestBid:     &bestBid,
			BestAsk:     &bestAsk,
			Timestamp:   &ts,
		}

		data, err := json.Marshal(payload)
		if err != nil {
			continue
		}

		pipe.Publish(ctx, services.PriceUpdateChannel, data)
		published++
	}

	if published == 0 {
		return
	}

	if _, err := pipe.Exec(ctx); err != nil {
		logger.Warn("market RTDS redis publish error: %v", err)
	}
}

func (h *MessageHandler) publishLastTradeUpdate(ctx context.Context, m *LastTradeMessage) {
	price := parseFloat(m.Price)
	ts := m.Timestamp
	payload := priceUpdatePayload{
		ConditionID:        m.Market,
		AssetID:            m.AssetID,
		LastTradePrice:     &price,
		LastTradeTimestamp: &ts,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	if err := h.Redis.Publish(ctx, services.PriceUpdateChannel, data).Err(); err != nil {
		logger.Warn("market RTDS redis publish error: %v", err)
	}
}

func (h *MessageHandler) publishWhaleUpdate(ctx context.Context, m *LastTradeMessage, volume float64, price float64, matchTime int64) {
	if h.Redis == nil {
		return
	}

	ts := time.Unix(matchTime, 0).UTC()
	event := services.WhaleEvent{
		Timestamp:   ts,
		MarketID:    m.Market,
		TokenID:     m.AssetID,
		Side:        strings.ToUpper(m.Side),
		SizeUSD:     volume,
		Price:       price,
		Wallet:      "",
		WalletTier:  "LIVE",
		WinRate:     -1,
		RealizedPnL: 0,
		IsWashTrade: false,
	}

	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	pipe := h.Redis.Pipeline()
	pipe.Publish(ctx, services.WhaleUpdateChannel, data)
	pipe.LPush(ctx, services.WhaleRecentListKey, data)
	pipe.LTrim(ctx, services.WhaleRecentListKey, 0, int64(services.WhaleRecentListMax-1))
	pipe.Expire(ctx, services.WhaleRecentListKey, services.WhaleRecentListTTL)

	if _, err := pipe.Exec(ctx); err != nil {
		logger.Warn("market RTDS redis whale update error: %v", err)
	}
}

func parseFloat(value string) float64 {
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return f
}

func parseTimestampToUnix(ts string) int64 {
	if ts == "" {
		return time.Now().Unix()
	}
	// Try RFC3339
	if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
		return parsed.Unix()
	}
	// Try numeric seconds
	if num, err := strconv.ParseInt(ts, 10, 64); err == nil {
		if num > 1_000_000_000_000 {
			return num / 1000
		}
		return num
	}
	return time.Now().Unix()
}
