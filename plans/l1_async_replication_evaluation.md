# Evaluation of Async Replication for L1 Cache in TieredCache

## Overview

This document evaluates the implementation, challenges, and risks of implementing asynchronous replication for the L1 (Badger-based SSD) cache layer in a clustered TieredCache deployment.

## Current L1 Design

The L1 cache in TieredCache is built on BadgerDB with:
- 32 shards for concurrent access
- Key-based sharding using FNV hash
- Configurable sync modes (immediate, periodic, disabled)
- Write-Ahead Log (WAL) for durability
- Background sync worker for periodic mode
- Disk usage tracking and capacity management

## Async Replication Implementation Approach

### 1. Replication Architecture

For async replication of L1, we propose:

**Primary-Secondary Model with Async Propagation**:
- Each L1 shard has a primary node responsible for writes
- Changes are asynchronously propagated to replica nodes
- Reads can be served from any replica (eventual consistency)
- Write acknowledgment returned after primary persists locally

**Replication Log Design**:
- Similar to WAL but optimized for cross-node transmission
- Contains: operation type, key, value, timestamp, version vector
- Persisted locally before network transmission
- Batched for network efficiency

### 2. Implementation Details

**Files to Modify**:
- `pkg/l1/l1_badger.go` - Core L1 operations
- New package: `pkg/l1/replication/` - Replication logic
- New package: `pkg/transport/` - Network layer

**Key Components**:

#### a. Replication Entry Structure
```go
type L1ReplicationEntry struct {
    ShardID    uint32
    Operation  OpType // Set, Delete
    Key        string
    Value      []byte
    Timestamp  time.Time
    Version    uint64 // For conflict detection
    TTL        time.Duration
}
```

#### b. Replication Manager (per L1 instance)
```go
type L1ReplicationManager struct {
    localNodeID   string
    replicaNodes  map[string]*ReplicaPeer
    replicationLog *ReplicationLog
    transport     *TransportLayer
    mu            sync.RWMutex
}
```

#### c. Modified Write Path
1. Client write request → L1 primary node
2. Primary validates and writes to local Badger shard
3. Primary creates replication entry and persists to replication log
4. Primary returns acknowledgment to client
5. Background replication worker transmits entries to replicas
6. Replica applies entry to its local Badger shard

#### d. Conflict Detection Mechanism
- Vector clocks or version vectors per key
- Last-Write-Wins (LWW) based on timestamp
- Application-specific conflict resolution hooks

### 3. Specific Implementation Steps

#### Step 1: Enhance L1 Configuration
Add replication configuration to `pkg/config/config.go`:
```go
type L1ReplicationConfig struct {
    Enabled           bool
    ReplicaCount      int
    ReplicationIntervalMs int
    BatchSize         int
    TimeoutMs         int
    ConflictResolution string // LWW, application
}
```

#### Step 2: Create Replication Log
New file: `pkg/l1/replication/log.go`
- Append-only log similar to WAL
- Optimized for sequential reads/writes
- Includes checkpointing for recovery
- Handles log rotation and cleanup

#### Step 3: Implement Transport Layer
New package: `pkg/transport/`
- gRPC or custom TCP-based protocol
- Connection pooling and reuse
- Message serialization (protobuf or gob)
- Retry logic with exponential backoff
- Flow control and backpressure handling

#### Step 4: Modify L1 Write Operations
In `pkg/l1/l1_badger.go`:
- `Set()` and `Delete()` methods
- After local write, create replication entry
- Add entry to replication log
- Signal replication worker

#### Step 5: Create Replication Worker
Background goroutine that:
- Reads entries from replication log
- Batches entries for efficiency
- Sends to all replica nodes via transport layer
- Handles acknowledgments and retries
- Marks entries as replicated in log

#### Step 6: Implement Replica Apply Logic
On replica nodes:
- Receive replication entries via transport
- Validate entries (checksum, version)
- Apply to local Badger shard
- Update local replication state
- Send acknowledgment if required

## Challenges

### 1. Consistency Guarantees
- **Challenge**: Async replication means replicas may lag behind primary
- **Impact**: Read-after-write inconsistency, stale reads
- **Mitigation**: 
  - Offer read consistency levels (strong, eventual)
  - Allow clients to specify consistency requirements
  - Implement read-repair mechanisms

### 2. Network Partition Handling
- **Challenge**: Network splits can cause divergence
- **Impact**: Split-brain scenarios, conflicting writes
- **Mitigation**:
  - Implement failure detection via gossip
  - Prevent writes to isolated primaries
  - Require manual intervention for merge decisions
  - Use anti-entropy processes to detect and resolve divergence

### 3. Replication Lag Management
- **Challenge**: Slow replicas or network issues cause increasing lag
- **Impact**: Memory pressure on primary (unreplicated entries), potential data loss if primary fails
- **Mitigation**:
  - Monitor replication lag metrics
  - Throttle primary writes when lag exceeds threshold
  - Alert operators for manual intervention
  - Implement log truncation safety mechanisms

### 4. Hot Shard Problems
- **Challenge**: Non-uniform key distribution causes some shards to receive disproportionate write load
- **Impact**: Uneven replication load, potential bottlenecks
- **Mitigation**:
  - Monitor shard-level write rates
  - Implement dynamic shard splitting/merging
  - Consider consistent hashing with virtual nodes

### 5. Version Vector Complexity
- **Challenge**: Tracking causality across cluster increases metadata overhead
- **Impact**: Increased memory usage, complexity in conflict resolution
- **Mitigation**:
  - Use hybrid logical clocks (HLC) for efficiency
  - Prune old version vectors periodically
  - Limit vector size with fallback to LWW

### 6. Recovery Complexity
- **Challenge**: Replica recovery must handle missing replication entries
- **Impact**: Longer recovery times, potential inconsistency
- **Mitigation**:
  - Persistent replication state on disk
  - Incremental recovery from peers
  - Log matching and gap detection algorithms

## Risks

### 1. Data Loss Risk
- **Risk**: Primary failure before replication completes
- **Probability**: Medium (depends on replication lag)
- **Impact**: Loss of recent writes
- **Mitigation**:
  - Synchronous replication option for critical data
  - Durable replication log (fsync before ack)
  - Replication acknowledgment from N replicas

### 2. Performance Degradation
- **Risk**: Replication overhead impacts primary performance
- **Probability**: High
- **Impact**: Increased write latency, reduced throughput
- **Mitigation**:
  - Async batching to amortize network costs
  - Separate network interfaces for replication
  - Compression of replication streams
  - Read/write path separation

### 3. Cluster Instability
- **Risk**: Replication issues cause cascading failures
- **Probability**: Low-Medium
- **Impact**: Cluster-wide performance degradation or outage
- **Mitigation**:
  - Circuit breaker patterns for replication
  - Graceful degradation when replicas unavailable
  - Comprehensive monitoring and alerting

### 4. Operational Complexity
- **Risk**: Increased difficulty in troubleshooting and management
- **Probability**: High
- **Impact**: Longer MTTR (Mean Time To Recovery)
- **Mitigation**:
  - Comprehensive metrics (lag, throughput, error rates)
  - Distributed tracing for replication paths
  - Administrative tools for manual intervention
  - Clear runbooks for common scenarios

### 5. Consistency Violations
- **Risk**: Application experiences unexpected consistency behavior
- **Probability**: Medium
- **Impact**: Data corruption, incorrect business logic
- **Mitigation**:
  - Clear documentation of consistency model
  - Client libraries with consistency level options
  - Chaos testing to validate behavior
  - Metrics for inconsistency detection

## Risk Mitigation Strategies

### 1. Phased Implementation
- Start with read-only replication (secondary nodes for reads only)
- Gradually enable async writes
- Monitor and tune before full deployment

### 2. Comprehensive Monitoring
- Replication lag per shard/peer
- Replication throughput and error rates
- Conflict detection and resolution rates
- Resource utilization (CPU, memory, network, disk)

### 3. Configuration Safeguards
- Maximum replication lag thresholds
- Automatic write throttling when lag too high
- Manual override for emergency situations
- Gradual rollout via feature flags

### 4. Testing Strategy
- Unit tests for replication log and entry handling
- Integration tests for primary-replica scenarios
- Chaos engineering tests (network partitions, node failures)
- Performance benchmarks under various load patterns
- Long-running soak tests to detect leaks

### 5. Operational Tooling
- CLI tools to inspect replication state
- Metrics endpoints for Prometheus/Grafana
- Debugging flags to trace replication paths
- Manual replication control (pause/resume per peer)

## Performance Considerations

### Write Path Impact
1. Local Badger write (unchanged)
2. Replication log append (disk I/O)
3. Memory buffering for batching
4. Network transmission (asynchronous, not on critical path)

### Read Path Impact
1. Local Badger read (unchanged for local hits)
2. Potential cross-node fetch for misses (configurable)
3. Version checking for conflict detection

### Storage Overhead
- Replication log disk usage (configurable retention)
- Version vector metadata per key
- Network buffers for in-flight replication

## Recommendations

1. **Start with Read Replicas**: Implement async replication for read scaling first, then add write replication.

2. **Use Established Protocols**: Consider using Redis-style replication or Apache Kafka as replication backbone for proven patterns.

3. **Implement Observability First**: Add comprehensive metrics and tracing before enabling replication in production.

4. **Provide Escape Hatches**: Allow operators to disable replication per shard or node for troubleshooting.

5. **Document Consistency Guarantees Clearly**: Users must understand the trade-offs of async replication.

6. **Plan for Operational Complexity**: Invest in tooling and documentation to manage the increased system complexity.

## Conclusion

Async replication of L1 cache can significantly improve read scalability and provide fault tolerance, but introduces complexity in consistency, failure handling, and operations. The implementation should follow a phased approach with strong emphasis on monitoring and operational tooling. Key success factors include:
- Proper lag monitoring and backpressure mechanisms
- Clear consistency model documentation
- Robust conflict detection and resolution
- Comprehensive testing under failure conditions
- Gradual rollout with extensive validation

The risks are manageable with proper design and operational practices, making async replication a valuable enhancement for TieredCache in clustered deployments.