# Consent Management Platform Requirement Specification

## 1. Introduction

### 1.1 Purpose
This document specifies the functional and non-functional requirements for the Consent Management Platform (CMP) designed to comply with India's Digital Personal Data Protection (DPDP) Act, 2023. The platform will manage the complete lifecycle of consent from creation through withdrawal, update, and expiry.

### 1.2 Scope
The Consent Management Platform provides:
- RESTful APIs for consent management operations
- Secure storage and retrieval of consent records using the TieredCache system
- Comprehensive audit logging for compliance
- Integration capabilities with identity management, data processing, and monitoring systems
- Administrative interfaces for configuration and monitoring

### 1.3 Definitions and Acronyms
- **DPDP Act**: Digital Personal Data Protection Act, 2023 (India)
- **Data Principal**: The individual to whom the personal data relates
- **Data Fiduciary**: Any person who alone or in conjunction with others determines the purpose and means of processing personal data
- **Consent Collector**: Entity/person who collects the consent on behalf of the data fiduciary
- **Consent**: Any freely given, specific, informed and unambiguous indication of the data principal's wishes
- **TieredCache**: The existing multi-tier caching system (L0: memory, L1: SSD, L2: cold storage)
- **API**: Application Programming Interface
- **RBAC**: Role-Based Access Control
- **ABAC**: Attribute-Based Access Control

### 1.4 References
- Digital Personal Data Protection (DPDP) Act, 2023
- TieredCache System Documentation
- OAuth 2.0 Authorization Framework
- OpenID Connect Core 1.0
- OWASP ASVS (Application Security Verification Standard)
- ISO/IEC 27001:2022 Information Security Management

## 2. Functional Requirements

### 2.1 Consent Lifecycle Management

#### 2.1.1 Consent Creation
The system SHALL allow recording of a new consent from a data principal.

**FR-1.1**: The system SHALL provide an API endpoint to create a consent record with the following mandatory fields:
- Data Principal ID
- Data Fiduciary ID
- Purpose
- Data Categories

**FR-1.2**: The system SHALL allow optional fields in consent creation:
- Consent Collector ID
- Consent Collection Channel
- Partner ID
- Expiry Timestamp
- Legal Reference
- Metadata

**FR-1.3**: Upon successful creation, the system SHALL generate a unique Consent ID and return it in the response.

**FR-1.4**: The system SHALL set the initial consent status to "given" and record the consent timestamp.

**FR-1.5**: The system SHALL initialize the version counter to 1 for new consents.

**FR-1.6**: The system SHALL validate that the purpose is specific and clearly defined.

#### 2.1.2 Consent Retrieval
The system SHALL allow retrieval of consent records by various identifiers.

**FR-2.1**: The system SHALL provide an API endpoint to retrieve a consent by its unique Consent ID.

**FR-2.2**: The system SHALL provide an API endpoint to retrieve consents by the combination of:
- Data Principal ID
- Data Fiduciary ID
- Purpose

**FR-2.3**: The system SHALL provide an API endpoint to retrieve consents by the combination of:
- Data Principal ID
- Data Fiduciary ID
- Consent Collector ID
- Purpose

**FR-2.4**: For retrieval by principal/fiduciary/purpose combinations, if multiple consents exist, the system SHALL return all matching consents.

**FR-2.5**: The system SHALL return appropriate error responses when no matching consent is found.

#### 2.1.3 Consent Withdrawal
The system SHALL allow data principals to withdraw their consent at any time.

**FR-3.1**: The system SHALL provide an API endpoint to withdraw consent using the Consent ID.

**FR-3.2**: The withdrawal request SHALL require the identifier of the party withdrawing consent (expected to be the data principal).

**FR-3.3**: Upon withdrawal, the system SHALL update the consent status to "withdrawn".

**FR-3.4**: The system SHALL record the withdrawal timestamp.

**FR-3.5**: The system SHALL increment the version counter upon withdrawal.

**FR-3.6**: The system SHALL record who performed the withdrawal and when.

**FR-3.7**: The withdrawal operation SHALL be idempotent (withdrawing an already withdrawn consent SHALL succeed without error).

#### 2.1.4 Consent Update
The system SHALL allow modification of certain consent attributes while the consent is active.

**FR-4.1**: The system SHALL provide an API endpoint to update consent details.

**FR-4.2**: Updatable fields SHALL include:
- Purpose
- Data Categories
- Expiry Timestamp
- Legal Reference
- Metadata
- Consent Collector ID
- Consent Collection Channel
- Partner ID

**FR-4.3**: The following fields SHALL NOT be updatable through this endpoint (they create an audit trail instead):
- Consent ID
- Data Principal ID
- Data Fiduciary ID
- Consent Status
- Consent Timestamp
- Withdrawal Timestamp

**FR-4.4**: Update requests SHALL require the identifier of the party performing the update.

**FR-4.5**: Upon successful update, the system SHALL increment the version counter.

**FR-4.6**: The system SHALL record who performed the update and when.

**FR-4.7**: The system SHALL prevent updates to consents that are not in "given" status (withdrawn, expired, or rejected).

#### 2.1.5 Consent Validity Checking
The system SHALL provide a mechanism to check if a consent is currently valid for processing.

**FR-5.1**: The system SHALL provide an API endpoint to check consent validity by Consent ID.

**FR-5.2**: A consent SHALL be considered valid if and only if:
- Status is "given"
- Current timestamp is before the Expiry Timestamp (if set)
- Consent has not been withdrawn

**FR-5.3**: The endpoint SHALL return a boolean indicating validity.

**FR-5.4**: The endpoint SHALL return appropriate error responses for invalid Consent IDs.

#### 2.1.6 Consent Listing
The system SHALL allow listing of consents for a data principal or data fiduciary.

**FR-6.1**: The system SHALL provide an API endpoint to list all consents for a given Data Principal ID.

**FR-6.2**: The system SHALL provide an API endpoint to list all consents for a given Data Fiduciary ID.

**FR-6.3**: Listing endpoints SHALL support optional filtering by consent status.

**FR-6.4**: Listing endpoints SHALL support pagination for large result sets.

#### 2.1.7 Expired Consent Processing
The system SHALL automatically process consents that have reached their expiry date.

**FR-7.1**: The system SHALL provide a background job mechanism to identify and process expired consents.

**FR-7.2**: For each consent with an expiry timestamp in the past and status "given", the system SHALL:
- Update the status to "expired"
- Record the processing timestamp
- Increment the version counter
- Update indexes to reflect the status change

**FR-7.3**: The background job SHALL be configurable in terms of frequency and batch size.

**FR-7.4**: The system SHALL log the number of consents processed during each execution.

### 2.2 Consent Data Model Requirements

**FR-8.1**: The consent record SHALL include the following fields:
- ConsentID (string, unique identifier)
- DataPrincipalID (string)
- DataFiduciaryID (string)
- ConsentCollectorID (string, optional)
- ConsentCollectionChannel (string, optional)
- PartnerID (string, optional)
- Purpose (string)
- DataCategories (array of strings)
- Status (enum: given, withdrawn, expired, rejected)
- ConsentTimestamp (datetime)
- ExpiryTimestamp (datetime, optional)
- WithdrawalTimestamp (datetime, optional)
- Version (integer)
- LegalReference (string, optional)
- Metadata (map of string to string)
- LastModifiedBy (string, optional)
- LastModifiedAt (datetime)

**FR-8.2**: The system SHALL enforce that DataPrincipalID, DataFiduciaryID, Purpose, and DataCategories are required at creation.

**FR-8.3**: The system SHALL validate that DataCategories is a non-empty array.

**FR-8.4**: The system SHALL ensure that ConsentTimestamp is set to the creation time.

**FR-8.5**: The system SHALL ensure that Version starts at 1 and increments with each modification.

### 2.3 Audit and Compliance Requirements

**FR-9.1**: The system SHALL maintain an immutable audit trail of all consent operations.

**FR-9.2**: Each audit entry SHALL include:
- Operation type (create, retrieve, update, withdraw, etc.)
- Consent ID
- Timestamp of operation
- User/actor performing the operation
- Details of what was changed (for update operations)
- Legal basis or reference for the operation
- Outcome (success/failure)

**FR-9.3**: Audit logs SHALL be protected from tampering and unauthorized access.

**FR-9.4**: The system SHALL support exporting audit logs for compliance reporting.

**FR-9.5**: The system SHALL retain audit logs for the period required by applicable regulations.

### 2.4 Security Requirements

**FR-10.1**: All API endpoints SHALL require authentication.

**FR-10.2**: The system SHALL support OAuth 2.0 and OpenID Connect for authentication.

**FR-10.3**: The system SHALL implement fine-grained authorization:
- Data principals can only manage their own consents
- Data fiduciaries can only manage consents they obtained
- Administrators can perform audit and maintenance operations

**FR-10.4**: Consent records containing personal data SHALL be encrypted at rest using AES-256-GCM.

**FR-10.5**: All data in transit SHALL be protected using TLS 1.2 or higher.

**FR-10.6**: The system SHALL implement rate limiting to prevent abuse.

**FR-10.7**: The system SHALL protect against common web vulnerabilities (OWASP Top 10).

**FR-10.8**: The system SHALL perform input validation and output encoding to prevent injection attacks.

### 2.5 Integration Requirements

**FR-11.1**: The system SHALL provide APIs for integration with Identity and Access Management (IAM) systems.

**FR-11.2**: The system SHALL provide event notifications for consent changes to data processing systems.

**FR-11.3**: The system SHALL provide health check endpoints for monitoring systems.

**FR-11.4**: The system SHALL support standardized error formats for easy integration.

**FR-11.5**: The system SHALL be containerizable for deployment in orchestration platforms like Kubernetes.

## 3. Non-Functional Requirements

### 3.1 Performance Requirements

**NFR-1.1**: Consent creation operations SHALL complete within 200ms under normal load.

**NFR-1.2**: Consent retrieval by ID operations SHALL complete within 100ms under normal load.

**NFR-1.3**: Consent retrieval by principal/fiduciary/purpose operations SHALL complete within 300ms under normal load.

**NFR-1.4**: The system SHALL support a minimum of 1000 consent operations per second.

**NFR-1.5**: Background expiry processing SHALL not impact foreground operation performance by more than 10%.

### 3.2 Scalability Requirements

**NFR-2.1**: The system SHALL be horizontally scalable to handle increasing load.

**NFR-2.2**: The system SHALL support partitioning of consent data by data fiduciary or geographic region.

**NFR-2.3**: The system SHALL automatically leverage the TieredCache system's scaling capabilities.

**NFR-2.4**: The system SHALL support rolling updates without downtime.

### 3.3 Availability and Reliability Requirements

**NFR-3.1**: The system SHALL achieve 99.9% uptime excluding scheduled maintenance windows.

**NFR-3.2**: The system SHALL implement graceful degradation when TieredCache tiers are unavailable.

**NFR-3.3**: The system SHALL provide clear error messages when dependencies are unavailable.

**NFR-3.4**: The system SHALL implement retry mechanisms for transient failures.

**NFR-3.5**: The system SHALL have automated failover capabilities for critical components.

### 3.4 Security Requirements

**NFR-4.1**: All stored consent records SHALL be encrypted using industry-standard encryption (AES-256-GCM).

**NFR-4.2**: Encryption keys SHALL be managed through a secure key management system.

**NFR-4.3**: Access to decrypted data SHALL be restricted to authorized services only.

**NFR-4.4**: The system SHALL undergo regular security penetration testing.

**NFR-4.5**: The system SHALL maintain detailed access logs for all sensitive operations.

**NFR-4.6**: Passwords and secrets SHALL never be stored in plain text.

### 3.5 Usability Requirements

**NFR-5.1**: API responses SHALL follow RESTful conventions and be easily consumable by clients.

**NFR-5.2**: Error messages SHALL be clear, actionable, and not leak sensitive information.

**NFR-5.3**: The system SHALL provide comprehensive API documentation (OpenAPI/Swagger format).

**NFR-5.4**: Date and time values SHALL be formatted in ISO 8601 UTC format.

**NFR-5.5**: The system SHALL support internationalization for error messages.

### 3.6 Maintainability Requirements

**NFR-6.1**: The codebase SHALL follow established coding standards and best practices.

**NFR-6.2**: The system SHALL be modular and loosely coupled to facilitate maintenance.

**NFR-6.3**: Comprehensive unit tests SHALL achieve minimum 80% code coverage.

**NFR-6.4**: Integration tests SHALL cover all major workflows.

**NFR-6.5**: The system SHALL provide detailed logging for troubleshooting.

**NFR-6.6**: Configuration SHALL be externalized and environment-specific.

### 3.7 Data Management Requirements

**NFR-7.1**: The system SHALL implement appropriate data retention policies.

**NFR-7.2**: Expired consents SHALL be moved to colder storage tiers after a configurable period.

**NFR-7.3**: The system SHALL support secure deletion of consent records when required by law.

**NFR-7.4**: Backup and recovery procedures SHALL be defined and tested regularly.

**NFR-7.5**: The system SHALL prevent accidental deletion of active consent records.

### 3.8 Compliance Requirements

**NFR-8.1**: The system SHALL facilitate compliance with the DPDP Act, 2023.

**NFR-8.2**: The system SHALL support data principal rights requests (access, correction, portability).

**NFR-8.3**: The system SHALL maintain records sufficient to demonstrate compliance during audits.

**NFR-8.4**: The system SHALL support regulatory reporting requirements.

**NFR-8.5**: The system SHALL implement privacy by design and default principles.

## 4. External Interface Requirements

### 4.1 User Interfaces
While the primary interface is programmatic, the system SHALL support:
- Administrative dashboard for monitoring and configuration
- Self-service portal for data principals to manage their consents
- Reporting interface for compliance and audit purposes

### 4.2 Hardware Interfaces
The system SHALL run on standard x86_64 or ARM64 server hardware with:
- Minimum 4GB RAM
- Minimum 2 CPU cores
- Sufficient storage for operating system and application

### 4.3 Software Interfaces
**4.3.1 Required Dependencies**
- TieredCache system (existing infrastructure)
- Compatible database for metadata (if needed)
- Message queue for event notifications (optional)
- Identity provider supporting OAuth 2.0/OpenID Connect

**4.3.2 APIs Provided**
- RESTful JSON APIs for consent management (detailed in API design document)
- Health check endpoints (HTTP GET /health)
- Metrics endpoints (HTTP GET /metrics in Prometheus format)
- Administrative endpoints (protected, for configuration and monitoring)

**4.3.3 APIs Consumed**
- Identity provider APIs for token validation
- Event publishing APIs for change notifications
- Logging system APIs for log aggregation
- Monitoring system APIs for metric submission

### 4.4 Communication Interfaces
- All external communication SHALL use HTTPS/TLS 1.2+
- Internal service communication SHALL use secure channels
- Message queues SHALL be encrypted where supported
- Database connections SHALL use encrypted connections

## 5. Constraints

### 5.1 Design Constraints
- The system MUST leverage the existing TieredCache infrastructure
- The system MUST be implemented in Go to maintain consistency with existing codebase
- The system MUST follow existing project patterns and conventions
- The system MUST be deployable in the existing container orchestration environment

### 5.2 Regulatory Constraints
- The system MUST comply with the Digital Personal Data Protection Act, 2023
- The system MUST adhere to data localization requirements if applicable
- The system MUST support the rights of data principals as defined in the Act

### 5.3 Business Constraints
- The system MUST be deployable within 3 months of approved design
- The system MUST not require changes to existing TieredCache core functionality
- The system MUST operate within allocated budget and resource constraints

## 6. Acceptance Criteria

### 6.1 Functional Acceptance Criteria
- **AC-1.1**: All functional requirements can be demonstrated through executable tests
- **AC-1.2**: Consent lifecycle operations (create, retrieve, update, withdraw, validate) work correctly
- **AC-1.3**: Consent can be retrieved by all supported identifier combinations
- **AC-1.4**: Expired consents are automatically processed by the background job
- **AC-1.5**: Audit logs capture all required information for compliance

### 6.2 Performance Acceptance Criteria
- **AC-2.1**: 95% of consent creation requests complete within 200ms
- **AC-2.2**: 95% of consent retrieval by ID requests complete within 100ms
- **AC-2.3**: System handles minimum 1000 operations per second with acceptable latency
- **AC-2.4**: Background processing does not degrade foreground performance beyond acceptable thresholds

### 6.3 Security Acceptance Criteria
- **AC-3.1**: All authentication and authorization mechanisms function correctly
- **AC-3.2**: Data at rest is encrypted using approved algorithms
- **AC-3.3**: Data in transit is protected using TLS 1.2+
- **AC-3.4**: No critical vulnerabilities are found in security penetration testing
- **AC-3.5**: Access controls prevent unauthorized access to consent records

### 6.4 Compliance Acceptance Criteria
- **AC-4.1**: System supports all required data principal rights under DPDP Act
- **AC-4.2**: Audit trail meets requirements for regulatory inspection
- **AC-4.3**: Consent records contain all necessary information for DPDP compliance
- **AC-4.4**: System can generate reports required for compliance demonstrations
- **AC-4.5**: Privacy by design principles are evident in the implementation

## 7. Appendices

### 7.1 Appendix A: Consent Status Transition Diagram
```
[given] --> [withdrawn]
   |          ^
   |          |
   v          |
[expired]    |
   ^          |
   |          |
   +-- [rejected] <--+
```

### 7.2 Appendix B: Consent Data Model JSON Schema
```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "Consent",
  "type": "object",
  "required": ["consent_id", "data_principal_id", "data_fiduciary_id", "purpose", "data_categories", "status", "consent_timestamp", "version"],
  "properties": {
    "consent_id": {"type": "string"},
    "data_principal_id": {"type": "string"},
    "data_fiduciary_id": {"type": "string"},
    "consent_collector_id": {"type": ["string", "null"]},
    "consent_collection_channel": {"type": ["string", "null"]},
    "partner_id": {"type": ["string", "null"]},
    "purpose": {"type": "string"},
    "data_categories": {
      "type": "array",
      "items": {"type": "string"},
      "minItems": 1
    },
    "status": {
      "type": "string",
      "enum": ["given", "withdrawn", "expired", "rejected"]
    },
    "consent_timestamp": {"type": "string", "format": "date-time"},
    "expiry_timestamp": {"type": ["string", "null"], "format": "date-time"},
    "withdrawal_timestamp": {"type": ["string", "null"], "format": "date-time"},
    "version": {"type": "integer", "minimum": 1},
    "legal_reference": {"type": ["string", "null"]},
    "metadata": {
      "type": "object",
      "additionalProperties": {"type": "string"}
    },
    "last_modified_by": {"type": ["string", "null"]},
    "last_modified_at": {"type": ["string", "null"], "format": "date-time"}
  }
}
```

### 7.3 Appendix C: API Endpoint Summary
| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | /api/v1/consents | Create a new consent |
| GET | /api/v1/consents/{consent_id} | Get consent by ID |
| GET | /api/v1/consents/find | Get consent by principal/fiduciary/purpose or principal/fiduciary/collector/purpose |
| POST | /api/v1/consents/{consent_id}/withdraw | Withdraw consent |
| PATCH | /api/v1/consents/{consent_id} | Update consent |
| GET | /api/v1/consents | List consents (with filtering) |
| POST | /api/v1/consents/process-expired | Process expired consents (background job) |
| GET | /api/v1/consents/{consent_id}/valid | Check consent validity |

### 7.4 Appendix D: Error Code Summary
| Error Code | HTTP Status | Description |
|------------|-------------|-------------|
| VALIDATION_ERROR | 400 | Invalid request data |
| AUTHENTICATION_ERROR | 401 | Missing or invalid authentication |
| AUTHORIZATION_ERROR | 403 | Insufficient permissions |
| CONSENT_NOT_FOUND | 404 | Consent record not found |
| CONSENT_INVALID_STATUS | 409 | Consent not in appropriate status for operation |
| CONSENT_ALREADY_WITHDRAWN | 409 | Attempt to withdraw already withdrawn consent |
| INTERNAL_ERROR | 500 | Unexpected internal error |
| SERVICE_UNAVAILABLE | 503 | Service temporarily unavailable |

## 8. Revision History
| Version | Date | Author | Description |
|---------|------|--------|-------------|
| 1.0 | 2026-03-14 | Kilo Code | Initial requirement specification based on consent management service design |