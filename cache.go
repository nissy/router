package router

import (
	"sync"
	"sync/atomic"
	"time"
)

const (
	shardCount             = 8
	shardMask              = shardCount - 1
	defaultCleanupInterval = time.Minute
	defaultExpiration      = time.Hour
	maxEntriesPerShard     = 2048
	defaultCacheMaxEntries = maxEntriesPerShard * shardCount
)

type cache struct {
	shards     [shardCount]*cacheShard
	cleaning   int32
	stopChan   chan struct{}
	maxEntries int
	stopped    atomic.Bool // Tracks whether the cache has been stopped
}

type cacheShard struct {
	sync.RWMutex
	entries map[uint64]*cacheEntry
}

type cacheEntry struct {
	handler   HandlerFunc
	timestamp int64
	hits      uint32
	params    map[string]string
}

// NewCache creates a new cache.
// maxEntries is the maximum number of entries that can be stored in the cache.
func newCacheWithMaxEntries(maxEntries int) *cache {
	c := &cache{
		stopChan:   make(chan struct{}),
		maxEntries: maxEntries,
	}
	for i := range c.shards {
		c.shards[i] = &cacheShard{
			entries: make(map[uint64]*cacheEntry),
		}
	}
	go c.cleanupLoop()
	return c
}

// Function kept for backward compatibility
func newCache() *cache {
	return newCacheWithMaxEntries(defaultCacheMaxEntries)
}

func (c *cache) get(key uint64) (HandlerFunc, bool) {
	handler, _, found := c.getWithParams(key)
	return handler, found
}

func (c *cache) set(key uint64, h HandlerFunc, params map[string]string) {
	if h == nil {
		return
	}

	sh := c.shards[key&shardMask]
	sh.Lock()
	if len(sh.entries) >= maxEntriesPerShard {
		var oldestKey uint64
		oldestTimestamp := int64(1<<63 - 1)
		for k, entry := range sh.entries {
			if entry.timestamp < oldestTimestamp {
				oldestTimestamp = entry.timestamp
				oldestKey = k
			}
		}
		delete(sh.entries, oldestKey)
	}
	sh.entries[key] = &cacheEntry{
		handler:   h,
		timestamp: time.Now().UnixNano(),
		hits:      0,
		params:    params,
	}
	sh.Unlock()
}

func (c *cache) getWithParams(key uint64) (HandlerFunc, map[string]string, bool) {
	sh := c.shards[key&shardMask]
	sh.RLock()
	e, ok := sh.entries[key]
	sh.RUnlock()

	if !ok {
		return nil, nil, false
	}
	atomic.StoreInt64(&e.timestamp, time.Now().UnixNano())
	return e.handler, e.params, true
}

func (c *cache) cleanupLoop() {
	ticker := time.NewTicker(defaultCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.cleanup()
		case <-c.stopChan:
			return
		}
	}
}

func (c *cache) cleanup() {
	if !atomic.CompareAndSwapInt32(&c.cleaning, 0, 1) {
		return
	}
	defer atomic.StoreInt32(&c.cleaning, 0)
	now := time.Now().UnixNano()
	threshold := now - int64(defaultExpiration)
	for _, sh := range c.shards {
		sh.Lock()
		for k, e := range sh.entries {
			if e.timestamp < threshold {
				delete(sh.entries, k)
			}
		}
		sh.Unlock()
	}
}

// stop stops the cache cleanup loop.
// This should be called during testing or shutdown.
// This method is safe to call multiple times.
func (c *cache) stop() {
	// Do nothing if already stopped
	if c.stopped.Load() {
		return
	}

	// set the stopped flag
	if c.stopped.CompareAndSwap(false, true) {
		// Close stopChan (only once)
		close(c.stopChan)
	}
}

// GetParams retrieves only the parameters from the cache.
func (c *cache) GetParams(key uint64) (map[string]string, bool) {
	_, params, found := c.getWithParams(key)
	return params, found
}
