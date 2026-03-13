# Active-Active Cluster Analysis for TieredCache

## Executive Summary

This document analyzes the impact and changes required to incorporate active-active cluster mode into the TieredCache system. The current design is a single-node, multi-tier caching system with L0 (in-memory), L1 (SSD), and L2 (cold storage) layers. Transitioning to an active-active cluster requires addressing data consistency, replication mechanisms, conflict resolution, and cluster membership management.

## Current Architecture Review

### Single-Node Design Characteristics

1. **Tiered Structure**:
   - L0: Otter-style in-memory cache with lock-free reads, Clock-Pro eviction
   - L1: Badger-based SSD cache with 32 shards, ZSTD compression
   - L2: Parallel sinks to Kafka, MinIO, PostgreSQL

2. **Key Components**:
   - Sharded design for concurrent access (L0: 64 shards, L1: 32 shards)
   - Write-Ahead Log (WAL) for durability and recovery
   - Background tiering workers for L0→L1 migration
   - Background sink workers for L2 persistence
   - Recovery manager for WAL replay on startup

3. **Scalability Bottlenecks Identified**:
   - Single point of failure (no redundancy)
   - Limited horizontal scaling (vertical scaling only via sharding)
   - No inter-node communication or data replication
   - Recovery dependent on local WAL only
   - Configuration and state are node-local

## Active-Active Cluster Patterns Research

### Common Approaches

1. **Shared-Nothing Architecture**:
   - Each node owns a subset of data (consistent hashing)
   - No shared storage between nodes
   - Requires data partitioning and rebalancing mechanisms

2. **Shared-Storage Architecture**:
   - All nodes access shared storage (e.g., distributed file system, database)
   - Simpler consistency but creates storage bottleneck
   - Not ideal for high-performance cache

3. **Replication-Based Approaches**:
   - **Primary-Backup**: One primary, others as backups (not active-active)
   - **Multi-Master Replication**: All nodes can accept writes
   - **Quorum-Based**: Requires majority agreement for writes
   - **Eventual Consistency**: Async replication with conflict resolution

### Selected Approach for TieredCache

For a high-performance caching system like TieredCache, we recommend a **hybrid approach**:
- **Consistent Hashing** for data partitioning across nodes
- **Async Replication** with **Conflict-Free Replicated Data Types (CRDTs)** or **Last-Write-Wins (LWW)** for conflict resolution
- **Gossip Protocol** for cluster membership and failure detection
- **Distributed WAL** or **Replication Log** for cross-node durability

## Required Changes for Active-Active Clustering

### 1. Cluster Membership and Discovery

**Files to Modify**:
- `pkg/tieredcache/tieredcache.go` - Add cluster management
- New package: `pkg/cluster/` - Membership, gossip, failure detection

**Changes**:
- Add cluster configuration (node ID, cluster size, seed nodes)
- Implement gossip protocol for membership changes
- Add failure detection and node removal
- Handle node join/leave events with data rebalancing

### 2. Data Partitioning and Distribution

**Files to Modify**:
- `pkg/l0/l0_cache.go` - Sharding logic needs cluster awareness
- `pkg/l1/l1_badger.go` - Shard mapping needs to be cluster-wide
- New package: `pkg/partitioning/` - Consistent hashing implementation

**Changes**:
- Replace local shard hash (`fnv.New32a() % shardCount`) with cluster-aware consistent hashing
- Add virtual nodes for better distribution
- Implement data migration during node join/leave
- Add routing layer to direct requests to appropriate node

### 3. Replication Mechanism

**Files to Modify**:
- `pkg/tieredcache/tieredcache.go` - Write paths need replication
- `pkg/l0/l0_cache.go` - Set/Delete operations
- `pkg/l1/l1_badger.go` - Set/Delete operations
- New package: `pkg/replication/` - Replication log and network layer

**Changes**:
- Intercept all write operations (Set, Delete) for replication
- Create replication log entries similar to WAL but for cluster
- Implement async replication to replica nodes
- Handle replication acknowledgments and retries
- Add replication lag monitoring

### 4. Conflict Resolution

**Files to Modify**:
- `pkg/tieredcache/tieredcache.go` - Read paths need conflict detection
- `pkg/l0/l0_cache.go` - Get operations
- `pkg/l1/l1_badger.go` - Get operations
- New package: `pkg/conflict/` - Conflict resolution policies

**Changes**:
- Add vector clocks or version vectors to cache entries
- Detect concurrent updates during read operations
- Implement conflict resolution policies (LWW, application-specific)
- Provide hooks for custom conflict resolution

### 5. Distributed Recovery and WAL

**Files to Modify**:
- `pkg/replay/recovery.go` - WAL replay logic
- `pkg/tieredcache/tieredcache.go` - Recovery initialization
- New package: `pkg/durability/` - Distributed WAL/replication log

**Changes**:
- Extend WAL to include cluster-wide sequence numbers
- Implement distributed consensus for WAL commits (optional, for strong consistency)
- Allow recovery from peer nodes' WAL in addition to local WAL
- Add checkpoint sharing between nodes

### 6. Configuration and State Management

**Files to Modify**:
- `pkg/config/config.go` - Add cluster configuration section
- `pkg/tieredcache/tieredcache.go` - Configuration loading
- New package: `pkg/state/` - Distributed state management

**Changes**:
- Add cluster configuration (replication factor, consistency level, etc.)
- Implement dynamic configuration updates via gossip
- Distribute tiering policies and sink configurations
- Share statistics and metrics across cluster

### 7. Network and Communication Layer

**New Files**:
- `pkg/transport/` - RPC/gRPC or custom binary protocol
- `pkg/messaging/` - Message serialization and handling

**Changes**:
- Add gRPC or custom TCP-based communication between nodes
- Implement request routing for cross-node operations
- Add connection pooling and load balancing
- Handle network partitions and split-brain scenarios

## Impact Assessment

### Performance Implications

1. **Increased Latency**:
   - Write operations: + network round-trip time for replication
   - Read operations: potential cross-node fetch if local miss
   - Mitigation: Async replication, read-locality optimization

2. **Throughput Changes**:
   - Write throughput limited by slowest replica in strong consistency model
   - Mitigation: Async replication with eventual consistency
   - Read throughput can scale linearly with cluster size

3. **Resource Utilization**:
   - Additional memory for replication buffers and version metadata
   - Network bandwidth for replication traffic
   - CPU for serialization, hashing, and conflict detection

### Consistency Guarantees

1. **Trade-offs**:
   - Strong consistency: Higher latency, lower availability during partitions
   - Eventual consistency: Lower latency, potential for stale reads
   - Recommended: Tunable consistency per operation (like Cassandra)

2. **Conflict Resolution**:
   - Last-Write-Wins (LWW) with synchronized clocks (NTP/PTP)
   - Application-specific conflict handlers
   - CRDTs for specific data types (counters, sets)

### Failure Handling

1. **Node Failures**:
   - Automatic detection via gossip protocol
   - Data rebalancing to remaining nodes
   - Read/write requests routed to healthy nodes

2. **Network Partitions**:
   - Split-brain detection and resolution
   - Configurable behavior: allow writes in minority partition or reject
   - Anti-entropy repair after partition heals

3. **Data Loss Prevention**:
   - Replication factor >= 2 for durability
   - Persistent replication log on disk
   - Cross-node WAL sharing for recovery

## Implementation Roadmap

### Phase 1: Foundation
- Implement cluster membership and gossip protocol
- Add node ID and cluster configuration
- Basic failure detection

### Phase 2: Data Partitioning
- Implement consistent hashing with virtual nodes
- Add request routing layer
- Initial data distribution (no migration yet)

### Phase 3: Replication
- Create replication log format
- Implement async replication to replicas
- Add basic acknowledgment handling

### Phase 4: Conflict Resolution
- Add version vectors to cache entries
- Implement LWW conflict resolution
- Provide conflict resolution hooks

### Phase 5: Distributed Recovery
- Extend WAL for cluster awareness
- Implement peer-assisted recovery
- Add checkpoint sharing

### Phase 6: Advanced Features
- Tunable consistency levels
- Dynamic rebalancing
- Cross-cluster replication
- Monitoring and metrics

## Files Requiring Modification

### Core TieredCache Files:
1. `pkg/tieredcache/tieredcache.go` - Main cache implementation
2. `pkg/l0/l0_cache.go` - L0 cache operations
3. `pkg/l1/l1_badger.go` - L1 cache operations
4. `pkg/replay/recovery.go` - WAL and recovery
5. `pkg/config/config.go` - Configuration structure

### New Packages to Create:
1. `pkg/cluster/` - Membership, gossip, failure detection
2. `pkg/partitioning/` - Consistent hashing, data distribution
3. `pkg/replication/` - Replication log, network replication
4. `pkg/conflict/` - Version vectors, resolution policies
5. `pkg/transport/` - Inter-node communication
6. `pkg/state/` - Distributed state management
7. `pkg/durability/` - Distributed WAL/log

## Recommendations

1. **Start with Eventual Consistency**: Implement async replication with LWW conflict resolution for best performance and simplicity.

2. **Use Battle-Tested Libraries**: Consider using established libraries for:
   - Gossip protocol: `hashicorp/memberlist`
   - Consistent hashing: `github.com/cespare/xxhash/v2` for hashing
   - gRPC: `google.golang.org/grpc` for inter-node communication

3. **Phased Rollout**: Implement features incrementally, starting with non-critical paths.

4. **Comprehensive Testing**: Develop chaos testing framework to validate behavior under network partitions, node failures, etc.

5. **Monitoring and Observability**: Add extensive metrics for replication lag, conflict rates, node health, etc.

6. **Documentation and Operability**: Provide clear operational guides for cluster management, troubleshooting, and tuning.

## Conclusion

Transitioning TieredCache to an active-active cluster architecture requires significant changes across multiple layers of the system. However, the resulting benefits in terms of scalability, fault tolerance, and performance under load make it a worthwhile evolution for high-demand applications. The recommended approach focuses on eventual consistency with tunable levels, leveraging the system's existing sharding design while adding the necessary distributed systems primitives.

The estimated effort would be substantial (several months for a team of 2-3 engineers) but would result in a production-ready clustered caching system capable of handling enterprise-scale workloads.