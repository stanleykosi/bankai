package rtds

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bankai-project/backend/internal/logger"
	"github.com/bankai-project/backend/internal/services"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

const chainlinkTopic = "crypto_prices_chainlink"

type ChainlinkClient struct {
	url     string
	conn    *websocket.Conn
	mu      sync.Mutex
	done    chan struct{}
	handler *ChainlinkHandler

	reconnecting bool
	reconnectMu  sync.Mutex
}

type chainlinkSubscription struct {
	Topic   string `json:"topic"`
	Type    string `json:"type"`
	Filters string `json:"filters,omitempty"`
}

type chainlinkSubscribeMessage struct {
	Action        string                  `json:"action"`
	Subscriptions []chainlinkSubscription `json:"subscriptions"`
}

type chainlinkEnvelope struct {
	Topic     string          `json:"topic"`
	Type      string          `json:"type"`
	Timestamp int64           `json:"timestamp"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type chainlinkPricePayload struct {
	Symbol    string  `json:"symbol"`
	Timestamp int64   `json:"timestamp"`
	Value     float64 `json:"value"`
}

type ChainlinkHandler struct {
	Redis *redis.Client
}

func NewChainlinkClient(handler *ChainlinkHandler) *ChainlinkClient {
	return &ChainlinkClient{
		url:     ActivityChannelURL,
		done:    make(chan struct{}),
		handler: handler,
	}
}

func NewChainlinkHandler(redis *redis.Client) *ChainlinkHandler {
	return &ChainlinkHandler{Redis: redis}
}

func (c *ChainlinkClient) Connect(ctx context.Context) error {
	return c.connectWithRetry(ctx)
}

func (c *ChainlinkClient) connectWithRetry(ctx context.Context) error {
	var err error
	backoff := time.Second

	for i := 0; i < MaxConnectRetries; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.done:
			return fmt.Errorf("client closed")
		default:
		}

		logger.Info("chainlink RTDS connecting: url=%s attempt=%d", c.url, i+1)
		c.conn, _, err = websocket.DefaultDialer.Dial(c.url, nil)
		if err == nil {
			logger.Info("chainlink RTDS connected")
			if err := c.sendSubscribe(); err != nil {
				logger.Warn("chainlink RTDS subscribe failed: %v", err)
			}
			go c.readLoop(ctx)
			go c.pingLoop(ctx)
			return nil
		}

		logger.Warn("chainlink RTDS connect failed: err=%v retry_in=%v", err, backoff)
		time.Sleep(backoff)
		backoff *= 2
	}

	return fmt.Errorf("failed to connect after %d attempts: %w", MaxConnectRetries, err)
}

func (c *ChainlinkClient) sendSubscribe() error {
	symbols := []string{"btc/usd", "eth/usd", "sol/usd", "xrp/usd"}
	subs := make([]chainlinkSubscription, 0, len(symbols)+1)
	subs = append(subs, chainlinkSubscription{
		Topic:   chainlinkTopic,
		Type:    "*",
		Filters: "",
	})
	for _, symbol := range symbols {
		filterBytes, _ := json.Marshal(map[string]string{"symbol": symbol})
		subs = append(subs, chainlinkSubscription{
			Topic:   chainlinkTopic,
			Type:    "*",
			Filters: string(filterBytes),
		})
	}

	msg := chainlinkSubscribeMessage{
		Action:        "subscribe",
		Subscriptions: subs,
	}
	return c.WriteJSON(msg)
}

func (c *ChainlinkClient) WriteJSON(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("connection is nil")
	}

	c.conn.SetWriteDeadline(time.Now().Add(WriteWait))
	return c.conn.WriteJSON(v)
}

func (c *ChainlinkClient) Close() error {
	close(c.done)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *ChainlinkClient) readLoop(ctx context.Context) {
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
				logger.Warn("chainlink RTDS connection lost; reconnecting")
				go func() {
					defer func() {
						c.reconnectMu.Lock()
						c.reconnecting = false
						c.reconnectMu.Unlock()
					}()
					if err := c.connectWithRetry(ctx); err != nil {
						logger.Error("chainlink RTDS reconnection failed: %v", err)
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

	conn.SetReadLimit(1024 * 1024)
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
					logger.Warn("chainlink RTDS read error: %v", err)
				}
				return
			}

			go func(msg []byte) {
				if err := c.handler.HandleMessage(ctx, msg); err != nil {
					logger.Warn("chainlink RTDS handler error: %v", err)
				}
			}(message)
		}
	}
}

func (c *ChainlinkClient) pingLoop(ctx context.Context) {
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

func (h *ChainlinkHandler) HandleMessage(ctx context.Context, msg []byte) error {
	if h == nil || h.Redis == nil {
		return nil
	}
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
			return fmt.Errorf("failed to parse chainlink RTDS batch: %w", err)
		}
		for _, raw := range batch {
			if err := h.HandleMessage(ctx, raw); err != nil {
				logger.Warn("chainlink RTDS batch item failed: %v", err)
			}
		}
		return nil
	}

	var env chainlinkEnvelope
	if err := json.Unmarshal(msg, &env); err != nil {
		return fmt.Errorf("failed to decode chainlink RTDS message: %w", err)
	}
	point, ok := parseChainlinkEnvelope(env)
	if !ok {
		return nil
	}
	return h.persistPoint(ctx, point)
}

func (h *ChainlinkHandler) persistPoint(ctx context.Context, point services.OraclePricePoint) error {
	if point.Price <= 0 || point.Asset == "" || point.UpdatedAt.IsZero() {
		return nil
	}
	if err := services.StoreChainlinkLatest(ctx, h.Redis, point.Asset, point.Price, point.UpdatedAt); err != nil {
		return err
	}

	payload, err := json.Marshal(map[string]any{
		"asset_symbol": point.Asset,
		"price":        point.Price,
		"timestamp":    point.UpdatedAt.Format(time.RFC3339Nano),
	})
	if err != nil {
		return err
	}

	return h.Redis.Publish(ctx, services.ChainlinkPriceUpdateChannel, payload).Err()
}

func parseChainlinkEnvelope(env chainlinkEnvelope) (services.OraclePricePoint, bool) {
	if point, ok := parsePrimaryChainlinkPayload(env); ok {
		return point, true
	}
	if point, ok := parseCryptoPricesBatch(env); ok {
		return point, true
	}
	if point, ok := parseSymbolValueFallback(env); ok {
		return point, true
	}
	return services.OraclePricePoint{}, false
}

func parsePrimaryChainlinkPayload(env chainlinkEnvelope) (services.OraclePricePoint, bool) {
	if env.Topic != chainlinkTopic || len(env.Payload) == 0 {
		return services.OraclePricePoint{}, false
	}
	var payload chainlinkPricePayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return services.OraclePricePoint{}, false
	}
	return buildOraclePricePoint(payload.Symbol, payload.Value, payload.Timestamp, env.Timestamp)
}

func parseCryptoPricesBatch(env chainlinkEnvelope) (services.OraclePricePoint, bool) {
	if env.Topic != "crypto_prices" || len(env.Payload) == 0 {
		return services.OraclePricePoint{}, false
	}

	var payload struct {
		Symbol string `json:"symbol"`
		Data   []struct {
			Timestamp int64   `json:"timestamp"`
			Value     float64 `json:"value"`
		} `json:"data"`
	}
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return services.OraclePricePoint{}, false
	}
	if !strings.Contains(payload.Symbol, "/") {
		return services.OraclePricePoint{}, false
	}
	if len(payload.Data) == 0 {
		return services.OraclePricePoint{}, false
	}
	last := payload.Data[len(payload.Data)-1]
	return buildOraclePricePoint(payload.Symbol, last.Value, last.Timestamp, env.Timestamp)
}

func parseSymbolValueFallback(env chainlinkEnvelope) (services.OraclePricePoint, bool) {
	if len(env.Payload) == 0 {
		return services.OraclePricePoint{}, false
	}
	var payload chainlinkPricePayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return services.OraclePricePoint{}, false
	}
	if !strings.Contains(payload.Symbol, "/") {
		return services.OraclePricePoint{}, false
	}
	return buildOraclePricePoint(payload.Symbol, payload.Value, payload.Timestamp, env.Timestamp)
}

func buildOraclePricePoint(symbol string, value float64, payloadTS int64, envelopeTS int64) (services.OraclePricePoint, bool) {
	if value <= 0 {
		return services.OraclePricePoint{}, false
	}
	asset := services.CanonicalOracleAsset(symbol)
	if asset == "" {
		return services.OraclePricePoint{}, false
	}

	tsMillis := payloadTS
	if tsMillis <= 0 {
		tsMillis = envelopeTS
	}
	if tsMillis > 0 && tsMillis < 1_000_000_000_000 {
		tsMillis *= 1000
	}
	if tsMillis <= 0 {
		tsMillis = time.Now().UTC().UnixMilli()
	}

	return services.OraclePricePoint{
		Asset:     asset,
		Price:     value,
		UpdatedAt: time.UnixMilli(tsMillis).UTC(),
	}, true
}
