package portable

import (
	"strings"
	"sync"
	"time"
)

// winOptPathRTTCache keeps a short-lived, bounded history of observed RTTs.
// Unlike the old preconnect capacity cache, this never stores SO_RCVBUF or
// touches a TCP socket before its handshake. Linux's TCP metrics cache
// inspired the separation, but this is NOT the Linux kernel cache.
type winOptPathRTTCache struct {
	mu         sync.Mutex
	entries    map[string]winOptPathRTTEntry
	maxEntries int
	ttl        time.Duration
}

type winOptPathRTTEntry struct {
	rtt     time.Duration
	expires time.Time
}

func newWinOptPathRTTCache(maxEntries int, ttl time.Duration) *winOptPathRTTCache {
	return &winOptPathRTTCache{entries: make(map[string]winOptPathRTTEntry), maxEntries: maxEntries, ttl: ttl}
}

func (c *winOptPathRTTCache) remember(key string, measured time.Duration, now time.Time) {
	if key == "" || measured < time.Millisecond || measured > 10*time.Second || c.maxEntries <= 0 || c.ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if old, ok := c.entries[key]; ok && now.Before(old.expires) {
		// An integer EWMA to avoid letting one transient RTT spike replace
		// the recent path estimate. Never change a TCP socket's RTT or RTO.
		measured = old.rtt + (measured-old.rtt)/8
	}
	if _, exists := c.entries[key]; !exists && len(c.entries) >= c.maxEntries {
		// Remove expired entries first; otherwise evict the oldest expiry.
		oldestKey := ""
		var oldest time.Time
		for k, v := range c.entries {
			if !now.Before(v.expires) {
				delete(c.entries, k)
				continue
			}
			if oldestKey == "" || v.expires.Before(oldest) {
				oldestKey, oldest = k, v.expires
			}
		}
		if len(c.entries) >= c.maxEntries && oldestKey != "" {
			delete(c.entries, oldestKey)
		}
	}
	c.entries[key] = winOptPathRTTEntry{rtt: measured, expires: now.Add(c.ttl)}
}

func (c *winOptPathRTTCache) lookup(key string, now time.Time) time.Duration {
	if key == "" {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	item, found := c.entries[key]
	if !found {
		return 0
	}
	if !now.Before(item.expires) {
		delete(c.entries, key)
		return 0
	}
	return item.rtt
}

// Capacity reuse is opt-in, because SO_RCVBUF before connect may modify
// SYN window/scale negotiation. By default *only* path metrics are reused.
func winOptPreconnectCapacityEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "on", "true", "enabled":
		return true
	}
	return false
}

var winOptRTTHistory = newWinOptPathRTTCache(256, 10*time.Minute)
