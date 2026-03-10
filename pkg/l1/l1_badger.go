package l1

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/dgraph-io/badger/v4/options"

	"tieredcache/pkg/common"
)

// L1Cache represents the L1 Badger SSD cache with 32 shards
type L1Cache struct {
	shards     []*badgerShard
	shardCount uint32

	// Configuration
	ssdPath      string
	vlogPath     string
	maxCapacity  uint64
	syncMode     string
	syncInterval time.Duration
	compression  string

	// Statistics
	stats Stats

	// State
	mu     sync.RWMutex
	closed atomic.Bool

	// Background workers
	ctx    context.Context
	cancel context.CancelFunc
}

// badgerShard represents a single Badger shard
type badgerShard struct {
	db     *badger.DB
	index  uint32
	weight int
	mu     sync.RWMutex
}

// Config contains L1 cache configuration
type Config struct {
	SSDPath        string
	ValueLogPath   string
	MaxCapacityGB  float64
	ShardCount     uint32
	SyncMode       string
	SyncIntervalMs uint32
	Compression    string
	MaxTableSize   int64
	NumGoroutines  int
}

// Stats represents L1 statistics
type Stats struct {
	Reads     uint64
	Writes    uint64
	Deletes   uint64
	Hits      uint64
	Misses    uint64
	DiskUsage uint64
	Weight    int
}

// New creates a new L1 cache
func New(cfg *Config) (*L1Cache, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration cannot be nil")
	}

	if cfg.SSDPath == "" {
		return nil, fmt.Errorf("ssd_path cannot be empty")
	}

	if cfg.MaxCapacityGB <= 0 {
		return nil, fmt.Errorf("max_capacity_gb must be greater than 0")
	}

	if cfg.ShardCount == 0 {
		cfg.ShardCount = common.DefaultL1ShardCount // Default 32 shards
	}

	// Calculate max capacity in bytes
	maxCapacity := uint64(cfg.MaxCapacityGB * 1024 * 1024 * 1024)

	// Create directories
	if err := os.MkdirAll(cfg.SSDPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create ssd_path: %w", err)
	}

	if cfg.ValueLogPath != "" {
		if err := os.MkdirAll(cfg.ValueLogPath, 0755); err != nil {
			return nil, fmt.Errorf("failed to create value_log_path: %w", err)
		}
	}

	// Determine sync mode
	syncMode := "periodic"
	if cfg.SyncMode != "" {
		syncMode = cfg.SyncMode
	}

	syncInterval := time.Second
	if cfg.SyncIntervalMs > 0 {
		syncInterval = time.Duration(cfg.SyncIntervalMs) * time.Millisecond
	}

	// Create shards
	shards := make([]*badgerShard, cfg.ShardCount)
	for i := uint32(0); i < cfg.ShardCount; i++ {
		shardPath := filepath.Join(cfg.SSDPath, fmt.Sprintf("shard_%d", i))
		if err := os.MkdirAll(shardPath, 0755); err != nil {
			// Clean up created shards
			for j := 0; j < int(i); j++ {
				shards[j].db.Close()
			}
			return nil, fmt.Errorf("failed to create shard directory: %w", err)
		}

		opts := badger.DefaultOptions(shardPath)

		// Configure value log if separate path is provided
		if cfg.ValueLogPath != "" {
			vlogPath := filepath.Join(cfg.ValueLogPath, fmt.Sprintf("shard_%d", i))
			opts.ValueDir = vlogPath
		}

		// Configure sync mode
		switch syncMode {
		case "immediate":
			opts.SyncWrites = true
		case "periodic":
			opts.SyncWrites = false
		case "disabled":
			opts.SyncWrites = false
			opts.InMemory = true
		}

		// Configure compression
		switch cfg.Compression {
		case "zstd":
			opts.Compression = options.ZSTD
		case "snappy":
			opts.Compression = options.Snappy
		default:
			opts.Compression = options.None
		}

		// Performance tuning
		if cfg.MaxTableSize > 0 {
			opts.MemTableSize = int64(cfg.MaxTableSize)
		}
		if cfg.NumGoroutines > 0 {
			opts.NumGoroutines = cfg.NumGoroutines
		}

		db, err := badger.Open(opts)
		if err != nil {
			// Clean up created shards
			for j := 0; j < int(i); j++ {
				shards[j].db.Close()
			}
			return nil, fmt.Errorf("failed to open badger db for shard %d: %w", i, err)
		}

		shards[i] = &badgerShard{
			db:    db,
			index: i,
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	cache := &L1Cache{
		shards:       shards,
		shardCount:   cfg.ShardCount,
		ssdPath:      cfg.SSDPath,
		vlogPath:     cfg.ValueLogPath,
		maxCapacity:  maxCapacity,
		syncMode:     syncMode,
		syncInterval: syncInterval,
		compression:  cfg.Compression,
		ctx:          ctx,
		cancel:       cancel,
	}

	// Start background sync if periodic mode
	if syncMode == "periodic" {
		go cache.startSyncWorker()
	}

	return cache, nil
}

// Get retrieves a value from the cache
func (c *L1Cache) Get(ctx context.Context, key string) ([]byte, error) {
	if c.closed.Load() {
		return nil, fmt.Errorf("cache is closed")
	}

	if key == "" {
		return nil, fmt.Errorf("key cannot be empty")
	}

	shard := c.getShard(key)

	shard.mu.RLock()
	defer shard.mu.RUnlock()

	var result []byte
	err := shard.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				atomic.AddUint64(&c.stats.Misses, 1)
				return fmt.Errorf("key not found")
			}
			return err
		}

		valCopy, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}
		result = valCopy
		atomic.AddUint64(&c.stats.Hits, 1)
		return nil
	})

	atomic.AddUint64(&c.stats.Reads, 1)

	if err != nil {
		return nil, err
	}

	return result, nil
}

// Set stores a value in the cache
func (c *L1Cache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if c.closed.Load() {
		return fmt.Errorf("cache is closed")
	}

	if key == "" {
		return fmt.Errorf("key cannot be empty")
	}

	if len(value) == 0 {
		return fmt.Errorf("value cannot be empty")
	}

	// Check capacity
	currentUsage := c.DiskUsage()
	if currentUsage >= c.maxCapacity {
		return fmt.Errorf("cache is at capacity")
	}

	shard := c.getShard(key)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	err := shard.db.Update(func(txn *badger.Txn) error {
		entry := &badger.Entry{
			Key:   []byte(key),
			Value: value,
		}
		if ttl > 0 {
			entry.ExpiresAt = uint64(time.Now().Add(ttl).Unix())
		}
		return txn.SetEntry(entry)
	})

	atomic.AddUint64(&c.stats.Writes, 1)

	return err
}

// Delete removes a value from the cache
func (c *L1Cache) Delete(ctx context.Context, key string) error {
	if c.closed.Load() {
		return fmt.Errorf("cache is closed")
	}

	if key == "" {
		return fmt.Errorf("key cannot be empty")
	}

	shard := c.getShard(key)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	err := shard.db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(key))
	})

	atomic.AddUint64(&c.stats.Deletes, 1)

	return err
}

// Exists checks if a key exists
func (c *L1Cache) Exists(ctx context.Context, key string) (bool, error) {
	if c.closed.Load() {
		return false, fmt.Errorf("cache is closed")
	}

	shard := c.getShard(key)

	shard.mu.RLock()
	defer shard.mu.RUnlock()

	exists := false
	err := shard.db.View(func(txn *badger.Txn) error {
		_, err := txn.Get([]byte(key))
		if err == nil {
			exists = true
		} else if err == badger.ErrKeyNotFound {
			exists = false
		} else {
			return err
		}
		return nil
	})

	return exists, err
}

// GetTier returns the current tier of a key
func (c *L1Cache) GetTier(ctx context.Context, key string) (int, error) {
	exists, err := c.Exists(ctx, key)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, fmt.Errorf("key not found")
	}
	return 1, nil // L1 = 1
}

// Stats returns cache statistics
func (c *L1Cache) Stats() (Stats, error) {
	if c.closed.Load() {
		return Stats{}, fmt.Errorf("cache is closed")
	}

	totalHits := uint64(0)
	totalMisses := uint64(0)
	totalReads := uint64(0)
	totalWrites := uint64(0)
	totalDeletes := uint64(0)
	totalWeight := 0

	for _, s := range c.shards {
		s.mu.RLock()
		totalHits += atomic.LoadUint64(&c.stats.Hits)
		totalMisses += atomic.LoadUint64(&c.stats.Misses)
		totalReads += atomic.LoadUint64(&c.stats.Reads)
		totalWrites += atomic.LoadUint64(&c.stats.Writes)
		totalDeletes += atomic.LoadUint64(&c.stats.Deletes)
		totalWeight += s.weight
		s.mu.RUnlock()
	}

	return Stats{
		Reads:     totalReads,
		Writes:    totalWrites,
		Deletes:   totalDeletes,
		Hits:      totalHits,
		Misses:    totalMisses,
		DiskUsage: c.DiskUsage(),
		Weight:    totalWeight,
	}, nil
}

// DiskUsage returns the current disk usage
func (c *L1Cache) DiskUsage() uint64 {
	var totalSize int64

	for _, shard := range c.shards {
		shard.mu.RLock()
		// Estimate size from database
		lsmSize, _ := shard.db.Size()
		totalSize += int64(lsmSize)
		shard.mu.RUnlock()
	}

	return uint64(totalSize)
}

// UsageRatio returns the current capacity usage ratio
func (c *L1Cache) UsageRatio() float64 {
	usage := c.DiskUsage()
	if c.maxCapacity == 0 {
		return 0
	}
	return float64(usage) / float64(c.maxCapacity)
}

// Close closes the cache
func (c *L1Cache) Close() error {
	if c.closed.Swap(true) {
		return nil
	}

	c.cancel()

	// Wait for background workers
	time.Sleep(common.DefaultCloseWaitTime)

	// Close all shards
	var closeErrors []error
	for _, shard := range c.shards {
		shard.mu.Lock()
		if err := shard.db.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("shard %d: %w", shard.index, err))
		}
		shard.mu.Unlock()
	}

	if len(closeErrors) > 0 {
		return fmt.Errorf("errors closing shards: %v", closeErrors)
	}

	return nil
}

// getShard returns the shard for a given key
func (c *L1Cache) getShard(key string) *badgerShard {
	h := fnv.New32a()
	h.Write([]byte(key))
	return c.shards[h.Sum32()%c.shardCount]
}

// startSyncWorker starts the periodic sync worker
func (c *L1Cache) startSyncWorker() {
	ticker := time.NewTicker(c.syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.sync()
		}
	}
}

// sync syncs all shards to disk
func (c *L1Cache) sync() {
	for _, shard := range c.shards {
		shard.mu.RLock()
		_ = shard.db.Sync() // Best effort sync
		shard.mu.RUnlock()
	}
}

// EvictCandidates returns entries that are candidates for eviction
func (c *L1Cache) EvictCandidates(count int) ([]string, error) {
	if c.closed.Load() {
		return nil, fmt.Errorf("cache is closed")
	}

	var candidates []string

	for _, shard := range c.shards {
		shard.mu.RLock()
		err := shard.db.View(func(txn *badger.Txn) error {
			opts := badger.DefaultIteratorOptions
			opts.PrefetchValues = false
			opts.PrefetchSize = count

			it := txn.NewIterator(opts)
			defer it.Close()

			for it.Rewind(); it.Valid() && len(candidates) < count; it.Next() {
				candidates = append(candidates, string(it.Item().Key()))
			}
			return nil
		})
		shard.mu.RUnlock()

		if err != nil {
			return candidates, err
		}

		if len(candidates) >= count {
			break
		}
	}

	return candidates, nil
}

// Weight returns the total weight of items in cache
func (c *L1Cache) Weight() int {
	return int(c.DiskUsage() / 4096) // Approximate weight in 4KB units
}

// NewIterator creates a new iterator for the cache
func (c *L1Cache) NewIterator(ctx context.Context) *L1Iterator {
	return &L1Iterator{
		cache: c,
		ctx:   ctx,
		shard: 0,
		txn:   nil,
		it:    nil,
		valid: false,
	}
}

// L1Iterator represents an iterator over L1 cache
type L1Iterator struct {
	cache *L1Cache
	ctx   context.Context
	shard uint32
	txn   *badger.Txn
	it    *badger.Iterator
	valid bool
}

// Next advances the iterator
func (it *L1Iterator) Next() bool {
	if it.it == nil {
		// Initialize on first shard
		return it.initShard()
	}

	it.it.Next()

	if !it.it.Valid() {
		// Move to next shard
		it.shard++
		return it.initShard()
	}

	return true
}

func (it *L1Iterator) initShard() bool {
	for it.shard < it.cache.shardCount {
		shard := it.cache.shards[it.shard]

		shard.mu.RLock()
		txn := shard.db.NewTransaction(false)
		it.txn = txn
		it.it = txn.NewIterator(badger.DefaultIteratorOptions)
		it.it.Rewind()
		shard.mu.RUnlock()

		if it.it.Valid() {
			return true
		}

		// This shard is empty, close and move to next
		it.it.Close()
		it.txn.Discard()
		it.shard++
	}

	return false
}

// Key returns the current key
func (it *L1Iterator) Key() string {
	if !it.valid {
		return ""
	}
	return string(it.it.Item().Key())
}

// Value returns the current value
func (it *L1Iterator) Value() ([]byte, error) {
	if !it.valid {
		return nil, fmt.Errorf("iterator not valid")
	}
	return it.it.Item().ValueCopy(nil)
}

// Close closes the iterator
func (it *L1Iterator) Close() {
	if it.it != nil {
		it.it.Close()
	}
	if it.txn != nil {
		it.txn.Discard()
	}
}

// Ensure we implement a basic cache interface
var _ interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Close() error
} = (*L1Cache)(nil)
