package l2

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"tieredcache/pkg/common"
)

// RetryQueue implements a persistent retry queue for failed sink operations
type RetryQueue struct {
	mu       sync.Mutex
	items    []SinkItem
	maxSize  int
	diskPath string
}

// NewRetryQueue creates a new retry queue
func NewRetryQueue(diskPath string, maxSize int) (*RetryQueue, error) {
	if diskPath != "" {
		if err := os.MkdirAll(diskPath, 0755); err != nil {
			return nil, fmt.Errorf("failed to create retry queue directory: %w", err)
		}
	}

	return &RetryQueue{
		items:    make([]SinkItem, 0),
		maxSize:  maxSize,
		diskPath: diskPath,
	}, nil
}

// Add adds an item to the retry queue
func (q *RetryQueue) Add(item SinkItem) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.maxSize > 0 && len(q.items) >= q.maxSize {
		return fmt.Errorf("retry queue is full (%d items)", q.maxSize)
	}

	q.items = append(q.items, item)

	// Persist to disk if disk path is configured
	if q.diskPath != "" {
		q.persistToDisk(item)
	}

	return nil
}

// GetAll returns all items in the queue and clears it
func (q *RetryQueue) GetAll() []SinkItem {
	q.mu.Lock()
	defer q.mu.Unlock()

	items := q.items
	q.items = make([]SinkItem, 0)
	return items
}

// Size returns the current size of the queue
func (q *RetryQueue) Size() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// persistToDisk persists a single item to disk
func (q *RetryQueue) persistToDisk(item SinkItem) error {
	filename := filepath.Join(q.diskPath, fmt.Sprintf("retry_%d.json", time.Now().UnixNano()))
	data, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("failed to marshal retry item: %w", err)
	}
	return os.WriteFile(filename, data, 0644)
}

// LoadFromDisk loads pending retry items from disk
func (q *RetryQueue) LoadFromDisk() error {
	if q.diskPath == "" {
		return nil
	}

	files, err := filepath.Glob(filepath.Join(q.diskPath, "retry_*.json"))
	if err != nil {
		return fmt.Errorf("failed to glob retry files: %w", err)
	}

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			// Log but continue
			fmt.Printf("warning: failed to read retry file %s: %v\n", f, err)
			continue
		}

		var item SinkItem
		if err := json.Unmarshal(data, &item); err != nil {
			fmt.Printf("warning: failed to unmarshal retry file %s: %v\n", f, err)
			continue
		}

		q.items = append(q.items, item)
		os.Remove(f) // Remove after loading
	}

	return nil
}

// DeadLetterQueue implements a persistent dead letter queue for failed operations
type DeadLetterQueue struct {
	mu       sync.Mutex
	items    []SinkItem
	maxSize  int
	diskPath string
}

// NewDeadLetterQueue creates a new dead letter queue
func NewDeadLetterQueue(diskPath string, maxSize int) (*DeadLetterQueue, error) {
	if diskPath != "" {
		if err := os.MkdirAll(diskPath, 0755); err != nil {
			return nil, fmt.Errorf("failed to create DLQ directory: %w", err)
		}
	}

	return &DeadLetterQueue{
		items:    make([]SinkItem, 0),
		maxSize:  maxSize,
		diskPath: diskPath,
	}, nil
}

// Add adds an item to the dead letter queue
func (dq *DeadLetterQueue) Add(item SinkItem, reason error) error {
	dq.mu.Lock()
	defer dq.mu.Unlock()

	// Add reason to metadata
	if item.Metadata == nil {
		item.Metadata = make(map[string]string)
	}
	item.Metadata["dlq_reason"] = reason.Error()
	item.Metadata["dlq_timestamp"] = time.Now().Format(time.RFC3339)

	// Check size limit
	if dq.maxSize > 0 && len(dq.items) >= dq.maxSize {
		return fmt.Errorf("dead letter queue is full (%d items)", dq.maxSize)
	}

	dq.items = append(dq.items, item)

	// Persist to disk
	if dq.diskPath != "" {
		filename := filepath.Join(dq.diskPath, fmt.Sprintf("dlq_%d.json", time.Now().UnixNano()))
		data, err := json.Marshal(item)
		if err != nil {
			return fmt.Errorf("failed to marshal DLQ item: %w", err)
		}
		if err := os.WriteFile(filename, data, 0644); err != nil {
			return fmt.Errorf("failed to write DLQ item: %w", err)
		}
	}

	return nil
}

// GetAll returns all items in the queue
func (dq *DeadLetterQueue) GetAll() []SinkItem {
	dq.mu.Lock()
	defer dq.mu.Unlock()
	items := dq.items
	dq.items = make([]SinkItem, 0)
	return items
}

// Size returns the current size of the queue
func (dq *DeadLetterQueue) Size() int {
	dq.mu.Lock()
	defer dq.mu.Unlock()
	return len(dq.items)
}

// RetryConfig contains configuration for retry behavior
type RetryConfig struct {
	MaxRetries        int
	InitialBackoff    time.Duration
	MaxBackoff        time.Duration
	BackoffMultiplier float64
}

// DefaultRetryConfig returns default retry configuration
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:        3,
		InitialBackoff:    100 * time.Millisecond,
		MaxBackoff:        30 * time.Second,
		BackoffMultiplier: 2.0,
	}
}

// CalculateBackoff calculates the next backoff duration
func (c *RetryConfig) CalculateBackoff(attempt int) time.Duration {
	backoff := c.InitialBackoff
	for i := 0; i < attempt && i < c.MaxRetries; i++ {
		backoff = time.Duration(float64(backoff) * c.BackoffMultiplier)
		if backoff > c.MaxBackoff {
			backoff = c.MaxBackoff
		}
	}
	return backoff
}

// RetryableError wraps an error with retry information
type RetryableError struct {
	Err       error
	Retryable bool
	Attempts  int
}

func (e *RetryableError) Error() string {
	return fmt.Sprintf("retryable error after %d attempts: %v", e.Attempts, e.Err)
}

func (e *RetryableError) Unwrap() error {
	return e.Err
}

// IsRetryable checks if an error is retryable
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Check for retryable error wrapper
	if tieredErr, ok := common.AsTieredCacheError(err); ok {
		return tieredErr.Retryable
	}

	// Network errors are typically retryable
	errStr := err.Error()
	retryablePatterns := []string{
		"connection refused",
		"connection reset",
		"timeout",
		"temporary failure",
		"i/o timeout",
		"no route to host",
		"network is unreachable",
	}

	for _, pattern := range retryablePatterns {
		if contains(errStr, pattern) {
			return true
		}
	}

	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
