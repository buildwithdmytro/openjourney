package render

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// MockClock allows time to be controlled in tests.
type MockClock struct {
	mu       sync.Mutex
	nowValue time.Time
}

func NewMockClock() *MockClock {
	return &MockClock{
		nowValue: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	}
}

func (mc *MockClock) Now() time.Time {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	return mc.nowValue
}

func (mc *MockClock) Advance(d time.Duration) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.nowValue = mc.nowValue.Add(d)
}

// TestCacheSetAndGet tests basic set and get operations.
func TestCacheSetAndGet(t *testing.T) {
	clock := NewMockClock()
	cache := NewTTLCache(10, clock)

	cache.Set("key1", "value1", 5*time.Minute)
	val, ok := cache.Get("key1")
	if !ok {
		t.Fatal("expected key1 to exist in cache")
	}
	if val != "value1" {
		t.Errorf("expected value1, got %v", val)
	}
}

// TestCacheExpiration tests that values expire after their TTL.
func TestCacheExpiration(t *testing.T) {
	clock := NewMockClock()
	cache := NewTTLCache(10, clock)

	cache.Set("key1", "value1", 5*time.Minute)

	// Value should exist before expiration.
	val, ok := cache.Get("key1")
	if !ok {
		t.Fatal("expected key1 to exist before expiration")
	}
	if val != "value1" {
		t.Errorf("expected value1, got %v", val)
	}

	// Advance time past the TTL.
	clock.Advance(6 * time.Minute)

	// Value should no longer exist after expiration.
	_, ok = cache.Get("key1")
	if ok {
		t.Fatal("expected key1 to be expired after TTL")
	}
}

// TestCacheExactExpiration tests expiration at the exact TTL boundary.
func TestCacheExactExpiration(t *testing.T) {
	clock := NewMockClock()
	cache := NewTTLCache(10, clock)

	cache.Set("key1", "value1", 5*time.Minute)

	// Advance exactly to the TTL duration (at the boundary).
	clock.Advance(5 * time.Minute)

	// At exactly the TTL, the value should still be expired (After is used, not >= ).
	// Since we use After(), the value at exactly TTL time should be expired.
	_, ok := cache.Get("key1")
	if ok {
		t.Fatal("expected key1 to be expired at TTL boundary")
	}
}

// TestCacheUpdate tests updating an existing key.
func TestCacheUpdate(t *testing.T) {
	clock := NewMockClock()
	cache := NewTTLCache(10, clock)

	cache.Set("key1", "value1", 5*time.Minute)
	cache.Set("key1", "value2", 10*time.Minute)

	val, ok := cache.Get("key1")
	if !ok {
		t.Fatal("expected key1 to exist after update")
	}
	if val != "value2" {
		t.Errorf("expected value2, got %v", val)
	}

	// Original TTL should be replaced; advance 7 minutes.
	clock.Advance(7 * time.Minute)

	// Value should still exist (new TTL is 10 minutes).
	val, ok = cache.Get("key1")
	if !ok {
		t.Fatal("expected key1 to exist after advancing 7 minutes")
	}
	if val != "value2" {
		t.Errorf("expected value2, got %v", val)
	}

	// Advance 4 more minutes (total 11).
	clock.Advance(4 * time.Minute)

	// Value should now be expired.
	_, ok = cache.Get("key1")
	if ok {
		t.Fatal("expected key1 to be expired after 11 minutes")
	}
}

// TestCacheEvictionWhenFull tests that the cache evicts the oldest entry when full.
func TestCacheEvictionWhenFull(t *testing.T) {
	clock := NewMockClock()
	cache := NewTTLCache(3, clock)

	// Fill the cache.
	cache.Set("key1", "value1", 5*time.Minute)
	clock.Advance(1 * time.Second)
	cache.Set("key2", "value2", 5*time.Minute)
	clock.Advance(1 * time.Second)
	cache.Set("key3", "value3", 5*time.Minute)

	if cache.Size() != 3 {
		t.Errorf("expected cache size 3, got %d", cache.Size())
	}

	// Add a fourth entry; the oldest (key1) should be evicted.
	clock.Advance(1 * time.Second)
	cache.Set("key4", "value4", 5*time.Minute)

	if cache.Size() != 3 {
		t.Errorf("expected cache size to remain 3 after eviction, got %d", cache.Size())
	}

	// key1 should be gone.
	_, ok := cache.Get("key1")
	if ok {
		t.Fatal("expected key1 to be evicted")
	}

	// Other keys should still exist.
	_, ok = cache.Get("key2")
	if !ok {
		t.Fatal("expected key2 to still exist")
	}

	_, ok = cache.Get("key3")
	if !ok {
		t.Fatal("expected key3 to still exist")
	}

	_, ok = cache.Get("key4")
	if !ok {
		t.Fatal("expected key4 to exist")
	}
}

// TestCacheEvictionMultiple tests eviction of multiple entries.
func TestCacheEvictionMultiple(t *testing.T) {
	clock := NewMockClock()
	cache := NewTTLCache(2, clock)

	cache.Set("key1", "value1", 5*time.Minute)
	clock.Advance(1 * time.Second)
	cache.Set("key2", "value2", 5*time.Minute)
	clock.Advance(1 * time.Second)

	// Adding key3 should evict key1.
	cache.Set("key3", "value3", 5*time.Minute)

	if cache.Size() != 2 {
		t.Errorf("expected cache size 2, got %d", cache.Size())
	}

	_, ok := cache.Get("key1")
	if ok {
		t.Fatal("expected key1 to be evicted")
	}

	// Adding key4 should evict key2 (now the oldest).
	clock.Advance(1 * time.Second)
	cache.Set("key4", "value4", 5*time.Minute)

	if cache.Size() != 2 {
		t.Errorf("expected cache size 2, got %d", cache.Size())
	}

	_, ok = cache.Get("key2")
	if ok {
		t.Fatal("expected key2 to be evicted")
	}

	_, ok = cache.Get("key3")
	if !ok {
		t.Fatal("expected key3 to still exist")
	}

	_, ok = cache.Get("key4")
	if !ok {
		t.Fatal("expected key4 to exist")
	}
}

// TestCacheUpdateDoesNotEvict tests that updating an existing key doesn't cause eviction.
func TestCacheUpdateDoesNotEvict(t *testing.T) {
	clock := NewMockClock()
	cache := NewTTLCache(2, clock)

	cache.Set("key1", "value1", 5*time.Minute)
	clock.Advance(1 * time.Second)
	cache.Set("key2", "value2", 5*time.Minute)

	if cache.Size() != 2 {
		t.Errorf("expected cache size 2, got %d", cache.Size())
	}

	// Update key1 (should not evict anything).
	cache.Set("key1", "value1_updated", 5*time.Minute)

	if cache.Size() != 2 {
		t.Errorf("expected cache size to remain 2, got %d", cache.Size())
	}

	val, ok := cache.Get("key1")
	if !ok || val != "value1_updated" {
		t.Fatal("expected key1 to be updated")
	}

	_, ok = cache.Get("key2")
	if !ok {
		t.Fatal("expected key2 to still exist")
	}
}

// TestCacheConcurrentAccess tests that concurrent reads and writes are safe.
func TestCacheConcurrentAccess(t *testing.T) {
	clock := NewMockClock()
	cache := NewTTLCache(1000, clock)

	var wg sync.WaitGroup
	const (
		numWriters = 5
		numReaders = 5
		opsPerGoroutine = 100
	)

	// Start writer goroutines.
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				key := fmt.Sprintf("writer_%d_key_%d", writerID, i)
				cache.Set(key, i, 5*time.Minute)
			}
		}(w)
	}

	// Start reader goroutines.
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				key := fmt.Sprintf("writer_%d_key_%d", readerID%numWriters, i)
				_, _ = cache.Get(key)
			}
		}(r)
	}

	wg.Wait()

	// Verify that the cache has entries (not exhaustive, just basic sanity).
	if cache.Size() == 0 {
		t.Fatal("expected cache to have entries after concurrent access")
	}
}

// TestCacheGetNonexistent tests getting a key that doesn't exist.
func TestCacheGetNonexistent(t *testing.T) {
	clock := NewMockClock()
	cache := NewTTLCache(10, clock)

	val, ok := cache.Get("nonexistent")
	if ok {
		t.Fatalf("expected ok=false for nonexistent key, got ok=true, val=%v", val)
	}
	if val != nil {
		t.Errorf("expected nil for nonexistent key, got %v", val)
	}
}

// TestCacheClear tests clearing the cache.
func TestCacheClear(t *testing.T) {
	clock := NewMockClock()
	cache := NewTTLCache(10, clock)

	cache.Set("key1", "value1", 5*time.Minute)
	cache.Set("key2", "value2", 5*time.Minute)

	if cache.Size() != 2 {
		t.Errorf("expected cache size 2, got %d", cache.Size())
	}

	cache.Clear()

	if cache.Size() != 0 {
		t.Errorf("expected cache size 0 after clear, got %d", cache.Size())
	}

	_, ok := cache.Get("key1")
	if ok {
		t.Fatal("expected key1 to be cleared")
	}
}

// TestCacheWithDifferentTypes tests storing different value types.
func TestCacheWithDifferentTypes(t *testing.T) {
	clock := NewMockClock()
	cache := NewTTLCache(10, clock)

	// Store different types.
	cache.Set("string", "hello", 5*time.Minute)
	cache.Set("int", 42, 5*time.Minute)
	cache.Set("slice", []string{"a", "b"}, 5*time.Minute)
	cache.Set("map", map[string]int{"x": 1}, 5*time.Minute)

	// Retrieve and verify.
	val, ok := cache.Get("string")
	if !ok || val != "hello" {
		t.Errorf("string retrieval failed: ok=%v, val=%v", ok, val)
	}

	val, ok = cache.Get("int")
	if !ok || val != 42 {
		t.Errorf("int retrieval failed: ok=%v, val=%v", ok, val)
	}

	val, ok = cache.Get("slice")
	if !ok {
		t.Fatalf("slice retrieval failed: ok=%v", ok)
	}
	if slice, ok := val.([]string); !ok || len(slice) != 2 || slice[0] != "a" {
		t.Errorf("slice retrieval mismatch: %v", val)
	}

	val, ok = cache.Get("map")
	if !ok {
		t.Fatalf("map retrieval failed: ok=%v", ok)
	}
	if m, ok := val.(map[string]int); !ok || m["x"] != 1 {
		t.Errorf("map retrieval mismatch: %v", val)
	}
}

// TestCacheZeroTTL tests behavior with zero TTL (immediate expiration).
func TestCacheZeroTTL(t *testing.T) {
	clock := NewMockClock()
	cache := NewTTLCache(10, clock)

	cache.Set("key1", "value1", 0)

	// Even without advancing time, the value should be expired immediately
	// because time.Now().Add(0) is still time.Now(), and After checks strict >
	val, ok := cache.Get("key1")
	if ok {
		t.Fatalf("expected zero-TTL value to be expired, got ok=true, val=%v", val)
	}
}

// TestCacheSize tests the Size method.
func TestCacheSize(t *testing.T) {
	clock := NewMockClock()
	cache := NewTTLCache(10, clock)

	if cache.Size() != 0 {
		t.Errorf("expected initial size 0, got %d", cache.Size())
	}

	cache.Set("key1", "value1", 5*time.Minute)
	if cache.Size() != 1 {
		t.Errorf("expected size 1 after first set, got %d", cache.Size())
	}

	cache.Set("key2", "value2", 5*time.Minute)
	if cache.Size() != 2 {
		t.Errorf("expected size 2 after second set, got %d", cache.Size())
	}

	cache.Set("key1", "value1_new", 5*time.Minute)
	if cache.Size() != 2 {
		t.Errorf("expected size to remain 2 after update, got %d", cache.Size())
	}
}

// TestCacheDefaultClockIsSystemClock tests that a nil clock defaults to SystemClock.
func TestCacheDefaultClockIsSystemClock(t *testing.T) {
	cache := NewTTLCache(10, nil)

	cache.Set("key1", "value1", 1*time.Second)

	val, ok := cache.Get("key1")
	if !ok {
		t.Fatal("expected key1 to exist with system clock")
	}
	if val != "value1" {
		t.Errorf("expected value1, got %v", val)
	}
}
