package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"tieredcache/pkg/common"
)

// Config represents the main configuration structure
type Config struct {
	// TieredCache is the root configuration
	TieredCache TieredCacheConfig `yaml:"tieredcache"`
}

// TieredCacheConfig contains all tiered cache settings
type TieredCacheConfig struct {
	// L0 is the in-memory cache configuration
	L0 L0Config `yaml:"l0"`

	// L1 is the SSD cache configuration
	L1 L1Config `yaml:"l1"`

	// L2 is the cold storage configuration
	L2 L2Config `yaml:"l2"`

	// Replay contains recovery settings
	Replay ReplayConfig `yaml:"replay"`

	// Logging configuration
	Logging LoggingConfig `yaml:"logging"`
}

// L0Config contains L0 in-memory cache settings
type L0Config struct {
	// MaxMemoryMB is the maximum memory for L0 in megabytes
	MaxMemoryMB uint32 `yaml:"max_memory_mb"`

	// MaxPayloadBytes is the maximum size of a single value in bytes
	MaxPayloadBytes uint32 `yaml:"max_payload_bytes"`

	// WeightedUnitBytes is the weight unit in bytes (default 4KB)
	WeightedUnitBytes uint32 `yaml:"weighted_unit_bytes"`

	// ShardCount is the number of shards for lock-free access
	ShardCount uint32 `yaml:"shard_count"`

	// EvictionPolicy is the eviction algorithm
	EvictionPolicy string `yaml:"eviction_policy"`

	// SnapshotIntervalSec is the interval for disk snapshots
	SnapshotIntervalSec uint32 `yaml:"snapshot_interval_sec"`

	// EnableStats enables statistics collection
	EnableStats bool `yaml:"enable_stats"`

	// SnapshotPath is the path to store snapshots
	SnapshotPath string `yaml:"snapshot_path"`
}

// L1Config contains L1 Badger SSD cache settings
type L1Config struct {
	// MaxCapacityTB is the maximum capacity in terabytes
	MaxCapacityTB float64 `yaml:"max_capacity_tb"`

	// ShardCount is the number of shards
	ShardCount uint32 `yaml:"shard_count"`

	// SSDPath is the path to the SSD mount point
	SSDPath string `yaml:"ssd_path"`

	// ValueLogPath is the path for value logs (optional, separate volume)
	ValueLogPath string `yaml:"value_log_path"`

	// SyncMode is the sync mode: immediate, periodic, disabled
	SyncMode string `yaml:"sync_mode"`

	// SyncIntervalMs is the periodic sync interval in milliseconds
	SyncIntervalMs uint32 `yaml:"sync_interval_ms"`

	// Compression is the compression algorithm: zstd, snappy, none
	Compression string `yaml:"compression"`

	// WALEnabled enables write-ahead log
	WALEnabled bool `yaml:"wal_enabled"`

	// MaxTablesSize is the maximum size of table files
	MaxTablesSize int64 `yaml:"max_tables_size"`

	// NumGoroutines is the number of goroutines for compactions
	NumGoroutines int `yaml:"num_goroutines"`
}

// L2Config contains L2 cold storage configuration
type L2Config struct {
	// Enabled enables the L2 tier
	Enabled bool `yaml:"enabled"`

	// Sinks contains sink configurations
	Sinks SinkConfig `yaml:"sinks"`

	// Tiering contains tiering policy settings
	Tiering TieringConfig `yaml:"tiering"`
}

// SinkConfig contains configurations for all sink backends
type SinkConfig struct {
	// Kafka is the Kafka sink configuration
	Kafka KafkaSinkConfig `yaml:"kafka"`

	// MinIO is the MinIO sink configuration
	MinIO MinIOSinkConfig `yaml:"minio"`

	// Postgres is the Postgres sink configuration
	Postgres PostgresSinkConfig `yaml:"postgres"`

	// FutureBackends contains configurations for future backends
	FutureBackends []FutureBackendConfig `yaml:"future_backends"`
}

// KafkaSinkConfig contains Kafka-specific settings
type KafkaSinkConfig struct {
	// Enabled enables the Kafka sink
	Enabled bool `yaml:"enabled"`

	// Brokers is the list of Kafka broker addresses
	Brokers []string `yaml:"brokers"`

	// Topic is the Kafka topic to produce to
	Topic string `yaml:"topic"`

	// BatchSize is the number of messages per batch
	BatchSize int `yaml:"batch_size"`

	// FlushIntervalMs is the flush interval in milliseconds
	FlushIntervalMs int `yaml:"flush_interval_ms"`

	// Compression is the compression type
	Compression string `yaml:"compression"`

	// RequiredAcks is the required acknowledgments
	RequiredAcks string `yaml:"required_acks"`

	// RetryBackoffMs is the retry backoff in milliseconds
	RetryBackoffMs int `yaml:"retry_backoff_ms"`

	// MaxRetries is the maximum number of retries
	MaxRetries int `yaml:"max_retries"`
}

// MinIOSinkConfig contains MinIO-specific settings
type MinIOSinkConfig struct {
	// Enabled enables the MinIO sink
	Enabled bool `yaml:"enabled"`

	// Endpoint is the MinIO server endpoint
	Endpoint string `yaml:"endpoint"`

	// Bucket is the bucket name
	Bucket string `yaml:"bucket"`

	// AccessKey is the access key
	AccessKey string `yaml:"access_key"`

	// SecretKey is the secret key
	SecretKey string `yaml:"secret_key"`

	// UseSSL enables SSL
	UseSSL bool `yaml:"use_ssl"`

	// Prefix is the object key prefix
	Prefix string `yaml:"prefix"`

	// BatchSize is the number of objects per batch
	BatchSize int `yaml:"batch_size"`

	// PartSize is the part size for multipart uploads
	PartSize int64 `yaml:"part_size"`
}

// PostgresSinkConfig contains Postgres-specific settings
type PostgresSinkConfig struct {
	// Enabled enables the Postgres sink
	Enabled bool `yaml:"enabled"`

	// Host is the Postgres host
	Host string `yaml:"host"`

	// Port is the Postgres port
	Port int `yaml:"port"`

	// Database is the database name
	Database string `yaml:"database"`

	// Username is the username
	Username string `yaml:"username"`

	// Password is the password
	Password string `yaml:"password"`

	// Table is the table name
	Table string `yaml:"table"`

	// BatchSize is the number of rows per batch
	BatchSize int `yaml:"batch_size"`

	// PoolSize is the connection pool size
	PoolSize int `yaml:"pool_size"`

	// SSLMode is the SSL mode
	SSLMode string `yaml:"ssl_mode"`
}

// FutureBackendConfig contains configuration for future backends
type FutureBackendConfig struct {
	// Name is the backend name
	Name string `yaml:"name"`

	// Type is the backend type (plugin name)
	Type string `yaml:"type"`

	// Config is the backend-specific configuration
	Config map[string]interface{} `yaml:"config"`

	// Enabled enables this backend
	Enabled bool `yaml:"enabled"`
}

// TieringConfig contains tiering policy settings
type TieringConfig struct {
	// L0ToL1Threshold is the memory usage threshold for L0->L1 tiering
	L0ToL1Threshold float64 `yaml:"l0_to_l1_threshold"`

	// L1ToL2Threshold is the disk usage threshold for L1->L2 tiering
	L1ToL2Threshold float64 `yaml:"l1_to_l2_threshold"`

	// TierIntervalSec is the interval between tiering checks
	TierIntervalSec uint32 `yaml:"tier_interval_sec"`

	// BatchSize is the number of items to tier per batch
	BatchSize int `yaml:"batch_size"`

	// MaxWorkers is the maximum number of tiering workers
	MaxWorkers int `yaml:"max_workers"`

	// AsyncEnabled enables async tiering
	AsyncEnabled bool `yaml:"async_enabled"`
}

// ReplayConfig contains recovery/replay settings
type ReplayConfig struct {
	// WALPath is the path to the write-ahead log
	WALPath string `yaml:"wal_path"`

	// MaxReplayWorkers is the number of parallel replay workers
	MaxReplayWorkers int `yaml:"max_replay_workers"`

	// VerifyOnRecovery enables data verification during recovery
	VerifyOnRecovery bool `yaml:"verify_on_recovery"`

	// CheckpointInterval is the number of operations between checkpoints
	CheckpointInterval int64 `yaml:"checkpoint_interval"`

	// EnableCheckpoint enables periodic checkpoints
	EnableCheckpoint bool `yaml:"enable_checkpoint"`

	// CheckpointPath is the path to store checkpoints
	CheckpointPath string `yaml:"checkpoint_path"`

	// MaxReplayTimeSec is the maximum time for replay in seconds
	MaxReplayTimeSec uint32 `yaml:"max_replay_time_sec"`
}

// LoggingConfig contains logging settings
type LoggingConfig struct {
	// Level is the log level: debug, info, warn, error
	Level string `yaml:"level"`

	// Format is the log format: json, text
	Format string `yaml:"format"`

	// Output is the output: stdout, file
	Output string `yaml:"output"`

	// FilePath is the log file path (if output is file)
	FilePath string `yaml:"file_path"`

	// MaxSizeMB is the max log file size in MB
	MaxSizeMB int `yaml:"max_size_mb"`

	// MaxBackups is the number of backup files to keep
	MaxBackups int `yaml:"max_backups"`
}

// DefaultConfig returns a configuration with default values
func DefaultConfig() *Config {
	return &Config{
		TieredCache: TieredCacheConfig{
			L0: L0Config{
				MaxMemoryMB:         8192,        // 8GB
				MaxPayloadBytes:     32768,       // 32KB
				WeightedUnitBytes:   4096,        // 4KB
				ShardCount:          64,          // 64 shards
				EvictionPolicy:      "clock_pro", // Clock-Pro algorithm
				SnapshotIntervalSec: 300,         // 5 minutes
				EnableStats:         true,
				SnapshotPath:        "./data/l0_snapshots",
			},
			L1: L1Config{
				MaxCapacityTB:  10.0, // 10TB
				ShardCount:     32,   // 32 shards
				SSDPath:        "./data/l1",
				ValueLogPath:   "./data/l1_vlog",
				SyncMode:       "periodic", // Periodic sync
				SyncIntervalMs: 1000,       // 1 second
				Compression:    "zstd",     // ZSTD compression
				WALEnabled:     true,       // Enable WAL
				MaxTablesSize:  256 << 20,  // 256MB
				NumGoroutines:  8,
			},
			L2: L2Config{
				Enabled: true,
				Sinks: SinkConfig{
					Kafka: KafkaSinkConfig{
						Enabled:         true,
						Brokers:         []string{"localhost:9092"},
						Topic:           "tieredcache-cold",
						BatchSize:       100,
						FlushIntervalMs: 1000,
						Compression:     "lz4",
						RequiredAcks:    "1",
						RetryBackoffMs:  100,
						MaxRetries:      3,
					},
					MinIO: MinIOSinkConfig{
						Enabled:   false,
						Endpoint:  "localhost:9000",
						Bucket:    "tieredcache",
						AccessKey: "minioadmin",
						SecretKey: "minioadmin",
						UseSSL:    false,
						Prefix:    "cold/",
						BatchSize: 50,
						PartSize:  5 << 20, // 5MB
					},
					Postgres: PostgresSinkConfig{
						Enabled:   false,
						Host:      "localhost",
						Port:      5432,
						Database:  "tieredcache",
						Username:  "postgres",
						Password:  "",
						Table:     "cold_data",
						BatchSize: 100,
						PoolSize:  10,
						SSLMode:   "disable",
					},
					FutureBackends: []FutureBackendConfig{},
				},
				Tiering: TieringConfig{
					L0ToL1Threshold: 0.85, // 85%
					L1ToL2Threshold: 0.90, // 90%
					TierIntervalSec: 60,   // 1 minute
					BatchSize:       1000,
					MaxWorkers:      4,
					AsyncEnabled:    true,
				},
			},
			Replay: ReplayConfig{
				WALPath:            "./data/wal",
				MaxReplayWorkers:   4,
				VerifyOnRecovery:   true,
				CheckpointInterval: 10000,
				EnableCheckpoint:   true,
				CheckpointPath:     "./data/checkpoints",
				MaxReplayTimeSec:   300, // 5 minutes
			},
			Logging: LoggingConfig{
				Level:      "info",
				Format:     "json",
				Output:     "stdout",
				FilePath:   "./logs/tieredcache.log",
				MaxSizeMB:  100,
				MaxBackups: 10,
			},
		},
	}
}

// Load loads configuration from a YAML file
func Load(path string) (*Config, error) {
	if path == "" {
		return nil, common.NewConfigError("path", path, "configuration path cannot be empty", "provide a valid file path")
	}

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, common.NewConfigError("path", path, "configuration file does not exist", "provide an existing configuration file")
	}

	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, common.NewConfigError("path", path, fmt.Sprintf("failed to read configuration file: %v", err))
	}

	// Parse YAML
	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, common.NewConfigError("content", string(data), fmt.Sprintf("failed to parse YAML: %v", err))
	}

	return cfg, nil
}

// LoadOrDefault loads configuration from a file or returns defaults
func LoadOrDefault(path string) (*Config, error) {
	if path == "" {
		return DefaultConfig(), nil
	}

	cfg, err := Load(path)
	if err != nil {
		// Return defaults if file doesn't exist
		if _, ok := err.(*common.ConfigError); ok {
			return DefaultConfig(), nil
		}
		return nil, err
	}

	return cfg, nil
}

// Save saves configuration to a YAML file
func Save(cfg *Config, path string) error {
	if cfg == nil {
		return common.NewConfigError("config", nil, "configuration cannot be nil")
	}

	if path == "" {
		return common.NewConfigError("path", path, "path cannot be empty")
	}

	// Validate before saving
	if err := Validate(cfg); err != nil {
		return err
	}

	// Marshal to YAML
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return common.NewConfigError("config", cfg, fmt.Sprintf("failed to marshal configuration: %v", err))
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return common.NewConfigError("path", path, fmt.Sprintf("failed to create directory: %v", err))
	}

	// Write file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return common.NewConfigError("path", path, fmt.Sprintf("failed to write configuration: %v", err))
	}

	return nil
}

// Validate validates the configuration
func Validate(cfg *Config) error {
	if cfg == nil {
		return common.NewConfigError("config", nil, "configuration cannot be nil", "provide a valid configuration")
	}

	var errors []error

	// Validate L0 config
	if err := validateL0Config(&cfg.TieredCache.L0); err != nil {
		errors = append(errors, err)
	}

	// Validate L1 config
	if err := validateL1Config(&cfg.TieredCache.L1); err != nil {
		errors = append(errors, err)
	}

	// Validate L2 config
	if err := validateL2Config(&cfg.TieredCache.L2); err != nil {
		errors = append(errors, err)
	}

	// Validate Replay config
	if err := validateReplayConfig(&cfg.TieredCache.Replay); err != nil {
		errors = append(errors, err)
	}

	// Validate Logging config
	if err := validateLoggingConfig(&cfg.TieredCache.Logging); err != nil {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return common.NewConfigError("config", cfg, fmt.Sprintf("validation failed: %v", errors))
	}

	return nil
}

func validateL0Config(cfg *L0Config) error {
	if cfg.MaxMemoryMB == 0 {
		return common.NewConfigError("tieredcache.l0.max_memory_mb", cfg.MaxMemoryMB, "must be greater than 0", "set a valid memory size (e.g., 8192)")
	}

	if cfg.MaxMemoryMB > 131072 { // 128GB
		return common.NewConfigError("tieredcache.l0.max_memory_mb", cfg.MaxMemoryMB, "exceeds maximum (128GB)", "reduce to 131072 or less")
	}

	if cfg.MaxPayloadBytes == 0 {
		return common.NewConfigError("tieredcache.l0.max_payload_bytes", cfg.MaxPayloadBytes, "must be greater than 0", "set a valid payload size (e.g., 32768)")
	}

	if cfg.MaxPayloadBytes > 1024*1024 { // 1MB
		return common.NewConfigError("tieredcache.l0.max_payload_bytes", cfg.MaxPayloadBytes, "exceeds maximum (1MB)", "reduce to 1048576 or less")
	}

	if cfg.WeightedUnitBytes == 0 {
		return common.NewConfigError("tieredcache.l0.weighted_unit_bytes", cfg.WeightedUnitBytes, "must be greater than 0", "use default 4096")
	}

	if cfg.ShardCount == 0 {
		return common.NewConfigError("tieredcache.l0.shard_count", cfg.ShardCount, "must be greater than 0", "use default 64")
	}

	if cfg.ShardCount > 256 {
		return common.NewConfigError("tieredcache.l0.shard_count", cfg.ShardCount, "exceeds maximum (256)", "reduce shard count")
	}

	validPolicies := map[string]bool{"clock_pro": true, "lru": true, "lfu": true, "arc": true}
	if !validPolicies[cfg.EvictionPolicy] {
		return common.NewConfigError("tieredcache.l0.eviction_policy", cfg.EvictionPolicy, "invalid eviction policy", "use one of: clock_pro, lru, lfu, arc")
	}

	if cfg.SnapshotIntervalSec == 0 {
		return common.NewConfigError("tieredcache.l0.snapshot_interval_sec", cfg.SnapshotIntervalSec, "must be greater than 0", "use default 300")
	}

	return nil
}

func validateL1Config(cfg *L1Config) error {
	if cfg.MaxCapacityTB <= 0 {
		return common.NewConfigError("tieredcache.l1.max_capacity_tb", cfg.MaxCapacityTB, "must be greater than 0", "set a valid capacity (e.g., 10)")
	}

	if cfg.MaxCapacityTB > 100 {
		return common.NewConfigError("tieredcache.l1.max_capacity_tb", cfg.MaxCapacityTB, "exceeds maximum (100TB)", "reduce capacity")
	}

	if cfg.ShardCount == 0 {
		return common.NewConfigError("tieredcache.l1.shard_count", cfg.ShardCount, "must be greater than 0", "use default 32")
	}

	if cfg.ShardCount > 128 {
		return common.NewConfigError("tieredcache.l1.shard_count", cfg.ShardCount, "exceeds maximum (128)", "reduce shard count")
	}

	if cfg.SSDPath == "" {
		return common.NewConfigError("tieredcache.l1.ssd_path", cfg.SSDPath, "cannot be empty", "provide a valid SSD path")
	}

	validSyncModes := map[string]bool{"immediate": true, "periodic": true, "disabled": true}
	if !validSyncModes[cfg.SyncMode] {
		return common.NewConfigError("tieredcache.l1.sync_mode", cfg.SyncMode, "invalid sync mode", "use one of: immediate, periodic, disabled")
	}

	if cfg.SyncMode == "periodic" && cfg.SyncIntervalMs == 0 {
		return common.NewConfigError("tieredcache.l1.sync_interval_ms", cfg.SyncIntervalMs, "must be greater than 0 when sync_mode is periodic", "use default 1000")
	}

	validCompressions := map[string]bool{"zstd": true, "snappy": true, "none": true}
	if !validCompressions[cfg.Compression] {
		return common.NewConfigError("tieredcache.l1.compression", cfg.Compression, "invalid compression", "use one of: zstd, snappy, none")
	}

	return nil
}

func validateL2Config(cfg *L2Config) error {
	if !cfg.Enabled {
		return nil // No further validation needed if disabled
	}

	// Validate Kafka config if enabled
	if cfg.Sinks.Kafka.Enabled {
		if len(cfg.Sinks.Kafka.Brokers) == 0 {
			return common.NewConfigError("tieredcache.l2.sinks.kafka.brokers", cfg.Sinks.Kafka.Brokers, "cannot be empty when enabled", "provide at least one broker")
		}
		if cfg.Sinks.Kafka.Topic == "" {
			return common.NewConfigError("tieredcache.l2.sinks.kafka.topic", cfg.Sinks.Kafka.Topic, "cannot be empty when enabled", "provide a topic name")
		}
	}

	// Validate MinIO config if enabled
	if cfg.Sinks.MinIO.Enabled {
		if cfg.Sinks.MinIO.Endpoint == "" {
			return common.NewConfigError("tieredcache.l2.sinks.minio.endpoint", cfg.Sinks.MinIO.Endpoint, "cannot be empty when enabled", "provide an endpoint")
		}
		if cfg.Sinks.MinIO.Bucket == "" {
			return common.NewConfigError("tieredcache.l2.sinks.minio.bucket", cfg.Sinks.MinIO.Bucket, "cannot be empty when enabled", "provide a bucket name")
		}
	}

	// Validate Postgres config if enabled
	if cfg.Sinks.Postgres.Enabled {
		if cfg.Sinks.Postgres.Host == "" {
			return common.NewConfigError("tieredcache.l2.sinks.postgres.host", cfg.Sinks.Postgres.Host, "cannot be empty when enabled", "provide a host")
		}
		if cfg.Sinks.Postgres.Port == 0 {
			return common.NewConfigError("tieredcache.l2.sinks.postgres.port", cfg.Sinks.Postgres.Port, "cannot be 0 when enabled", "use default 5432")
		}
		if cfg.Sinks.Postgres.Database == "" {
			return common.NewConfigError("tieredcache.l2.sinks.postgres.database", cfg.Sinks.Postgres.Database, "cannot be empty when enabled", "provide a database name")
		}
		if cfg.Sinks.Postgres.Table == "" {
			return common.NewConfigError("tieredcache.l2.sinks.postgres.table", cfg.Sinks.Postgres.Table, "cannot be empty when enabled", "provide a table name")
		}
	}

	// Validate tiering config
	if cfg.Tiering.L0ToL1Threshold <= 0 || cfg.Tiering.L0ToL1Threshold > 1 {
		return common.NewConfigError("tieredcache.l2.tiering.l0_to_l1_threshold", cfg.Tiering.L0ToL1Threshold, "must be between 0 and 1", "use default 0.85")
	}

	if cfg.Tiering.L1ToL2Threshold <= 0 || cfg.Tiering.L1ToL2Threshold > 1 {
		return common.NewConfigError("tieredcache.l2.tiering.l1_to_l2_threshold", cfg.Tiering.L1ToL2Threshold, "must be between 0 and 1", "use default 0.90")
	}

	if cfg.Tiering.TierIntervalSec == 0 {
		return common.NewConfigError("tieredcache.l2.tiering.tier_interval_sec", cfg.Tiering.TierIntervalSec, "must be greater than 0", "use default 60")
	}

	return nil
}

func validateReplayConfig(cfg *ReplayConfig) error {
	if cfg.WALPath == "" {
		return common.NewConfigError("tieredcache.replay.wal_path", cfg.WALPath, "cannot be empty", "provide a valid WAL path")
	}

	if cfg.MaxReplayWorkers <= 0 {
		return common.NewConfigError("tieredcache.replay.max_replay_workers", cfg.MaxReplayWorkers, "must be greater than 0", "use default 4")
	}

	if cfg.MaxReplayWorkers > 32 {
		return common.NewConfigError("tieredcache.replay.max_replay_workers", cfg.MaxReplayWorkers, "exceeds maximum (32)", "reduce workers")
	}

	if cfg.CheckpointInterval <= 0 {
		return common.NewConfigError("tieredcache.replay.checkpoint_interval", cfg.CheckpointInterval, "must be greater than 0", "use default 10000")
	}

	if cfg.EnableCheckpoint && cfg.CheckpointPath == "" {
		return common.NewConfigError("tieredcache.replay.checkpoint_path", cfg.CheckpointPath, "cannot be empty when checkpoint is enabled", "provide a valid path")
	}

	if cfg.MaxReplayTimeSec == 0 {
		return common.NewConfigError("tieredcache.replay.max_replay_time_sec", cfg.MaxReplayTimeSec, "must be greater than 0", "use default 300")
	}

	return nil
}

func validateLoggingConfig(cfg *LoggingConfig) error {
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[cfg.Level] {
		return common.NewConfigError("tieredcache.logging.level", cfg.Level, "invalid log level", "use one of: debug, info, warn, error")
	}

	validFormats := map[string]bool{"json": true, "text": true}
	if !validFormats[cfg.Format] {
		return common.NewConfigError("tieredcache.logging.format", cfg.Format, "invalid log format", "use one of: json, text")
	}

	validOutputs := map[string]bool{"stdout": true, "file": true}
	if !validOutputs[cfg.Output] {
		return common.NewConfigError("tieredcache.logging.output", cfg.Output, "invalid output", "use one of: stdout, file")
	}

	if cfg.Output == "file" && cfg.FilePath == "" {
		return common.NewConfigError("tieredcache.logging.file_path", cfg.FilePath, "cannot be empty when output is file", "provide a log file path")
	}

	return nil
}

// GetWeightedUnit returns the weighted unit for calculations
func (c *Config) GetWeightedUnit() int {
	return int(c.TieredCache.L0.WeightedUnitBytes)
}

// GetMaxPayloadSize returns the max payload size
func (c *Config) GetMaxPayloadSize() int {
	return int(c.TieredCache.L0.MaxPayloadBytes)
}

// GetMaxMemoryBytes returns the max memory in bytes
func (c *Config) GetMaxMemoryBytes() uint64 {
	return uint64(c.TieredCache.L0.MaxMemoryMB) * 1024 * 1024
}

// GetMaxCapacityBytes returns the max L1 capacity in bytes
func (c *Config) GetMaxCapacityBytes() uint64 {
	return uint64(c.TieredCache.L1.MaxCapacityTB * 1024 * 1024 * 1024 * 1024)
}

// GetSyncMode returns the sync mode as duration
func (c *Config) GetSyncMode() time.Duration {
	return time.Duration(c.TieredCache.L1.SyncIntervalMs) * time.Millisecond
}

// GetTieringInterval returns the tiering interval
func (c *Config) GetTieringInterval() time.Duration {
	return time.Duration(c.TieredCache.L2.Tiering.TierIntervalSec) * time.Second
}

// GetSnapshotInterval returns the snapshot interval
func (c *Config) GetSnapshotInterval() time.Duration {
	return time.Duration(c.TieredCache.L0.SnapshotIntervalSec) * time.Second
}
