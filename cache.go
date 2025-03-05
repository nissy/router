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
)

type Cache struct {
	shards   [shardCount]*cacheShard
	cleaning int32
	stopChan chan struct{}
}

type cacheShard struct {
	sync.RWMutex
	entries map[uint64]*cacheEntry
}

type cacheEntry struct {
	handler   HandlerFunc
	timestamp int64
	hits      uint32
}

func newCache() *Cache {
	c := &Cache{
		stopChan: make(chan struct{}),
	}
	for i := range c.shards {
		c.shards[i] = &cacheShard{
			entries: make(map[uint64]*cacheEntry),
		}
	}
	go c.cleanupLoop()
	return c
}

func (c *Cache) Get(key uint64) (HandlerFunc, bool) {
	sh := c.shards[key&shardMask]
	sh.RLock()
	e, ok := sh.entries[key]
	sh.RUnlock()
	if !ok {
		return nil, false
	}
	atomic.StoreInt64(&e.timestamp, time.Now().UnixNano())
	return e.handler, true
}

func (c *Cache) Set(key uint64, h HandlerFunc) {
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
	}
	sh.Unlock()
}

func (c *Cache) cleanupLoop() {
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

func (c *Cache) cleanup() {
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

func (c *Cache) Stop() {
	close(c.stopChan)
}
