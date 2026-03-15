# Consent Data Model for DPDP Act Compliance

## Overview
This document defines the data model for a consent management service compliant with India's Digital Personal Data Protection (DPDP) Act, 2023.

## Core Principles from DPDP Act
- Consent must be free, specific, informed, unambiguous, and given through a clear affirmative action
- Data principals have the right to withdraw consent at any time
- Consent must be for a specific purpose
- Data fiduciaries must maintain records of consent

## Consent Entity

### Fields
| Field | Type | Description | DPDP Requirement |
|-------|------|-------------|------------------|
| ConsentID | string | Unique identifier for the consent | Record keeping |
| DataPrincipalID | string | Identifier of the user giving consent | Data principal rights |
| DataFiduciaryID | string | Identifier of the entity obtaining consent | Accountability |
| ConsentCollectorID | string | Identifier of the entity/person collecting the consent (could be different from fiduciary) | Accountability |
| ConsentCollectionChannel | string | Channel via which consent is collected (e.g., website, mobile app, call center, paper form) | Transparency |
| PartnerID | string | Identifier of any partner involved in the consent collection process | Accountability |
| Purpose | ConsentPurpose | Specific purpose for data processing | Purpose limitation |
| DataCategories | []string | Specific categories of personal data covered | Specificity |
| Status | ConsentStatus | Current status of consent | Consent management |
| ConsentTimestamp | time.Time | When consent was given | Record keeping |
| ExpiryTimestamp | *time.Time | When consent expires (if applicable) | Storage limitation |
| WithdrawalTimestamp | *time.Time | When consent was withdrawn (if applicable) | Right to withdraw |
| Version | int | Version number for tracking changes | Record keeping |
| LegalReference | string | Legal basis or reference for consent | Lawfulness |
| Metadata | map[string]string | Additional context or metadata | Flexibility |
| LastModifiedBy | string | Who last modified this consent | Audit trail |
| LastModifiedAt | time.Time | When consent was last modified | Audit trail |

### ConsentStatus Enum
- `given`: Consent has been provided and is active
- `withdrawn`: Consent has been withdrawn by data principal
- `expired`: Consent has reached its expiry date
- `rejected`: Consent was rejected or deemed invalid

### ConsentPurpose Type
String type representing the specific purpose for which consent is given (e.g., "marketing", "service_provision", "analytics")

## Key Features Supporting DPDP Compliance

1. **Consent Lifecycle Management**
   - Creation with explicit timestamp
   - Withdrawal capability at any time
   - Automatic expiry handling
   - Version tracking for changes

2. **Audit Trail**
   - Tracks who modified consent and when
   - Maintains history through versioning
   - Records withdrawal timestamps

3. **Purpose Limitation**
   - Explicit purpose field
   - Data categories specificity
   - Legal reference tracking

4. **Data Principal Rights**
   - Easy withdrawal mechanism
   - Clear status indicators
   - Timestamp records for all actions

5. **Enhanced Accountability and Transparency**
   - Consent collector identification for clear responsibility tracking
   - Collection channel information for transparency about how consent was obtained
   - Partner information for full disclosure of entities involved in the consent process

## Integration with TieredCache
The consent records will be stored in the TieredCache system:
- L0 (In-memory): Active consents for fast lookup
- L1 (SSD): Persistent storage of all consent records
- L2 (Cold Storage): Archived/expired consents for long-term retention

## Validation Rules
1. ConsentID must be unique
2. DataPrincipalID and DataFiduciaryID must not be empty
3. Purpose must be specified
4. At least one data category must be specified
5. If ExpiryTimestamp is set, it must be after ConsentTimestamp
6. Version must increment with each modification
7. ConsentCollectorID should be specified when different from DataFiduciaryID
8. ConsentCollectionChannel should be specified for transparency
9. PartnerID should be specified when partners are involved in consent collection