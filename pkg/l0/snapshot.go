package l0

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"tieredcache/pkg/common"
	"time"
)

// SnapshotHeader contains metadata for snapshot files
type SnapshotHeader struct {
	Version     int       `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	EntryCount  int       `json:"entry_count"`
	MemoryUsage uint64    `json:"memory_usage"`
	Checksum    uint32    `json:"-"`
}

const (
	// SnapshotVersion is the current snapshot format version
	SnapshotVersion = 1

	// SnapshotMagic is the magic number for snapshot files
	SnapshotMagic = 0x534E4150 // "SNAP" in hex

	// MaxSnapshotsToKeep is the maximum number of snapshots to retain
	MaxSnapshotsToKeep = 3
)

// WriteSnapshot writes an atomic snapshot of the cache to disk
// Uses write-to-temp + rename pattern for atomicity
func (c *L0Cache) WriteSnapshot() error {
	if c.snapshotting.Swap(true) {
		return nil // Already snapshotting
	}
	defer c.snapshotting.Store(false)

	// Ensure snapshot directory exists
	if err := os.MkdirAll(c.snapshotPath, 0755); err != nil {
		return fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	// Generate unique snapshot filename with timestamp
	timestamp := time.Now().UnixNano()
	snapshotFile := filepath.Join(c.snapshotPath, fmt.Sprintf("snapshot_%d.snap", timestamp))
	tempFile := snapshotFile + ".tmp"

	// Create temp file
	f, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("failed to create temp snapshot file: %w", err)
	}

	// Write magic number and version
	if err := binary.Write(f, binary.BigEndian, uint32(SnapshotMagic)); err != nil {
		f.Close()
		os.Remove(tempFile)
		return fmt.Errorf("failed to write magic number: %w", err)
	}

	if err := binary.Write(f, binary.BigEndian, uint32(SnapshotVersion)); err != nil {
		f.Close()
		os.Remove(tempFile)
		return fmt.Errorf("failed to write version: %w", err)
	}

	// Calculate total entries and memory usage
	totalEntries := 0
	totalMemory := uint64(0)
	for _, shard := range c.shards {
		shard.mu.RLock()
		totalEntries += len(shard.entries)
		for _, entry := range shard.entries {
			totalMemory += uint64(entry.Size)
		}
		shard.mu.RUnlock()
	}

	// Write header
	header := SnapshotHeader{
		Version:     SnapshotVersion,
		CreatedAt:   time.Now(),
		EntryCount:  totalEntries,
		MemoryUsage: totalMemory,
	}

	headerBytes, err := json.Marshal(header)
	if err != nil {
		f.Close()
		os.Remove(tempFile)
		return fmt.Errorf("failed to marshal header: %w", err)
	}

	// Write header length and header
	if err := binary.Write(f, binary.BigEndian, uint32(len(headerBytes))); err != nil {
		f.Close()
		os.Remove(tempFile)
		return fmt.Errorf("failed to write header length: %w", err)
	}

	if _, err := f.Write(headerBytes); err != nil {
		f.Close()
		os.Remove(tempFile)
		return fmt.Errorf("failed to write header: %w", err)
	}

	// Create CRC32 hash for entries
	hash := crc32.NewIEEE()

	// Write entries
	entryCount := 0
	for _, shard := range c.shards {
		shard.mu.RLock()
		for key, entry := range shard.entries {
			// Serialize entry
			entryData, err := json.Marshal(entry)
			if err != nil {
				shard.mu.RUnlock()
				f.Close()
				os.Remove(tempFile)
				return fmt.Errorf("failed to marshal entry %s: %w", key, err)
			}

			// Write entry length and data
			if err := binary.Write(f, binary.BigEndian, uint32(len(entryData))); err != nil {
				shard.mu.RUnlock()
				f.Close()
				os.Remove(tempFile)
				return fmt.Errorf("failed to write entry length: %w", err)
			}

			if _, err := f.Write(entryData); err != nil {
				shard.mu.RUnlock()
				f.Close()
				os.Remove(tempFile)
				return fmt.Errorf("failed to write entry: %w", err)
			}

			// Update hash
			hash.Write(entryData)
			entryCount++
		}
		shard.mu.RUnlock()
	}

	// Write checksum at the end
	checksum := hash.Sum32()
	if err := binary.Write(f, binary.BigEndian, checksum); err != nil {
		f.Close()
		os.Remove(tempFile)
		return fmt.Errorf("failed to write checksum: %w", err)
	}

	// Sync to disk
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tempFile)
		return fmt.Errorf("failed to sync snapshot: %w", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("failed to close snapshot file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempFile, snapshotFile); err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("failed to commit snapshot: %w", err)
	}

	// Clean old snapshots
	c.cleanOldSnapshots()

	return nil
}

// ReadSnapshot reads and validates a snapshot from disk
func (c *L0Cache) ReadSnapshot(path string) (SnapshotHeader, error) {
	header := SnapshotHeader{}

	f, err := os.Open(path)
	if err != nil {
		return header, fmt.Errorf("failed to open snapshot file: %w", err)
	}
	defer f.Close()

	// Read and verify magic number
	var magic uint32
	if err := binary.Read(f, binary.BigEndian, &magic); err != nil {
		return header, fmt.Errorf("failed to read magic number: %w", err)
	}
	if magic != SnapshotMagic {
		return header, fmt.Errorf("invalid snapshot: bad magic number (got 0x%X, expected 0x%X)", magic, SnapshotMagic)
	}

	// Read and verify version
	var version uint32
	if err := binary.Read(f, binary.BigEndian, &version); err != nil {
		return header, fmt.Errorf("failed to read version: %w", err)
	}
	if version != SnapshotVersion {
		return header, fmt.Errorf("unsupported snapshot version: %d (expected %d)", version, SnapshotVersion)
	}

	// Read header
	var headerLen uint32
	if err := binary.Read(f, binary.BigEndian, &headerLen); err != nil {
		return header, fmt.Errorf("failed to read header length: %w", err)
	}

	headerBytes := make([]byte, headerLen)
	if _, err := io.ReadFull(f, headerBytes); err != nil {
		return header, fmt.Errorf("failed to read header: %w", err)
	}

	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return header, fmt.Errorf("failed to unmarshal header: %w", err)
	}

	// Read all entries (they will be verified by checksum)
	// We don't restore here - we just validate the snapshot is readable
	// The actual restore is done by Restore() method

	return header, nil
}

// RestoreFromSnapshot restores the cache from a snapshot file
func (c *L0Cache) RestoreFromSnapshot(path string) error {
	if c.closed.Load() {
		return fmt.Errorf("cache is closed")
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open snapshot file: %w", err)
	}
	defer f.Close()

	// Read and verify magic number
	var magic uint32
	if err := binary.Read(f, binary.BigEndian, &magic); err != nil {
		return fmt.Errorf("failed to read magic number: %w", err)
	}
	if magic != SnapshotMagic {
		return fmt.Errorf("invalid snapshot: bad magic number")
	}

	// Read and verify version
	var version uint32
	if err := binary.Read(f, binary.BigEndian, &version); err != nil {
		return fmt.Errorf("failed to read version: %w", err)
	}
	if version != SnapshotVersion {
		return fmt.Errorf("unsupported snapshot version: %d", version)
	}

	// Read header
	var headerLen uint32
	if err := binary.Read(f, binary.BigEndian, &headerLen); err != nil {
		return fmt.Errorf("failed to read header length: %w", err)
	}

	headerBytes := make([]byte, headerLen)
	if _, err := io.ReadFull(f, headerBytes); err != nil {
		return fmt.Errorf("failed to read header: %w", err)
	}

	var header SnapshotHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return fmt.Errorf("failed to unmarshal header: %w", err)
	}

	// Create CRC32 hash for verification
	hash := crc32.NewIEEE()

	// Read and restore entries
	entriesRestored := 0
	for {
		var entryLen uint32
		err := binary.Read(f, binary.BigEndian, &entryLen)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read entry length: %w", err)
		}

		entryBytes := make([]byte, entryLen)
		if _, err := io.ReadFull(f, entryBytes); err != nil {
			return fmt.Errorf("failed to read entry: %w", err)
		}

		// Update hash for verification
		hash.Write(entryBytes)

		// Unmarshal entry
		var entry common.CacheEntry
		if err := json.Unmarshal(entryBytes, &entry); err != nil {
			return fmt.Errorf("failed to unmarshal entry: %w", err)
		}

		// Restore entry to cache
		if err := c.Set(c.ctx, entry.Key, entry.Value, entry.TTL); err != nil {
			// Log but continue - partial restore is better than none
			fmt.Printf("warning: failed to restore entry %s: %v\n", entry.Key, err)
		}
		entriesRestored++
	}

	// Verify checksum
	var storedChecksum uint32
	if err := binary.Read(f, binary.BigEndian, &storedChecksum); err != nil {
		return fmt.Errorf("failed to read checksum: %w", err)
	}

	computedChecksum := hash.Sum32()
	if storedChecksum != computedChecksum {
		return fmt.Errorf("snapshot checksum mismatch: stored=0x%X, computed=0x%X", storedChecksum, computedChecksum)
	}

	return nil
}

// cleanOldSnapshots removes old snapshot files, keeping only the most recent ones
func (c *L0Cache) cleanOldSnapshots() {
	// List all snapshot files
	files, err := filepath.Glob(filepath.Join(c.snapshotPath, "snapshot_*.snap"))
	if err != nil {
		return
	}

	if len(files) <= MaxSnapshotsToKeep {
		return
	}

	// Sort by modification time (oldest first)
	// Note: This is a simple implementation; for production, parse timestamp from filename
	for i := 0; i < len(files)-MaxSnapshotsToKeep; i++ {
		os.Remove(files[i])
	}
}

// FindLatestSnapshot finds the most recent snapshot file in the given directory
func FindLatestSnapshot(dir string) (string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "snapshot_*.snap"))
	if err != nil {
		return "", err
	}

	if len(files) == 0 {
		return "", fmt.Errorf("no snapshot files found")
	}

	// Find most recent by modification time
	var latest string
	var latestTime time.Time
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		if latest == "" || info.ModTime().After(latestTime) {
			latest = f
			latestTime = info.ModTime()
		}
	}

	if latest == "" {
		return "", fmt.Errorf("no valid snapshot files found")
	}

	return latest, nil
}
