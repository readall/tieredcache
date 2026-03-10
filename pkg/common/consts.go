package common

import "time"

// =============================================================================
// Cache Constants
// =============================================================================

// Default shard counts for L0 and L1 caches
const (
	// DefaultL0ShardCount is the default number of shards for L0 cache
	DefaultL0ShardCount uint32 = 64

	// DefaultL1ShardCount is the default number of shards for L1 cache
	DefaultL1ShardCount uint32 = 32
)

// WeightedUnit is the standard weight unit in bytes (4KB)
// This is used for calculating cache entry weights
const WeightedUnitBytes int = 4096

// MinFrequencySize is the minimum size for the frequency array in clock algorithm
const MinFrequencySize int = 1024

// =============================================================================
// Timing Constants
// =============================================================================

const (
	// DefaultCloseWaitTime is the time to wait for background goroutines to finish
	DefaultCloseWaitTime = 100 * time.Millisecond
)

// =============================================================================
// Recovery Constants
// =============================================================================

const (
	// DefaultMaxReplayWorkers is the default number of workers for replay
	DefaultMaxReplayWorkers int = 4

	// DefaultCheckpointInterval is the default number of operations between checkpoints
	DefaultCheckpointInterval int64 = 10000

	// WALEntryChannelBuffer is the buffer size for WAL entry channel during replay
	WALEntryChannelBuffer int = 100

	// ErrorChannelBuffer is the buffer size for error channel during replay
	ErrorChannelBuffer int = 10

	// WALHeaderSize is the size of the WAL entry header (size prefix)
	WALHeaderSize int = 8
)

// =============================================================================
// Default Paths
// =============================================================================

const (
	// DefaultConfigPath is the default path to the configuration file
	DefaultConfigPath string = "configs/config.yaml"
)
