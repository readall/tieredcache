# Consent Management Service API Design

## Overview
This document defines the API for a consent management service that complies with India's Digital Personal Data Protection (DPDP) Act, 2023. The service will be built on top of the existing TieredCache system.

## Service Interface

### Core Operations
The consent management service provides the following core operations:

1. **Create Consent** - Record a new consent from a data principal
2. **Get Consent** - Retrieve consent details by consent ID or by principal/fiduciary/purpose combinations
3. **Withdraw Consent** - Allow data principal to withdraw consent at any time
4. **Update Consent** - Modify consent details (purpose, data categories, expiry)
5. **Check Consent Validity** - Verify if consent is currently valid
6. **List Consents** - Retrieve consents for a data principal or data fiduciary
7. **Expire Consents** - Process expired consents (typically run as a background job)

### Data Transfer Objects

#### CreateConsentRequest
```go
type CreateConsentRequest struct {
    DataPrincipalID       string   `json:"data_principal_id" validate:"required"`
    DataFiduciaryID       string   `json:"data_fiduciary_id" validate:"required"`
    ConsentCollectorID    string   `json:"consent_collector_id,omitempty"` // Entity/person collecting consent
    ConsentCollectionChannel string   `json:"consent_collection_channel,omitempty"` // Channel via which consent is collected
    PartnerID             string   `json:"partner_id,omitempty"` // Partner involved in consent collection
    Purpose               string   `json:"purpose" validate:"required"`
    DataCategories        []string `json:"data_categories" validate:"required,min=1"`
    ExpiryTimestamp       *time.Time `json:"expiry_timestamp,omitempty"`
    LegalReference        string   `json:"legal_reference,omitempty"`
    Metadata              map[string]string `json:"metadata,omitempty"`
}
```

#### CreateConsentResponse
```go
type CreateConsentResponse struct {
    ConsentID         string    `json:"consent_id"`
    Status            string    `json:"status"`
    ConsentTimestamp  time.Time `json:"consent_timestamp"`
    Version           int       `json:"version"`
}
```

#### GetConsentResponse
```go
type GetConsentResponse struct {
    ConsentID             string    `json:"consent_id"`
    DataPrincipalID       string    `json:"data_principal_id"`
    DataFiduciaryID       string    `json:"data_fiduciary_id"`
    ConsentCollectorID    string    `json:"consent_collector_id,omitempty"`
    ConsentCollectionChannel string    `json:"consent_collection_channel,omitempty"`
    PartnerID             string    `json:"partner_id,omitempty"`
    Purpose               string    `json:"purpose"`
    DataCategories        []string  `json:"data_categories"`
    Status                string    `json:"status"`
    ConsentTimestamp      time.Time `json:"consent_timestamp"`
    ExpiryTimestamp       *time.Time `json:"expiry_timestamp,omitempty"`
    WithdrawalTimestamp   *time.Time `json:"withdrawal_timestamp,omitempty"`
    Version               int       `json:"version"`
    LegalReference        string    `json:"legal_reference,omitempty"`
    Metadata              map[string]string `json:"metadata,omitempty"`
    LastModifiedBy        string    `json:"last_modified_by,omitempty"`
    LastModifiedAt        time.Time `json:"last_modified_at"`
}
```

#### WithdrawConsentRequest
```go
type WithdrawConsentRequest struct {
    ConsentID     string `json:"consent_id" validate:"required"`
    WithdrawnBy   string `json:"withdrawn_by" validate:"required"` // Should be the data principal ID
}
```

#### UpdateConsentRequest
```go
type UpdateConsentRequest struct {
    ConsentID             string   `json:"consent_id" validate:"required"`
    Purpose               string   `json:"purpose,omitempty"`
    DataCategories        []string `json:"data_categories,omitempty"`
    ExpiryTimestamp       *time.Time `json:"expiry_timestamp,omitempty"`
    LegalReference        string   `json:"legal_reference,omitempty"`
    Metadata              map[string]string `json:"metadata,omitempty"`
    ConsentCollectorID    string   `json:"consent_collector_id,omitempty"`
    ConsentCollectionChannel string   `json:"consent_collection_channel,omitempty"`
    PartnerID             string   `json:"partner_id,omitempty"`
    UpdatedBy             string   `json:"updated_by" validate:"required"`
}
```

### Service Interface Definition

```go
type ConsentService interface {
    // CreateConsent records a new consent
    CreateConsent(ctx context.Context, req *CreateConsentRequest) (*CreateConsentResponse, error)
    
    // GetConsent retrieves consent details by consent ID
    GetConsent(ctx context.Context, consentID string) (*GetConsentResponse, error)
    
    // GetConsentByPrincipalAndPurpose retrieves consent by data principal, fiduciary, and purpose
    GetConsentByPrincipalAndPurpose(ctx context.Context, dataPrincipalID string, dataFiduciaryID string, purpose string) (*GetConsentResponse, error)
    
    // GetConsentByPrincipalFiduciaryCollectorAndPurpose retrieves consent by data principal, fiduciary, collector, and purpose
    GetConsentByPrincipalFiduciaryCollectorAndPurpose(ctx context.Context, dataPrincipalID string, dataFiduciaryID string, consentCollectorID string, purpose string) (*GetConsentResponse, error)
    
    // GetConsentsByDataPrincipal retrieves all consents for a data principal
    GetConsentsByDataPrincipal(ctx context.Context, dataPrincipalID string) ([]*GetConsentResponse, error)
    
    // GetConsentsByDataFiduciary retrieves all consents for a data fiduciary
    GetConsentsByDataFiduciary(ctx context.Context, dataFiduciaryID string) ([]*GetConsentResponse, error)
    
    // WithdrawConsent allows a data principal to withdraw consent
    WithdrawConsent(ctx context.Context, req *WithdrawConsentRequest) error
    
    // UpdateConsent modifies consent details
    UpdateConsent(ctx context.Context, req *UpdateConsentRequest) (*GetConsentResponse, error)
    
    // IsConsentValid checks if a consent is currently valid
    IsConsentValid(ctx context.Context, consentID string) (bool, error)
    
    // ProcessExpiredConsents marks expired consents as expired (background job)
    ProcessExpiredConsents(ctx context.Context) (int, error)
}
```

## TieredCache Integration Strategy

### Storage Approach
Consent records will be stored in the TieredCache system with the following strategy:

1. **Primary Key**: ConsentID (for direct lookup)
2. **Secondary Indexes**: 
   - DataPrincipalID -> Set of ConsentIDs
   - DataFiduciaryID -> Set of ConsentIDs
   - Status -> Set of ConsentIDs (for efficient querying)
   - Purpose -> Set of ConsentIDs (for efficient querying by purpose)
   - Composite indexes for common query patterns:
     - DataPrincipalID:DataFiduciaryID:Purpose -> ConsentID
     - DataPrincipalID:DataFiduciaryID:ConsentCollectorID:Purpose -> ConsentID

### Storage Tiers
- **L0 (In-Memory)**: Active consents (status = given) for fast access
- **L1 (SSD)**: All consent records for persistence and recovery
- **L2 (Cold Storage)**: Archived/expired consents for long-term retention and audit

### Cache Key Structure
- Consent records: `consent:{consent_id}`
- Data principal index: `index:dp:{data_principal_id}`
- Data fiduciary index: `index:df:{data_fiduciary_id}`
- Status index: `index:status:{status}`
- Purpose index: `index:purpose:{purpose}`
- Principal-Fiduciary-Purpose index: `index:pfp:{data_principal_id}:{data_fiduciary_id}:{purpose}`
- Principal-Fiduciary-Collector-Purpose index: `index:pfcp:{data_principal_id}:{data_fiduciary_id}:{consent_collector_id}:{purpose}`

### Example Storage Flow
1. When creating a consent:
   - Store consent record in L0 and L1
   - Add ConsentID to data principal index in L0 and L1
   - Add ConsentID to data fiduciary index in L0 and L1
   - Add ConsentID to status index (given) in L0 and L1
   - Add ConsentID to purpose index in L0 and L1
   - Add ConsentID to principal-fiduciary-purpose index in L0 and L1
   - Add ConsentID to principal-fiduciary-collector-purpose index in L0 and L1

2. When withdrawing consent:
   - Update consent record in L0 and L1 (status = withdrawn)
   - Remove ConsentID from status index (given) in L0 and L1
   - Add ConsentID to status index (withdrawn) in L0 and L1

3. When checking validity:
   - Retrieve consent record from L0 (fallback to L1)
   - Check status and expiry timestamp

## API Endpoints (RESTful)

### Consent Creation
```
POST /api/v1/consents
Content-Type: application/json

{
    "data_principal_id": "user123",
    "data_fiduciary_id": "company456",
    "consent_collector_id": "collector789",
    "consent_collection_channel": "website",
    "partner_id": "partnerXYZ",
    "purpose": "marketing",
    "data_categories": ["email", "phone_number"],
    "expiry_timestamp": "2025-12-31T23:59:59Z",
    "legal_reference": "DPDP Act Section 6",
    "metadata": {
        "campaign_id": "summer2024",
        "channel": "email"
    }
}
```

Response:
```json
{
    "consent_id": "consent_abc123",
    "status": "given",
    "consent_timestamp": "2024-03-14T10:30:00Z",
    "version": 1
}
```

### Get Consent by ID
```
GET /api/v1/consents/consent_abc123
```

Response:
```json
{
    "consent_id": "consent_abc123",
    "data_principal_id": "user123",
    "data_fiduciary_id": "company456",
    "consent_collector_id": "collector789",
    "consent_collection_channel": "website",
    "partner_id": "partnerXYZ",
    "purpose": "marketing",
    "data_categories": ["email", "phone_number"],
    "status": "given",
    "consent_timestamp": "2024-03-14T10:30:00Z",
    "expiry_timestamp": "2025-12-31T23:59:59Z",
    "version": 1,
    "legal_reference": "DPDP Act Section 6",
    "metadata": {
        "campaign_id": "summer2024",
        "channel": "email"
    },
    "last_modified_at": "2024-03-14T10:30:00Z"
}
```

### Get Consent by Principal, Fiduciary, and Purpose
```
GET /api/v1/consents/find?data_principal_id=user123&data_fiduciary_id=company456&purpose=marketing
```

Response:
```json
{
    "consent_id": "consent_abc123",
    "data_principal_id": "user123",
    "data_fiduciary_id": "company456",
    "consent_collector_id": "collector789",
    "consent_collection_channel": "website",
    "partner_id": "partnerXYZ",
    "purpose": "marketing",
    "data_categories": ["email", "phone_number"],
    "status": "given",
    "consent_timestamp": "2024-03-14T10:30:00Z",
    "expiry_timestamp": "2025-12-31T23:59:59Z",
    "version": 1,
    "legal_reference": "DPDP Act Section 6",
    "metadata": {
        "campaign_id": "summer2024",
        "channel": "email"
    },
    "last_modified_at": "2024-03-14T10:30:00Z"
}
```

### Get Consent by Principal, Fiduciary, Collector, and Purpose
```
GET /api/v1/consents/find?data_principal_id=user123&data_fiduciary_id=company456&consent_collector_id=collector789&purpose=marketing
```

Response:
```json
{
    "consent_id": "consent_abc123",
    "data_principal_id": "user123",
    "data_fiduciary_id": "company456",
    "consent_collector_id": "collector789",
    "consent_collection_channel": "website",
    "partner_id": "partnerXYZ",
    "purpose": "marketing",
    "data_categories": ["email", "phone_number"],
    "status": "given",
    "consent_timestamp": "2024-03-14T10:30:00Z",
    "expiry_timestamp": "2025-12-31T23:59:59Z",
    "version": 1,
    "legal_reference": "DPDP Act Section 6",
    "metadata": {
        "campaign_id": "summer2024",
        "channel": "email"
    },
    "last_modified_at": "2024-03-14T10:30:00Z"
}
```

### Withdraw Consent
```
POST /api/v1/consents/consent_abc123/withdraw
Content-Type: application/json

{
    "withdrawn_by": "user123"
}
```

Response: 204 No Content

### Update Consent
```
PATCH /api/v1/consents/consent_abc123
Content-Type: application/json

{
    "purpose": "service_provision",
    "data_categories": ["name", "email", "address"],
    "expiry_timestamp": "2026-12-31T23:59:59Z",
    "updated_by": "user123"
}
```

Response:
```json
{
    "consent_id": "consent_abc123",
    "data_principal_id": "user123",
    "data_fiduciary_id": "company456",
    "consent_collector_id": "collector789",
    "consent_collection_channel": "website",
    "partner_id": "partnerXYZ",
    "purpose": "service_provision",
    "data_categories": ["name", "email", "address"],
    "status": "given",
    "consent_timestamp": "2024-03-14T10:30:00Z",
    "expiry_timestamp": "2026-12-31T23:59:59Z",
    "withdrawal_timestamp": null,
    "version": 2,
    "legal_reference": "DPDP Act Section 6",
    "metadata": {
        "campaign_id": "summer2024",
        "channel": "email"
    },
    "last_modified_by": "user123",
    "last_modified_at": "2024-03-14T14:45:00Z"
}
```

### List Consents by Data Principal
```
GET /api/v1/consents?data_principal_id=user123&status=given
```

Response:
```json
[
    {
        "consent_id": "consent_abc123",
        "data_principal_id": "user123",
        "data_fiduciary_id": "company456",
        "consent_collector_id": "collector789",
        "consent_collection_channel": "website",
        "partner_id": "partnerXYZ",
        "purpose": "marketing",
        "data_categories": ["email", "phone_number"],
        "status": "given",
        "consent_timestamp": "2024-03-14T10:30:00Z",
        "expiry_timestamp": "2025-12-31T23:59:59Z",
        "version": 1
    }
]
```

## Error Handling

The service will return appropriate HTTP status codes and error messages:

- 400 Bad Request: Invalid request data
- 401 Unauthorized: Missing or invalid authentication
- 403 Forbidden: Insufficient permissions
- 404 Not Found: Consent not found
- 409 Conflict: Consent already withdrawn/expired
- 422 Unprocessable Entity: Validation failed
- 500 Internal Server Error: Unexpected error
- 503 Service Unavailable: Service temporarily unavailable

Error response format:
```json
{
    "error": {
        "code": "VALIDATION_ERROR",
        "message": "Data principal ID is required",
        "details": {
            "field": "data_principal_id",
            "issue": "required field missing"
        }
    }
}
```

## Security Considerations

1. **Authentication & Authorization**
   - All endpoints require authentication
   - Data principals can only manage their own consents
   - Data fiduciaries can only manage consents they obtained
   - Administrators can perform audit and maintenance operations

2. **Data Protection**
   - Consent records containing personal data must be encrypted at rest
   - Access logs must be maintained for all consent operations
   - Regular security assessments and penetration testing

3. **Audit Trail**
   - All consent operations must be logged with:
     - Who performed the operation
     - When it was performed
     - What was changed
     - Legal basis for the operation

## Implementation Notes

1. **Idempotency**
   - Create operations should be idempotent where possible
   - Withdraw operations should be idempotent (withdrawing an already withdrawn consent should succeed)

2. **Consistency**
   - The service should provide read-after-write consistency for consent operations
   - Distributed transactions may be needed for updating multiple indexes

3. **Performance**
   - Active consent lookups should be served from L0 cache
   - Background jobs for expiry processing should not impact foreground operations
   - Proper indexing strategies for efficient querying

4. **Scalability**
   - The service should be horizontally scalable
   - Shared state should be managed through the TieredCache system
   - Load balancing and auto-scaling capabilities