# Load Test Results (March 2026)

## Test Configuration

- **L0 Cache**: 12GB memory, Clock-Pro eviction, 64 shards
- **L1 Cache**: Badger SSD, 12 shards, 2GB block cache, 1522 MB disk usage
- **Payload Sizes**: 2KB, 4KB
- **Duration**: 60 seconds
- **Write Workers**: 8
- **Read Workers**: 20
- **Miss Workers**: 5
- **Key Range**: 100,000
- **Miss Percentage**: 30%

## L0 Cache Statistics

| Metric | Value |
|--------|-------|
| Entries | 130,945 |
| Memory Used | 383.48 MB (3.12% of 12GB) |
| Hit Rate | **88.65%** |
| Hits | 1,313,957 |
| Misses | 168,269 |
| Sets | 592,544 |
| Evictions | 0 |

## L1 Cache Statistics

| Metric | Value |
|--------|-------|
| Disk Usage | 1522.17 MB |
| Hit Rate | 49.48% |
| Hits | 89,191 |
| Misses | 91,056 |
| Reads | 180,247 |
| Writes | 493,726 |

## Throughput Performance

| Metric | Value |
|--------|-------|
| Write TPS | 7,829.24 |
| Read TPS | 22,985.70 |
| Total Operations | 1,940,022 |
| Elapsed Time | 1m 0.002s |
| Error Rate | 0% |

## Overall Hit Rate

| Metric | Value |
|--------|-------|
| Read Hit Rate | **93.81%** |

## Write Latency (by payload size)

| Size | Count | P50 | P90 | P99 | Avg | TPS |
|------|-------|-----|-----|-----|-----|-----|
| 2 KB | 235,258 | 0 μs | 0 μs | 337.6 μs | 7.4 μs | 3,920.82 |
| 4 KB | 234,514 | 0 μs | 0 μs | 336.9 μs | 7.5 μs | 3,908.42 |

## Read Latency (by payload size)

| Size | Count | P50 | P90 | P99 | Avg | TPS |
|------|-------|-----|-----|-----|-----|-----|
| 2 KB | 691,190 | 0 μs | 0 μs | 0 μs | 0.317 μs | 11,519.40 |
| 4 KB | 688,004 | 0 μs | 0 μs | 0 μs | 0.341 μs | 11,466.30 |

## Verification Results

| Test | Successful | Failed | Rate |
|------|------------|--------|------|
| L1 Verification (Set -> L1.Get) | 11,978 | 0 | 100.00% |
| L1 Direct Verification (L1.Set -> Get) | 11,976 | 0 | 100.00% |

## Key Observations

1. **Zero Evictions**: L0 cache properly sized - no evictions occurred during the test
2. **High Read Hit Rate**: 88.65% from L0 (sub-microsecond latency)
3. **Excellent Read Performance**: P50 = 0 μs for L0 hits (extremely fast)
4. **Write Durability**: All writes (469,772) completed without errors
5. **L1 Performance**: 49.48% hit rate on SSD backend with sub-millisecond P99
6. **Combined Hit Rate**: 93.81% overall read hit rate across L0 + L1
7. **Scalability**: ~30,815 TPS combined throughput (write + read)
8. **Pre-warming**: L0 pre-warming loaded 33,581 entries before hitting memory limit
