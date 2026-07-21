package render

import (
	"sync"
	"time"
)

// Clock allows time to be injected for testing.
type Clock interface {
	Now() time.Time
}

// SystemClock returns the current system time.
type SystemClock struct{}

func (sc SystemClock) Now() time.Time {
	return time.Now()
}

// entry represents a cached value with its expiration time.
type entry struct {
	value     interface{}
	expiresAt time.Time
	createdAt time.Time
}

// TTLCache is a bounded, time-limited cache for storing values.
// It evicts oldest entries when the size limit is reached.
type TTLCache struct {
	mu       sync.RWMutex
	entries  map[string]*entry
	maxSize  int
	clock    Clock
}

// NewTTLCache creates a new TTL cache with a maximum size.
// The cache is bounded by size; when full, the oldest entry is evicted.
func NewTTLCache(maxSize int, clock Clock) *TTLCache {
	if clock == nil {
		clock = SystemClock{}
	}
	return &TTLCache{
		entries: make(map[string]*entry),
		maxSize: maxSize,
		clock:   clock,
	}
}

// Get retrieves a value from the cache if it exists and has not expired.
// Returns (nil, false) if the key is not found or has expired.
func (c *TTLCache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}

	// Check if the entry has expired. At or after the expiration time, it's expired.
	if !c.clock.Now().Before(e.expiresAt) {
		return nil, false
	}

	return e.value, true
}

// Set stores a value in the cache with a TTL duration.
// If the cache is at max size when adding a new entry, the oldest entry is evicted.
func (c *TTLCache) Set(key string, value interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.clock.Now()

	// Check if this is an update (key already exists) or a new entry.
	isNewEntry := c.entries[key] == nil

	// If adding a new entry and we're at max size, evict the oldest entry.
	if isNewEntry && len(c.entries) >= c.maxSize {
		c.evictOldest()
	}

	// Store the new entry.
	c.entries[key] = &entry{
		value:     value,
		expiresAt: now.Add(ttl),
		createdAt: now,
	}
}

// evictOldest removes the oldest entry from the cache.
// This should only be called while holding the write lock.
func (c *TTLCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for key, e := range c.entries {
		if oldestKey == "" || e.createdAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = e.createdAt
		}
	}

	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}

// Clear removes all entries from the cache.
// Useful for testing and cleanup.
func (c *TTLCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*entry)
}

// Size returns the current number of entries in the cache.
func (c *TTLCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}
