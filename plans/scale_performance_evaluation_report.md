# Scale and Performance Evaluation Report
## TieredCache Integration for Consent Management Service

### Executive Summary
This report evaluates the TieredCache system's ability to handle the scale and performance requirements for a consent management service targeting 1 billion data principals, thousands of collectors, and multiple purpose combinations. Based on analysis of the current architecture, performance benchmarks, and proposed integration patterns, the TieredCache system shows strong potential but requires specific optimizations and configuration changes to meet the target scale.

### Key Findings

#### 1. Current Performance Characteristics
From LOADTEST_RESULTS.md and SPEC.md:
- **L0 Cache**: Sub-microsecond read latency (P50 = 0 μs), 88.65% hit rate with 12GB memory
- **L1 Cache**: Sub-millisecond read latency, 49.48% hit rate with 8GB SSD storage
- **Combined Throughput**: ~30,815 TPS (write + read) in test configuration
- **Write Throughput**: 7,829 TPS with 8 workers
- **Read Throughput**: 22,985 TPS with 20 workers

#### 2. Storage Requirements Estimation
For 1 billion data principals with multiple consents per principal:
- Assuming average 2 consents per data principal: 2 billion consent records
- Average consent record size: ~1KB (JSON serialized)
- Primary storage requirement: ~2TB for consent records
- Index storage requirement: Significant additional overhead (estimated 3-5x primary storage)
- **Total estimated storage**: 8-10TB across all tiers

#### 3. Identified Bottlenecks
1. **Index Update Overhead**: Each consent operation requires 6-8 index updates (JSON array manipulation)
2. **Sharding Limitations**: Current 64 L0 shards and 12 L1 shards may cause hotspots with skewed access patterns
3. **Tiering Policy Aggressiveness**: Current thresholds (85% L0, 90% L1) may cause excessive tiering under load
4. **Background Job Impact**: Expired consent processing could create read amplification
5. **WAL Write Amplification**: Each consent operation generates WAL entries for durability

### Recommendations

#### 1. Architectural Optimizations
**a. Sharding Strategy**
- Increase L0 shard count from 64 to 256+ to better distribute load
- Increase L1 shard count from 12 to 64+ for better SSD utilization
- Implement consistent hashing to minimize resharding during scaling

**b. Index Storage Optimization**
- Replace JSON array sets with TieredCache composite keys (Option C from integration doc)
- Use separate keys per index member: `index:dp:{data_principal_id}:{consent_id}:true`
- This eliminates read-modify-write cycles for index updates
- Enables atomic index operations and better shard distribution

**c. Tiering Policy Adjustments**
- Increase L0 to L1 threshold to 0.95 (95%) to keep more hot data in memory
- Decrease L1 to L2 threshold to 0.80 (80%) to move cold data faster
- Increase tiering interval to 300s (5 minutes) to reduce background overhead
- Implement adaptive tiering based on access patterns

**d. Write Optimization**
- Enable asynchronous writes for non-critical index updates
- Batch index updates where possible (e.g., bulk consent operations)
- Consider separating critical path (consent record) from auxiliary path (indexes)

#### 2. Configuration Changes
Update `configs/config.yaml` for target scale:

```yaml
tieredcache:
  l0:
    max_memory_mb: 65536          # 64GB for L0 (increase from 12GB)
    shard_count: 256              # Increase from 64
    eviction_policy: clock_pro
    
  l1:
    max_capacity_tb: 16           # 16TB for L1 (increase from 8GB)
    shard_count: 64               # Increase from 12
    block_cache_size_mb: 8192     # 8GB block cache (increase from 2GB)
    num_goroutines: 32            # Increase from 8
    
  tiering:
    l0_to_l1_threshold: 0.95      # Increase from 0.85
    l1_to_l2_threshold: 0.80      # Decrease from 0.90
    tier_interval_sec: 300        # Increase from 60s
    max_workers: 16               # Increase from 4
    
  replay:
    max_replay_workers: 16        # Increase from 4
    checkpoint_interval: 5000     # Decrease from 10000 for faster recovery
    
  l2:
    enabled: true
    sinks:
      kafka:
        batch_size: 1000          # Increase from 100
        flush_interval_ms: 5000   # Increase from 1000ms
```

#### 3. Consistency and Durability Considerations
For DPDP Act compliance:
- **Immediate Sync Mode**: Enable immediate sync for L1 to ensure durability
- **WAL Retention**: Increase WAL retention to cover peak processing windows
- **Checksum Validation**: Enable end-to-end checksums for data integrity
- **Audit Logging**: Implement comprehensive audit trail separate from cache
- **Background Reconciliation**: Implement continuous index consistency checking

#### 4. Expected Performance After Optimizations
With proposed changes:
- **L0 Read Latency**: Maintain sub-microsecond for hot data
- **L1 Read Latency**: Maintain sub-millisecond
- **Write Throughput**: Projected 50K+ TPS (6x current)
- **Read Throughput**: Projected 100K+ TPS (4x current)
- **Storage Efficiency**: Reduced write amplification from index updates
- **Scalability**: Linear scaling with additional shards and resources

### Implementation Roadmap

#### Phase 1: Immediate Wins (Week 1-2)
1. Increase shard counts in configuration
2. Adjust tiering thresholds
3. Enable immediate sync mode
4. Increase L0 and L1 memory/disk allocation

#### Phase 2: Index Optimization (Week 3-4)
1. Implement composite key index storage
2. Update consent service to use new index pattern
3. Remove JSON array manipulation overhead
4. Add background reconciliation job

#### Phase 3: Advanced Tuning (Week 5-6)
1. Implement adaptive tiering policies
2. Optimize WAL configuration
3. Add comprehensive monitoring and alerting
4. Conduct load testing at target scale

### Risk Mitigation
1. **Hotspot Risk**: Monitor shard distribution and implement dynamic resharding
2. **Memory Pressure**: Implement graceful degradation when L0 exceeds thresholds
3. **Index Consistency**: Use version vectors or timestamps for conflict resolution
4. **Recovery Time**: Optimize checkpointing and parallel replay
5. **Compliance**: Regular audit of data handling procedures

### Conclusion
The TieredCache system can effectively support the consent management service at the target scale of 1 billion data principals with the recommended optimizations. The key to success lies in:
1. Proper resource allocation (especially L0 memory and L1 capacity)
2. Eliminating index update overhead through better storage patterns
3. Tuning tiering policies for the expected workload characteristics
4. Ensuring durability and consistency for compliance requirements

With these changes, the system should comfortably handle the expected load while maintaining the performance characteristics required for effective consent management.