/**
 * @description
 * Redis connection manager using go-redis.
 * Used for caching Gamma API responses, rate limiting, and pub/sub for Websockets.
 *
 * @dependencies
 * - github.com/redis/go-redis/v9
 */

package db

import (
	"context"
	"log"

	"github.com/bankai-project/backend/internal/config"
	"github.com/redis/go-redis/v9"
)

// ConnectRedis initializes the Redis client
func ConnectRedis(cfg *config.Config) (*redis.Client, error) {
	opt, err := redis.ParseURL(cfg.Redis.URL)
	if err != nil {
		return nil, err
	}

	client := redis.NewClient(opt)

	// Ping to verify connection
	ctx := context.Background()
	if _, err := client.Ping(ctx).Result(); err != nil {
		return nil, err
	}

	log.Println("✅ Connected to Redis")
	return client, nil
}

