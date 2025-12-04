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
	"log"
	"strings"
	"sync"
	"time"

	"github.com/bankai-project/backend/internal/config"
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

	// subscriptions holds the current list of asset IDs to track
	subscriptions []string
	subMu         sync.Mutex

	// reconnecting prevents multiple simultaneous reconnection attempts
	reconnecting bool
	reconnectMu  sync.Mutex
}

func NewClient(cfg *config.Config, handler *MessageHandler) *Client {
	// We use the specific market channel URL - this is a fixed endpoint
	return &Client{
		url:     MarketChannelURL,
		handler: handler,
		done:    make(chan struct{}),
	}
}

// Connect establishes the WebSocket connection and starts the read loop
func (c *Client) Connect(ctx context.Context) error {
	return c.connectWithRetry(ctx)
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

		log.Printf("Connecting to Polymarket WS: %s (Attempt %d)", c.url, i+1)
		c.conn, _, err = websocket.DefaultDialer.Dial(c.url, nil)
		if err == nil {
			log.Println("✅ Connected to Polymarket WS")

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

		log.Printf("Failed to connect: %v. Retrying in %v...", err, backoff)
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

	c.subMu.Lock()
	c.subscriptions = dedupeAssetIDs(append(c.subscriptions, ids...))
	c.subMu.Unlock()

	return c.sendSubscribe(ids)
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
				log.Println("WS Connection lost, reconnecting...")
				go func() {
					defer func() {
						c.reconnectMu.Lock()
						c.reconnecting = false
						c.reconnectMu.Unlock()
					}()
					if err := c.connectWithRetry(ctx); err != nil {
						log.Printf("Reconnection failed: %v", err)
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
					log.Printf("WS Read error: %v", err)
				}
				return
			}

			// Async process to not block reader
			go func(msg []byte) {
				if err := c.handler.HandleMessage(ctx, msg); err != nil {
					log.Printf("Error handling message: %v", err)
				}
			}(message)
		}
	}
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
