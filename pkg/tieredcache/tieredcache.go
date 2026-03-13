package tieredcache

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"tieredcache/pkg/common"
	"tieredcache/pkg/config"
	"tieredcache/pkg/l0"
	"tieredcache/pkg/l1"
	"tieredcache/pkg/l2"
	"tieredcache/pkg/replay"
)

// TieredCache is the main tiered cache implementation
type TieredCache struct {
	// Tiers
	l0 *l0.L0Cache
	l1 *l1.L1Cache
	l2 *l2.SinkManager

	// Recovery
	recovery *replay.RecoveryManager

	// Configuration
	cfg *config.Config

	// State
	mu           sync.RWMutex
	closed       atomic.Bool
	initializing atomic.Bool
	initialized  atomic.Bool

	// Background workers
	ctx    context.Context
	cancel context.CancelFunc
}

// New creates a new tiered cache
func New(cfg *config.Config) (*TieredCache, error) {
	if cfg == nil {
		return nil, common.NewConfigError("config", nil, "configuration cannot be nil")
	}

	// Validate configuration
	if err := config.Validate(cfg); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	cache := &TieredCache{
		cfg:    cfg,
		ctx:    ctx,
		cancel: cancel,
	}

	// Initialize recovery manager first (for WAL)
	recovery, err := replay.NewRecoveryManager(
		cfg.TieredCache.Replay.WALPath,
		cfg.TieredCache.Replay.CheckpointPath,
		cfg.TieredCache.Replay.MaxReplayWorkers,
		cfg.TieredCache.Replay.CheckpointInterval,
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create recovery manager: %w", err)
	}
	cache.recovery = recovery

	return cache, nil
}

// Initialize initializes all tiers
func (c *TieredCache) Initialize() error {
	if c.closed.Load() {
		return common.NewInitError("tieredcache", "initialize", common.ErrCodeClosed, false)
	}

	if c.initializing.Swap(true) {
		return common.NewInitError("tieredcache", "initialize", common.ErrCodeInitFailed, false)
	}
	defer c.initializing.Store(false)

	// Initialize L0
	l0Cfg := &l0.Config{
		MaxMemoryMB:     c.cfg.TieredCache.L0.MaxMemoryMB,
		MaxPayloadBytes: c.cfg.TieredCache.L0.MaxPayloadBytes,
		WeightedUnit:    c.cfg.TieredCache.L0.WeightedUnitBytes,
		ShardCount:      c.cfg.TieredCache.L0.ShardCount,
		SnapshotPath:    c.cfg.TieredCache.L0.SnapshotPath,
		SnapshotInt:     time.Duration(c.cfg.TieredCache.L0.SnapshotIntervalSec) * time.Second,
		EnableSnapshot:  c.cfg.TieredCache.L0.EnableSnapshot,
	}

	l0Cache, err := l0.New(l0Cfg)
	if err != nil {
		return common.NewInitError("l0", "initialize", err, true)
	}
	c.l0 = l0Cache

	// Initialize L1
	l1Cfg := &l1.Config{
		SSDPath:          c.cfg.TieredCache.L1.SSDPath,
		ValueLogPath:     c.cfg.TieredCache.L1.ValueLogPath,
		MaxCapacityGB:    c.cfg.TieredCache.L1.MaxCapacityGB,
		ShardCount:       c.cfg.TieredCache.L1.ShardCount,
		SyncMode:         c.cfg.TieredCache.L1.SyncMode,
		SyncIntervalMs:   c.cfg.TieredCache.L1.SyncIntervalMs,
		Compression:      c.cfg.TieredCache.L1.Compression,
		MaxTableSize:     c.cfg.TieredCache.L1.MaxTablesSize,
		NumGoroutines:    c.cfg.TieredCache.L1.NumGoroutines,
		WALEnabled:       c.cfg.TieredCache.L1.WALEnabled,
		BlockCacheSizeMB: c.cfg.TieredCache.L1.BlockCacheSizeMB,
	}

	l1Cache, err := l1.New(l1Cfg)
	if err != nil {
		c.l0.Close()
		return common.NewInitError("l1", "initialize", err, true)
	}
	c.l1 = l1Cache

	// Initialize L2
	if c.cfg.TieredCache.L2.Enabled {
		l2Manager := l2.NewSinkManager()
		// Add configured sinks based on config
		// (Sinks would be created based on config - Kafka, MinIO, Postgres)
		c.l2 = l2Manager
	}

	// Perform recovery/replay
	if c.cfg.TieredCache.Replay.VerifyOnRecovery {
		if err := c.performRecovery(); err != nil {
			// Log but continue - might be first run
			fmt.Printf("warning: recovery failed: %v\n", err)
		}
	}

	// Rebuild L0 based on configuration
	switch c.cfg.TieredCache.L0.RebuildFrom {
	case common.RebuildFromSnapshot:
		// Rebuild from snapshot
		go func() {
			if err := c.rebuildFromSnapshot(); err != nil {
				fmt.Printf("warning: L0 snapshot restore failed: %v\n", err)
			}
		}()
	case common.RebuildFromL1:
		// Rebuild from L1 (in background)
		go func() {
			if err := c.preWarmL0(); err != nil {
				fmt.Printf("warning: L0 rebuild from L1 failed: %v\n", err)
			}
		}()
	case common.RebuildFromNone:
		// No rebuild - cold start
		fmt.Println("L0 cold start - no rebuild")
	default:
		// Default behavior: rebuild from L1 if pre-warming is enabled
		if c.cfg.TieredCache.Replay.EnableL0PreWarm {
			go func() {
				if err := c.preWarmL0(); err != nil {
					fmt.Printf("warning: L0 rebuild from L1 failed: %v\n", err)
				}
			}()
		}
	}

	// Start background workers
	go c.startTieringWorker()

	c.initialized.Store(true)
	return nil
}

// performRecovery performs recovery from WAL
func (c *TieredCache) performRecovery() error {
	result, err := c.recovery.Recover(c.ctx, func(entry *replay.WALEntry) error {
		switch entry.Operation {
		case replay.OpSet:
			// Determine target tier
			if entry.Tier == 0 {
				return c.l0.Set(c.ctx, entry.Key, entry.Value, 0)
			} else if entry.Tier == 1 {
				return c.l1.Set(c.ctx, entry.Key, entry.Value, 0)
			}
		case replay.OpDelete:
			if entry.Tier == 0 {
				return c.l0.Delete(c.ctx, entry.Key)
			} else if entry.Tier == 1 {
				return c.l1.Delete(c.ctx, entry.Key)
			}
		}
		return nil
	})

	if err != nil {
		return err
	}

	if !result.Success {
		return fmt.Errorf("recovery completed with errors: %v", result.Errors)
	}

	fmt.Printf("Recovery complete: %d entries replayed in %v\n",
		result.EntriesReplayed, result.Duration)

	return nil
}

// preWarmL0 pre-warms L0 cache with data from L1 after recovery
// This implementation is memory-bounded to prevent OOM during recovery
func (c *TieredCache) preWarmL0() error {
	if c.l0 == nil || c.l1 == nil {
		return nil
	}

	fmt.Println("Starting L0 pre-warming from L1...")

	workers := c.cfg.TieredCache.Replay.PreWarmWorkers
	batchSize := c.cfg.TieredCache.Replay.PreWarmBatchSize

	// Calculate memory limits for pre-warming
	// Use 50% of L0 max memory for pre-warming to leave room for new entries
	maxMemoryForPrewarm := uint64(float64(c.cfg.TieredCache.L0.MaxMemoryMB) * 0.5 * 1024 * 1024)
	var memStats runtime.MemStats

	// Create channels for parallel processing
	entryChan := make(chan *l1Entry, batchSize)
	resultChan := make(chan error, workers)

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for entry := range entryChan {
				// Set in L0 (best effort - may fail if L0 is full)
				if err := c.l0.Set(c.ctx, entry.key, entry.value, 0); err != nil {
					resultChan <- err
				}
			}
		}()
	}

	// Iterate through L1 and send to workers
	iter := c.l1.NewIterator(c.ctx)
	defer iter.Close()

	var count int
	var bytesLoaded uint64
	batch := make([]*l1Entry, 0, batchSize)

	for iter.Next() {
		// Check memory pressure periodically
		runtime.ReadMemStats(&memStats)

		// Abort if approaching memory limit
		if memStats.Alloc > maxMemoryForPrewarm {
			fmt.Printf("Pre-warming paused: memory usage (%dMB) exceeded limit (%dMB)\n",
				memStats.Alloc/(1024*1024), maxMemoryForPrewarm/(1024*1024))
			break
		}

		key := iter.Key()
		value, err := iter.Value()
		if err != nil {
			continue
		}

		// Skip entries that would exceed memory limit
		if bytesLoaded+uint64(len(value)) > maxMemoryForPrewarm {
			fmt.Printf("Pre-warming complete: reached memory limit (%d entries loaded)\n", count)
			break
		}

		batch = append(batch, &l1Entry{key: key, value: value})
		bytesLoaded += uint64(len(value))

		if len(batch) >= batchSize {
			// Send batch to workers
			for _, e := range batch {
				entryChan <- e
			}
			count += len(batch)
			batch = batch[:0] // Reset batch
		}
	}

	// Send remaining entries
	for _, e := range batch {
		entryChan <- e
	}
	count += len(batch)

	close(entryChan)
	wg.Wait()
	close(resultChan)

	// Check for errors
	var errors []error
	for err := range resultChan {
		if err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		fmt.Printf("L0 pre-warming completed with %d errors\n", len(errors))
	} else {
		fmt.Printf("L0 pre-warming complete: %d entries loaded, %d bytes\n", count, bytesLoaded)
	}

	return nil
}

// rebuildFromSnapshot rebuilds L0 from snapshot on startup
func (c *TieredCache) rebuildFromSnapshot() error {
	if c.l0 == nil {
		return nil
	}

	snapshotPath := c.cfg.TieredCache.L0.SnapshotPath
	if snapshotPath == "" {
		snapshotPath = "./data/l0_snapshots"
	}

	fmt.Printf("Restoring L0 from snapshot: %s\n", snapshotPath)

	// Find the latest snapshot file in the directory
	latestFile, err := l0.FindLatestSnapshot(snapshotPath)
	if err != nil {
		fmt.Printf("No snapshot found to restore: %v\n", err)
		return nil // Not an error - just no snapshot to restore
	}

	fmt.Printf("Found latest snapshot: %s\n", latestFile)

	err = c.l0.Restore(latestFile)
	if err != nil {
		return fmt.Errorf("failed to restore from snapshot: %w", err)
	}

	fmt.Println("L0 restored from snapshot successfully")
	return nil
}

// l1Entry is a helper struct for pre-warming
type l1Entry struct {
	key   string
	value []byte
}

// Get retrieves a value from the cache, checking all tiers
func (c *TieredCache) Get(ctx context.Context, key string) ([]byte, error) {
	if !c.initialized.Load() {
		return nil, common.NewInitError("tieredcache", "get", common.ErrCodeInitFailed, false)
	}

	if c.closed.Load() {
		return nil, common.NewInitError("tieredcache", "get", common.ErrCodeClosed, false)
	}

	// Try L0 first
	if c.l0 != nil {
		val, err := c.l0.Get(ctx, key)
		if err == nil {
			return val, nil
		}
	}

	// Try L1
	if c.l1 != nil {
		val, err := c.l1.Get(ctx, key)
		if err == nil {
			// Promote to L0
			if c.l0 != nil {
				go c.l0.Set(ctx, key, val, 0) // Best effort
			}
			return val, nil
		}
	}

	// Try L2 (if enabled)
	if c.l2 != nil {
		sink, ok := c.l2.GetSink("default")
		if ok {
			val, err := sink.Read(c.ctx, key)
			if err == nil {
				return val, nil
			}
		}
	}

	return nil, common.ErrCodeNotFound
}

// Set stores a value in the cache
func (c *TieredCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if !c.initialized.Load() {
		return common.NewInitError("tieredcache", "set", common.ErrCodeInitFailed, false)
	}

	if c.closed.Load() {
		return common.NewInitError("tieredcache", "set", common.ErrCodeClosed, false)
	}

	// Validate size
	if len(value) > int(c.cfg.TieredCache.L0.MaxPayloadBytes) {
		return common.NewConfigError("value", len(value),
			fmt.Sprintf("exceeds max payload size %d", c.cfg.TieredCache.L0.MaxPayloadBytes))
	}

	var lastErr error

	// Write to L0
	if c.l0 != nil {
		if err := c.l0.Set(ctx, key, value, ttl); err != nil {
			lastErr = err
		}
	}

	// Write to L1 (SSD cache) for persistence
	if c.l1 != nil {
		if err := c.l1.Set(ctx, key, value, ttl); err != nil {
			// Log but don't fail - L0 write succeeded
			fmt.Printf("warning: L1 write failed: %v\n", err)
		}
	}

	// Write to WAL for recovery (only if WAL is needed based on sync_mode)
	// When sync_mode is "immediate", WAL is redundant since data is already durable
	if c.recovery != nil && c.l1 != nil && c.l1.ShouldUseWAL() {
		entry := &replay.WALEntry{
			Operation: replay.OpSet,
			Key:       key,
			Value:     value,
			Tier:      0, // L0
		}
		if err := c.recovery.Write(ctx, entry); err != nil {
			// Log but don't fail
			fmt.Printf("warning: WAL write failed: %v\n", err)
		}
	}

	return lastErr
}

// Delete removes a value from all tiers
func (c *TieredCache) Delete(ctx context.Context, key string) error {
	if !c.initialized.Load() {
		return common.NewInitError("tieredcache", "delete", common.ErrCodeInitFailed, false)
	}

	if c.closed.Load() {
		return common.NewInitError("tieredcache", "delete", common.ErrCodeClosed, false)
	}

	var lastErr error

	// Delete from all tiers
	if c.l0 != nil {
		if err := c.l0.Delete(ctx, key); err != nil {
			lastErr = err
		}
	}

	if c.l1 != nil {
		if err := c.l1.Delete(ctx, key); err != nil {
			lastErr = err
		}
	}

	// Write to WAL (only if WAL is needed based on sync_mode)
	// When sync_mode is "immediate", WAL is redundant since data is already durable
	if c.recovery != nil && c.l1 != nil && c.l1.ShouldUseWAL() {
		entry := &replay.WALEntry{
			Operation: replay.OpDelete,
			Key:       key,
			Tier:      0,
		}
		if err := c.recovery.Write(ctx, entry); err != nil {
			fmt.Printf("warning: WAL delete failed: %v\n", err)
		}
	}

	return lastErr
}

// SetToL1 sets a value directly in L1 (SSD tier)
// This bypasses L0 and is useful for testing L1 persistence directly
func (c *TieredCache) SetToL1(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if !c.initialized.Load() {
		return common.NewInitError("tieredcache", "set_to_l1", common.ErrCodeInitFailed, false)
	}

	if c.closed.Load() {
		return common.NewInitError("tieredcache", "set_to_l1", common.ErrCodeClosed, false)
	}

	if c.l1 == nil {
		return fmt.Errorf("L1 not initialized")
	}

	return c.l1.Set(ctx, key, value, ttl)
}

// GetFromL1 retrieves a value directly from L1 (SSD tier)
// This bypasses L0 and is useful for testing tier persistence
func (c *TieredCache) GetFromL1(ctx context.Context, key string) ([]byte, error) {
	if !c.initialized.Load() {
		return nil, common.NewInitError("tieredcache", "get_from_l1", common.ErrCodeInitFailed, false)
	}

	if c.closed.Load() {
		return nil, common.NewInitError("tieredcache", "get_from_l1", common.ErrCodeClosed, false)
	}

	if c.l1 == nil {
		return nil, fmt.Errorf("L1 not initialized")
	}

	return c.l1.Get(ctx, key)
}

// Stats returns cache statistics
func (c *TieredCache) Stats() (CacheStats, error) {
	if !c.initialized.Load() {
		return CacheStats{}, common.NewInitError("tieredcache", "stats", common.ErrCodeInitFailed, false)
	}

	stats := CacheStats{}

	if c.l0 != nil {
		l0Stats, err := c.l0.Stats()
		if err == nil {
			stats.L0 = l0Stats
		}
	}

	if c.l1 != nil {
		l1Stats, err := c.l1.Stats()
		if err == nil {
			stats.L1 = l1Stats
		}
	}

	if c.l2 != nil {
		stats.L2 = c.l2.Stats()
	}

	return stats, nil
}

// Close closes all tiers
func (c *TieredCache) Close() error {
	if c.closed.Swap(true) {
		return nil
	}

	c.cancel()

	var closeErrors []error

	// Close L0
	if c.l0 != nil {
		if err := c.l0.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("L0: %w", err))
		}
	}

	// Close L1
	if c.l1 != nil {
		if err := c.l1.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("L1: %w", err))
		}
	}

	// Close L2
	if c.l2 != nil {
		if err := c.l2.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("L2: %w", err))
		}
	}

	// Close recovery manager
	if c.recovery != nil {
		if err := c.recovery.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("recovery: %w", err))
		}
	}

	if len(closeErrors) > 0 {
		return fmt.Errorf("errors closing tiers: %v", closeErrors)
	}

	return nil
}

// startTieringWorker starts the background tiering worker
func (c *TieredCache) startTieringWorker() {
	if !c.cfg.TieredCache.L2.Enabled {
		return
	}

	ticker := time.NewTicker(c.cfg.GetTieringInterval())
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.performTiering()
		}
	}
}

// performTiering performs data tiering between levels
func (c *TieredCache) performTiering() {
	if c.l0 == nil || c.l1 == nil {
		return
	}

	// Check if L0 needs tiering
	usage := c.l0.UsageRatio()
	threshold := c.cfg.TieredCache.L2.Tiering.L0ToL1Threshold

	if usage < threshold {
		return
	}

	// Get eviction candidates from L0
	candidates, err := c.l0.EvictCandidates(c.cfg.TieredCache.L2.Tiering.BatchSize)
	if err != nil {
		fmt.Printf("warning: failed to get evict candidates: %v\n", err)
		return
	}

	// Move to L1 using two-phase tiering: copy -> verify -> delete
	for _, entry := range candidates {
		// Phase 1: Copy to L1
		if err := c.l1.Set(c.ctx, entry.Key, entry.Value, entry.TTL); err != nil {
			fmt.Printf("warning: failed to tier to L1: %v\n", err)
			continue
		}

		// Phase 2: Verify copy exists in L1 (prevents data loss)
		if _, err := c.l1.Get(c.ctx, entry.Key); err != nil {
			fmt.Printf("warning: tiering verification failed for %s: %v\n", entry.Key, err)
			continue
		}

		// Phase 3: Delete from L0 (only after successful verification)
		if err := c.l0.Demote(c.ctx, entry.Key); err != nil && err != common.ErrCodeNotFound {
			// Log but don't fail - data is safe in L1
			fmt.Printf("warning: failed to demote from L0: %v\n", err)
		}
	}
}

// CacheStats contains statistics for all tiers
type CacheStats struct {
	L0 l0.Stats
	L1 l1.Stats
	L2 map[string]l2.SinkStats
}
