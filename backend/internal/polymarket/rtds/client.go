/**
 * @description
 * WebSocket Client for Polymarket CLOB (Market Channel).
 * Manages the persistent connection, subscriptions, and keep-alive logic.
 *
 * Key features:
 * - Connects to `wss://ws-subscriptions-clob.polymarket.com/ws/market`.
 * - Handles automatic reconnection with exponential backoff.
 * - Manages subscriptions (subscribing to assets/markets).
 * - Thread-safe writing.
 *
 * @dependencies
 * - github.com/gorilla/websocket
 * - backend/internal/config
 */

package rtds

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/logger"
	"github.com/gorilla/websocket"
)

const (
	// The CLOB Market Channel Endpoint
	// Doc: wss://ws-subscriptions-clob.polymarket.com/ws/market
	MarketChannelURL = "wss://ws-subscriptions-clob.polymarket.com/ws/market"

	WriteWait             = 10 * time.Second
	PongWait              = 60 * time.Second
	PingPeriod            = (PongWait * 9) / 10
	MaxConnectRetries     = 5
	maxAssetsPerSubscribe = 400
	queueDropLogInterval  = 2 * time.Second
	priceFlushInterval    = 75 * time.Millisecond
)

type SubscriptionMessage struct {
	Type     string   `json:"type"`       // "market"
	AssetIDs []string `json:"assets_ids"` // Note: API uses "assets_ids" (plural) not "asset_ids"
}

type Client struct {
	url     string
	conn    *websocket.Conn
	mu      sync.Mutex
	done    chan struct{}
	handler *MessageHandler

	workerPoolSize int
	messageQueue   chan []byte
	workersOnce    sync.Once
	priceOnce      sync.Once

	priceMu     sync.Mutex
	priceLatest map[string][]byte

	// subscriptions holds the current list of asset IDs to track
	subscriptions []string
	subMu         sync.Mutex

	// reconnecting prevents multiple simultaneous reconnection attempts
	reconnecting bool
	reconnectMu  sync.Mutex

	queueDropMu     sync.Mutex
	queueDropped    int
	queueDropWindow time.Time
}

func NewClient(cfg *config.Config, handler *MessageHandler) *Client {
	workerPoolSize := 128
	queueSize := 16384
	if cfg != nil {
		if cfg.Services.RTDSWorkerPoolSize > 0 {
			workerPoolSize = cfg.Services.RTDSWorkerPoolSize
		}
		if cfg.Services.RTDSQueueSize > 0 {
			queueSize = cfg.Services.RTDSQueueSize
		}
	}

	// We use the specific market channel URL - this is a fixed endpoint
	return &Client{
		url:            MarketChannelURL,
		handler:        handler,
		done:           make(chan struct{}),
		workerPoolSize: workerPoolSize,
		messageQueue:   make(chan []byte, queueSize),
		priceLatest:    make(map[string][]byte),
	}
}

// Connect establishes the WebSocket connection and starts the read loop
func (c *Client) Connect(ctx context.Context) error {
	c.startWorkers(ctx)
	return c.connectWithRetry(ctx)
}

func (c *Client) startWorkers(ctx context.Context) {
	c.workersOnce.Do(func() {
		for i := 0; i < c.workerPoolSize; i++ {
			go func(workerID int) {
				for {
					select {
					case <-ctx.Done():
						return
					case <-c.done:
						return
					case msg := <-c.messageQueue:
						if err := c.handler.HandleMessage(ctx, msg); err != nil {
							logger.Warn("market RTDS worker[%d] message handling failed: %v", workerID, err)
						}
					}
				}
			}(i + 1)
		}
	})

	c.priceOnce.Do(func() {
		go c.runPriceCoalescer(ctx)
	})
}

func (c *Client) connectWithRetry(ctx context.Context) error {
	var err error
	backoff := 1 * time.Second

	for i := 0; i < MaxConnectRetries; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.done:
			return fmt.Errorf("client closed")
		default:
		}

		logger.Info("market RTDS connecting: url=%s attempt=%d", c.url, i+1)
		c.conn, _, err = websocket.DefaultDialer.Dial(c.url, nil)
		if err == nil {
			logger.Info("market RTDS connected")

			// Resubscribe if we have existing subscriptions (reconnection scenario)
			c.subMu.Lock()
			if len(c.subscriptions) > 0 {
				go c.sendSubscribe(c.subscriptions)
			}
			c.subMu.Unlock()

			go c.readLoop(ctx)
			go c.pingLoop(ctx)
			return nil
		}

		logger.Warn("market RTDS connect failed: err=%v retry_in=%v", err, backoff)
		time.Sleep(backoff)
		backoff *= 2
	}

	return fmt.Errorf("failed to connect after %d attempts: %w", MaxConnectRetries, err)
}

// Subscribe adds assets to the tracking list and sends the subscription message
func (c *Client) Subscribe(assetIDs []string) error {
	ids := dedupeAssetIDs(assetIDs)
	if len(ids) == 0 {
		return nil
	}

	newIDs := make([]string, 0, len(ids))
	c.subMu.Lock()
	existing := make(map[string]struct{}, len(c.subscriptions)+len(ids))
	for _, id := range c.subscriptions {
		existing[id] = struct{}{}
	}
	for _, id := range ids {
		if _, ok := existing[id]; ok {
			continue
		}
		existing[id] = struct{}{}
		c.subscriptions = append(c.subscriptions, id)
		newIDs = append(newIDs, id)
	}
	c.subMu.Unlock()

	if len(newIDs) == 0 {
		return nil
	}

	return c.sendSubscribe(newIDs)
}

func (c *Client) sendSubscribe(assets []string) error {
	if len(assets) == 0 {
		return nil
	}

	for start := 0; start < len(assets); start += maxAssetsPerSubscribe {
		end := start + maxAssetsPerSubscribe
		if end > len(assets) {
			end = len(assets)
		}

		msg := SubscriptionMessage{
			Type:     "market",
			AssetIDs: assets[start:end],
		}

		if err := c.WriteJSON(msg); err != nil {
			return err
		}

		// avoid spamming the gateway with back-to-back large messages
		time.Sleep(25 * time.Millisecond)
	}

	return nil
}

// ReplaceSubscriptions swaps the entire tracked asset list atomically.
func (c *Client) ReplaceSubscriptions(assetIDs []string) error {
	ids := dedupeAssetIDs(assetIDs)

	c.subMu.Lock()
	c.subscriptions = ids
	c.subMu.Unlock()

	return c.sendSubscribe(ids)
}

// WriteJSON sends a JSON message to the websocket thread-safely
func (c *Client) WriteJSON(v interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("connection is nil")
	}

	c.conn.SetWriteDeadline(time.Now().Add(WriteWait))
	return c.conn.WriteJSON(v)
}

// Close gracefully closes the connection
func (c *Client) Close() error {
	close(c.done)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) readLoop(ctx context.Context) {
	defer func() {
		c.mu.Lock()
		if c.conn != nil {
			c.conn.Close()
			c.conn = nil
		}
		c.mu.Unlock()

		// Trigger reconnection if context is not done and client is not closed
		select {
		case <-c.done:
			return
		case <-ctx.Done():
			return
		default:
			// Only reconnect if not already reconnecting
			c.reconnectMu.Lock()
			if !c.reconnecting {
				c.reconnecting = true
				c.reconnectMu.Unlock()
				logger.Warn("market RTDS connection lost; reconnecting")
				go func() {
					defer func() {
						c.reconnectMu.Lock()
						c.reconnecting = false
						c.reconnectMu.Unlock()
					}()
					if err := c.connectWithRetry(ctx); err != nil {
						logger.Error("market RTDS reconnection failed: %v", err)
					}
				}()
			} else {
				c.reconnectMu.Unlock()
			}
		}
	}()

	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return
	}

	conn.SetReadLimit(1024 * 1024 * 10) // 10MB limit
	conn.SetReadDeadline(time.Now().Add(PongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(PongWait))
		return nil
	})

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		default:
			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					logger.Warn("market RTDS read error: %v", err)
				}
				return
			}

			if c.handler != nil {
				if key, isPrice := c.handler.PriceCoalesceKey(message); isPrice {
					if key != "" {
						c.enqueueCoalescedPrice(key, message)
					}
					continue
				}
				if !c.handler.ShouldEnqueueMessage(message) {
					continue
				}
			}

			// Bounded queue to cap concurrent handlers under load.
			msgCopy := append([]byte(nil), message...)
			if dropped := c.enqueueMessage(msgCopy); dropped {
				c.logQueueDrop(dropped)
			}
		}
	}
}

func (c *Client) enqueueCoalescedPrice(key string, message []byte) {
	if strings.TrimSpace(key) == "" || len(message) == 0 {
		return
	}
	copyMsg := append([]byte(nil), message...)
	c.priceMu.Lock()
	c.priceLatest[key] = copyMsg
	c.priceMu.Unlock()
}

func (c *Client) drainCoalescedPrices() [][]byte {
	c.priceMu.Lock()
	if len(c.priceLatest) == 0 {
		c.priceMu.Unlock()
		return nil
	}

	out := make([][]byte, 0, len(c.priceLatest))
	for _, msg := range c.priceLatest {
		out = append(out, msg)
	}
	c.priceLatest = make(map[string][]byte, len(out))
	c.priceMu.Unlock()
	return out
}

func (c *Client) runPriceCoalescer(ctx context.Context) {
	if c.handler == nil {
		return
	}

	ticker := time.NewTicker(priceFlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case <-ticker.C:
			batch := c.drainCoalescedPrices()
			for _, msg := range batch {
				if err := c.handler.HandleMessage(ctx, msg); err != nil {
					logger.Warn("market RTDS price coalescer handling failed: %v", err)
				}
			}
		}
	}
}

// enqueueMessage tries to enqueue the newest payload and, under pressure, evicts one stale queued item.
// It returns true when at least one message was dropped.
func (c *Client) enqueueMessage(message []byte) bool {
	select {
	case c.messageQueue <- message:
		return false
	default:
	}

	dropped := false
	select {
	case <-c.messageQueue:
		dropped = true
	default:
	}

	select {
	case c.messageQueue <- message:
		return dropped
	default:
		return true
	}
}

func (c *Client) logQueueDrop(dropped bool) {
	if !dropped {
		return
	}

	now := time.Now().UTC()
	c.queueDropMu.Lock()
	defer c.queueDropMu.Unlock()

	c.queueDropped++
	if c.queueDropWindow.IsZero() {
		c.queueDropWindow = now
		return
	}

	if now.Sub(c.queueDropWindow) < queueDropLogInterval {
		return
	}

	window := now.Sub(c.queueDropWindow).Round(time.Millisecond)
	logger.Warn(
		"market RTDS queue pressure (cap=%d workers=%d): dropped=%d in %s",
		cap(c.messageQueue),
		c.workerPoolSize,
		c.queueDropped,
		window,
	)
	c.queueDropped = 0
	c.queueDropWindow = now
}

func (c *Client) pingLoop(ctx context.Context) {
	ticker := time.NewTicker(PingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case <-ticker.C:
			c.mu.Lock()
			if c.conn == nil {
				c.mu.Unlock()
				return
			}
			c.conn.SetWriteDeadline(time.Now().Add(WriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.mu.Unlock()
				return
			}
			c.mu.Unlock()
		}
	}
}

func dedupeAssetIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(ids))
	unique := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}
