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
	"time"

	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/logger"
	"github.com/redis/go-redis/v9"
)

// ConnectRedis initializes the Redis client
func ConnectRedis(cfg *config.Config) (*redis.Client, error) {
	opt, err := redis.ParseURL(cfg.Redis.URL)
	if err != nil {
		return nil, err
	}

	if opt.ReadTimeout == 0 {
		opt.ReadTimeout = 5 * time.Second
	}
	if opt.WriteTimeout == 0 {
		opt.WriteTimeout = 5 * time.Second
	}
	if opt.DialTimeout == 0 {
		opt.DialTimeout = 5 * time.Second
	}
	if opt.PoolTimeout == 0 {
		opt.PoolTimeout = 5 * time.Second
	}
	if opt.MaxRetries == 0 {
		opt.MaxRetries = 2
	}
	if opt.MinRetryBackoff == 0 {
		opt.MinRetryBackoff = 200 * time.Millisecond
	}
	if opt.MaxRetryBackoff == 0 {
		opt.MaxRetryBackoff = 2 * time.Second
	}
	if opt.PoolSize == 0 {
		opt.PoolSize = 20
	}
	if opt.MinIdleConns == 0 {
		opt.MinIdleConns = 5
	}

	client := redis.NewClient(opt)

	// Ping to verify connection
	ctx := context.Background()
	if _, err := client.Ping(ctx).Result(); err != nil {
		return nil, err
	}

	logger.Info("✅ Connected to Redis")
	return client, nil
}
