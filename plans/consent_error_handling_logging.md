# Error Handling and Logging for Consent Management Service

## Overview
This document defines the error handling and logging strategy for the consent management service, building upon the existing patterns in the TieredCache system while addressing the specific requirements of DPDP Act compliance.

## Error Handling Strategy

### 1. Error Classification
Following the existing pattern in `pkg/common/errors.go`, we'll define consent-specific error codes:

#### Consent-Specific Error Codes
```go
// Consent errors
ErrCodeConsentNotFound        ErrCode = "CONSENT_NOT_FOUND"
ErrCodeConsentAlreadyGiven    ErrCode = "CONSENT_ALREADY_GIVEN"
ErrCodeConsentAlreadyWithdrawn ErrCode = "CONSENT_ALREADY_WITHDRAWN"
ErrCodeConsentExpired         ErrCode = "CONSENT_EXPIRED"
ErrCodeConsentRejected        ErrCode = "CONSENT_REJECTED"
ErrCodeInvalidConsentStatus   ErrCode = "INVALID_CONSENT_STATUS"
ErrCodeConsentUpdateNotAllowed ErrCode = "CONSENT_UPDATE_NOT_ALLOWED"
ErrCodeInsufficientConsent    ErrCode = "INSUFFICIENT_CONSENT"
ErrCodeConsentPurposeMismatch ErrCode = "CONSENT_PURPOSE_MISMATCH"
ErrCodeConsentDataCategoryMismatch ErrCode = "CONSENT_DATA_CATEGORY_MISMATCH"
ErrCodeConsentValidationFailed ErrCode = "CONSENT_VALIDATION_FAILED"
ErrCodeConsentStorageFailed   ErrCode = "CONSENT_STORAGE_FAILED"
ErrCodeConsentIndexFailed     ErrCode = "CONSENT_INDEX_FAILED"
```

### 2. Error Types
We'll extend the existing error types with consent-specific structures:

#### ConsentError
```go
// ConsentError represents consent-specific errors
type ConsentError struct {
    TieredCacheError
    ConsentID   string `json:"consent_id,omitempty"`
    DataPrincipalID string `json:"data_principal_id,omitempty"`
    DataFiduciaryID string `json:"data_fiduciary_id,omitempty"`
    Purpose     string `json:"purpose,omitempty"`
}
```

#### ConsentValidationError
```go
// ConsentValidationError represents validation errors for consent data
type ConsentValidationError struct {
    TieredCacheError
    Field   string `json:"field,omitempty"`
    Value   interface{} `json:"value,omitempty"`
    Constraint string `json:"constraint,omitempty"`
}
```

### 3. Error Handling Patterns

#### Storage Operations
For all storage operations with TieredCache, we'll follow the existing pattern:
1. Attempt the operation
2. If it returns an error, wrap it with context
3. Determine if the error is retryable based on the type
4. Return appropriate error to caller

Example:
```go
func (s *consentStorage) StoreConsent(ctx context.Context, consent *Consent) error {
    data, err := json.Marshal(consent)
    if err != nil {
        return common.WrapError(
            common.ErrCodeInternal,
            "failed to marshal consent",
            common.TierUnknown,
            "consent_storage",
            err,
        )
    }
    
    key := fmt.Sprintf("consent:%s", consent.ConsentID)
    if err := s.cache.Set(ctx, key, data, 0); err != nil {
        return common.WrapError(
            common.ErrCodeWriteFailed,
            "failed to store consent",
            common.TierL0, // Since Set writes to L0 first
            "consent_storage",
            err,
        ).WithContext("consent_id", consent.ConsentID)
    }
    
    return nil
}
```

#### Validation Errors
Validation errors should provide specific details about what failed:
```go
func validateConsentRequest(req *CreateConsentRequest) error {
    if req.DataPrincipalID == "" {
        return &ConsentValidationError{
            TieredCacheError: common.TieredCacheError{
                Code:    common.ErrCodeConfigValidation,
                Message: "data principal ID is required",
            },
            Field:   "data_principal_id",
            Value:   req.DataPrincipalID,
            Constraint: "required",
        }
    }
    
    if req.Purpose == "" {
        return &ConsentValidationError{
            TieredCacheError: common.TieredCacheError{
                Code:    common.ErrCodeConfigValidation,
                Message: "purpose is required",
            },
            Field:   "purpose",
            Value:   req.Purpose,
            Constraint: "required",
        }
    }
    
    if len(req.DataCategories) == 0 {
        return &ConsentValidationError{
            TieredCacheError: common.TieredCacheError{
                Code:    common.ErrCodeConfigValidation,
                Message: "at least one data category is required",
            },
            Field:   "data_categories",
            Value:   req.DataCategories,
            Constraint: "min=1",
        }
    }
    
    return nil
}
```

#### Business Logic Errors
Business logic errors should clearly indicate why an operation cannot be performed:
```go
func (s *consentService) WithdrawConsent(ctx context.Context, req *WithdrawConsentRequest) error {
    // Get existing consent
    consent, err := s.storage.GetConsent(ctx, req.ConsentID)
    if err != nil {
        if errors.Is(err, common.ErrCodeNotFound) {
            return common.WrapError(
                ErrCodeConsentNotFound,
                "consent not found",
                common.TierUnknown,
                "consent_service",
                err,
            ).WithContext("consent_id", req.ConsentID)
        }
        return err // Storage error already wrapped
    }
    
    // Verify requester is the data principal
    if req.WithdrawnBy != consent.DataPrincipalID {
        return common.WrapError(
            common.ErrCodeConfigValidation,
            "only the data principal can withdraw consent",
            common.TierUnknown,
            "consent_service",
            nil,
        ).WithContext("consent_id", req.ConsentID)
    }
    
    // Check if consent can be withdrawn
    if !consent.IsValid() {
        switch consent.Status {
        case ConsentStatusWithdrawn:
            return common.WrapError(
                ErrCodeConsentAlreadyWithdrawn,
                "consent has already been withdrawn",
                common.TierUnknown,
                "consent_service",
                nil,
            ).WithContext("consent_id", req.ConsentID)
        case ConsentStatusExpired:
            return common.WrapError(
                ErrCodeConsentExpired,
                "consent has expired",
                common.TierUnknown,
                "consent_service",
                nil,
            ).WithContext("consent_id", req.ConsentID)
        case ConsentStatusRejected:
            return common.WrapError(
                ErrCodeConsentRejected,
                "consent has been rejected",
                common.TierUnknown,
                "consent_service",
                nil,
            ).WithContext("consent_id", req.ConsentID)
        default:
            return common.WrapError(
                ErrCodeInvalidConsentStatus,
                fmt.Sprintf("cannot withdraw consent with status %s", consent.Status),
                common.TierUnknown,
                "consent_service",
                nil,
            ).WithContext("consent_id", req.ConsentID)
        }
    }
    
    // Proceed with withdrawal...
}
```

### 4. Error Response Mapping to HTTP Status Codes
For the REST API layer, we'll map errors to appropriate HTTP status codes:

| Error Code | HTTP Status | Description |
|------------|-------------|-------------|
| ErrCodeConsentNotFound | 404 Not Found | Consent not found |
| ErrCodeConsentAlreadyGiven | 409 Conflict | Consent already given |
| ErrCodeConsentAlreadyWithdrawn | 409 Conflict | Consent already withdrawn |
| ErrCodeConsentExpired | 410 Gone | Consent has expired |
| ErrCodeConsentRejected | 400 Bad Request | Consent was rejected |
| ErrCodeInvalidConsentStatus | 400 Bad Request | Invalid consent status for operation |
| ErrCodeConsentUpdateNotAllowed | 403 Forbidden | Cannot update consent in current status |
| ErrCodeInsufficientConsent | 403 Forbidden | Consent does not cover requested operation |
| ErrCodeConsentPurposeMismatch | 400 Bad Request | Purpose doesn't match consent |
| ErrCodeConsentDataCategoryMismatch | 400 Bad Request | Data categories don't match consent |
| ErrCodeConsentValidationFailed | 422 Unprocessable Entity | Validation failed |
| ErrCodeConsentStorageFailed | 500 Internal Server Error | Storage operation failed |
| ErrCodeConsentIndexFailed | 500 Internal Server Error | Index operation failed |
| ErrCodeConfigInvalid | 400 Bad Request | Invalid configuration |
| ErrCodeConfigMissing | 400 Bad Request | Missing configuration |
| ErrCodeConfigValidation | 422 Unprocessable Entity | Configuration validation failed |
| ErrCodeInitFailed | 503 Service Unavailable | Initialization failed |
| ErrCodeOpenFailed | 503 Service Unavailable | Open failed |
| ErrCodeCloseFailed | 500 Internal Server Error | Close failed |
| ErrCodeClosed | 503 Service Unavailable | Service closed |
| ErrCodeWriteFailed | 500 Internal Server Error | Write operation failed |
| ErrCodeReadFailed | 500 Internal Server Error | Read operation failed |
| ErrCodeDeleteFailed | 500 Internal Server Error | Delete operation failed |
| ErrCodeCorrupted | 500 Internal Server Error | Data corruption detected |
| ErrCodeEvictFailed | 500 Internal Server Error | Eviction failed |
| ErrCodePromoteFailed | 500 Internal Server Error | Promotion failed |
| ErrCodeTierFull | 507 Insufficient Storage | Storage tier full |
| ErrCodeSinkFailed | 502 Bad Gateway | L2 sink failed |
| ErrCodeSinkRetry | 503 Service Unavailable | L2 sink retryable failure |
| ErrCodeSinkDeadLetter | 500 Internal Server Error | L2 sink dead letter |
| ErrCodeRecoveryFailed | 500 Internal Server Error | Recovery failed |
| ErrCodeReplayFailed | 500 Internal Server Error | Replay failed |
| ErrCodeWALCorrupted | 500 Internal Server Error | WAL corrupted |
| ErrCodeInconsistent | 500 Internal Server Error | Inconsistent state |
| ErrCodeCheckpointFailed | 500 Internal Server Error | Checkpoint failed |
| ErrCodeTimeout | 504 Gateway Timeout | Operation timed out |
| ErrCodeCancelled | 499 Client Closed Request | Request cancelled |
| ErrCodeInternal | 500 Internal Server Error | Internal error |

### 5. Retry Logic
Following the existing pattern, we'll mark errors as retryable when appropriate:

- Storage errors (write/read/delete) may be retryable if they're transient
- Network errors are typically retryable
- Validation errors are never retryable
- Business logic errors are never retryable
- Internal errors may be retryable depending on the cause

The existing `IsRetryable` function in `pkg/common/errors.go` will work for our errors as long as we properly set the `Retryable` field when wrapping errors.

## Logging Strategy

### 1. Logging Framework
We'll use the existing logging approach from the TieredCache system, which appears to use the standard Go `log` package with structured logging capabilities.

### 2. Log Levels
We'll follow standard log levels:
- **DEBUG**: Detailed information for troubleshooting development issues
- **INFO**: General operational information
- **WARN**: Potentially harmful situations that don't prevent operation
- **ERROR**: Error events that might still allow the application to continue
- **FATAL**: Severe errors that cause premature termination

### 3. What to Log

#### Consent Operations (Audit Trail)
For DPDP compliance, we need to maintain an audit trail of all consent-related operations. We'll log:
- Who performed the operation (when applicable)
- What operation was performed
- On which consent/resource
- When it occurred (timestamp)
- Outcome (success/failure)
- Legal basis for the operation (when applicable)

Example audit log entry:
```
INFO  consent_audit: Operation=CreateConsent DataPrincipalID=user123 DataFiduciaryID=company456 ConsentID=consent_abc123 Purpose=marketing Outcome=Success Timestamp=2024-03-14T10:30:00Z LegalReference="DPDP Act Section 6"
```

#### Operational Logging
For system monitoring and troubleshooting:
- Startup/shutdown events
- Configuration changes
- Storage tier status changes
- Background job execution
- Performance metrics
- Error conditions

Example operational log entry:
```
INFO  consent_service: Starting consent service version 1.0.0
WARN  consent_storage: L0 cache usage at 85% (threshold: 90%)
ERROR consent_service: Failed to process expired consents: storage error: tier=l1, key=consent_abc123: write failed (retryable=false)
```

#### Debug Logging
For troubleshooting:
- Detailed request/response information (being careful not to log personal data)
- Internal state changes
- Algorithm execution details
- Performance timing

Example debug log entry:
```
DEBUG consent_service: Processing withdrawal request ConsentID=consent_abc123 WithdrawnBy=user123 CurrentStatus=given
```

### 4. What NOT to Log
To protect personal data and maintain privacy:
- Never log personal data identifiers in plain text in regular logs
- Avoid logging consent content that contains personal data
- Be cautious with audit logs - they may contain personal data but should be protected
- Never log passwords, tokens, or other sensitive credentials
- Avoid logging full request bodies that may contain personal data

Instead, we'll log:
- ConsentID (non-personal identifier)
- Hashed or truncated personal data identifiers when necessary for troubleshooting
- Operation types and outcomes
- Timestamps and durations
- Error types and messages (without personal data)

### 5. Log Format
We'll use structured logging (key=value format) for easier parsing and analysis:
```
timestamp level component: message key1=value1 key2=value2
```

Example:
```
2024-03-14T10:30:00Z INFO consent_service: Operation=CreateConsent ConsentID=consent_abc123 DataPrincipalID=user123 DataFiduciaryID=company456 Purpose=marketing Outcome=Success
```

### 6. Audit Log Protection
Audit logs containing personal data must be:
- Protected with the same security measures as the data they describe
- Retained for the period required by law
- Regularly reviewed for suspicious activity
- Immutable and tamper-evident where possible
- Access-controlled to authorized personnel only

### 7. Implementation Approach

#### Centralized Logging Helper
We'll create a logging helper in the consent service package:
```go
package consent

import (
    "time"
    
    "tieredcache/pkg/common"
)

// AuditLog logs consent-related operations for compliance
func AuditLog(operation string, consentID string, outcome string, fields ...interface{}) {
    // Build log entry
    msg := fmt.Sprintf("Operation=%s ConsentID=%s Outcome=%s", operation, consentID, outcome)
    
    // Add additional fields
    for i := 0; i < len(fields); i += 2 {
        if i+1 < len(fields) {
            msg += fmt.Sprintf(" %v=%v", fields[i], fields[i+1])
        }
    }
    
    // Log at INFO level
    common.InfoLog(msg)
}

// ErrorLog logs error conditions
func ErrorLog(err error, context ...interface{}) {
    // Build log entry
    msg := fmt.Sprintf("Error: %v", err)
    
    // Add context fields
    for i := 0; i < len(context); i += 2 {
        if i+1 < len(context) {
            msg += fmt.Sprintf(" %v=%v", context[i], context[i+1])
        }
    }
    
    // Log at ERROR level
    common.ErrorLog(msg)
}

// InfoLog logs general information
func InfoLog(msg string, fields ...interface{}) {
    // Add timestamp
    timestamp := time.Now().UTC().Format(time.RFC3339)
    fullMsg := fmt.Sprintf("%s %s", timestamp, msg)
    
    // Add additional fields
    for i := 0; i < len(fields); i += 2 {
        if i+1 < len(fields) {
            fullMsg += fmt.Sprintf(" %v=%v", fields[i], fields[i+1])
        }
    }
    
    // Log at INFO level
    common.InfoLog(fullMsg)
}
```

#### Integration with Existing Logging
We'll leverage the existing logging infrastructure in the TieredCache system where possible, extending it with consent-specific helpers.

### 8. Log Rotation and Retention
- Application logs: Rotate daily, retain for 30 days
- Audit logs: Rotate daily, retain for period required by law (typically 3-5 years)
- Error logs: Rotate daily, retain for 90 days
- Debug logs: Rotate hourly, retain for 7 days (only enabled in debug mode)

### 9. Security Monitoring Integration
Logs will be forwarded to a SIEM system for:
- Real-time alerting on security events
- Anomaly detection
- Compliance reporting
- Forensic analysis

### 10. Example Implementation

Here's how error handling and logging would work together in a consent creation function:

```go
func (s *consentService) CreateConsent(ctx context.Context, req *CreateConsentRequest) (*CreateConsentResponse, error) {
    // Log the request (without personal data)
    InfoLog("Received CreateConsent request", 
        "DataPrincipalIDHash", hashID(req.DataPrincipalID),
        "DataFiduciaryIDHash", hashID(req.DataFiduciaryID),
        "Purpose", req.Purpose,
        "DataCategoriesCount", len(req.DataCategories))
    
    // Validate request
    if err := validateConsentRequest(req); err != nil {
        ErrorLog(err, 
            "operation", "CreateConsent",
            "DataPrincipalIDHash", hashID(req.DataPrincipalID),
            "DataFiduciaryIDHash", hashID(req.DataFiduciaryID))
        return nil, err
    }
    
    // Check if consent already exists (optional, depending on requirements)
    existingConsent, err := s.storage.GetConsentByPrincipalAndPurpose(ctx, req.DataPrincipalID, req.Purpose)
    if err != nil && !errors.Is(err, common.ErrCodeNotFound) {
        ErrorLog(err,
            "operation", "CreateConsent",
            "DataPrincipalIDHash", hashID(req.DataPrincipalID),
            "purpose", req.Purpose)
        return nil, err
    }
    
    if existingConsent != nil {
        // Log duplicate consent attempt
        InfoLog("Duplicate consent attempt",
            "ConsentID", existingConsent.ConsentID,
            "DataPrincipalIDHash", hashID(req.DataPrincipalID),
            "purpose", req.Purpose)
        
        return nil, common.WrapError(
            ErrCodeConsentAlreadyGiven,
            "consent already exists for this purpose",
            common.TierUnknown,
            "consent_service",
            nil,
        ).WithContext("consent_id", existingConsent.ConsentID)
    }
    
    // Create consent object
    consent := NewConsent(
        req.DataPrincipalID,
        req.DataFiduciaryID,
        ConsentPurpose(req.Purpose),
        req.DataCategories,
        req.ExpiryTimestamp,
    )
    
    // Set additional fields
    if req.LegalReference != "" {
        consent.LegalReference = req.LegalReference
    }
    if req.Metadata != nil {
        consent.Metadata = req.Metadata
    }
    
    // Store consent
    if err := s.storage.StoreConsent(ctx, consent); err != nil {
        ErrorLog(err,
            "operation", "CreateConsent",
            "ConsentID", consent.ConsentID,
            "DataPrincipalIDHash", hashID(consent.DataPrincipalID),
            "DataFiduciaryIDHash", hashID(consent.DataFiduciaryID))
        return nil, err
    }
    
    // Update indexes
    if err := s.updateIndexes(ctx, consent); err != nil {
        ErrorLog(err,
            "operation", "CreateConsent",
            "ConsentID", consent.ConsentID,
            "DataPrincipalIDHash", hashID(consent.DataPrincipalID),
            "DataFiduciaryIDHash", hashID(consent.DataFiduciaryID))
        // Note: We don't return the error here because the consent was stored
        // In a production system, we might want to implement a compensation transaction
        // or have a background job to reconcile indexes
    }
    
    // Log successful creation (audit trail)
    AuditLog("CreateConsent", consent.ConsentID, "Success",
        "DataPrincipalID", consent.DataPrincipalID, // Audit log may contain personal data
        "DataFiduciaryID", consent.DataFiduciaryID,
        "Purpose", consent.Purpose,
        "DataCategories", strings.Join(consent.DataCategories, ","),
        "Version", consent.Version)
    
    // Return response
    return &CreateConsentResponse{
        ConsentID:        consent.ConsentID,
        Status:           string(consent.Status),
        ConsentTimestamp: consent.ConsentTimestamp,
        Version:          consent.Version,
    }, nil
}
```

This approach ensures:
1. Proper error handling with context preservation
2. Comprehensive logging for operations, errors, and audit trails
3. Protection of personal data in regular logs while maintaining audit capability
4. Consistency with the existing TieredCache error handling patterns
5. DPDP Act compliance through audit trail maintenance