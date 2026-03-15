# Consent Management Service Design for DPDP Act Compliance

## Overview
This document provides a comprehensive overview of the consent management service designed to comply with India's Digital Personal Data Protection (DPDP) Act, 2023. The service is built on top of the existing TieredCache multi-tier caching system to provide high-performance, scalable, and durable consent management.

## Table of Contents
1. [Introduction](#introduction)
2. [DPDP Act Requirements](#dpdp-act-requirements)
3. [Consent Data Model](#consent-data-model)
4. [Service API Design](#service-api-design)
5. [TieredCache Integration](#tieredcache-integration)
6. [Security and Privacy Considerations](#security-and-privacy-considerations)
7. [Error Handling and Logging](#error-handling-and-logging)
8. [Integration Points](#integration-points)
9. [Implementation Roadmap](#implementation-roadmap)
10. [Compliance Verification](#compliance-verification)

---

## Introduction
The Digital Personal Data Protection (DPDP) Act, 2023 regulates the processing of digital personal data in India. A key requirement of the Act is that personal data may only be processed with the consent of the data principal, except in certain specified circumstances.

This consent management service provides a comprehensive solution for managing the complete lifecycle of consent in accordance with the DPDP Act, including:
- Consent creation with specific, informed, and unambiguous requirements
- Consent withdrawal at any time by the data principal
- Consent expiry management
- Consent validation for data processing operations
- Audit trail maintenance for compliance
- Secure storage and protection of consent records

The service leverages the existing TieredCache system for efficient storage and retrieval of consent records across multiple tiers (L0 in-memory, L1 SSD, L2 cold storage).

---

## DPDP Act Requirements
The design addresses the following key requirements from the DPDP Act:

### Consent Requirements
- **Free**: Consent must be given freely without coercion, undue influence, or misrepresentation
- **Specific**: Consent must be for a specific purpose
- **Informed**: Data principal must be informed about the personal data to be processed and the purpose
- **Unambiguous**: Consent must be given through a clear affirmative action
- **Easy Withdrawal**: It must be as easy to withdraw consent as to give it
- **Purpose Limitation**: Personal data must only be processed for the specified purpose
- **Storage Limitation**: Personal data must not be retained longer than necessary

### Data Principal Rights
- Right to give, withdraw, and manage consent
- Right to access information about their consent
- Right to withdraw consent at any time
- Right to data portability
- Right to erasure (right to be forgotten)

### Data Fiduciary Obligations
- Maintain records of consent
- Implement appropriate security measures
- Notify of personal data breaches
- Conduct data protection impact assessments
- Appoint Data Protection Officer where required

---

## Consent Data Model
The consent data model is designed to capture all necessary information for DPDP Act compliance while minimizing the storage of unnecessary personal data.

### Core Consent Entity
| Field | Type | Description |
|-------|------|-------------|
| ConsentID | string | Unique identifier for the consent |
| DataPrincipalID | string | Identifier of the user giving consent |
| DataFiduciaryID | string | Identifier of the entity obtaining consent |
| ConsentCollectorID | string | Identifier of the entity/person collecting the consent (could be different from fiduciary) |
| ConsentCollectionChannel | string | Channel via which consent is collected (e.g., website, mobile app, call center, paper form) |
| PartnerID | string | Identifier of any partner involved in the consent collection process |
| Purpose | ConsentPurpose | Specific purpose for data processing |
| DataCategories | []string | Specific categories of personal data covered |
| Status | ConsentStatus | Current status of consent |
| ConsentTimestamp | time.Time | When consent was given |
| ExpiryTimestamp | *time.Time | When consent expires (if applicable) |
| WithdrawalTimestamp | *time.Time | When consent was withdrawn (if applicable) |
| Version | int | Version number for tracking changes |
| LegalReference | string | Legal basis or reference for consent |
| Metadata | map[string]string | Additional context or metadata |
| LastModifiedBy | string | Who last modified this consent |
| LastModifiedAt | time.Time | When consent was last modified |

### Consent Status Enum
- `given`: Consent has been provided and is active
- `withdrawn`: Consent has been withdrawn by data principal
- `expired`: Consent has reached its expiry date
- `rejected`: Consent was rejected or deemed invalid

### Key Features
1. **Consent Lifecycle Management**: Tracks consent from creation through withdrawal or expiry
2. **Audit Trail**: Records who modified consent and when
3. **Version Control**: Tracks changes to consent over time
4. **Purpose Limitation**: Explicitly stores purpose for which consent was given
5. **Data Minimization**: Only stores necessary information for consent management
6. **Enhanced Accountability and Transparency**: 
   - Consent collector identification for clear responsibility tracking
   - Collection channel information for transparency about how consent was obtained
   - Partner information for full disclosure of entities involved in the consent process

Full details are available in [consent_data_model.md](plans/consent_data_model.md).

---

## Service API Design
The service provides a RESTful API for managing consent throughout its lifecycle.

### Core Operations
1. **Create Consent** - Record a new consent from a data principal
2. **Get Consent** - Retrieve consent details by consent ID or by principal/fiduciary/purpose combinations
3. **Withdraw Consent** - Allow data principal to withdraw consent at any time
4. **Update Consent** - Modify consent details (purpose, data categories, expiry)
5. **Check Consent Validity** - Verify if consent is currently valid
6. **List Consents** - Retrieve consents for a data principal or data fiduciary
7. **Expire Consents** - Process expired consents (background job)

### API Endpoints
- `POST /api/v1/consents` - Create a new consent
- `GET /api/v1/consents/{consent_id}` - Get consent details by ID
- `GET /api/v1/consents/find` - Get consent by principal, fiduciary, and purpose (or principal, fiduciary, collector, and purpose)
- `POST /api/v1/consents/{consent_id}/withdraw` - Withdraw consent
- `PATCH /api/v1/consents/{consent_id}` - Update consent
- `GET /api/v1/consents` - List consents (with filtering options)
- `POST /api/v1/consents/process-expired` - Process expired consents

### Data Transfer Objects
Well-defined request and response objects ensure clear API contracts:
- CreateConsentRequest/Response
- GetConsentResponse
- WithdrawConsentRequest
- UpdateConsentRequest
- GetConsentsByDataPrincipalResponse
- GetConsentsByDataFiduciaryResponse

Full API specification is available in [consent_service_api.md](plans/consent_service_api.md).

---

## TieredCache Integration
The service integrates with the existing TieredCache system for efficient storage and retrieval.

### Storage Strategy
- **Primary Storage**: Consent records stored as `consent:{consent_id}` in L0 and L1 tiers
- **Secondary Indexes**: 
  - Data Principal Index: `index:dp:{data_principal_id}` → Set of ConsentIDs
  - Data Fiduciary Index: `index:df:{data_fiduciary_id}` → Set of ConsentIDs
  - Purpose Index: `index:purpose:{purpose}` → Set of ConsentIDs
  - Status Index: `index:status:{status}` → Set of ConsentIDs
  - Principal-Fiduciary-Purpose Index: `index:pfp:{data_principal_id}:{data_fiduciary_id}:{purpose}` → ConsentID
  - Principal-Fiduciary-Collector-Purpose Index: `index:pfcp:{data_principal_id}:{data_fiduciary_id}:{consent_collector_id}:{purpose}` → ConsentID

### Storage Tiers Usage
- **L0 (In-Memory)**: Active consents (status = given) for fast access
- **L1 (SSD)**: All consent records for persistence and recovery
- **L2 (Cold Storage)**: Archived/expired consents for long-term retention

### Integration Points
Detailed integration patterns for:
- Consent creation flow
- Consent retrieval flow by ID
- Consent retrieval flow by principal, fiduciary, and purpose
- Consent retrieval flow by principal, fiduciary, collector, and purpose
- Consent withdrawal flow
- Consent update flow
- Consent validity check flow
- List consents flow
- Expired consent processing flow

Full integration details are available in [consent_tieredcache_integration.md](plans/consent_tieredcache_integration.md).

---

## Security and Privacy Considerations
The service implements comprehensive security and privacy measures to protect personal data and ensure DPDP Act compliance.

### Data Protection Principles
- **Data Minimization**: Only store necessary personal data
- **Purpose Limitation**: Explicitly store and enforce purpose limitations
- **Storage Limitation**: Implement expiry mechanisms and secure deletion

### Technical Security Measures
- **Encryption**: AES-256-GCM for data at rest, TLS 1.2+ for data in transit
- **Access Control**: Strong authentication (MFA, OAuth 2.0) and fine-grained authorization (RBAC, ABAC)
- **Secure Coding**: Input validation, output encoding, dependency scanning
- **API Security**: Rate limiting, API gateway, OWASP Top 10 protection

### Privacy-Enhancing Technologies
- **Pseudonymization**: Replace identifiable information with pseudonyms where possible
- **Data Segregation**: Separate storage for different data types with appropriate controls
- **Audit Trail**: Comprehensive logging of all consent-related operations

### Incident Response
- Breach detection and response procedures
- 72-hour notification to Data Protection Board of India
- Containment, eradication, and recovery processes
- Post-incident analysis and improvement

Full security and privacy details are available in [consent_security_privacy.md](plans/consent_security_privacy.md).

---

## Error Handling and Logging
The service implements robust error handling and comprehensive logging for operations, debugging, and compliance.

### Error Handling
- Consent-specific error codes following existing TieredCache patterns
- Proper error wrapping with context and retryability information
- Mapping of errors to appropriate HTTP status codes
- Validation errors with specific field-level details
- Business logic errors explaining why operations cannot be performed

### Logging Strategy
- **Audit Logs**: Comprehensive logging of all consent operations for compliance
- **Operational Logs**: System monitoring and troubleshooting information
- **Debug Logs**: Detailed information for troubleshooting (careful to avoid personal data)
- **Structured Logging**: Key=value format for easier parsing and analysis
- **Log Protection**: Audit logs protected with same security measures as data

Full error handling and logging details are available in [consent_error_handling_logging.md](plans/consent_error_handling_logging.md).

---

## Integration Points
The service integrates with various other systems and services in the ecosystem.

### Key Integration Areas
1. **Identity and Access Management**: Authentication and authorization services
2. **Data Processing Systems**: Real-time consent validation and change notifications
3. **Monitoring and Observability**: Metrics, tracing, health checks, log aggregation
4. **Deployment and Orchestration**: Container images, Kubernetes manifests, configuration management
5. **Data Subject Rights Fulfillment**: Systems for handling access, correction, portability, and erasure requests
6. **Compliance and Reporting**: Audit forwarding, report generation, regulatory notifications
7. **External Consent Systems**: Federated consent management and standardized APIs
8. **Testing and Development**: Test containers, mock services, contract testing

Full integration point details are available in [consent_integration_points.md](plans/consent_integration_points.md).

---

## Implementation Roadmap
The implementation will proceed in phases to ensure steady progress and early value delivery.

### Phase 1: Core Infrastructure
- Define consent data model
- Implement basic storage integration with TieredCache
- Create core service API endpoints (create, get, withdraw, validate)
- Implement basic error handling and logging

### Phase 2: Core Functionality
- Implement consent update functionality
- Add index structures for efficient querying (including purpose-based and composite indexes)
- Implement consent listing capabilities
- Add background job for expired consent processing
- Enhance error handling and logging

### Phase 3: Security and Compliance
- Implement encryption for data at rest and in transit
- Add authentication and authorization
- Implement comprehensive audit logging
- Add security monitoring and alerting
- Implement data subject rights fulfillment APIs

### Phase 4: Integration and Observability
- Implement data processing system integration (validation API, event notifications)
- Add metrics collection and health checks
- Implement distributed tracing
- Add log aggregation integration
- Implement deployment orchestration (Docker, Kubernetes)

### Phase 5: Advanced Features and Hardening
- Implement external consent systems integration
- Add compliance reporting capabilities
- Implement advanced security measures (pseudonymization, data segregation)
- Add performance optimizations
- Conduct security testing and penetration testing
- Prepare for production deployment

---

## Compliance Verification
The service includes multiple mechanisms to verify and maintain DPDP Act compliance.

### Internal Verification Mechanisms
- **Audit Trail**: Complete record of all consent operations
- **Automated Tests**: Unit, integration, and end-to-end tests for all functionality
- **Security Testing**: Regular vulnerability scanning and penetration testing
- **Code Reviews**: Mandatory peer review for all changes
- **Configuration Validation**: Ensuring secure configurations

### External Verification Mechanisms
- **Independent Audits**: Annual third-party security and compliance audits
- **Penetration Testing**: Regular testing by qualified third parties
- **Bug Bounty Program**: Responsible disclosure program for security vulnerabilities
- **Compliance Certifications**: Pursue relevant certifications where available

### Ongoing Compliance Activities
- **Regular Policy Reviews**: Update policies and procedures based on regulatory guidance
- **Training Programs**: Regular security and privacy training for all personnel
- **Incident Response Drills**: Regular testing of breach response procedures
- **Regulatory Monitoring**: Stay updated on DPDP Act amendments and guidelines
- **Privacy by Design**: Continuously evaluate and improve privacy protections

---

## Conclusion
This consent management service provides a comprehensive, secure, and compliant solution for managing consent under India's DPDP Act, 2023. By leveraging the existing TieredCache system, the service achieves high performance and scalability while ensuring the durability and security of consent records.

The design addresses all key requirements of the DPDP Act, including:
- Proper consent lifecycle management
- Data principal rights fulfillment
- Data fiduciary obligations
- Security and privacy protection
- Audit trail maintenance
- Integration with broader ecosystem
- Enhanced transparency through tracking of consent collectors, collection channels, and partners

Through phased implementation and comprehensive compliance verification, the service will provide a trustworthy foundation for consent management in digital ecosystems operating in India.

---

## References
- Digital Personal Data Protection (DPDP) Act, 2023
- TieredCache System Documentation
- OAuth 2.0 and OpenID Connect Standards
- OWASP Top 10 Security Risks
- GDPR and other international privacy regulations (for comparative guidance)