# TieredCache Remediation Implementation Plan

## Overview

This document provides actionable implementation steps for the most critical failure and corruption issues identified in the failure analysis.

---

## Priority 1: Critical Issues (Fix Immediately)

### 1. WAL Corruption Handling - Graceful Degradation

**File:** `pkg/replay/recovery.go`

**Current Issue:** WAL corruption causes complete recovery failure (line 334-337)

**Current Code:**
```go
if !verifyChecksum(entry) {
    return entries, fmt.Errorf("checksum mismatch at sequence %d", entry.Sequence)
}
```

**Remediation Steps:**

1. Add max entry size constant
2. Implement entry size validation before checksum
3. Add corrupted entry tracking in RecoveryResult
4. Continue recovery instead of halting

**Implementation:**

```go
// Add to consts.go
const MaxWALEntrySize int = 1024 * 1024 // 1MB

// In recovery.go, modify replayWAL:
func (m *RecoveryManager) replayWAL(ctx context.Context, checkpointSeq uint64) ([]*WALEntry, error) {
    // ... existing code ...
    
    for {
        // Validate size before reading
        sizeBuf := make([]byte, common.WALHeaderSize)
        n, err := r.file.Read(sizeBuf)
        if err == io.EOF {
            break
        }
        if n < common.WALHeaderSize {
            // Incomplete header - end of valid WAL
            break
        }
        
        size := binary.BigEndian.Uint64(sizeBuf)
        
        // Validate size
        if size > uint64(common.MaxWALEntrySize) {
            // Corrupted entry size - skip remaining file
            m.corruptedEntries++
            break
        }
        
        // Read and verify entry
        entry, err := reader.ReadEntry()
        if err != nil {
            // Skip corrupted entry, continue
            m.skippedEntries++
            continue
        }
        
        // ... rest of processing
    }
    
    return entries, nil
}
```

### 2. L0 Snapshot Atomic Writes

**File:** `pkg/l0/l0_cache.go`

**Current Issue:** Snapshot writes are not atomic - crash during snapshot can corrupt data

**Current Code:** Line 496-500 shows direct file writes

**Remediation Steps:**

1. Implement atomic write pattern (write-to-temp + rename)
2. Add CRC32 checksum to snapshot
3. Maintain version header for format validation

**Implementation:**

```go
// In snapshot.go or l0_cache.go

func (c *L0Cache) snapshot() error {
    snapshotFile := filepath.Join(c.snapshotPath, fmt.Sprintf("snapshot_%d.snap", time.Now().Unix()))
    tempFile := snapshotFile + ".tmp"
    
    // Create temp file
    f, err := os.Create(tempFile)
    if err != nil {
        return fmt.Errorf("failed to create temp snapshot: %w", err)
    }
    
    // Write snapshot with checksum
    encoder := json.NewEncoder(f)
    encoder.SetEscapeHTML(false)
    
    // Write header
    header := SnapshotHeader{
        Version:    1,
        CreatedAt:  time.Now(),
        EntryCount: len(c.shards[0].entries) * int(c.shardCount),
    }
    if err := json.NewEncoder(f).Encode(header); err != nil {
        f.Close()
        os.Remove(tempFile)
        return err
    }
    
    // Write entries
    for _, shard := range c.shards {
        shard.mu.RLock()
        for key, entry := range shard.entries {
            if err := encoder.Encode(entry); err != nil {
                shard.mu.RUnlock()
                f.Close()
                os.Remove(tempFile)
                return err
            }
        }
        shard.mu.RUnlock()
    }
    
    // Calculate checksum
    f.Sync()
    f.Seek(0, io.SeekStart)
    hash := crc32.NewIEEE()
    io.Copy(hash, f)
    checksum := hash.Sum32()
    
    // Write checksum at end
    if err := binary.Write(f, binary.BigEndian, checksum); err != nil {
        f.Close()
        os.Remove(tempFile)
        return err
    }
    
    f.Close()
    
    // Atomic rename
    if err := os.Rename(tempFile, snapshotFile); err != nil {
        os.Remove(tempFile)
        return fmt.Errorf("failed to commit snapshot: %w", err)
    }
    
    // Clean old snapshots (keep last 3)
    c.cleanOldSnapshots(3)
    
    return nil
}
```

### 3. L1 Sync Mode Default Fix

**File:** `configs/config.yaml`

**Current Issue:** Default sync mode is "periodic" which can lose data on crash

**Current Config:**
```yaml
sync_mode: periodic  # Can lose up to 1 second of writes
```

**Remediation:**
Change default to "immediate" for production, add warning for periodic mode

---

## Priority 2: High Priority Issues

### 4. L2 Sink Retry Mechanism with DLQ

**File:** `pkg/l2/sinks.go`

**Current Issue:** No retry mechanism - failed writes are lost

**Implementation Pattern:**

```go
// New retry queue structure
type RetryQueue struct {
    items    []SinkItem
    maxSize  int
    mu       sync.Mutex
}

func (q *RetryQueue) Add(item SinkItem) error {
    q.mu.Lock()
    defer q.mu.Unlock()
    
    if len(q.items) >= q.maxSize {
        return fmt.Errorf("retry queue full")
    }
    q.items = append(q.items, item)
    return nil
}

// Dead letter queue with persistence
type DeadLetterQueue struct {
    path string
    mu   sync.Mutex
}

func (dq *DeadLetterQueue) Write(item SinkItem) error {
    // Persist to disk for crash recovery
    filename := filepath.Join(dq.path, fmt.Sprintf("dlq_%d.json", time.Now().UnixNano()))
    data, _ := json.Marshal(item)
    return os.WriteFile(filename, data, 0644)
}
```

### 5. Tiering Race Condition Fix

**File:** `pkg/tieredcache/tieredcache.go` - Add migration state tracking

**Implementation:**

```go
// Add to tieredcache.go
type migrationState struct {
    key       string
    fromTier  int
    toTier    int
    status    string  // "migrating", "migrated", "confirmed", "failed"
    timestamp time.Time
}

var migrations sync.Map // map[string]*migrationState

func (c *TieredCache) migrateToL1(key string, value []byte) error {
    // Two-phase migration
    
    // Phase 1: Copy to L1
    if err := c.l1.Set(c.ctx, key, value, 0); err != nil {
        return err
    }
    
    // Phase 2: Verify in L1
    if _, err := c.l1.Get(c.ctx, key); err != nil {
        return fmt.Errorf("migration verification failed")
    }
    
    // Phase 3: Delete from L0 (only after verification)
    return c.l0.Delete(c.ctx, key)
}
```

### 6. Pre-warming Memory Bounds

**File:** `pkg/tieredcache/tieredcache.go`

**Implementation:**

```go
func (c *TieredCache) preWarmL0() error {
    // Get available memory before starting
    var memStats runtime.MemStats
    runtime.ReadMemStats(&memStats)
    
    maxMemoryForPrewarm := uint64(float64(c.cfg.TieredCache.L0.MaxMemoryMB) * 0.5 * 1024 * 1024) // 50% of max
    var bytesLoaded uint64
    
    iter := c.l1.NewIterator(c.ctx)
    defer iter.Close()
    
    for iter.Next() {
        // Check memory pressure periodically
        runtime.ReadMemStats(&memStats)
        
        // Pause if approaching limit
        if memStats.Alloc > maxMemoryForPrewarm {
            // Wait for memory to be freed or signal completion
            time.Sleep(100 * time.Millisecond)
            
            // Abort if still over limit
            if memStats.Alloc > maxMemoryForPrewarm {
                return fmt.Errorf("pre-warming aborted: memory limit reached")
            }
        }
        
        // ... existing loading logic
    }
}
```

---

## Priority 3: Monitoring & Observability

### 7. Health Check Endpoints

Add health check that verifies:
- L0 memory usage
- L1 disk usage
- WAL accessibility
- L2 sink connectivity

### 8. Critical Metrics

| Metric | Alert Threshold | Action |
|--------|-----------------|--------|
| L0 Memory Usage | > 90% | Alert + Increase eviction |
| L1 Disk Usage | > 85% | Alert + Trigger tiering |
| WAL Size | > 1GB | Alert + Force checkpoint |
| L2 Sink Errors | > 10/min | Alert + Check connectivity |
| Recovery Time | > 5 min | Alert + Investigate |
| DLQ Size | > 1000 | Critical + Immediate action |

---

## Testing Requirements

### Chaos Tests to Implement

1. **Test: WAL Corruption Recovery**
   - Write 1000 entries
   - Corrupt WAL file (random bytes in middle)
   - Verify recovery continues with partial data

2. **Test: L0 Snapshot During Write**
   - Start continuous writes
   - Trigger snapshot
   - Kill process mid-snapshot
   - Verify recovery works

3. **Test: L2 Sink Failure**
   - Start writes to Kafka
   - Kill Kafka broker mid-write
   - Verify retry mechanism works

4. **Test: Concurrent Tiering**
   - Start multiple tiering operations
   - Verify no data loss

---

## Implementation Checklist

- [ ] 1.1 WAL corruption graceful handling
- [ ] 1.2 Add max WAL entry size
- [ ] 1.3 Track skipped/corrupted entries in recovery result
- [ ] 2.1 Atomic snapshot writes
- [ ] 2.2 Snapshot CRC32 checksum
- [ ] 2.3 Snapshot version header
- [ ] 2.4 Rolling snapshots (keep 3)
- [ ] 3.1 Change default sync mode
- [ ] 4.1 L2 retry queue implementation
- [ ] 4.2 Dead letter queue with disk persistence
- [ ] 4.3 Idempotency keys
- [ ] 5.1 Two-phase tiering implementation
- [ ] 5.2 Migration verification step
- [ ] 6.1 Memory-bounded pre-warming
- [ ] 6.2 Pre-warming abort on memory pressure
- [ ] 7.1 Health check implementation
- [ ] 7.2 Critical metrics alerts
- [ ] 8.1 WAL corruption chaos test
- [ ] 8.2 Snapshot crash test
- [ ] 8.3 L2 sink failure test
- [ ] 8.4 Concurrent tiering test
