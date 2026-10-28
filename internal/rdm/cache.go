package rdm

import (
	"sync"
	"time"
)

type cacheItem struct {
	body    []byte
	expires time.Time
}

// cache is a small TTL cache of raw response bodies. A nil *cache is a
// disabled cache.
type cache struct {
	mu    sync.Mutex
	ttl   time.Duration
	now   func() time.Time
	items map[string]cacheItem
}

func newCache(ttl time.Duration, now func() time.Time) *cache {
	if ttl < 0 {
		return nil
	}
	return &cache{ttl: ttl, now: now, items: make(map[string]cacheItem)}
}

func (c *cache) get(key string) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	it, ok := c.items[key]
	if !ok {
		return nil, false
	}
	if !c.now().Before(it.expires) {
		delete(c.items, key)
		return nil, false
	}
	return it.body, true
}

func (c *cache) put(key string, body []byte) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	if _, exists := c.items[key]; !exists && len(c.items) >= maxCacheSize {
		for k, it := range c.items {
			if !now.Before(it.expires) {
				delete(c.items, k)
			}
		}
		for k := range c.items {
			if len(c.items) < maxCacheSize {
				break
			}
			delete(c.items, k)
		}
	}
	c.items[key] = cacheItem{body: body, expires: now.Add(c.ttl)}
}
