# TieredCache Configuration

This document describes all configuration options available in TieredCache.

## Table of Contents

- [L0 In-Memory Cache](#l0-in-memory-cache)
- [L1 SSD Cache](#l1-ssd-cache)
- [L2 Cold Storage](#l2-cold-storage)
- [Replay/Recovery](#replayrecovery)
- [Logging](#logging)

---

## L0 In-Memory Cache

The L0 cache is an in-memory tier using a Clock-Pro algorithm for eviction.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `max_memory_mb` | uint32 | 8192 | Maximum memory for L0 in megabytes (8GB) |
| `max_payload_bytes` | uint32 | 32768 | Maximum size of a single value in bytes (32KB) |
| `weighted_unit_bytes` | uint32 | 4096 | Weight unit in bytes for cache entry size calculation (4KB) |
| `shard_count` | uint32 | CPU×2 | Number of shards for lock-free concurrent access |
| `eviction_policy` | string | "clock_pro" | Eviction algorithm: "clock_pro", "lru", or "lfu" |
| `enable_snapshot` | bool | true | Enable periodic disk snapshots |
| `snapshot_interval_sec` | uint32 | 300 | Interval for disk snapshots in seconds (5 minutes) |
| `rebuild_from` | string | "snapshot" | Where to rebuild L0 on startup: "snapshot", "l1", or "none" |
| `enable_stats` | bool | true | Enable statistics collection |
| `snapshot_path` | string | "./data/l0_snapshots" | Path to store snapshots |

### Rebuild Sources (`rebuild_from`)

| Value | Description |
|-------|-------------|
| `"snapshot"` | Restore L0 from the latest disk snapshot |
| `"l1"` | Rebuild L0 by scanning all data from L1 cache |
| `"none"` | Cold start - L0 starts empty (no rebuild) |

### Eviction Policies

| Policy | Description |
|--------|-------------|
| `clock_pro` | Clock-Pro algorithm - maintains access history for better hit rates |
| `lru` | Least Recently Used - simple but may have lower hit rates |
| `lfu` | Least Frequently Used - tracks access frequency |

---

## L1 SSD Cache

The L1 cache is a Badger-based SSD tier for persistent storage.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `max_capacity_tb` | float64 | 10.0 | Maximum capacity in terabytes |
| `shard_count` | uint32 | CPU | Number of database shards |
| `ssd_path` | string | "./data/l1" | Primary SSD mount point |
| `value_log_path` | string | "./data/l1_vlog" | Path for value logs (use separate volume recommended) |
| `sync_mode` | string | "periodic" | Sync mode: "immediate", "periodic", or "disabled" |
| `sync_interval_ms` | uint32 | 1000 | Periodic sync interval in milliseconds |
| `compression` | string | "zstd" | Compression algorithm: "zstd", "snappy", or "none" |
| `wal_enabled` | bool | true | Enable write-ahead log for durability |
| `max_tables_size` | int64 | 268435456 | Maximum size of table files in bytes (256MB) |
| `num_goroutines` | int | 8 | Number of goroutines for compactions |

### Sync Modes

| Mode | Description |
|------|-------------|
| `immediate` | Write data immediately to disk (most durable, slower) |
| `periodic` | Batch writes and sync periodically (balanced) |
| `disabled` | Let OS handle syncing (fastest, less durable) |

### Compression

| Algorithm | Description |
|-----------|-------------|
| `zstd` | Zstandard - best compression ratio, moderate speed |
| `snappy` | Fast compression, lower ratio |
| `none` | No compression |

---

## L2 Cold Storage

The L2 tier provides cold storage integration for data tiering.

### General Settings

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | true | Enable the L2 tier |

### Tiering Policy

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `l0_to_l1_threshold` | float64 | 0.85 | Memory usage threshold for L0→L1 tiering (85%) |
| `l1_to_l2_threshold` | float64 | 0.90 | Disk usage threshold for L1→L2 tiering (90%) |
| `tier_interval_sec` | uint32 | 60 | Interval between tiering checks in seconds |
| `batch_size` | int | 1000 | Number of items to tier per batch |
| `max_workers` | int | 4 | Maximum number of tiering workers |
| `async_enabled` | bool | true | Enable asynchronous tiering |

### Kafka Sink

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | false | Enable Kafka sink |
| `brokers` | []string | [] | List of Kafka broker addresses |
| `topic` | string | "tieredcache-cold" | Target Kafka topic |
| `batch_size` | int | 100 | Records per batch |
| `flush_interval_ms` | int | 1000 | Flush interval in milliseconds |
| `compression` | string | "lz4" | Compression: "lz4", "gzip", "snappy", "none" |
| `required_acks` | string | "1" | Acknowledgment level: "0", "1", "-1" |
| `retry_backoff_ms` | int | 100 | Retry backoff in milliseconds |
| `max_retries` | int | 3 | Maximum retry attempts |

### MinIO Sink

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | false | Enable MinIO sink |
| `endpoint` | string | "localhost:9000" | MinIO server endpoint |
| `bucket` | string | "tieredcache" | Bucket name |
| `access_key` | string | - | MinIO access key |
| `secret_key` | string | - | MinIO secret key |
| `use_ssl` | bool | false | Use SSL/TLS |
| `prefix` | string | "cold/" | Object key prefix |
| `batch_size` | int | 50 | Objects per batch |
| `part_size` | int | 5242880 | Multi-part part size (5MB) |

### PostgreSQL Sink

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | false | Enable PostgreSQL sink |
| `host` | string | "localhost" | PostgreSQL host |
| `port` | int | 5432 | PostgreSQL port |
| `database` | string | "tieredcache" | Database name |
| `username` | string | "postgres" | Username |
| `password` | string | "" | Password |
| `table` | string | "cold_data" | Table name |
| `batch_size` | int | 100 | Rows per batch |
| `pool_size` | int | 10 | Connection pool size |
| `ssl_mode` | string | "disable" | SSL mode: "disable", "require", "verify-ca" |

---

## Replay/Recovery

Settings for WAL replay and crash recovery.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `wal_path` | string | "./data/wal" | Path to write-ahead log |
| `max_replay_workers` | int | 4 | Number of parallel replay workers |
| `verify_on_recovery` | bool | true | Enable data verification during recovery |
| `enable_l0_prewarm` | bool | true | Enable pre-warming L0 after recovery |
| `prewarm_batch_size` | int | 1000 | Batch size for pre-warming |
| `prewarm_workers` | int | 4 | Number of workers for pre-warming |
| `checkpoint_interval` | int64 | 10000 | Operations between checkpoints |
| `enable_checkpoint` | bool | true | Enable periodic checkpoints |
| `checkpoint_path` | string | "./data/checkpoints" | Path to store checkpoints |
| `max_replay_time_sec` | uint32 | 300 | Maximum replay time in seconds (5 minutes) |

### Pre-warming

When `enable_l0_prewarm` is true, after a crash recovery the L0 cache will be populated with data from L1 in the background. This improves performance by warming up the faster in-memory cache.

The `rebuild_from` option in L0 config determines whether to rebuild from:
- **Snapshot**: Fastest - loads from pre-saved snapshot
- **L1**: Slower but always available - scans all L1 data
- **None**: Skip rebuilding (cold start)

---

## Logging

Settings for application logging.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `level` | string | "info" | Log level: "debug", "info", "warn", "error" |
| `format` | string | "json" | Log format: "json" or "text" |
| `output` | string | "stdout" | Output: "stdout" or "file" |
| `file_path` | string | "./logs/tieredcache.log" | Log file path |
| `max_size_mb` | int | 100 | Maximum log file size in MB |
| `max_backups` | int | 10 | Number of backup files to keep |

---

## Example Configuration

```yaml
tieredcache:
  l0:
    max_memory_mb: 8192
    max_payload_bytes: 32768
    weighted_unit_bytes: 4096
    shard_count: 64
    eviction_policy: clock_pro
    enable_snapshot: true
    snapshot_interval_sec: 300
    rebuild_from: "snapshot"
    enable_stats: true
    snapshot_path: ./data/l0_snapshots

  l1:
    max_capacity_tb: 10
    shard_count: 32
    ssd_path: /mnt/nvme0
    value_log_path: /mnt/nvme1
    sync_mode: periodic
    sync_interval_ms: 1000
    compression: zstd
    wal_enabled: true
    max_tables_size: 268435456
    num_goroutines: 8

  l2:
    enabled: true
    tiering:
      l0_to_l1_threshold: 0.85
      l1_to_l2_threshold: 0.90
      tier_interval_sec: 60
      batch_size: 1000
      max_workers: 4
      async_enabled: true

  replay:
    wal_path: ./data/wal
    max_replay_workers: 4
    verify_on_recovery: true
    enable_l0_prewarm: true
    prewarm_batch_size: 1000
    prewarm_workers: 4
    checkpoint_interval: 10000
    enable_checkpoint: true
    checkpoint_path: ./data/checkpoints
    max_replay_time_sec: 300

  logging:
    level: info
    format: json
    output: stdout
```

---

## Environment Variables

Configuration can also be overridden using environment variables:

| Variable | Description |
|----------|-------------|
| `TIEREDCACHE_CONFIG_PATH` | Path to config file (default: configs/config.yaml) |
