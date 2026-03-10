package tieredcache

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tosha-tian/tieredcache/pkg/common"
	"github.com/tosha-tian/tieredcache/pkg/config"
	"github.com/tosha-tian/tieredcache/pkg/l0"
	"github.com/tosha-tian/tieredcache/pkg/l1"
	"github.com/tosha-tian/tieredcache/pkg/l2"
	"github.com/tosha-tian/tieredcache/pkg/replay"
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
		return common.NewInitError("tieredcache", "initialize", common.ErrCodeClosed.Error(), false)
	}

	if c.initializing.Swap(true) {
		return common.NewInitError("tieredcache", "initialize", "already initializing", false)
	}
	defer c.initializing.Store(false)

	// Initialize L0
	l0Cfg := &l0.Config{
		MaxMemoryMB:     c.cfg.TieredCache.L0.MaxMemoryMB,
		MaxPayloadBytes: c.cfg.TieredCache.L0.MaxPayloadBytes,
		WeightedUnit:    c.cfg.TieredCache.L0.WeightedUnitBytes,
		ShardCount:      c.cfg.TieredCache.L0.ShardCount,
		SnapshotPath:    c.cfg.TieredCache.L0.SnapshotPath,
		SnapshotInt:     c.cfg.TieredCache.L0.SnapshotIntervalSec,
	}

	l0Cache, err := l0.New(l0Cfg)
	if err != nil {
		return common.NewInitError("l0", "initialize", err, true)
	}
	c.l0 = l0Cache

	// Initialize L1
	l1Cfg := &l1.Config{
		SSDPath:        c.cfg.TieredCache.L1.SSDPath,
		ValueLogPath:   c.cfg.TieredCache.L1.ValueLogPath,
		MaxCapacityTB:  c.cfg.TieredCache.L1.MaxCapacityTB,
		ShardCount:     c.cfg.TieredCache.L1.ShardCount,
		SyncMode:       c.cfg.TieredCache.L1.SyncMode,
		SyncIntervalMs: c.cfg.TieredCache.L1.SyncIntervalMs,
		Compression:    c.cfg.TieredCache.L1.Compression,
		MaxTableSize:   c.cfg.TieredCache.L1.MaxTablesSize,
		NumGoroutines:  c.cfg.TieredCache.L1.NumGoroutines,
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

// Get retrieves a value from the cache, checking all tiers
func (c *TieredCache) Get(ctx context.Context, key string) ([]byte, error) {
	if !c.initialized.Load() {
		return nil, common.NewInitError("tieredcache", "get", "cache not initialized", false)
	}

	if c.closed.Load() {
		return nil, common.NewInitError("tieredcache", "get", common.ErrCodeClosed.Error(), false)
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
		val, err := c.l2.GetSink("default") // Would need proper sink lookup
		if err == nil {
			return val, nil
		}
	}

	return nil, common.ErrCodeNotFound
}

// Set stores a value in the cache
func (c *TieredCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if !c.initialized.Load() {
		return common.NewInitError("tieredcache", "set", "cache not initialized", false)
	}

	if c.closed.Load() {
		return common.NewInitError("tieredcache", "set", common.ErrCodeClosed.Error(), false)
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

	// Write to WAL for recovery
	if c.recovery != nil {
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
		return common.NewInitError("tieredcache", "delete", "cache not initialized", false)
	}

	if c.closed.Load() {
		return common.NewInitError("tieredcache", "delete", common.ErrCodeClosed.Error(), false)
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

	// Write to WAL
	if c.recovery != nil {
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

// Stats returns cache statistics
func (c *TieredCache) Stats() (CacheStats, error) {
	if !c.initialized.Load() {
		return CacheStats{}, common.NewInitError("tieredcache", "stats", "cache not initialized", false)
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

	// Move to L1
	for _, entry := range candidates {
		if err := c.l1.Set(c.ctx, entry.Key, entry.Value, entry.TTL); err != nil {
			fmt.Printf("warning: failed to tier to L1: %v\n", err)
			continue
		}

		// Remove from L0
		if err := c.l0.Demote(c.ctx, entry.Key); err != nil {
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
