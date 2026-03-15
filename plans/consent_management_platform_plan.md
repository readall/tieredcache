# Consent Management Platform Plan
## A Platform Built on TieredCache for Scale and Performance

### Executive Summary
This document outlines the plan for a new Consent Management Platform (CMP) that leverages the TieredCache library as its core storage layer. The platform is designed to handle 1 billion data principals, thousands of collectors, and multiple purpose combinations with high performance and DPDP Act compliance.

### Project Overview
The Consent Management Platform will be a standalone git repository that imports and utilizes the TieredCache library for efficient, multi-tiered storage of consent records. The platform will provide APIs for consent collection, management, querying, and compliance reporting.

### Architecture Overview
```
+---------------------+    +---------------------+    +---------------------+
|   API Gateway       |    |   Consent Service   |    |   Admin Dashboard   |
+---------------------+    +---------------------+    +---------------------+
           |                         |                         |
           v                         v                         v
+---------------------+    +---------------------+    +---------------------+
|   Auth Service      |    |   Consent Core      |    |   Compliance Engine |
+---------------------+    +---------------------+    +---------------------+
           |                         |                         |
           +-----------+-------------+-------------+-----------+
                       |             |             |
                       v             v             v
               +-------------------------------+
               |        TieredCache Layer      |
               |  (L0: Memory, L1: SSD, L2: Cold)|
               +-------------------------------+
                       |             |             |
                       v             v             v
               +-------------------------------+
               |   Monitoring & Alerting       |
               |   (Prometheus, Grafana, ELK)  |
               +-------------------------------+
```

### Key Components

#### 1. Consent Service (Main Application)
- RESTful API for consent operations
- gRPC service for high-throughput internal communication
- Business logic for consent lifecycle management
- Integration with TieredCache for storage

#### 2. Consent Core
- Consent validation engine
- Purpose-based access control
- Consent withdrawal and expiry processing
- Audit trail generation

#### 3. Compliance Engine
- DPDP Act compliance checks
- Data subject request handling (access, portability, deletion)
- Consent receipt generation
- Regulatory reporting

#### 4. TieredCache Integration Layer
- Wrapper/service for TieredCache operations
- Consent-specific key design and indexing
- Batch operations for efficiency
- Error handling and retry logic

### Data Model

#### Consent Record
```json
{
  "consent_id": "uuid",
  "data_principal_id": "string",
  "collector_id": "string",
  "purpose_id": "string",
  "status": "granted|denied|withdrawn|expired",
  "timestamp": "ISO8601",
  "version": "integer",
  "metadata": {
    "ip_address": "string",
    "user_agent": "string",
    "legal_basis": "string"
  }
}
```

#### Indexing Strategy (Optimized for TieredCache)
To eliminate JSON array manipulation overhead identified in the evaluation:
- Primary key: `consent:{consent_id}` → consent record JSON
- Data Principal Index: `index:dp:{data_principal_id}:{consent_id}:{status}:true`
- Collector Index: `index:coll:{collector_id}:{consent_id}:{purpose_id}:true`
- Purpose Index: `index:purpose:{purpose_id}:{consent_id}:{data_principal_id}:true`
- Composite indices for common queries

### API Design

#### Consent Management API
```
POST   /consents                    # Record a new consent
GET    /consents/{consent_id}       # Get consent by ID
GET    /consents?dp_id={id}         # Get consents for data principal
GET    /consents?collector={id}     # Get consents for collector
PUT    /consents/{consent_id}       # Update consent (status, metadata)
DELETE /consents/{consent_id}       # Withdraw consent
POST   /consents/batch              # Batch consent operations
```

#### Compliance API
```
GET    /compliance/requests         # List data subject requests
POST   /compliance/requests         # Create new request (access, deletion, etc.)
GET    /compliance/requests/{req_id}# Get request status
POST   /compliance/requests/{req_id}/cancel # Cancel request
GET    /compliance/reports          # Generate compliance reports
```

### TieredCache Usage Patterns

#### Write Path
1. Consent service receives consent record
2. Validate and enrich consent data
3. Store primary record: `Set(consent:{id}, consent_json)`
4. Update indices:
   - `Set(index:dp:{dp_id}:{consent_id}:{status}, "true")`
   - `Set(index:coll:{coll_id}:{consent_id}:{purpose}, "true")`
   - `Set(index:purpose:{purpose_id}:{consent_id}:{dp_id}, "true")`
5. Async: Write audit log to separate storage

#### Read Path
1. Get consent by ID: `Get(consent:{id})`
2. List consents for data principal:
   - Scan keys matching `index:dp:{dp_id}:*`
   - Extract consent IDs from matching keys
   - Batch get consent records
3. Similar patterns for collector and purpose queries

### Configuration
The platform will use a dedicated configuration file that references and potentially overrides TieredCache settings:

```yaml
# consent-platform/config.yaml
tieredcache:
  # Inherit base TieredCache configuration but can override
  l0:
    max_memory_mb: 65536    # 64GB
    shard_count: 256
  l1:
    max_capacity_gb: 16384  # 16TB
    shard_count: 64
    block_cache_size_mb: 8192
    num_goroutines: 32
    sync_mode: immediate
  tiering:
    l0_to_l1_threshold: 0.95
    l1_to_l2_threshold: 0.80
    tier_interval_sec: 300
    max_workers: 16
  replay:
    max_replay_workers: 16
    checkpoint_interval: 5000
  l2:
    sinks:
      kafka:
        batch_size: 1000
        flush_interval_ms: 5000

# Consent Platform Specific Settings
consent_platform:
  api:
    port: 8080
    read_timeout: 30s
    write_timeout: 30s
  consent:
    default_ttl_hours: 87600  # 10 years
    batch_size: 1000
    max_concurrent_requests: 1000
  compliance:
    request_ttl_days: 30
    audit_retention_years: 7
```

### Scaling Considerations (Building on Evaluation)

Based on the TieredCache scale evaluation:
- **Sharding**: 256 L0 shards, 64 L1 shards for optimal distribution
- **Memory**: 64GB L0 for hot consent records and indices
- **Storage**: 16TB L1 SSD for warm consent data
- **Index Optimization**: Composite keys eliminate read-modify-write cycles
- **Tiering Policies**: Adjusted thresholds to keep frequently accessed consents in L0/L1
- **Write Optimization**: Immediate sync mode for durability, batch operations
- **Expected Performance**: 50K+ TPS write, 100K+ TPS read throughput

### Implementation Roadmap

#### Phase 1: Foundation (Weeks 1-3)
- [ ] Initialize git repository with basic structure
- [ ] Implement core consent data model
- [ ] Create TieredCache wrapper/service layer
- [ ] Implement basic CRUD APIs for consents
- [ ] Configure basic TieredCache integration
- [ ] Write unit tests for core components

#### Phase 2: Core Functionality (Weeks 4-6)
- [ ] Implement consent validation engine
- [ ] Add purpose-based access control
- [ ] Implement consent withdrawal and expiry
- [ ] Create indexing layer with composite keys
- [ ] Implement batch consent operations
- [ ] Add comprehensive integration tests

#### Phase 3: Compliance & Reporting (Weeks 7-9)
- [ ] Implement compliance engine for DPDP Act
- [ ] Add data subject request handling
- [ ] Create audit trail generation
- [ ] Implement compliance reporting APIs
- [ ] Add monitoring and alerting
- [ ] Performance testing and optimization

#### Phase 4: Production Readiness (Weeks 10-12)
- [ ] Security hardening and penetration testing
- [ ] Load testing at target scale (simulated)
- [ ] Disaster recovery and backup procedures
- [ ] Documentation and deployment guides
- [ ] Beta testing with pilot customers

### Testing Strategy

#### Unit Tests
- Test consent validation logic
- Test TieredCache wrapper operations
- Test indexing key generation and parsing
- Test compliance rule engines

#### Integration Tests
- Test end-to-end consent lifecycle
- Test TieredCache integration under load
- Test batch operations consistency
- Test recovery and restart scenarios

#### Performance Tests
- Baseline performance measurement
- Load testing to validate 50K+ TPS write, 100K+ TPS read
- Stress testing to identify bottlenecks
- Long-running stability tests

#### Compliance Tests
- Verify DPDP Act requirement implementations
- Test data subject request workflows
- Validate audit trail completeness
- Test consent withdrawal processing

### Risk Mitigation

1. **Hotspot Prevention**: 
   - Use consistent hashing for shard distribution
   - Monitor shard utilization and rebalance if needed
   - Implement rate limiting per data principal/collector

2. **Data Consistency**:
   - Use version vectors for conflict detection
   - Implement read-repair mechanisms
   - Regular consistency audits

3. **Operational Complexity**:
   - Comprehensive health checks and monitoring
   - Automated failover and recovery procedures
   - Blue-green deployment strategy

4. **Compliance Risk**:
   - Regular third-party audits
   - Automated compliance validation tests
   - Data lineage tracking

### Dependencies
- TieredCache library (imported as dependency)
- Web framework (Gin/Echo or gRPC)
- Monitoring stack (Prometheus, Grafana)
- Logging stack (ELK or similar)
- Message queue (Kafka for event streaming)
- Cryptography libraries for secure handling

### Deployment Architecture
```
+---------------------+
|   Load Balancer     |
+---------------------+
|   API Gateway Tier  | (3+ nodes for HA)
+---------------------+
|   Consent Service   | (6+ nodes for scalability)
+---------------------+
|   TieredCache Nodes | (Distributed cluster)
+---------------------+
|   Monitoring Stack  |
+---------------------+
|   Backup Storage    |
+---------------------+
```

### Success Metrics
- Latency: 99% of consent operations < 10ms
- Throughput: 50K+ TPS write, 100K+ TPS read
- Availability: 99.9% uptime SLA
- Compliance: 100% DPDP Act requirement coverage
- Scalability: Linear performance improvement with added nodes

### Next Steps
1. Create git repository for consent-management-platform
2. Set up CI/CD pipeline
3. Implement Phase 1 foundation components
4. Begin TieredCache integration and testing