package l0

import (
	"context"
	"fmt"
	"hash/fnv"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"tieredcache/pkg/common"
)

// L0Cache represents the L0 Otter-style in-memory cache
type L0Cache struct {
	shards     []*shard
	shardCount uint32

	// Configuration
	maxMemory      uint64
	maxPayload     int
	weightedUnit   int
	snapshotPath   string
	snapshotInt    time.Duration
	enableSnapshot bool

	// Statistics
	stats Stats

	// State
	mu           sync.RWMutex
	closed       atomic.Bool
	snapshotting atomic.Bool

	// Background workers
	ctx    context.Context
	cancel context.CancelFunc
}

// shard represents a single sharded cache segment
type shard struct {
	// Storage - using a map with lock-free reads
	entries map[string]*common.CacheEntry

	// Clock-Pro algorithm state
	clockHand int
	frequency []clockEntry
	freqSize  int
	freqCap   int

	// Metrics
	usedMemory uint64
	weight     int

	// Synchronization
	mu sync.RWMutex

	// Stats
	hits   uint64
	misses uint64
	evicts uint64
}

// clockEntry represents an entry in the clock ring
type clockEntry struct {
	key       string
	reference bool // accessed bit
	weight    int
}

// Config contains L0 cache configuration
type Config struct {
	MaxMemoryMB     uint32
	MaxPayloadBytes uint32
	WeightedUnit    uint32
	ShardCount      uint32
	SnapshotPath    string
	SnapshotInt     time.Duration
	EnableSnapshot  bool
}

// New creates a new L0 cache
func New(cfg *Config) (*L0Cache, error) {
	if cfg == nil {
		return nil, common.NewConfigError("config", nil, "configuration cannot be nil")
	}

	// Validate config
	if cfg.MaxMemoryMB == 0 {
		return nil, common.NewConfigError("config.max_memory_mb", cfg.MaxMemoryMB, "must be greater than 0")
	}

	if cfg.MaxPayloadBytes == 0 {
		return nil, common.NewConfigError("config.max_payload_bytes", cfg.MaxPayloadBytes, "must be greater than 0")
	}

	if cfg.ShardCount == 0 {
		cfg.ShardCount = common.DefaultL0ShardCount // Default
	}

	if cfg.WeightedUnit == 0 {
		cfg.WeightedUnit = uint32(common.WeightedUnitBytes) // Default 4KB
	}

	// Calculate memory per shard
	maxMemory := uint64(cfg.MaxMemoryMB) * 1024 * 1024
	perShardMem := maxMemory / uint64(cfg.ShardCount)
	freqSize := int(perShardMem / uint64(cfg.WeightedUnit) / 4) // Rough estimate

	if freqSize < common.MinFrequencySize {
		freqSize = common.MinFrequencySize
	}

	// Create shards
	shards := make([]*shard, cfg.ShardCount)
	for i := uint32(0); i < cfg.ShardCount; i++ {
		shards[i] = &shard{
			entries:   make(map[string]*common.CacheEntry),
			frequency: make([]clockEntry, freqSize),
			freqSize:  0,
			freqCap:   freqSize,
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	cache := &L0Cache{
		shards:         shards,
		shardCount:     cfg.ShardCount,
		maxMemory:      maxMemory,
		maxPayload:     int(cfg.MaxPayloadBytes),
		weightedUnit:   int(cfg.WeightedUnit),
		snapshotPath:   cfg.SnapshotPath,
		snapshotInt:    cfg.SnapshotInt,
		enableSnapshot: cfg.EnableSnapshot,
		ctx:            ctx,
		cancel:         cancel,
	}

	// Start background goroutines
	go cache.startSnapshotTimer()

	return cache, nil
}

// Get retrieves a value from the cache
func (c *L0Cache) Get(ctx context.Context, key string) ([]byte, error) {
	if c.closed.Load() {
		return nil, common.NewInitError("l0", "get", common.ErrCodeClosed, false)
	}

	if key == "" {
		return nil, common.NewConfigError("key", key, "key cannot be empty")
	}

	// Validate payload size
	if len(key) > c.maxPayload {
		return nil, common.NewConfigError("key", key, fmt.Sprintf("key exceeds max payload size (%d bytes)", c.maxPayload))
	}

	shard := c.getShard(key)
	shard.mu.RLock()
	defer shard.mu.RUnlock()

	entry, ok := shard.entries[key]
	if !ok {
		atomic.AddUint64(&shard.misses, 1)
		atomic.AddUint64(&c.stats.Misses, 1)
		return nil, common.ErrCodeNotFound
	}

	// Check expiration
	if entry.IsExpired() {
		atomic.AddUint64(&shard.misses, 1)
		atomic.AddUint64(&c.stats.Misses, 1)
		return nil, common.ErrCodeNotFound
	}

	// Update access metadata
	entry.AccessedAt = time.Now()
	entry.AccessCount++

	atomic.AddUint64(&shard.hits, 1)
	atomic.AddUint64(&c.stats.Hits, 1)

	return entry.Value, nil
}

// Set stores a value in the cache
func (c *L0Cache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if c.closed.Load() {
		return common.NewInitError("l0", "set", common.ErrCodeClosed, false)
	}

	if key == "" {
		return common.NewConfigError("key", key, "key cannot be empty")
	}

	if len(value) == 0 {
		return common.NewConfigError("value", value, "value cannot be empty")
	}

	if len(value) > c.maxPayload {
		return common.NewConfigError("value", len(value), fmt.Sprintf("value exceeds max payload size (%d bytes)", c.maxPayload))
	}

	shard := c.getShard(key)
	entry := common.NewCacheEntry(key, value, ttl)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	// Check if we need to evict
	newWeight := entry.Weight
	for shard.usedMemory+uint64(newWeight) > c.getShardMemoryLimit() && shard.entriesLen() > 0 {
		if !c.evictOne(shard) {
			break // Nothing more to evict
		}
	}

	// Check existing entry
	oldEntry, exists := shard.entries[key]
	if exists {
		shard.usedMemory -= uint64(oldEntry.Size)
		shard.weight -= oldEntry.Weight
	}

	// Add new entry
	shard.entries[key] = entry
	shard.usedMemory += uint64(entry.Size)
	shard.weight += entry.Weight

	// Add to clock
	c.addToClock(shard, key, entry.Weight)

	atomic.AddUint64(&c.stats.Sets, 1)

	return nil
}

// Delete removes a value from the cache
func (c *L0Cache) Delete(ctx context.Context, key string) error {
	if c.closed.Load() {
		return common.NewInitError("l0", "delete", common.ErrCodeClosed, false)
	}

	if key == "" {
		return common.NewConfigError("key", key, "key cannot be empty")
	}

	shard := c.getShard(key)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	entry, ok := shard.entries[key]
	if !ok {
		return common.ErrCodeNotFound
	}

	shard.usedMemory -= uint64(entry.Size)
	shard.weight -= entry.Weight
	delete(shard.entries, key)

	atomic.AddUint64(&c.stats.Deletes, 1)

	return nil
}

// GetOrSet retrieves a value or sets it if not found
func (c *L0Cache) GetOrSet(ctx context.Context, key string, value []byte, ttl time.Duration) ([]byte, bool, error) {
	// Try get first
	val, err := c.Get(ctx, key)
	if err == nil {
		return val, false, nil
	}

	if err != common.ErrCodeNotFound {
		return nil, false, err
	}

	// Not found, set
	if err := c.Set(ctx, key, value, ttl); err != nil {
		return nil, false, err
	}

	return value, true, nil
}

// Exists checks if a key exists in the cache
func (c *L0Cache) Exists(ctx context.Context, key string) (bool, error) {
	if c.closed.Load() {
		return false, common.NewInitError("l0", "exists", common.ErrCodeClosed, false)
	}

	shard := c.getShard(key)

	shard.mu.RLock()
	defer shard.mu.RUnlock()

	entry, ok := shard.entries[key]
	if !ok {
		return false, nil
	}

	if entry.IsExpired() {
		return false, nil
	}

	return true, nil
}

// GetTier returns the current tier of a key (for multi-tier awareness)
func (c *L0Cache) GetTier(ctx context.Context, key string) (common.Tier, error) {
	if c.closed.Load() {
		return common.TierUnknown, common.NewInitError("l0", "get_tier", common.ErrCodeClosed, false)
	}

	shard := c.getShard(key)

	shard.mu.RLock()
	defer shard.mu.RUnlock()

	_, ok := shard.entries[key]
	if !ok {
		return common.TierUnknown, common.ErrCodeNotFound
	}

	return common.TierL0, nil
}

// Stats returns cache statistics
func (c *L0Cache) Stats() (Stats, error) {
	if c.closed.Load() {
		return Stats{}, common.NewInitError("l0", "stats", common.ErrCodeClosed, false)
	}

	totalHits := uint64(0)
	totalMisses := uint64(0)
	totalEvicts := uint64(0)
	totalEntries := 0
	totalWeight := 0
	totalMemory := uint64(0)

	for _, s := range c.shards {
		s.mu.RLock()
		totalHits += atomic.LoadUint64(&s.hits)
		totalMisses += atomic.LoadUint64(&s.misses)
		totalEvicts += atomic.LoadUint64(&s.evicts)
		totalEntries += len(s.entries)
		totalWeight += s.weight
		totalMemory += atomic.LoadUint64(&s.usedMemory)
		s.mu.RUnlock()
	}

	return Stats{
		Hits:        totalHits,
		Misses:      totalMisses,
		Sets:        atomic.LoadUint64(&c.stats.Sets),
		Deletes:     atomic.LoadUint64(&c.stats.Deletes),
		Evictions:   totalEvicts,
		Entries:     totalEntries,
		TotalWeight: totalWeight,
		MemoryUsed:  totalMemory,
		MemoryLimit: c.maxMemory,
		HitRate:     calculateHitRate(totalHits, totalMisses),
	}, nil
}

// Close closes the cache
func (c *L0Cache) Close() error {
	if c.closed.Swap(true) {
		return nil // Already closed
	}

	c.cancel()

	// Take a final snapshot before closing
	if err := c.snapshot(); err != nil {
		// Log but don't fail close
		fmt.Printf("warning: failed to take final snapshot: %v\n", err)
	}

	// Wait for background goroutines
	time.Sleep(common.DefaultCloseWaitTime)

	return nil
}

// getShard returns the shard for a given key
func (c *L0Cache) getShard(key string) *shard {
	h := fnv.New32a()
	h.Write([]byte(key))
	return c.shards[h.Sum32()%c.shardCount]
}

// getShardMemoryLimit returns the memory limit per shard
func (c *L0Cache) getShardMemoryLimit() uint64 {
	return c.maxMemory / uint64(c.shardCount)
}

// evictOne attempts to evict one entry from a shard
// Returns true if an entry was evicted, false if nothing to evict
func (c *L0Cache) evictOne(shard *shard) bool {
	if shard.freqSize == 0 {
		return false
	}

	// Clock-Pro second chance algorithm
	for i := 0; i < shard.freqSize; i++ {
		idx := (shard.clockHand + i) % shard.freqSize
		ce := &shard.frequency[idx]

		if ce.key == "" {
			continue
		}

		if !ce.reference {
			// Not recently used, evict
			if entry, ok := shard.entries[ce.key]; ok {
				delete(shard.entries, ce.key)
				shard.usedMemory -= uint64(entry.Size)
				shard.weight -= entry.Weight
				atomic.AddUint64(&shard.evicts, 1)

				// Clear clock entry
				ce.key = ""
				ce.weight = 0
				shard.freqSize--

				shard.clockHand = (idx + 1) % shard.freqCap
				return true
			}
		} else {
			// Second chance - clear reference bit
			ce.reference = false
		}
	}

	return false
}

// addToClock adds a key to the clock ring
func (c *L0Cache) addToClock(shard *shard, key string, weight int) {
	// Find empty slot
	for i := 0; i < shard.freqCap; i++ {
		idx := (shard.clockHand + i) % shard.freqCap
		if shard.frequency[idx].key == "" {
			shard.frequency[idx] = clockEntry{
				key:       key,
				reference: true,
				weight:    weight,
			}
			if shard.freqSize < shard.freqCap {
				shard.freqSize++
			}
			return
		}
	}

	// No empty slots, advance clock and replace
	shard.clockHand = (shard.clockHand + 1) % shard.freqCap
}

// entriesLen returns the number of entries (approximate for performance)
func (s *shard) entriesLen() int {
	return len(s.entries)
}

// startSnapshotTimer starts the periodic snapshot timer
func (c *L0Cache) startSnapshotTimer() {
	if !c.enableSnapshot {
		return
	}

	if c.snapshotInt <= 0 {
		return
	}

	ticker := time.NewTicker(c.snapshotInt)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if !c.snapshotting.Load() {
				go c.snapshot()
			}
		}
	}
}

// snapshot takes an atomic snapshot of the cache
func (c *L0Cache) snapshot() error {
	if c.snapshotting.Swap(true) {
		return nil // Already snapshotting
	}
	defer c.snapshotting.Store(false)

	// Use atomic snapshot with CRC32 verification
	return c.WriteSnapshot()
}

// Restore restores the cache from a snapshot
func (c *L0Cache) Restore(path string) error {
	if c.closed.Load() {
		return common.NewInitError("l0", "restore", common.ErrCodeClosed, false)
	}

	// Use the atomic snapshot restore with verification
	return c.RestoreFromSnapshot(path)
}

// EvictCandidates returns entries that are candidates for eviction
func (c *L0Cache) EvictCandidates(count int) ([]*common.CacheEntry, error) {
	if c.closed.Load() {
		return nil, common.NewInitError("l0", "evict_candidates", common.ErrCodeClosed, false)
	}

	var candidates []*common.CacheEntry

	for _, shard := range c.shards {
		shard.mu.RLock()
		for _, entry := range shard.entries {
			if len(candidates) >= count {
				break
			}
			candidates = append(candidates, entry)
		}
		shard.mu.RUnlock()

		if len(candidates) >= count {
			break
		}
	}

	return candidates, nil
}

// Promote moves an entry to another tier (returns the entry data for promotion)
func (c *L0Cache) Promote(ctx context.Context, key string) (*common.CacheEntry, error) {
	if c.closed.Load() {
		return nil, common.NewInitError("l0", "promote", common.ErrCodeClosed, false)
	}

	shard := c.getShard(key)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	entry, ok := shard.entries[key]
	if !ok {
		return nil, common.ErrCodeNotFound
	}

	return entry, nil
}

// Demote removes an entry from L0 (for tier-down after L1 write succeeds)
func (c *L0Cache) Demote(ctx context.Context, key string) error {
	if c.closed.Load() {
		return common.NewInitError("l0", "demote", common.ErrCodeClosed, false)
	}

	shard := c.getShard(key)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	entry, ok := shard.entries[key]
	if !ok {
		return common.ErrCodeNotFound
	}

	delete(shard.entries, key)
	shard.usedMemory -= uint64(entry.Size)
	shard.weight -= entry.Weight

	// Also remove from clock
	for i := 0; i < shard.freqCap; i++ {
		if shard.frequency[i].key == key {
			shard.frequency[i].key = ""
			shard.frequency[i].weight = 0
			shard.freqSize--
			break
		}
	}

	return nil
}

// Weight returns the total weight used by the cache
func (c *L0Cache) Weight() int {
	total := 0
	for _, s := range c.shards {
		s.mu.RLock()
		total += s.weight
		s.mu.RUnlock()
	}
	return total
}

// MemoryUsage returns the current memory usage
func (c *L0Cache) MemoryUsage() uint64 {
	total := uint64(0)
	for _, s := range c.shards {
		total += atomic.LoadUint64(&s.usedMemory)
	}
	return total
}

// UsageRatio returns the current memory usage ratio
func (c *L0Cache) UsageRatio() float64 {
	return float64(c.MemoryUsage()) / float64(c.maxMemory)
}

// CalculateHitRate calculates hit rate
func calculateHitRate(hits, misses uint64) float64 {
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// Gc triggers garbage collection to free memory
func (c *L0Cache) Gc() {
	runtime.GC()
}

// Ensure we implement the common.Cache interface
var _ interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Close() error
} = (*L0Cache)(nil)
