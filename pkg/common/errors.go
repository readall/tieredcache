package common

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Tier represents the storage tier
type Tier int

const (
	TierUnknown Tier = iota
	TierL0           // In-memory cache (Otter-style)
	TierL1           // SSD cache (Badger)
	TierL2           // Cold storage (Kafka/MinIO/Postgres)
)

func (t Tier) String() string {
	switch t {
	case TierL0:
		return "L0"
	case TierL1:
		return "L1"
	case TierL2:
		return "L2"
	default:
		return "Unknown"
	}
}

// ParseTier parses a tier string
func ParseTier(s string) (Tier, error) {
	s = strings.ToLower(s)
	switch s {
	case "l0", "tierl0", "memory", "otter":
		return TierL0, nil
	case "l1", "tierl1", "ssd", "badger":
		return TierL1, nil
	case "l2", "tierl2", "cold", "archive":
		return TierL2, nil
	default:
		return TierUnknown, fmt.Errorf("unknown tier: %s", s)
	}
}

// ErrCode represents error categories
type ErrCode string

// Error implements the error interface
func (e ErrCode) Error() string {
	return string(e)
}

const (
	// Config errors
	ErrCodeConfigInvalid    ErrCode = "CONFIG_INVALID"
	ErrCodeConfigMissing    ErrCode = "CONFIG_MISSING"
	ErrCodeConfigValidation ErrCode = "CONFIG_VALIDATION"

	// Initialization errors
	ErrCodeInitFailed  ErrCode = "INIT_FAILED"
	ErrCodeOpenFailed  ErrCode = "OPEN_FAILED"
	ErrCodeCloseFailed ErrCode = "CLOSE_FAILED"
	ErrCodeClosed      ErrCode = "CLOSED"

	// Storage errors
	ErrCodeNotFound     ErrCode = "NOT_FOUND"
	ErrCodeWriteFailed  ErrCode = "WRITE_FAILED"
	ErrCodeReadFailed   ErrCode = "READ_FAILED"
	ErrCodeDeleteFailed ErrCode = "DELETE_FAILED"
	ErrCodeCorrupted    ErrCode = "CORRUPTED"

	// Tiering errors
	ErrCodeEvictFailed   ErrCode = "EVICT_FAILED"
	ErrCodePromoteFailed ErrCode = "PROMOTE_FAILED"
	ErrCodeTierFull      ErrCode = "TIER_FULL"

	// Sink errors
	ErrCodeSinkFailed     ErrCode = "SINK_FAILED"
	ErrCodeSinkRetry      ErrCode = "SINK_RETRY"
	ErrCodeSinkDeadLetter ErrCode = "SINK_DEAD_LETTER"

	// Recovery errors
	ErrCodeRecoveryFailed   ErrCode = "RECOVERY_FAILED"
	ErrCodeReplayFailed     ErrCode = "REPLAY_FAILED"
	ErrCodeWALCorrupted     ErrCode = "WAL_CORRUPTED"
	ErrCodeInconsistent     ErrCode = "INCONSISTENT"
	ErrCodeCheckpointFailed ErrCode = "CHECKPOINT_FAILED"

	// General errors
	ErrCodeTimeout   ErrCode = "TIMEOUT"
	ErrCodeCancelled ErrCode = "CANCELLED"
	ErrCodeInternal  ErrCode = "INTERNAL"
)

// TieredCacheError is the base error type for all tieredcache errors
type TieredCacheError struct {
	Code      ErrCode                `json:"code"`
	Message   string                 `json:"message"`
	Component string                 `json:"component,omitempty"`
	Tier      Tier                   `json:"tier,omitempty"`
	Err       error                  `json:"error,omitempty"`
	Retryable bool                   `json:"retryable"`
	Context   map[string]interface{} `json:"context,omitempty"`
}

func (e *TieredCacheError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v (component=%s, tier=%s, retryable=%v)",
			e.Code, e.Message, e.Err, e.Component, e.Tier, e.Retryable)
	}
	return fmt.Sprintf("[%s] %s (component=%s, tier=%s, retryable=%v)",
		e.Code, e.Message, e.Component, e.Tier, e.Retryable)
}

func (e *TieredCacheError) Unwrap() error {
	return e.Err
}

// WithContext adds context to the error
func (e *TieredCacheError) WithContext(key string, value interface{}) *TieredCacheError {
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	e.Context[key] = value
	return e
}

// ConfigError represents configuration validation errors
type ConfigError struct {
	Field   string
	Value   interface{}
	Reason  string
	Suggest string
}

func (e *ConfigError) Error() string {
	if e.Suggest != "" {
		return fmt.Sprintf("config error: field '%s' = %v: %s (suggest: %s)", e.Field, e.Value, e.Reason, e.Suggest)
	}
	return fmt.Sprintf("config error: field '%s' = %v: %s", e.Field, e.Value, e.Reason)
}

func (e *ConfigError) Unwrap() error {
	return nil
}

// InitError represents initialization failures
type InitError struct {
	Component string
	Operation string
	Err       error
	Retryable bool
}

func (e *InitError) Error() string {
	return fmt.Sprintf("init error: %s.%s failed: %v (retryable=%v)", e.Component, e.Operation, e.Err, e.Retryable)
}

func (e *InitError) Unwrap() error {
	return e.Err
}

// StorageError represents storage layer errors
type StorageError struct {
	Tier      string
	Key       string
	Err       error
	Retryable bool
}

func (e *StorageError) Error() string {
	return fmt.Sprintf("storage error: tier=%s, key=%s: %v (retryable=%v)", e.Tier, e.Key, e.Err, e.Retryable)
}

func (e *StorageError) Unwrap() error {
	return e.Err
}

// RecoveryError represents recovery process errors
type RecoveryError struct {
	Phase    string
	Position int64
	Err      error
}

func (e *RecoveryError) Error() string {
	return fmt.Sprintf("recovery error: phase=%s, position=%d: %v", e.Phase, e.Position, e.Err)
}

func (e *RecoveryError) Unwrap() error {
	return e.Err
}

// SinkError represents L2 sink failures
type SinkError struct {
	Backend   string
	Action    string
	Err       error
	Retryable bool
}

func (e *SinkError) Error() string {
	return fmt.Sprintf("sink error: backend=%s, action=%s: %v (retryable=%v)", e.Backend, e.Action, e.Err, e.Retryable)
}

func (e *SinkError) Unwrap() error {
	return e.Err
}

// Error helpers for common scenarios

// NewConfigError creates a new config error
func NewConfigError(field string, value interface{}, reason string, suggest ...string) *ConfigError {
	err := &ConfigError{
		Field:  field,
		Value:  value,
		Reason: reason,
	}
	if len(suggest) > 0 {
		err.Suggest = suggest[0]
	}
	return err
}

// NewInitError creates a new init error
func NewInitError(component, operation string, err error, retryable bool) *InitError {
	return &InitError{
		Component: component,
		Operation: operation,
		Err:       err,
		Retryable: retryable,
	}
}

// NewStorageError creates a new storage error
func NewStorageError(tier, key string, err error, retryable bool) *StorageError {
	return &StorageError{
		Tier:      tier,
		Key:       key,
		Err:       err,
		Retryable: retryable,
	}
}

// NewRecoveryError creates a new recovery error
func NewRecoveryError(phase string, position int64, err error) *RecoveryError {
	return &RecoveryError{
		Phase:    phase,
		Position: position,
		Err:      err,
	}
}

// NewSinkError creates a new sink error
func NewSinkError(backend, action string, err error, retryable bool) *SinkError {
	return &SinkError{
		Backend:   backend,
		Action:    action,
		Err:       err,
		Retryable: retryable,
	}
}

// IsRetryable checks if an error is retryable
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	var tieredErr *TieredCacheError
	if errors.As(err, &tieredErr) {
		return tieredErr.Retryable
	}

	var initErr *InitError
	if errors.As(err, &initErr) {
		return initErr.Retryable
	}

	var storageErr *StorageError
	if errors.As(err, &storageErr) {
		return storageErr.Retryable
	}

	var sinkErr *SinkError
	if errors.As(err, &sinkErr) {
		return sinkErr.Retryable
	}

	return false
}

// IsCritical checks if an error is critical (not retryable and requires attention)
func IsCritical(err error) bool {
	return !IsRetryable(err)
}

// WrapError wraps an error with context
func WrapError(code ErrCode, message string, tier Tier, component string, err error) *TieredCacheError {
	return &TieredCacheError{
		Code:      code,
		Message:   message,
		Tier:      tier,
		Component: component,
		Err:       err,
		Retryable: IsRetryable(err),
	}
}

// AsTieredCacheError attempts to cast error to TieredCacheError
func AsTieredCacheError(err error) (*TieredCacheError, bool) {
	var tieredErr *TieredCacheError
	if errors.As(err, &tieredErr) {
		return tieredErr, true
	}
	return nil, false
}

// CacheEntry represents a cached item
type CacheEntry struct {
	Key         string
	Value       []byte
	Tier        Tier
	Size        int // Size in bytes
	Weight      int // Weight in 4KB units
	CreatedAt   time.Time
	AccessedAt  time.Time
	AccessCount uint64
	TTL         time.Duration
}

// NewCacheEntry creates a new cache entry
func NewCacheEntry(key string, value []byte, ttl time.Duration) *CacheEntry {
	now := time.Now()
	size := len(value)
	weight := (size + WeightedUnitBytes - 1) / WeightedUnitBytes

	return &CacheEntry{
		Key:         key,
		Value:       value,
		Tier:        TierL0,
		Size:        size,
		Weight:      weight,
		CreatedAt:   now,
		AccessedAt:  now,
		AccessCount: 1,
		TTL:         ttl,
	}
}

// IsExpired checks if the entry has expired
func (e *CacheEntry) IsExpired() bool {
	if e.TTL <= 0 {
		return false
	}
	return time.Since(e.CreatedAt) > e.TTL
}
