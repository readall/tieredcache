# Consent Service Integration with TieredCache

## Overview
This document describes how the consent management service will integrate with the existing TieredCache system to provide efficient storage, retrieval, and management of consent records while ensuring DPDP Act compliance.

## Integration Architecture

### High-Level Design
The consent service will use the TieredCache as its primary storage layer, leveraging its multi-tier architecture for optimal performance and durability:

```
Consent Service Layer
        ↓ (API)
Consent Service Implementation
        ↓ (Storage Interface)
TieredCache System
        ↓
┌─────────────┐    ┌──────────────┐    ┌──────────────────────┐
│   L0 Cache  │ →  │  L1 Badger   │ →  │     L2 Cold Tier     │
│ (In-Memory) │    │  (SSD)       │    │                      │
│             │    │              │    │  ┌────┐ ┌────┐ ┌───┐ │
│             │    │              │    │  │Kafk│ │Mini│ │PG │ │
│             │    │              │    │  │a   │ │o   │ │   │ │
│             │    │              │    │  │ └────┘ └────┘ └───┘ │
└─────────────┘    └──────────────┘    └──────────────────────┘
```

### Storage Strategy

#### 1. Primary Storage (Consent Records)
- **Key Format**: `consent:{consent_id}`
- **Value**: Serialized Consent struct (JSON or protobuf)
- **Tiers**: 
  - L0: Active consents (status = given) for fast access
  - L1: All consent records for persistence
  - L2: Archived/expired consents for long-term retention

#### 2. Secondary Indexes (for efficient querying)
To support queries by data principal, data fiduciary, purpose, status, and composite queries, we'll maintain separate index structures:

**Data Principal Index**
- **Key Format**: `index:dp:{data_principal_id}`
- **Value**: Set of consent IDs (stored as JSON array or using TieredCache's internal set structure)
- **Purpose**: Find all consents for a specific user

**Data Fiduciary Index**
- **Key Format**: `index:df:{data_fiduciary_id}`
- **Value**: Set of consent IDs
- **Purpose**: Find all consents obtained by a specific entity

**Purpose Index**
- **Key Format**: `index:purpose:{purpose}`
- **Value**: Set of consent IDs
- **Purpose**: Find all consents for a specific purpose

**Status Index**
- **Key Format**: `index:status:{status}`
- **Value**: Set of consent IDs
- **Purpose**: Find all consents with a specific status (given, withdrawn, expired, rejected)

**Principal-Fiduciary-Purpose Index (Composite)**
- **Key Format**: `index:pfp:{data_principal_id}:{data_fiduciary_id}:{purpose}`
- **Value**: Consent ID (or set if multiple consents allowed for same combination)
- **Purpose**: Find consent by principal, fiduciary, and purpose combination

**Principal-Fiduciary-Collector-Purpose Index (Composite)**
- **Key Format**: `index:pfcp:{data_principal_id}:{data_fiduciary_id}:{consent_collector_id}:{purpose}`
- **Value**: Consent ID (or set if multiple consents allowed for same combination)
- **Purpose**: Find consent by principal, fiduciary, collector, and purpose combination

### Integration Points

#### 1. Consent Creation Flow
```
1. Service receives CreateConsentRequest
2. Validate request data
3. Generate ConsentID
4. Create Consent object with status=given
5. Store consent record in TieredCache:
   - L0.Put(consent:{consent_id}, serialized_consent)
   - L1.Put(consent:{consent_id}, serialized_consent)
6. Update indexes:
   - L0.PushToSet(index:dp:{data_principal_id}, consent_id)
   - L1.PushToSet(index:dp:{data_principal_id}, consent_id)
   - L0.PushToSet(index:df:{data_fiduciary_id}, consent_id)
   - L1.PushToSet(index:df:{data_fiduciary_id}, consent_id)
   - L0.PushToSet(index:purpose:{purpose}, consent_id)
   - L1.PushToSet(index:purpose:{purpose}, consent_id)
   - L0.PushToSet(index:status:given, consent_id)
   - L1.PushToSet(index:status:given, consent_id)
   - L0.PushToSet(index:pfp:{data_principal_id}:{data_fiduciary_id}:{purpose}, consent_id)
   - L1.PushToSet(index:pfp:{data_principal_id}:{data_fiduciary_id}:{purpose}, consent_id)
   - L0.PushToSet(index:pfcp:{data_principal_id}:{data_fiduciary_id}:{consent_collector_id}:{purpose}, consent_id)
   - L1.PushToSet(index:pfcp:{data_principal_id}:{data_fiduciary_id}:{consent_collector_id}:{purpose}, consent_id)
7. Return CreateConsentResponse
```

#### 2. Consent Retrieval Flow by ID
```
1. Service receives GetConsentRequest with consent_id
2. Try to get from L0: L0.Get(consent:{consent_id})
3. If not found in L0, try L1: L1.Get(consent:{consent_id})
4. If found, deserialize and return
5. If not found in either, return not found error
```

#### 3. Consent Retrieval Flow by Principal, Fiduciary, and Purpose
```
1. Service receives GetConsentByPrincipalAndPurposeRequest with data_principal_id, data_fiduciary_id, purpose
2. Try to get from L0: L0.Get(index:pfp:{data_principal_id}:{data_fiduciary_id}:{purpose})
3. If not found in L0, try L1: L1.Get(index:pfp:{data_principal_id}:{data_fiduciary_id}:{purpose})
4. If found, deserialize to get consent ID(s)
5. For each consent ID, get consent record from TieredCache (L0 then L1)
6. Return the consent record(s)
7. If not found in either, return not found error
```

#### 4. Consent Retrieval Flow by Principal, Fiduciary, Collector, and Purpose
```
1. Service receives GetConsentByPrincipalFiduciaryCollectorAndPurposeRequest with data_principal_id, data_fiduciary_id, consent_collector_id, purpose
2. Try to get from L0: L0.Get(index:pfcp:{data_principal_id}:{data_fiduciary_id}:{consent_collector_id}:{purpose})
3. If not found in L0, try L1: L1.Get(index:pfcp:{data_principal_id}:{data_fiduciary_id}:{consent_collector_id}:{purpose})
4. If found, deserialize to get consent ID(s)
5. For each consent ID, get consent record from TieredCache (L0 then L1)
6. Return the consent record(s)
7. If not found in either, return not found error
```

#### 5. Consent Withdrawal Flow
```
1. Service receives WithdrawConsentRequest
2. Validate request (ensure withdrawn_by matches data_principal_id)
3. Get existing consent from TieredCache (L0 then L1)
4. If not found, return error
5. If found and status is not given, return appropriate error
6. Update consent object:
   - Set status = withdrawn
   - Set withdrawal_timestamp = now
   - Increment version
   - Set last_modified_by = withdrawn_by
   - Set last_modified_at = now
7. Store updated consent in TieredCache:
   - L0.Put(consent:{consent_id}, updated_serialized_consent)
   - L1.Put(consent:{consent_id}, updated_serialized_consent)
8. Update status indexes:
   - L0.RemoveFromSet(index:status:given, consent_id)
   - L1.RemoveFromSet(index:status:given, consent_id)
   - L0.PushToSet(index:status:withdrawn, consent_id)
   - L1.PushToSet(index:status:withdrawn, consent_id)
9. Return success
```

#### 6. Consent Update Flow
```
1. Service receives UpdateConsentRequest
2. Validate request
3. Get existing consent from TieredCache (L0 then L1)
4. If not found, return error
5. If found and status is not given, return error (cannot update withdrawn/expired consent)
6. Update consent object with provided fields
7. Increment version
8. Set last_modified_by = updated_by
9. Set last_modified_at = now
10. Store updated consent in TieredCache:
    - L0.Put(consent:{consent_id}, updated_serialized_consent)
    - L1.Put(consent:{consent_id}, updated_serialized_consent)
11. Update indexes if purpose changed:
    - Remove from old purpose index
    - Add to new purpose index
    - Update composite indexes accordingly
12. Return updated consent
```

#### 7. Consent Validity Check Flow
```
1. Service receives IsConsentValidRequest with consent_id
2. Get consent from TieredCache (L0 then L1)
3. If not found, return false (invalid)
4. Check consent status:
   - If status is withdrawn, expired, or rejected → return false
5. Check expiry:
   - If expiry_timestamp is set and now > expiry_timestamp → return false
6. Return true (valid)
```

#### 8. List Consents Flow
```
1. Service receives ListConsentsRequest (by data_principal_id or data_fiduciary_id)
2. Get index set from TieredCache:
   - For data principal: L0.Get(index:dp:{data_principal_id}) then L1.Get if needed
   - For data fiduciary: L0.Get(index:df:{data_fiduciary_id}) then L1.Get if needed
3. If index not found, return empty list
4. Deserialize index to get set of consent IDs
5. For each consent ID, get consent record from TieredCache (L0 then L1)
6. Filter by status if requested
7. Return list of consents
```

#### 9. Expired Consent Processing Flow (Background Job)
```
1. Background job runs periodically
2. Scan for potentially expired consents:
   - Approach 1: Query index:status:given and check each consent's expiry
   - Approach 2: Maintain a sorted set by expiry timestamp for efficient scanning
3. For each consent in status:given:
   - Get consent record
   - Check if expired (expiry_timestamp < now)
   - If expired:
     a. Update consent status to expired
     b. Store updated consent in TieredCache
     c. Move consent ID from index:status:given to index:status:expired
     d. Update purpose indexes (remove from purpose sets)
     e. Update composite indexes (remove from pfp and pfcp sets)
4. Return count of processed consents
```

### Technical Implementation Details

#### 1. Storage Abstraction Layer
The consent service will use a storage abstraction layer that interacts with TieredCache:

```go
type ConsentStorage interface {
    StoreConsent(ctx context.Context, consent *Consent) error
    GetConsent(ctx context.Context, consentID string) (*Consent, error)
    GetConsentByPrincipalAndPurpose(ctx context.Context, dataPrincipalID string, dataFiduciaryID string, purpose string) ([]*Consent, error)
    GetConsentByPrincipalFiduciaryCollectorAndPurpose(ctx context.Context, dataPrincipalID string, dataFiduciaryID string, consentCollectorID string, purpose string) ([]*Consent, error)
    UpdateConsent(ctx context.Context, consent *Consent) error
    DeleteConsent(ctx context.Context, consentID string) error
    AddToIndex(ctx context.Context, indexKey string, consentID string) error
    RemoveFromIndex(ctx context.Context, indexKey string, consentID string) error
    GetIndexMembers(ctx context.Context, indexKey string) ([]string, error)
}
```

#### 2. TieredCache Implementation
```go
type tieredcacheConsentStorage struct {
    cache *tieredcache.TieredCache
}

func (s *tieredcacheConsentStorage) StoreConsent(ctx context.Context, consent *Consent) error {
    // Serialize consent
    data, err := json.Marshal(consent)
    if err != nil {
        return err
    }
    
    // Store in both L0 and L1
    key := fmt.Sprintf("consent:%s", consent.ConsentID)
    if err := s.cache.Set(ctx, key, data, 0); err != nil {
        return err
    }
    // Note: TieredCache.Set already writes to both L0 and L1
    return nil
}

func (s *tieredcacheConsentStorage) GetConsent(ctx context.Context, consentID string) (*Consent, error) {
    key := fmt.Sprintf("consent:%s", consentID)
    data, err := s.cache.Get(ctx, key)
    if err != nil {
        return nil, err
    }
    
    var consent Consent
    if err := json.Unmarshal(data, &consent); err != nil {
        return nil, err
    }
    return &consent, nil
}

func (s *tieredcacheConsentStorage) GetConsentByPrincipalAndPurpose(ctx context.Context, dataPrincipalID string, dataFiduciaryID string, purpose string) ([]*Consent, error) {
    // Get from composite index first
    indexKey := fmt.Sprintf("index:pfp:%s:%s:%s", dataPrincipalID, dataFiduciaryID, purpose)
    consentIDs, err := s.GetIndexMembers(ctx, indexKey)
    if err != nil {
        return nil, err
    }
    
    var consents []*Consent
    for _, consentID := range consentIDs {
        consent, err := s.GetConsent(ctx, consentID)
        if err != nil {
            // Log error but continue with other consents
            continue
        }
        consents = append(consents, consent)
    }
    
    return consents, nil
}

// Similar implementation for GetConsentByPrincipalFiduciaryCollectorAndPurpose...

// Similar implementations for other methods...
```

#### 3. Index Management
Since TieredCache doesn't have built-in set operations, we'll implement sets using slices or use a separate service:

Option A: Store sets as JSON arrays
- Get current set
- Deserialize to []string
- Add/remove element
- Serialize back
- Store back

Option B: Use a dedicated indexing service
- Could use Redis or another system just for indexes
- More complex but better performance for large sets

Option C: Use TieredCache with composite keys
- Store each index member as a separate key: `index:dp:{data_principal_id}:{consent_id}:true`
- To get all members, scan for keys with prefix (requires iteration support)

Given that we're extending an existing system, Option A (JSON arrays) is simplest to implement initially.

### Consistency and Durability Considerations

#### Write Consistency
When updating both consent records and indexes, we need to ensure consistency:

1. **Approach 1: Transactional Outbox Pattern**
   - Write all changes to a transaction log first
   - Have a background process that applies changes to TieredCache
   - Provides eventual consistency

2. **Approach 2: Two-Phase Commit Simulation**
   - Phase 1: Write to temporary locations
   - Phase 2: Move to final locations
   - If failure occurs during phase 1, clean up temporaries
   - If failure occurs during phase 2, retry until success

3. **Approach 3: Accept Brief Inconsistency**
   - For consent management, brief inconsistencies are acceptable
   - Implement retry mechanisms and consistency checks
   - Use timestamps to resolve conflicts

Given the requirements of the DPDP Act and the nature of consent data, Approach 3 is acceptable with proper monitoring and alerting.

#### Failure Scenarios and Recovery
1. **Partial Index Updates**
   - If consent record is updated but index update fails
   - Solution: Background reconciliation job that verifies index consistency
   
2. **Cache Tier Failures**
   - If L0 is unavailable, fall back to L1
   - If L1 is unavailable, system should degrade gracefully
   - L2 is for archival only, not critical for operation

3. **Corruption Detection**
   - Store checksums with consent records
   - Verify checksums on read
   - Log and alert on corruption

### Performance Optimization

#### 1. Read Optimization
- Active consent lookups (by consent_id) served from L0
- Frequently accessed indexes kept in L0
- Use of promotion heuristics in TieredCache to keep hot data in L0

#### 2. Write Optimization
- Batch index updates where possible
- Use asynchronous writes for non-critical operations
- Leverage TieredCache's write-behind capabilities

#### 3. Memory Management
- Set appropriate TTLs for different types of data
- Active consents: Long TTL or no expiry
- Indexes: Moderate TTL
- Archived data: Moved to L2

### Monitoring and Observability

#### 1. Metrics to Track
- Consent creation rate
- Consent withdrawal rate
- Consent validation requests
- Index lookup performance
- Cache hit/miss ratios for L0 and L1
- Storage tier utilization

#### 2. Logging
- Log all consent operations with consent ID (but not personal data)
- Log errors and failed operations
- Audit trail for compliance

#### 3. Health Checks
- Verify TieredCache connectivity
- Check that indexes are consistent with consent records
- Validate that background jobs are running

### Implementation Roadmap

#### Phase 1: Basic Storage Integration
- Implement ConsentStorage interface using TieredCache
- Store and retrieve consent records
- Basic error handling

#### Phase 2: Index Implementation
- Implement index storage using JSON arrays
- Create helper functions for index operations
- Basic query capabilities including the new purpose-based and composite indexes

#### Phase 3: Full API Implementation
- Implement all consent service operations including the new retrieval methods
- Add validation and error handling
- Basic logging

#### Phase 4: Optimization and Monitoring
- Add performance optimizations
- Implement monitoring and metrics
- Add comprehensive logging and audit trails
- Implement background jobs for expiry processing

### Compliance Considerations

#### 1. Data Minimization
- Only store necessary personal data in consent records
- Avoid storing sensitive personal data unless required for the consent purpose

#### 2. Purpose Limitation
- Explicitly store purpose in consent records
- Validate that data usage aligns with stated purpose

#### 3. Storage Limitation
- Implement expiry mechanisms for consents
- Archive expired consents to L2 after a period
- Secure deletion of consents when required by law

#### 4. Rights of Data Principals
- Easy withdrawal mechanism
- Ability to access their consent records
- Clear audit trail of all operations

#### 5. Accountability
- Comprehensive logging of all operations
- Version tracking for all changes
- Legal reference tracking