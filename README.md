tiered cache


implementation and robust error handling,
Multi-Tier Data tiering (L0 Otter → L1 Badger SSD → L2 Cold Tier(s) ) with 
Background Sink: Parallel L2s (Kafka + MinIO + Postgres + Any Future Backend) 
(Otter L0 + 32-Sharded Badger L1, 10 TB, max 32 KB / 4 KB-weighted payloads, slow SSD, March 2026)
with Full “Replay” / Recovery Process on Application Restart
and high consistency and lock-free where possible

The primary use cases could be:
1. If your service has Read:Write ratio skewed heavily towards reads
2. If the application/service needs extremely high transaction throughput with persistence and miniscule budget for failures. In such situations, network hop even withing same availability zone is not preferred
3. High amount of in-memory application data with hot/vip customer problem

This library is aimed for >90% of L0 cache hit followed by >99% of L1 cache hit.
The L0 response times should be in in range of 10us and L1 response should be sub mili-second.
On fast SSD with great RAID, the L1 can also support 20-30us response time for read.

L3 is a tier most for offline use and reduction is cost for operations.
So if there is a pattern where offline processing requires all transactions to be reliably persisted at high volume this becomes the gateway.

As write through processing is implemented, all CREATE an UPDATE operations are persisted. Reads completely lock free and thus such a high throughput.

---

## Load Test Results (March 2026)

### Test Configuration
- **L0 Cache**: 8GB memory, Clock-Pro eviction, 32 shards
- **L1 Cache**: Badger SSD, 32 shards, 10TB capacity
- **Payload Sizes**: 1-16 KB (4KB-weighted)
- **Duration**: 60 seconds
- **Operations**: ~1.14 million total operations

### L0 Cache Statistics
| Metric | Value |
|--------|-------|
| Entries | 97,251 |
| Memory Used | 843 MB (10.29% of 8GB) |
| Hit Rate | **73.90%** |
| Hits | 573,886 |
| Misses | 202,726 |
| Evictions | 0 |

### Throughput Performance
| Metric | Value |
|--------|-------|
| Write TPS | 5,942.75 |
| Read TPS | 9,492.48 |
| Total Operations | 1,135,892 |
| Elapsed Time | 1m 0.457s |
| Error Rate | 0% |

### Write Latency (by payload size)
| Size | Count | P50 | P90 | P99 | Avg |
|------|-------|-----|-----|-----|-----|
| 1 KB | 39,915 | 1.05 ms | 4.26 ms | 161 ms | 8.43 ms |
| 3 KB | 40,071 | 1.04 ms | 6.90 ms | 171 ms | 9.91 ms |
| 5 KB | 39,755 | 1.06 ms | 5.81 ms | 168 ms | 9.11 ms |
| 7 KB | 40,325 | 1.06 ms | 6.18 ms | 158 ms | 9.12 ms |
| 9 KB | 39,755 | 1.08 ms | 4.99 ms | 163 ms | 7.84 ms |
| 11 KB | 39,623 | 1.08 ms | 6.09 ms | 178 ms | 8.63 ms |
| 13 KB | 39,842 | 1.08 ms | 4.91 ms | 155 ms | 7.98 ms |
| 15 KB | 39,884 | 1.08 ms | 5.96 ms | 188 ms | 9.37 ms |
| 16 KB | 40,110 | 1.09 ms | 7.19 ms | 200 ms | 10.72 ms |

### Read Latency (by payload size)
| Size | Count | P50 | P90 | P99 | Avg |
|------|-------|-----|-----|-----|-----|
| 1 KB | 266,475 | **0 μs** | **0 μs** | 2.82 ms | 166 μs |
| 3 KB | 64,195 | 0 μs | 0 μs | 1.00 ms | 32 μs |
| 5 KB | 63,423 | 0 μs | 0 μs | 1.00 ms | 50 μs |
| 7 KB | 63,754 | 0 μs | 0 μs | 1.00 ms | 39 μs |
| 9 KB | 64,167 | 0 μs | 0 μs | 1.00 ms | 35 μs |
| 11 KB | 63,069 | 0 μs | 0 μs | 794 μs | 38 μs |
| 13 KB | 64,098 | 0 μs | 0 μs | 1.00 ms | 38 μs |
| 15 KB | 63,358 | 0 μs | 0 μs | 999 μs | 32 μs |
| 16 KB | 64,073 | 0 μs | 0 μs | 998 μs | 38 μs |

### Key Observations

1. **Zero Evictions**: L0 cache properly sized - no entries were evicted during the test
2. **High Read Hit Rate**: 73.90% of reads served from L0 (sub-microsecond latency)
3. **Excellent Read Performance**: P50 = 0 μs for all payload sizes (L0 hits)
4. **Write Durability**: All 359,280 writes completed without errors
5. **L1 (Badger) Performance**: P99 read latency under 3ms, showing good SSD performance
6. **Scalability**: Maintained ~15,000 total TPS combined (reads + writes)
