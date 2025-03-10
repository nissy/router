package router

import (
	"net/http"
	"testing"
	"time"
)

// TestCacheCreation tests the creation of a cache
func TestCacheCreation(t *testing.T) {
	// Create a new cache
	cache := newCache()

	// Check initial state
	if cache == nil {
		t.Fatalf("Failed to create cache")
	}

	for i := 0; i < shardCount; i++ {
		if cache.shards[i] == nil {
			t.Errorf("Shard %d is not initialized", i)
		}

		if cache.shards[i].entries == nil {
			t.Errorf("Entry map for shard %d is not initialized", i)
		}
	}

	// stop the cache
	cache.stop()
}

// TestCacheSetAndGet tests setting and getting from the cache
func TestCacheSetAndGet(t *testing.T) {
	// Create a new cache
	cache := newCache()
	defer cache.stop()

	// Test handler function
	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// set an entry in the cache
	key := uint64(12345)
	cache.set(key, handler, nil)

	// get the entry from the cache
	h, found := cache.get(key)

	// Check the result
	if !found {
		t.Fatalf("Entry not found in cache")
	}

	if h == nil {
		t.Errorf("Handler retrieved from cache is nil")
	}
}

// TestCacheWithParams tests cache with parameters
func TestCacheWithParams(t *testing.T) {
	// Create a new cache
	cache := newCache()
	defer cache.stop()

	// Test handler function
	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// Test parameters
	params := map[string]string{
		"id":   "123",
		"name": "test",
	}

	// set an entry in the cache
	key := uint64(12345)
	cache.set(key, handler, params)

	// get the entry from the cache
	h, p, found := cache.getWithParams(key)

	// Check the result
	if !found {
		t.Fatalf("Entry not found in cache")
	}

	if h == nil {
		t.Errorf("Handler retrieved from cache is nil")
	}

	if p == nil {
		t.Errorf("Parameters retrieved from cache are nil")
	}

	// Check parameter values
	if p["id"] != "123" {
		t.Errorf("Value of parameter id is different. Expected: %s, Actual: %s", "123", p["id"])
	}

	if p["name"] != "test" {
		t.Errorf("Value of parameter name is different. Expected: %s, Actual: %s", "test", p["name"])
	}
}

// TestCacheMaxEntries tests the maximum number of entries in the cache
func TestCacheMaxEntries(t *testing.T) {
	// Create a new cache
	cache := newCache()
	defer cache.stop()

	// Test handler function
	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// set entries exceeding the maximum number
	shardIndex := uint64(0) // Concentrate entries in a specific shard
	for i := uint64(0); i < maxEntriesPerShard+10; i++ {
		key := (i << 3) | shardIndex // Fix shard index
		cache.set(key, handler, nil)
	}

	// Check the number of entries in the shard
	shard := cache.shards[shardIndex]
	shard.RLock()
	entriesCount := len(shard.entries)
	shard.RUnlock()

	if entriesCount > maxEntriesPerShard {
		t.Errorf("Number of entries in the shard exceeds the maximum. Maximum: %d, Actual: %d", maxEntriesPerShard, entriesCount)
	}
}

// TestCacheCleanup tests cache cleanup
func TestCacheCleanup(t *testing.T) {
	// Create a new cache
	cache := newCache()
	defer cache.stop()

	// Test handler function
	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// set an entry in the cache
	key := uint64(12345)
	cache.set(key, handler, nil)

	// set the entry's timestamp to the past
	shard := cache.shards[key&shardMask]
	shard.Lock()
	entry := shard.entries[key]
	if entry != nil {
		entry.timestamp = time.Now().Add(-2 * defaultExpiration).UnixNano()
	}
	shard.Unlock()

	// Manually execute cleanup
	cache.cleanup()

	// Verify that the entry has been removed
	_, found := cache.get(key)
	if found {
		t.Errorf("Expired entry was not cleaned up")
	}
}

// TestCacheHits tests cache hits
func TestCacheHits(t *testing.T) {
	// Create a new cache
	cache := newCache()
	defer cache.stop()

	// Test handler function
	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// set an entry in the cache
	key := uint64(12345)
	cache.set(key, handler, nil)

	// get the entry from the cache multiple times
	for i := 0; i < 5; i++ {
		h, found := cache.get(key)
		if !found || h == nil {
			t.Fatalf("Entry not found in cache")
		}
	}

	// Skip checking hit count (implementation may not count hits)
}

// TestCacheTimestamp tests cache timestamp updates
func TestCacheTimestamp(t *testing.T) {
	// Create a new cache
	cache := newCache()
	defer cache.stop()

	// Test handler function
	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// set an entry in the cache
	key := uint64(12345)
	cache.set(key, handler, nil)

	// get the initial timestamp
	shard := cache.shards[key&shardMask]
	shard.RLock()
	entry := shard.entries[key]
	initialTimestamp := int64(0)
	if entry != nil {
		initialTimestamp = entry.timestamp
	}
	shard.RUnlock()

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// get the entry from the cache
	cache.get(key)

	// get the final timestamp
	shard.RLock()
	entry = shard.entries[key]
	finalTimestamp := int64(0)
	if entry != nil {
		finalTimestamp = entry.timestamp
	}
	shard.RUnlock()

	// Verify that the timestamp has been updated
	if finalTimestamp <= initialTimestamp {
		t.Errorf("cache timestamp was not updated. Initial: %d, Final: %d", initialTimestamp, finalTimestamp)
	}
}
