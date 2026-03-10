package replay

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"tieredcache/pkg/common"
	"time"
)

// WALEntry represents a write-ahead log entry
type WALEntry struct {
	Sequence  uint64
	Timestamp time.Time
	Operation Operation
	Key       string
	Value     []byte
	Metadata  map[string]string
	Tier      int // 0=L0, 1=L1, 2=L2
	Checksum  uint64
}

// Operation represents the type of operation
type Operation int

const (
	OpSet Operation = iota
	OpDelete
	OpPromote
	OpDemote
)

// RecoveryManager manages recovery and replay
type RecoveryManager struct {
	walPath        string
	checkpointPath string
	maxWorkers     int

	// State
	sequence uint64
	closed   atomic.Bool

	// Corruption tracking
	skippedEntries uint64
	corruptedSize  uint64

	// Checkpointing
	lastCheckpoint     uint64
	lastCheckpointTime time.Time
	checkpointInterval int64

	// Writers
	mu     sync.Mutex
	wal    *os.File
	writer *WALWriter

	// Context
	ctx    context.Context
	cancel context.CancelFunc
}

// WALWriter writes WAL entries
type WALWriter struct {
	file   *os.File
	writer io.Writer
	seq    uint64
}

// NewRecoveryManager creates a new recovery manager
func NewRecoveryManager(walPath string, checkpointPath string, maxWorkers int, checkpointInterval int64) (*RecoveryManager, error) {
	if walPath == "" {
		return nil, fmt.Errorf("wal_path cannot be empty")
	}

	if maxWorkers <= 0 {
		maxWorkers = common.DefaultMaxReplayWorkers
	}

	if checkpointInterval <= 0 {
		checkpointInterval = common.DefaultCheckpointInterval
	}

	// Create directories
	if err := os.MkdirAll(walPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create wal directory: %w", err)
	}

	if checkpointPath != "" {
		if err := os.MkdirAll(checkpointPath, 0755); err != nil {
			return nil, fmt.Errorf("failed to create checkpoint directory: %w", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	mgr := &RecoveryManager{
		walPath:            walPath,
		checkpointPath:     checkpointPath,
		maxWorkers:         maxWorkers,
		checkpointInterval: checkpointInterval,
		ctx:                ctx,
		cancel:             cancel,
	}

	// Initialize WAL
	if err := mgr.initWAL(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize WAL: %w", err)
	}

	return mgr, nil
}

// initWAL initializes the write-ahead log
func (m *RecoveryManager) initWAL() error {
	walFile := filepath.Join(m.walPath, "wal.log")

	file, err := os.OpenFile(walFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open WAL file: %w", err)
	}

	m.wal = file
	m.writer = &WALWriter{
		file:   file,
		writer: file,
		seq:    m.sequence,
	}

	// Get current sequence from existing file
	if stat, err := file.Stat(); err == nil && stat.Size() > 0 {
		// Will need to scan to get last sequence
		m.sequence = 0
	}

	return nil
}

// Write writes a WAL entry
func (m *RecoveryManager) Write(ctx context.Context, entry *WALEntry) error {
	if m.closed.Load() {
		return fmt.Errorf("recovery manager is closed")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.sequence++
	entry.Sequence = m.sequence
	entry.Timestamp = time.Now()
	entry.Checksum = computeChecksum(entry)

	data, err := encodeEntry(entry)
	if err != nil {
		return fmt.Errorf("failed to encode entry: %w", err)
	}

	_, err = m.writer.writer.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write WAL: %w", err)
	}

	// Check for checkpoint
	if m.sequence%uint64(m.checkpointInterval) == 0 {
		go m.checkpoint() // Async checkpoint
	}

	return nil
}

// checkpoint creates a checkpoint
func (m *RecoveryManager) checkpoint() error {
	if m.checkpointPath == "" {
		return nil
	}

	m.mu.Lock()
	seq := m.sequence
	m.mu.Unlock()

	// Save checkpoint
	checkpoint := Checkpoint{
		Sequence:  seq,
		Timestamp: time.Now(),
		WALPath:   m.walPath,
	}

	data, err := json.Marshal(checkpoint)
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint: %w", err)
	}

	filename := filepath.Join(m.checkpointPath, fmt.Sprintf("checkpoint_%d.json", seq))
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write checkpoint: %w", err)
	}

	m.lastCheckpoint = seq
	m.lastCheckpointTime = time.Now()

	return nil
}

// RecoveryResult contains the result of a recovery operation
type RecoveryResult struct {
	EntriesReplayed uint64
	EntriesSkipped  uint64 // Entries skipped due to corruption
	CorruptedSize   uint64 // Total bytes of corrupted data
	Duration        time.Duration
	Errors          []error
	Success         bool
}

// Recover performs recovery from WAL and checkpoints
func (m *RecoveryManager) Recover(ctx context.Context, applyFunc func(*WALEntry) error) (*RecoveryResult, error) {
	if m.closed.Load() {
		return nil, fmt.Errorf("recovery manager is closed")
	}

	start := time.Now()
	result := &RecoveryResult{
		Success: true,
	}

	// Find latest checkpoint
	latestCheckpoint, err := m.findLatestCheckpoint()
	if err != nil {
		result.Success = false
		result.Errors = append(result.Errors, err)
		return result, err
	}

	// Find and replay WAL entries after checkpoint
	entries, err := m.replayWAL(ctx, latestCheckpoint)
	if err != nil {
		result.Success = false
		result.Errors = append(result.Errors, err)
		return result, err
	}

	// Apply entries
	var wg sync.WaitGroup
	entryChan := make(chan *WALEntry, common.WALEntryChannelBuffer)
	errorChan := make(chan error, common.ErrorChannelBuffer)

	// Start workers
	for i := 0; i < m.maxWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for entry := range entryChan {
				if err := applyFunc(entry); err != nil {
					errorChan <- err
				}
			}
		}()
	}

	// Send entries to workers
EntriesLoop:
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			break EntriesLoop
		case errorChan <- nil:
		default:
		}
		entryChan <- entry
		result.EntriesReplayed++
	}

	close(entryChan)
	wg.Wait()
	close(errorChan)

	// Collect errors
	for err := range errorChan {
		if err != nil {
			result.Errors = append(result.Errors, err)
		}
	}

	if len(result.Errors) > 0 {
		result.Success = false
	}

	// Populate corruption tracking
	result.EntriesSkipped = m.skippedEntries
	result.CorruptedSize = m.corruptedSize

	result.Duration = time.Since(start)

	// Update sequence to latest
	if len(entries) > 0 {
		m.sequence = entries[len(entries)-1].Sequence
	}

	return result, nil
}

// replayWAL replays WAL entries after the checkpoint
func (m *RecoveryManager) replayWAL(ctx context.Context, checkpointSeq uint64) ([]*WALEntry, error) {
	walFile := filepath.Join(m.walPath, "wal.log")

	file, err := os.Open(walFile)
	if err != nil {
		if os.IsNotExist(err) {
			return []*WALEntry{}, nil
		}
		return nil, fmt.Errorf("failed to open WAL: %w", err)
	}
	defer file.Close()

	var entries []*WALEntry
	reader := &WALReader{file: file}

	for {
		select {
		case <-ctx.Done():
			return entries, ctx.Err()
		default:
		}

		// Read size with validation
		sizeBuf := make([]byte, common.WALHeaderSize)
		n, err := file.Read(sizeBuf)
		if err == io.EOF {
			break
		}
		if n < common.WALHeaderSize {
			// Incomplete header - end of valid WAL
			break
		}

		size := binary.BigEndian.Uint64(sizeBuf)

		// Validate size to detect corruption
		if size > uint64(common.MaxWALEntrySize) {
			// Corrupted entry size - skip remaining file
			m.corruptedSize += uint64(common.WALHeaderSize)
			m.skippedEntries++
			break
		}

		// Read entry
		if cap(reader.buffer) < int(size) {
			reader.buffer = make([]byte, size)
		}
		reader.buffer = reader.buffer[:size]

		n, err = file.Read(reader.buffer)
		if err == io.EOF {
			// Partial entry at end of file - truncate
			break
		}
		if err != nil {
			// Read error - skip corrupted entry and continue
			m.corruptedSize += uint64(n) + uint64(common.WALHeaderSize)
			m.skippedEntries++
			continue
		}
		if n < int(size) {
			// Incomplete entry - truncate
			m.corruptedSize += uint64(n) + uint64(common.WALHeaderSize)
			m.skippedEntries++
			break
		}

		// Decode entry
		entry, err := decodeEntry(reader.buffer)
		if err != nil {
			// Decode error - skip corrupted entry and continue
			m.corruptedSize += uint64(size) + uint64(common.WALHeaderSize)
			m.skippedEntries++
			continue
		}

		// Skip entries before checkpoint
		if entry.Sequence <= checkpointSeq {
			continue
		}

		// Verify checksum
		if !verifyChecksum(entry) {
			// Checksum mismatch - skip corrupted entry but continue
			m.corruptedSize += uint64(size) + uint64(common.WALHeaderSize)
			m.skippedEntries++
			continue
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// findLatestCheckpoint finds the latest checkpoint
func (m *RecoveryManager) findLatestCheckpoint() (uint64, error) {
	if m.checkpointPath == "" {
		return 0, nil
	}

	files, err := os.ReadDir(m.checkpointPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to read checkpoint directory: %w", err)
	}

	var latestSeq uint64

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		// Parse checkpoint file
		var checkpoint Checkpoint
		data, err := os.ReadFile(filepath.Join(m.checkpointPath, file.Name()))
		if err != nil {
			continue
		}

		if err := json.Unmarshal(data, &checkpoint); err != nil {
			continue
		}

		if checkpoint.Sequence > latestSeq {
			latestSeq = checkpoint.Sequence
			_ = file.Name() // Store for debugging if needed
		}
	}

	return latestSeq, nil
}

// Close closes the recovery manager
func (m *RecoveryManager) Close() error {
	if m.closed.Swap(true) {
		return nil
	}

	m.cancel()

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.wal != nil {
		if err := m.wal.Sync(); err != nil {
			return fmt.Errorf("failed to sync WAL: %w", err)
		}
		if err := m.wal.Close(); err != nil {
			return fmt.Errorf("failed to close WAL: %w", err)
		}
	}

	return nil
}

// Checkpoint represents a checkpoint
type Checkpoint struct {
	Sequence  uint64    `json:"sequence"`
	Timestamp time.Time `json:"timestamp"`
	WALPath   string    `json:"wal_path"`
}

// WALReader reads WAL entries
type WALReader struct {
	file   *os.File
	buffer []byte
}

// ReadEntry reads a single WAL entry
func (r *WALReader) ReadEntry() (*WALEntry, error) {
	// Read size (8 bytes)
	sizeBuf := make([]byte, common.WALHeaderSize)
	_, err := r.file.Read(sizeBuf)
	if err != nil {
		return nil, err
	}

	size := binary.BigEndian.Uint64(sizeBuf)

	// Read entry
	if cap(r.buffer) < int(size) {
		r.buffer = make([]byte, size)
	}
	r.buffer = r.buffer[:size]

	_, err = r.file.Read(r.buffer)
	if err != nil {
		return nil, err
	}

	// Decode
	return decodeEntry(r.buffer)
}

// encodeEntry encodes a WAL entry to bytes
func encodeEntry(entry *WALEntry) ([]byte, error) {
	data, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}

	// Prepend size
	result := make([]byte, 8+len(data))
	binary.BigEndian.PutUint64(result[:8], uint64(len(data)))
	copy(result[8:], data)

	return result, nil
}

// decodeEntry decodes bytes to a WAL entry
func decodeEntry(data []byte) (*WALEntry, error) {
	var entry WALEntry
	err := json.Unmarshal(data, &entry)
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

// computeChecksum computes a checksum for a WAL entry
func computeChecksum(entry *WALEntry) uint64 {
	h := fnv.New64a()

	binary.Write(h, binary.BigEndian, entry.Sequence)
	h.Write([]byte(entry.Key))
	h.Write(entry.Value)
	h.Write([]byte(entry.Timestamp.String()))
	binary.Write(h, binary.BigEndian, int64(entry.Operation))

	return h.Sum64()
}

// verifyChecksum verifies the checksum of a WAL entry
func verifyChecksum(entry *WALEntry) bool {
	expected := computeChecksum(entry)
	return entry.Checksum == expected
}
