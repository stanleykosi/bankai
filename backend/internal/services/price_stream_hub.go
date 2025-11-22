package services

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// PriceStreamHub multiplexes Redis pub/sub messages to many SSE clients without spawning
// a Redis subscription per HTTP request.
type PriceStreamHub struct {
	redis       *redis.Client
	channelName string

	mu          sync.RWMutex
	subscribers map[chan []byte]struct{}
}

func NewPriceStreamHub(redis *redis.Client, channel string) *PriceStreamHub {
	hub := &PriceStreamHub{
		redis:       redis,
		channelName: channel,
		subscribers: make(map[chan []byte]struct{}),
	}

	go hub.run()

	return hub
}

func (h *PriceStreamHub) run() {
	ctx := context.Background()

	for {
		pubsub := h.redis.Subscribe(ctx, h.channelName)
		ch := pubsub.Channel(redis.WithChannelSize(16384))

		for msg := range ch {
			h.broadcast([]byte(msg.Payload))
		}

		_ = pubsub.Close()

		// Avoid tight loop if Redis connection drops
		time.Sleep(time.Second)
	}
}

func (h *PriceStreamHub) broadcast(payload []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for sub := range h.subscribers {
		select {
		case sub <- payload:
		default:
			// Subscriber is too slow; drop message to keep hub responsive
			select {
			case <-sub:
			default:
			}
			select {
			case sub <- payload:
			default:
			}
		}
	}
}

// Subscribe registers a new listener and returns a channel plus cleanup function.
func (h *PriceStreamHub) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 512)

	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()

	unsubscribe := func() {
		h.mu.Lock()
		if _, ok := h.subscribers[ch]; ok {
			delete(h.subscribers, ch)
			close(ch)
		}
		h.mu.Unlock()
	}

	return ch, unsubscribe
}
