package rtds

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/services"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

const (
	ActivityChannelURL = "wss://ws-live-data.polymarket.com"
)

type ActivityClient struct {
	url     string
	conn    *websocket.Conn
	mu      sync.Mutex
	done    chan struct{}
	handler *ActivityHandler

	auth *activityClobAuth

	reconnecting bool
	reconnectMu  sync.Mutex
}

type activityClobAuth struct {
	Key        string `json:"key"`
	Secret     string `json:"secret"`
	Passphrase string `json:"passphrase"`
}

type activitySubscription struct {
	Topic    string            `json:"topic"`
	Type     string            `json:"type"`
	Filters  string            `json:"filters,omitempty"`
	ClobAuth *activityClobAuth `json:"clob_auth,omitempty"`
}

type activitySubscribeMessage struct {
	Action        string                 `json:"action"`
	Subscriptions []activitySubscription `json:"subscriptions"`
}

func NewActivityClient(cfg *config.Config, handler *ActivityHandler) *ActivityClient {
	var auth *activityClobAuth
	if cfg != nil {
		if cfg.Polymarket.BuilderAPIKey != "" && cfg.Polymarket.BuilderSecret != "" && cfg.Polymarket.BuilderPass != "" {
			auth = &activityClobAuth{
				Key:        cfg.Polymarket.BuilderAPIKey,
				Secret:     cfg.Polymarket.BuilderSecret,
				Passphrase: cfg.Polymarket.BuilderPass,
			}
		}
	}

	return &ActivityClient{
		url:     ActivityChannelURL,
		handler: handler,
		done:    make(chan struct{}),
		auth:    auth,
	}
}

func (c *ActivityClient) Connect(ctx context.Context) error {
	if c.auth == nil {
		return fmt.Errorf("activity stream requires builder credentials")
	}
	return c.connectWithRetry(ctx)
}

func (c *ActivityClient) connectWithRetry(ctx context.Context) error {
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

		log.Printf("Connecting to Polymarket RTDS: %s (Attempt %d)", c.url, i+1)
		c.conn, _, err = websocket.DefaultDialer.Dial(c.url, nil)
		if err == nil {
			log.Println("✅ Connected to Polymarket RTDS")
			if err := c.sendSubscribe(); err != nil {
				log.Printf("RTDS subscribe failed: %v", err)
			}
			go c.readLoop(ctx)
			go c.pingLoop(ctx)
			return nil
		}

		log.Printf("Failed to connect to RTDS: %v. Retrying in %v...", err, backoff)
		time.Sleep(backoff)
		backoff *= 2
	}

	return fmt.Errorf("failed to connect after %d attempts: %w", MaxConnectRetries, err)
}

func (c *ActivityClient) sendSubscribe() error {
	msg := activitySubscribeMessage{
		Action: "subscribe",
		Subscriptions: []activitySubscription{
			{
				Topic:    "activity",
				Type:     "orders_matched",
				ClobAuth: c.auth,
			},
		},
	}
	return c.WriteJSON(msg)
}

func (c *ActivityClient) WriteJSON(v interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("connection is nil")
	}

	c.conn.SetWriteDeadline(time.Now().Add(WriteWait))
	return c.conn.WriteJSON(v)
}

func (c *ActivityClient) Close() error {
	close(c.done)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *ActivityClient) readLoop(ctx context.Context) {
	defer func() {
		c.mu.Lock()
		if c.conn != nil {
			c.conn.Close()
			c.conn = nil
		}
		c.mu.Unlock()

		select {
		case <-c.done:
			return
		case <-ctx.Done():
			return
		default:
			c.reconnectMu.Lock()
			if !c.reconnecting {
				c.reconnecting = true
				c.reconnectMu.Unlock()
				log.Println("RTDS connection lost, reconnecting...")
				go func() {
					defer func() {
						c.reconnectMu.Lock()
						c.reconnecting = false
						c.reconnectMu.Unlock()
					}()
					if err := c.connectWithRetry(ctx); err != nil {
						log.Printf("RTDS reconnection failed: %v", err)
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

	conn.SetReadLimit(1024 * 1024 * 10)
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
					log.Printf("RTDS read error: %v", err)
				}
				return
			}

			go func(msg []byte) {
				if err := c.handler.HandleMessage(ctx, msg); err != nil {
					log.Printf("RTDS handler error: %v", err)
				}
			}(message)
		}
	}
}

func (c *ActivityClient) pingLoop(ctx context.Context) {
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

type ActivityHandler struct {
	Redis     *redis.Client
	AlphaHub  *services.AlphaHubService
	cacheMu   sync.Mutex
	cache     map[string]services.WalletSnapshot
	dedupeTTL time.Duration
}

func NewActivityHandler(redis *redis.Client, alphaHub *services.AlphaHubService) *ActivityHandler {
	return &ActivityHandler{
		Redis:     redis,
		AlphaHub:  alphaHub,
		cache:     make(map[string]services.WalletSnapshot),
		dedupeTTL: 24 * time.Hour,
	}
}

type activityEnvelope struct {
	Topic        string          `json:"topic"`
	Type         string          `json:"type"`
	Timestamp    int64           `json:"timestamp"`
	Payload      activityPayload `json:"payload"`
	ConnectionID string          `json:"connection_id,omitempty"`
}

type activityPayload struct {
	Asset           string      `json:"asset"`
	ConditionID     string      `json:"conditionId"`
	EventSlug       string      `json:"eventSlug"`
	Slug            string      `json:"slug"`
	Title           string      `json:"title"`
	Icon            string      `json:"icon"`
	Image           string      `json:"image"`
	Side            string      `json:"side"`
	Price           json.Number `json:"price"`
	Size            json.Number `json:"size"`
	ProxyWallet     string      `json:"proxyWallet"`
	Timestamp       int64       `json:"timestamp"`
	TransactionHash string      `json:"transactionHash"`
}

func (h *ActivityHandler) HandleMessage(ctx context.Context, msg []byte) error {
	msg = bytes.TrimSpace(msg)
	if len(msg) == 0 {
		return nil
	}

	switch msg[0] {
	case '{', '[':
	default:
		text := strings.ToUpper(string(msg))
		if text == "PING" || text == "PONG" {
			return nil
		}
		return nil
	}

	if msg[0] == '[' {
		var batch []json.RawMessage
		if err := json.Unmarshal(msg, &batch); err != nil {
			return fmt.Errorf("failed to parse RTDS batch: %w", err)
		}
		for _, raw := range batch {
			if err := h.HandleMessage(ctx, raw); err != nil {
				log.Printf("RTDS batch item failed: %v", err)
			}
		}
		return nil
	}

	var env activityEnvelope
	decoder := json.NewDecoder(bytes.NewReader(msg))
	decoder.UseNumber()
	if err := decoder.Decode(&env); err != nil {
		return fmt.Errorf("failed to decode RTDS message: %w", err)
	}

	if env.Topic != "activity" || env.Type != "orders_matched" {
		return nil
	}

	h.touchHeartbeat(ctx)

	payload := env.Payload
	if payload.ConditionID == "" || payload.Asset == "" {
		return nil
	}
	wallet := strings.ToLower(strings.TrimSpace(payload.ProxyWallet))
	price := parseNumber(payload.Price)
	size := parseNumber(payload.Size)
	if price <= 0 || size <= 0 {
		return nil
	}
	volume := price * size

	if volume < services.WhaleThresholdUSD {
		return nil
	}

	matchTime := payload.Timestamp
	if matchTime == 0 && env.Timestamp > 0 {
		matchTime = env.Timestamp / 1000
	}
	if matchTime == 0 {
		matchTime = time.Now().Unix()
	}

	if !h.shouldProcessTransaction(ctx, payload.TransactionHash) {
		return nil
	}

	snapshot := h.lookupWalletSnapshot(ctx, wallet)
	tier := snapshot.Tier
	if tier == "" {
		tier = "Bronze"
	}

	event := services.WhaleEvent{
		Timestamp:   time.Unix(matchTime, 0).UTC(),
		MarketID:    payload.ConditionID,
		TokenID:     payload.Asset,
		Side:        strings.ToUpper(payload.Side),
		SizeUSD:     volume,
		Price:       price,
		Wallet:      wallet,
		WalletTier:  tier,
		WinRate:     snapshot.WinRate,
		RealizedPnL: snapshot.RealizedPnL,
		Slug:        payload.Slug,
		Title:       payload.Title,
		MarketIcon:  payload.Icon,
		MarketImage: payload.Image,
		IsWashTrade: false,
	}

	h.publishWhale(ctx, event)
	return nil
}

func (h *ActivityHandler) touchHeartbeat(ctx context.Context) {
	if h.Redis == nil {
		return
	}
	_ = h.Redis.Set(ctx, services.WhaleRTDSHeartbeatKey, time.Now().Unix(), services.WhaleRTDSHeartbeatTTL).Err()
}

func (h *ActivityHandler) shouldProcessTransaction(ctx context.Context, hash string) bool {
	if h.Redis == nil {
		return true
	}
	hash = strings.ToLower(strings.TrimSpace(hash))
	if hash == "" {
		return true
	}
	key := fmt.Sprintf("analysis:whales:tx:%s", hash)
	ok, err := h.Redis.SetNX(ctx, key, "1", h.dedupeTTL).Result()
	if err != nil {
		return true
	}
	return ok
}

func (h *ActivityHandler) lookupWalletSnapshot(ctx context.Context, wallet string) services.WalletSnapshot {
	if wallet == "" || h.AlphaHub == nil {
		return services.WalletSnapshot{Address: wallet}
	}

	h.cacheMu.Lock()
	snapshot, ok := h.cache[wallet]
	h.cacheMu.Unlock()
	if ok {
		return snapshot
	}

	snapshot = h.AlphaHub.GetWalletSnapshot(ctx, wallet)

	h.cacheMu.Lock()
	h.cache[wallet] = snapshot
	h.cacheMu.Unlock()
	return snapshot
}

func (h *ActivityHandler) publishWhale(ctx context.Context, event services.WhaleEvent) {
	if h.Redis == nil {
		return
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
		log.Printf("Redis whale update error: %v", err)
	}
}

func parseNumber(value json.Number) float64 {
	if value == "" {
		return 0
	}
	if f, err := value.Float64(); err == nil {
		return f
	}
	return 0
}
