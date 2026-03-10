package l2

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Sink represents a cold storage sink backend
type Sink interface {
	// Name returns the sink name
	Name() string

	// Type returns the sink type
	Type() string

	// Write writes data to the sink
	Write(ctx context.Context, key string, value []byte, metadata map[string]string) error

	// WriteBatch writes multiple items to the sink
	WriteBatch(ctx context.Context, items []SinkItem) error

	// Read reads data from the sink
	Read(ctx context.Context, key string) ([]byte, error)

	// Delete deletes data from the sink
	Delete(ctx context.Context, key string) error

	// Ping checks if the sink is available
	Ping(ctx context.Context) error

	// Close closes the sink
	Close() error
}

// SinkItem represents an item to be written to a sink
type SinkItem struct {
	Key       string
	Value     []byte
	Metadata  map[string]string
	Timestamp time.Time
}

// SinkStats represents sink statistics
type SinkStats struct {
	Name         string
	Type         string
	Writes       uint64
	Reads        uint64
	Deletes      uint64
	Errors       uint64
	BytesWritten uint64
	BytesRead    uint64
	LatencyAvg   time.Duration
}

// SinkManager manages multiple sink backends
type SinkManager struct {
	sinks  map[string]Sink
	mu     sync.RWMutex
	stats  map[string]*SinkStats
	closed atomic.Bool
}

// NewSinkManager creates a new sink manager
func NewSinkManager() *SinkManager {
	return &SinkManager{
		sinks: make(map[string]Sink),
		stats: make(map[string]*SinkStats),
	}
}

// RegisterSink registers a sink backend
func (m *SinkManager) RegisterSink(sink Sink) error {
	if sink == nil {
		return fmt.Errorf("sink cannot be nil")
	}

	name := sink.Name()
	if name == "" {
		return fmt.Errorf("sink name cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sinks[name]; exists {
		return fmt.Errorf("sink %s already registered", name)
	}

	m.sinks[name] = sink
	m.stats[name] = &SinkStats{
		Name:       name,
		Type:       sink.Type(),
		LatencyAvg: 0,
	}

	return nil
}

// GetSink returns a sink by name
func (m *SinkManager) GetSink(name string) (Sink, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sink, ok := m.sinks[name]
	return sink, ok
}

// GetSinks returns all registered sinks
func (m *SinkManager) GetSinks() map[string]Sink {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]Sink, len(m.sinks))
	for k, v := range m.sinks {
		result[k] = v
	}

	return result
}

// Write writes data to all enabled sinks
func (m *SinkManager) Write(ctx context.Context, key string, value []byte, metadata map[string]string) error {
	m.mu.RLock()
	sinks := make([]Sink, 0, len(m.sinks))
	for _, s := range m.sinks {
		sinks = append(sinks, s)
	}
	m.mu.RUnlock()

	if len(sinks) == 0 {
		return fmt.Errorf("no sinks registered")
	}

	var lastErr error
	var wg sync.WaitGroup

	for _, sink := range sinks {
		wg.Add(1)
		go func(s Sink) {
			defer wg.Done()

			start := time.Now()
			err := s.Write(ctx, key, value, metadata)
			duration := time.Since(start)

			m.mu.Lock()
			if stats, ok := m.stats[s.Name()]; ok {
				atomic.AddUint64(&stats.Writes, 1)
				atomic.AddUint64(&stats.BytesWritten, uint64(len(value)))
				if err != nil {
					atomic.AddUint64(&stats.Errors, 1)
				}
				// Update average latency
				oldAvg := atomic.LoadInt64((*int64)(&stats.LatencyAvg))
				newCount := atomic.AddUint64(&stats.Writes, 1)
				newAvg := (oldAvg*int64(newCount-1) + int64(duration)) / int64(newCount)
				atomic.StoreInt64((*int64)(&stats.LatencyAvg), newAvg)
			}
			m.mu.Unlock()

			if err != nil {
				lastErr = err
			}
		}(sink)
	}

	wg.Wait()

	return lastErr
}

// WriteBatch writes data to all enabled sinks in batch
func (m *SinkManager) WriteBatch(ctx context.Context, items []SinkItem) error {
	if len(items) == 0 {
		return nil
	}

	m.mu.RLock()
	sinks := make([]Sink, 0, len(m.sinks))
	for _, s := range m.sinks {
		sinks = append(sinks, s)
	}
	m.mu.RUnlock()

	if len(sinks) == 0 {
		return fmt.Errorf("no sinks registered")
	}

	var lastErr error
	var wg sync.WaitGroup

	for _, sink := range sinks {
		wg.Add(1)
		go func(s Sink) {
			defer wg.Done()
			err := s.WriteBatch(ctx, items)
			if err != nil {
				lastErr = err
			}
		}(sink)
	}

	wg.Wait()

	return lastErr
}

// Delete deletes data from all sinks
func (m *SinkManager) Delete(ctx context.Context, key string) error {
	m.mu.RLock()
	sinks := make([]Sink, 0, len(m.sinks))
	for _, s := range m.sinks {
		sinks = append(sinks, s)
	}
	m.mu.RUnlock()

	var lastErr error
	var wg sync.WaitGroup

	for _, sink := range sinks {
		wg.Add(1)
		go func(s Sink) {
			defer wg.Done()
			err := s.Delete(ctx, key)
			if err != nil {
				lastErr = err
			}
		}(sink)
	}

	wg.Wait()

	return lastErr
}

// PingAll pings all sinks and returns results
func (m *SinkManager) PingAll(ctx context.Context) map[string]error {
	m.mu.RLock()
	sinks := make([]Sink, 0, len(m.sinks))
	for _, s := range m.sinks {
		sinks = append(sinks, s)
	}
	m.mu.RUnlock()

	results := make(map[string]error)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, sink := range sinks {
		wg.Add(1)
		go func(s Sink) {
			defer wg.Done()
			err := s.Ping(ctx)
			mu.Lock()
			results[s.Name()] = err
			mu.Unlock()
		}(sink)
	}

	wg.Wait()

	return results
}

// Stats returns statistics for all sinks
func (m *SinkManager) Stats() map[string]SinkStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]SinkStats)
	for name, stats := range m.stats {
		result[name] = *stats
	}

	return result
}

// Close closes all sinks
func (m *SinkManager) Close() error {
	if m.closed.Swap(true) {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var closeErrors []error
	for name, sink := range m.sinks {
		if err := sink.Close(); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("%s: %w", name, err))
		}
	}

	if len(closeErrors) > 0 {
		return fmt.Errorf("errors closing sinks: %v", closeErrors)
	}

	return nil
}

// SerializeItem serializes a sink item for storage
func SerializeItem(item SinkItem) ([]byte, error) {
	return json.Marshal(item)
}

// DeserializeItem deserializes a sink item
func DeserializeItem(data []byte) (SinkItem, error) {
	var item SinkItem
	err := json.Unmarshal(data, &item)
	return item, err
}
