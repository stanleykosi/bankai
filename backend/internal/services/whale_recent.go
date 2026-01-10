package services

import (
	"context"
	"encoding/json"

	"github.com/redis/go-redis/v9"
)

func (s *AlphaHubService) GetRecentWhales(ctx context.Context, limit int) ([]WhaleEvent, error) {
	if s.redis == nil {
		return []WhaleEvent{}, nil
	}

	if limit <= 0 {
		limit = 15
	}
	if limit > WhaleRecentListMax {
		limit = WhaleRecentListMax
	}

	raw, err := s.redis.LRange(ctx, WhaleRecentListKey, 0, int64(limit-1)).Result()
	if err != nil {
		if err == redis.Nil {
			return []WhaleEvent{}, nil
		}
		return nil, err
	}

	whales := make([]WhaleEvent, 0, len(raw))
	for _, entry := range raw {
		var whale WhaleEvent
		if err := json.Unmarshal([]byte(entry), &whale); err != nil {
			continue
		}
		whales = append(whales, whale)
	}

	return whales, nil
}
