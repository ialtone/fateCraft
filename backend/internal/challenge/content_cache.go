package challenge

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	defaultCacheMaxVersions = 16
	defaultCacheMaxBytes    = int64(256 << 20)
)

type CacheConfig struct {
	MaxVersions int
	MaxBytes    int64
}

// CacheStats deliberately contains no prompt, worldbook, or definition data.
type CacheStats struct {
	Entries           int     `json:"entries"`
	EstimatedBytes    int64   `json:"estimated_bytes"`
	MaxVersions       int     `json:"max_versions"`
	MaxBytes          int64   `json:"max_bytes"`
	Hits              uint64  `json:"hits"`
	Misses            uint64  `json:"misses"`
	HitRate           float64 `json:"hit_rate"`
	Loads             uint64  `json:"loads"`
	LoadFailures      uint64  `json:"load_failures"`
	CoalescedLoads    uint64  `json:"coalesced_loads"`
	Evictions         uint64  `json:"evictions"`
	LoadDurationMS    int64   `json:"load_duration_ms"`
	CompileDurationMS int64   `json:"compile_duration_ms"`
}

type versionCacheKey struct {
	id   uuid.UUID
	hash string
}

type versionCacheEntry struct {
	key      versionCacheKey
	document *VersionDocument
	compiled *compiledVersionContent
	bytes    int64
}

type versionLoadFlight struct {
	done  chan struct{}
	entry *versionCacheEntry
	err   error
}

// versionContentCache retains only immutable published/retired content.
type versionContentCache struct {
	mu sync.Mutex

	config    CacheConfig
	values    map[versionCacheKey]*versionCacheEntry
	byVersion map[uuid.UUID]versionCacheKey
	order     []versionCacheKey
	bytes     int64
	inflight  map[uuid.UUID]*versionLoadFlight

	hits            uint64
	misses          uint64
	loads           uint64
	loadFailures    uint64
	coalescedLoads  uint64
	evictions       uint64
	loadDuration    time.Duration
	compileDuration time.Duration
}

func normalizeCacheConfig(config CacheConfig) CacheConfig {
	if config.MaxVersions <= 0 {
		config.MaxVersions = defaultCacheMaxVersions
	}
	if config.MaxBytes <= 0 {
		config.MaxBytes = defaultCacheMaxBytes
	}
	return config
}

func newVersionContentCache(configs ...CacheConfig) *versionContentCache {
	config := CacheConfig{}
	if len(configs) > 0 {
		config = configs[0]
	}
	config = normalizeCacheConfig(config)
	return &versionContentCache{
		config:    config,
		values:    make(map[versionCacheKey]*versionCacheEntry, config.MaxVersions),
		byVersion: make(map[uuid.UUID]versionCacheKey, config.MaxVersions),
		order:     make([]versionCacheKey, 0, config.MaxVersions),
		inflight:  make(map[uuid.UUID]*versionLoadFlight),
	}
}

func (c *versionContentCache) get(id uuid.UUID) (*versionCacheEntry, bool) {
	if c == nil || id == uuid.Nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key, ok := c.byVersion[id]
	if !ok {
		c.misses++
		return nil, false
	}
	entry, ok := c.values[key]
	if !ok {
		delete(c.byVersion, id)
		c.misses++
		return nil, false
	}
	c.hits++
	return entry, true
}

// acquire combines the cache lookup and cold-load election under one lock so
// a load cannot finish in the gap between a miss and flight registration.
func (c *versionContentCache) acquire(id uuid.UUID) (*versionCacheEntry, *versionLoadFlight, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if key, ok := c.byVersion[id]; ok {
		if entry, exists := c.values[key]; exists {
			c.hits++
			return entry, nil, false
		}
		delete(c.byVersion, id)
	}
	c.misses++
	if flight, ok := c.inflight[id]; ok {
		c.coalescedLoads++
		return nil, flight, false
	}
	flight := &versionLoadFlight{done: make(chan struct{})}
	c.inflight[id] = flight
	return nil, flight, true
}

func (c *versionContentCache) wait(ctx context.Context, flight *versionLoadFlight) (*versionCacheEntry, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-flight.done:
		return flight.entry, flight.err
	}
}

func (c *versionContentCache) finishLoad(id uuid.UUID, flight *versionLoadFlight, entry *versionCacheEntry, err error, loadDuration, compileDuration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loads++
	c.loadDuration += loadDuration
	c.compileDuration += compileDuration
	if err != nil {
		c.loadFailures++
	} else if entry != nil {
		c.putLocked(entry)
	}
	flight.entry = entry
	flight.err = err
	delete(c.inflight, id)
	close(flight.done)
}

func (c *versionContentCache) put(entry *versionCacheEntry) {
	if c == nil || entry == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.putLocked(entry)
}

func (c *versionContentCache) putLocked(entry *versionCacheEntry) {
	if entry.key.id == uuid.Nil || entry.key.hash == "" || entry.bytes <= 0 || entry.bytes > c.config.MaxBytes {
		return
	}
	if oldKey, ok := c.byVersion[entry.key.id]; ok {
		if old, exists := c.values[oldKey]; exists {
			c.bytes -= old.bytes
			delete(c.values, oldKey)
		}
		for index, key := range c.order {
			if key == oldKey {
				c.order = append(c.order[:index], c.order[index+1:]...)
				break
			}
		}
	}
	for len(c.order) > 0 && (len(c.order) >= c.config.MaxVersions || c.bytes+entry.bytes > c.config.MaxBytes) {
		c.evictOldestLocked()
	}
	c.values[entry.key] = entry
	c.byVersion[entry.key.id] = entry.key
	c.order = append(c.order, entry.key)
	c.bytes += entry.bytes
}

func (c *versionContentCache) evictOldestLocked() {
	key := c.order[0]
	c.order = c.order[1:]
	if entry, ok := c.values[key]; ok {
		c.bytes -= entry.bytes
		delete(c.values, key)
	}
	if current, ok := c.byVersion[key.id]; ok && current == key {
		delete(c.byVersion, key.id)
	}
	c.evictions++
}

func (c *versionContentCache) stats() CacheStats {
	if c == nil {
		return CacheStats{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	stats := CacheStats{
		Entries: len(c.values), EstimatedBytes: c.bytes,
		MaxVersions: c.config.MaxVersions, MaxBytes: c.config.MaxBytes,
		Hits: c.hits, Misses: c.misses, Loads: c.loads,
		LoadFailures: c.loadFailures, CoalescedLoads: c.coalescedLoads, Evictions: c.evictions,
		LoadDurationMS: c.loadDuration.Milliseconds(), CompileDurationMS: c.compileDuration.Milliseconds(),
	}
	if total := stats.Hits + stats.Misses; total > 0 {
		stats.HitRate = float64(stats.Hits) / float64(total)
	}
	return stats
}
