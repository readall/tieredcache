# TieredCache Scale and Performance Optimization Implementation Plan

Based on the scale_performance_evaluation_report.md, this plan outlines the changes needed to optimize TieredCache for the consent management service targeting 1 billion data principals.

## Summary of Changes

### 1. Configuration Updates (configs/config.yaml)
- Increase L0 max_memory_mb to 65536 (64GB)
- Increase L0 shard_count to 256
- Increase L1 max_capacity_gb to 16384 (16TB)
- Increase L1 shard_count to 64
- Increase L1 block_cache_size_mb to 8192 (8GB)
- Increase L1 num_goroutines to 32
- Set tiering l0_to_l1_threshold to 0.95
- Set tiering l1_to_l2_threshold to 0.80
- Set tiering tier_interval_sec to 300
- Set tiering max_workers to 16
- Set replay max_replay_workers to 16
- Set replay checkpoint_interval to 5000
- Set l2.sinks.kafka.batch_size to 1000
- Set l2.sinks.kafka.flush_interval_ms to 5000
- Ensure l1.sync_mode is set to immediate

### 2. Architectural Optimizations

#### a. Sharding Strategy
- Increase L0 shard count from 64 to 256
- Increase L1 shard count from 12 to 64
- Verify code supports increased shard counts (no hardcoded limits)

#### b. Index Storage Optimization (Requires Consent Service Changes)
- Replace JSON array sets with TieredCache composite keys
- Use separate keys per index member: `index:dp:{data_principal_id}:{consent_id}:true`
- This eliminates read-modify-write cycles for index updates
- Enables atomic index operations and better shard distribution

#### c. Tiering Policy Adjustments
- Increase L0 to L1 threshold to 0.95 (95%) to keep more hot data in memory
- Decrease L1 to L2 threshold to 0.80 (80%) to move cold data faster
- Increase tiering interval to 300s (5 minutes) to reduce background overhead
- Implement adaptive tiering based on access patterns (Phase 3)

#### d. Write Optimization
- Enable asynchronous writes for non-critical index updates
- Batch index updates where possible (e.g., bulk consent operations)
- Consider separating critical path (consent record) from auxiliary path (indexes)

### 3. Implementation Roadmap

#### Phase 1: Immediate Wins (Week 1-2)
1. Update configs/config.yaml with recommended values
2. Verify code supports increased shard counts
3. Verify tiering policy adjustments are configurable
4. Enable immediate sync mode
5. Increase L0 and L1 memory/disk allocation

#### Phase 2: Index Optimization (Week 3-4)
1. Implement composite key index storage in TieredCache
2. Update consent service to use new index pattern
3. Remove JSON array manipulation overhead
4. Add background reconciliation job

#### Phase 3: Advanced Tuning (Week 5-6)
1. Implement adaptive tiering policies
2. Optimize WAL configuration (enable checksum validation)
3. Add comprehensive monitoring and alerting
4. Conduct load testing at target scale

### 4. Testing Strategy

#### Unit Tests
- Verify configuration loading with new values
- Test shard distribution with increased counts
- Validate tiering policy threshold boundaries
- Test WAL checksum validation

#### Integration Tests
- Test end-to-end flow with new configuration
- Verify tiering behavior with adjusted thresholds
- Test index operations with composite keys
- Validate recovery with new checkpoint interval

#### Performance Tests
- Baseline performance with current configuration
- Performance validation after each phase
- Load testing to validate 50K+ TPS write and 100K+ TPS read
- Stress testing to identify bottlenecks

#### Compliance Tests
- Verify DPDP Act compliance requirements
- Audit trail validation
- Data integrity checks
- Consent withdrawal and expiry processing

### 5. Risk Mitigation

1. **Hotspot Risk**: Monitor shard distribution and implement dynamic resharding
2. **Memory Pressure**: Implement graceful degradation when L0 exceeds thresholds
3. **Index Consistency**: Use version vectors or timestamps for conflict resolution
4. **Recovery Time**: Optimize checkpointing and parallel replay
5. **Compliance**: Regular audit of data handling procedures

### 6. Expected Performance After Optimizations
- **L0 Read Latency**: Maintain sub-microsecond for hot data
- **L1 Read Latency**: Maintain sub-millisecond
- **Write Throughput**: Projected 50K+ TPS (6x current)
- **Read Throughput**: Projected 100K+ TPS (4x current)
- **Storage Efficiency**: Reduced write amplification from index updates
- **Scalability**: Linear scaling with additional shards and resources

### 7. File Modifications Required

#### Configuration Files
- `configs/config.yaml` - Update with optimized values

#### Code Changes (if implementing adaptive tiering or WAL optimizations)
- `pkg/tieredcache/tieredcache.go` - Tiering policy adjustments
- `pkg/replay/wal.go` - WAL checksum validation
- `pkg/l1/wal.go` - L1 WAL configuration
- `pkg/tiering/policy.go` - Adaptive tiering policies (if created)

#### Consent Service Changes (Separate Repository)
- Index storage implementation using composite keys
- Updated index query patterns
- Background reconciliation job

### 8. Dependencies
- No external dependencies required for configuration changes
- Consent service changes required for index optimization
- Monitoring stack (Prometheus/Grafana) recommended for Phase 3

### 9. Rollback Procedure
1. Revert configs/config.yaml to previous values
2. Restart TieredCache service
3. Verify system stability
4. If issues persist, check logs for configuration errors

## Conclusion
These optimizations will enable TieredCache to meet the scale requirements of 1 billion data principals with improved performance and storage efficiency. The phased approach minimizes risk while delivering incremental improvements.