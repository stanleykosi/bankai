package rtds

import (
	"sync"
	"time"
)

// CacheAllowlist tracks which asset IDs should be cached, with TTL-based expiry.
// This lets the worker keep RTDS subscriptions for all markets while only caching
// data for assets that are actively requested by the app.
type CacheAllowlist struct {
	mu      sync.RWMutex
	entries map[string]time.Time
	ttl     time.Duration
}

func NewCacheAllowlist(ttl time.Duration) *CacheAllowlist {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	return &CacheAllowlist{
		entries: make(map[string]time.Time),
		ttl:     ttl,
	}
}

func (a *CacheAllowlist) Allow(tokens []string) {
	if a == nil {
		return
	}
	now := time.Now()
	exp := now.Add(a.ttl)
	a.mu.Lock()
	for _, token := range tokens {
		if token == "" {
			continue
		}
		a.entries[token] = exp
	}
	a.mu.Unlock()
}

func (a *CacheAllowlist) IsAllowed(token string) bool {
	if a == nil {
		return false
	}
	a.mu.RLock()
	exp, ok := a.entries[token]
	a.mu.RUnlock()
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		a.mu.Lock()
		delete(a.entries, token)
		a.mu.Unlock()
		return false
	}
	return true
}
