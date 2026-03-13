# Load Test Results (March 2026)

## Test Configuration

- **L0 Cache**: 8GB memory, Clock-Pro eviction, 64 shards
- **L1 Cache**: Badger SSD, 12 shards, 8GB capacity
- **Payload Sizes**: 2KB, 4KB
- **Duration**: 60 seconds

## L0 Cache Statistics

| Metric | Value |
|--------|-------|
| Entries | 97,251 |
| Memory Used | 843 MB (10.29% of 8GB) |
| Hit Rate | **73.90%** |
| Hits | 573,886 |
| Misses | 202,726 |
| Evictions | 0 |

## Throughput Performance

| Metric | Value |
|--------|-------|
| Write TPS | 5,942.75 |
| Read TPS | 9,492.48 |
| Total Operations | 1,135,892 |
| Elapsed Time | 1m 0.457s |
| Error Rate | 0% |

## Write Latency (by payload size)

| Size | Count | P50 | P90 | P99 | Avg |
|------|-------|-----|-----|-----|-----|
| 2 KB | ~40K | 1.05 ms | 4.26 ms | 161 ms | 8.43 ms |
| 4 KB | ~40K | 1.04 ms | 6.90 ms | 171 ms | 9.91 ms |

## Read Latency (by payload size)

| Size | Count | P50 | P90 | P99 | Avg |
|------|-------|-----|-----|-----|-----|
| 2 KB | ~266K | **0 μs** | **0 μs** | 2.82 ms | 166 μs |
| 4 KB | ~64K | 0 μs | 0 μs | 1.00 ms | 32 μs |

## Key Observations

1. **Zero Evictions**: L0 cache properly sized
2. **High Read Hit Rate**: 73.90% from L0 (sub-microsecond latency)
3. **Excellent Read Performance**: P50 = 0 μs for L0 hits
4. **Write Durability**: All writes completed without errors
5. **L1 Performance**: P99 < 3ms on SSD
6. **Scalability**: ~15,000 TPS combined