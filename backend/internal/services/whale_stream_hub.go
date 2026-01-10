package services

import (
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	WhaleUpdateChannel = "analysis:whale_updates"
	WhaleRecentListKey = "analysis:whales:recent"
	WhaleRecentListMax = 200
	WhaleRecentListTTL = 6 * time.Hour
	WhaleThresholdUSD  = 1_000.0
)

type WhaleStreamHub = PriceStreamHub

func NewWhaleStreamHub(redis *redis.Client) *WhaleStreamHub {
	return NewPriceStreamHub(redis, WhaleUpdateChannel)
}
