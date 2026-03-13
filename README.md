# TieredCache

A high-performance multi-tier data caching system with robust error handling.

## Architecture

- **L0 (In-Memory)**: Otter-style cache with Clock-Pro eviction, lock-free reads
- **L1 (SSD)**: Badger-based persistent storage with 32 shards, ZSTD compression
- **L2 (Cold Storage)**: Parallel sinks to Kafka, MinIO, PostgreSQL

### Key Features
- Multi-tier data tiering with automatic promotion
- Background sink to multiple backends
- Full replay/recovery on application restart
- High consistency and lock-free design
- Write-through processing for durability

### Use Cases
1. Services with read-heavy workloads (Read:Write > 9:1)
2. High-throughput applications needing persistence without network hops
3. Hot/vip customer data caching

### Performance Targets
- L0 Response Time: < 10μs (sub-microsecond)
- L1 Response Time: < 1ms (sub-millisecond)
- Write Throughput: 100K ops/sec
- L0 Hit Rate: >90%
- L1 Hit Rate: >99%

---

## Quick Start

### Run the Application
```bash
go run cmd/tieredcache/main.go [config-path]
```

### Run Load Test
```bash
go run cmd/loadtest/main.go
```

### Load Test Options
```bash
# Duration and workers
-duration=60s -write-workers=10 -read-workers=10 -miss-workers=5

# Verification workers (test tier persistence)
-verify-workers=2          # Test: Set -> L1.Get
-l1-direct-workers=2       # Test: L1.Set -> Get

# Payload sizes
-payload-sizes=2,4         # KB sizes to test

# Key range
-key-range=100000          # Number of unique keys
```

---

## Load Test Results

Detailed benchmark results are available in [LOADTEST_RESULTS.md](LOADTEST_RESULTS.md).

---

## Verification Tests

The load test includes verification workers to ensure data persistence across tiers:

### Verify Workers (`-verify-workers`)
Tests: `cache.Set()` → `cache.GetFromL1()`
- Writes data to the cache (goes to L0 and L1)
- Reads directly from L1 to verify persistence
- Validates data integrity (size and content)

### L1 Direct Workers (`-l1-direct-workers`)
Tests: `cache.SetToL1()` → `cache.Get()`
- Sets data directly in L1 (bypassing L0)
- Reads through normal Get path (L0 → L1 fallback)
- Validates L1 can be read via tiered access

Example output:
```
--- L1 Direct Verification (L1.Set -> Get) ---
  Successful Verifications: 1500
  Failed Verifications: 0
  Verification Rate: 100.00%
```

---

## Configuration

Configuration is stored in `configs/config.yaml`.

### L0 (In-Memory)
```yaml
l0:
  max_memory_mb: 12288
  max_payload_bytes: 32768
  weighted_unit_bytes: 4096
  shard_count: 64
  eviction_policy: clock_pro
```

### L1 (SSD)
```yaml
l1:
  max_capacity_gb: 8
  shard_count: 12
  ssd_path: ./data
  sync_mode: immediate
  compression: zstd
```

### L2 (Cold Storage)
```yaml
l2:
  enabled: false
  sinks:
    kafka:
      enabled: true
      brokers:
        - localhost:9092
```

---

## Project Structure

```
tieredcache/
├── cmd/
│   ├── tieredcache/       # Main application
│   └── loadtest/          # Load testing tool
├── pkg/
│   ├── tieredcache/       # Core cache implementation
│   ├── l0/                # In-memory cache (Otter-style)
│   ├── l1/                # SSD cache (Badger)
│   ├── l2/                # Cold storage sinks
│   ├── replay/            # WAL and recovery
│   ├── config/            # Configuration management
│   └── common/            # Shared types and errors
├── configs/
│   └── config.yaml        # Configuration file
└── README.md
```

---

## API

```go
// Create cache
cache, err := tieredcache.New(cfg)
err = cache.Initialize()

// Basic operations
value, err := cache.Get(ctx, key)
err = cache.Set(ctx, key, value, ttl)
err = cache.Delete(ctx, key)

// Direct tier access (Only for testing and verification use)
// Shall not be used in normal operation as it violates the 
// API guarantee
value, err = cache.GetFromL1(ctx, key)  // Direct L1 access
err = cache.SetToL1(ctx, key, value, 0)  // Direct L1 write

// Stats
stats, err = cache.Stats()

// Cleanup
err = cache.Close()
