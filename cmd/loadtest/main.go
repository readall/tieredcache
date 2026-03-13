package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"tieredcache/pkg/common"
	"tieredcache/pkg/config"
	"tieredcache/pkg/tieredcache"
)

// LoadTestConfig holds the load test configuration
type LoadTestConfig struct {
	// Duration
	Duration time.Duration

	// Workers
	WriteWorkers    int // Number of write workers
	ReadWorkers     int // Number of read workers
	MissWorkers     int // Number of miss test workers
	VerifyWorkers   int // Number of verification workers (Set -> L1.Get)
	L1DirectWorkers int // Number of L1 direct workers (L1.Set -> Get)

	// Payload sizes (in KB) - 1KB, 3KB, 5KB, 7KB, 9KB, 11KB, 13KB, 15KB, 16KB (max)
	PayloadSizes []int

	// Stats interval
	StatsInterval time.Duration

	// Key range
	KeyRange int

	// Cache miss percentage
	MissPercentage int
}

// LatencyRecorder records latencies for percentile calculation
type LatencyRecorder struct {
	mu      sync.Mutex
	values  []int64
	maxSize int
}

// NewLatencyRecorder creates a new latency recorder
func NewLatencyRecorder(maxSize int) *LatencyRecorder {
	return &LatencyRecorder{
		values:  make([]int64, 0, maxSize),
		maxSize: maxSize,
	}
}

func (r *LatencyRecorder) Record(latency int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Keep only last maxSize values for memory efficiency
	if len(r.values) >= r.maxSize {
		// Remove oldest 10%
		removeCount := r.maxSize / 10
		copy(r.values[:], r.values[removeCount:])
		r.values = r.values[:len(r.values)-removeCount]
	}
	r.values = append(r.values, latency)
}

func (r *LatencyRecorder) GetPercentiles() (p50, p90, p99 int64, avg int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.values) == 0 {
		return 0, 0, 0, 0
	}

	// Create a sorted copy
	sorted := make([]int64, len(r.values))
	copy(sorted, r.values)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	n := len(sorted)

	// Calculate percentiles
	p50Idx := int(float64(n) * 0.50)
	p90Idx := int(float64(n) * 0.90)
	p99Idx := int(float64(n) * 0.99)

	if p50Idx >= n {
		p50Idx = n - 1
	}
	if p90Idx >= n {
		p90Idx = n - 1
	}
	if p99Idx >= n {
		p99Idx = n - 1
	}

	// Calculate average
	var total int64
	for _, v := range sorted {
		total += v
	}
	avg = total / int64(n)

	return sorted[p50Idx], sorted[p90Idx], sorted[p99Idx], avg
}

// SizeStats holds stats for a specific payload size
type SizeStats struct {
	mu           sync.Mutex
	WriteCount   uint64
	WriteLatency *LatencyRecorder
	ReadCount    uint64
	ReadLatency  *LatencyRecorder
}

func newSizeStats() *SizeStats {
	return &SizeStats{
		WriteLatency: NewLatencyRecorder(10000),
		ReadLatency:  NewLatencyRecorder(10000),
	}
}

func (s *SizeStats) RecordWrite(latency int64) {
	atomic.AddUint64(&s.WriteCount, 1)
	s.WriteLatency.Record(latency)
}

func (s *SizeStats) RecordRead(latency int64) {
	atomic.AddUint64(&s.ReadCount, 1)
	s.ReadLatency.Record(latency)
}

func (s *SizeStats) GetWriteStats() (count uint64, p50, p90, p99, avg int64) {
	count = atomic.LoadUint64(&s.WriteCount)
	p50, p90, p99, avg = s.WriteLatency.GetPercentiles()
	return
}

func (s *SizeStats) GetReadStats() (count uint64, p50, p90, p99, avg int64) {
	count = atomic.LoadUint64(&s.ReadCount)
	p50, p90, p99, avg = s.ReadLatency.GetPercentiles()
	return
}

// LoadTestStats holds the load test statistics
type LoadTestStats struct {
	// Operation counts
	TotalWrites uint64
	TotalReads  uint64
	TotalMisses uint64
	WriteErrors uint64
	ReadErrors  uint64
	MissErrors  uint64

	// L1 Verification stats (Set then verify with L1.Get)
	VerificationSuccess uint64
	VerificationFailure uint64

	// L1 Direct Verification stats (L1.Set then verify with tieredcache.Get)
	L1DirectVerifySuccess uint64
	L1DirectVerifyFailure uint64

	// By payload size
	SizeStats map[int]*SizeStats

	// Timing
	startTime time.Time
	mu        sync.Mutex
}

func newLoadTestStats(payloadSizes []int) *LoadTestStats {
	stats := &LoadTestStats{
		SizeStats: make(map[int]*SizeStats),
		startTime: time.Now(),
	}

	// Initialize size stats for each payload size
	for _, size := range payloadSizes {
		stats.SizeStats[size] = newSizeStats()
	}

	return stats
}

func (s *LoadTestStats) recordWrite(latency int64, size int) {
	atomic.AddUint64(&s.TotalWrites, 1)

	if stats, ok := s.SizeStats[size]; ok {
		stats.RecordWrite(latency)
	}
}

func (s *LoadTestStats) recordRead(latency int64, size int, found bool) {
	if found {
		atomic.AddUint64(&s.TotalReads, 1)
	} else {
		atomic.AddUint64(&s.TotalMisses, 1)
	}

	if found {
		if stats, ok := s.SizeStats[size]; ok {
			stats.RecordRead(latency)
		}
	} else {
		// Record miss with size 1 (smallest bucket)
		if stats, ok := s.SizeStats[1]; ok {
			stats.RecordRead(latency)
		}
	}
}

func (s *LoadTestStats) recordWriteError() {
	atomic.AddUint64(&s.WriteErrors, 1)
}

func (s *LoadTestStats) recordReadError() {
	atomic.AddUint64(&s.ReadErrors, 1)
}

func (s *LoadTestStats) recordMissError() {
	atomic.AddUint64(&s.MissErrors, 1)
}

func (s *LoadTestStats) GetElapsedTime() time.Duration {
	return time.Since(s.startTime)
}

// GeneratePayload generates a random payload of the specified size in KB
func GeneratePayload(sizeKB int) []byte {
	size := sizeKB * 1024
	// Cap at 16KB as per requirement
	if size > 16*1024 {
		size = 16 * 1024
	}
	payload := make([]byte, size)
	rand.Read(payload)
	return payload
}

// GenerateKey generates a key based on index
func GenerateKey(index int) string {
	return fmt.Sprintf("loadtest_key_%08d", index)
}

func runLoadTest(cfg *LoadTestConfig) error {
	// Initialize the tiered cache
	tieredCfg, err := config.Load("configs/config.yaml")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	cache, err := tieredcache.New(tieredCfg)
	if err != nil {
		return fmt.Errorf("failed to create tiered cache: %w", err)
	}

	if err := cache.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize tiered cache: %w", err)
	}
	defer cache.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	go func() {
		<-sigChan
		fmt.Println("\nReceived interrupt signal, stopping load test...")
		cancel()
	}()

	// Use payload sizes from config
	payloadSizes := cfg.PayloadSizes

	stats := newLoadTestStats(payloadSizes)

	fmt.Printf("Starting load test with configuration:\n")
	fmt.Printf("  Duration: %v\n", cfg.Duration)
	fmt.Printf("  Write Workers: %d\n", cfg.WriteWorkers)
	fmt.Printf("  Read Workers: %d\n", cfg.ReadWorkers)
	fmt.Printf("  Miss Workers: %d\n", cfg.MissWorkers)
	fmt.Printf("  Verify Workers: %d (Set -> L1.Get)\n", cfg.VerifyWorkers)
	fmt.Printf("  L1 Direct Workers: %d (L1.Set -> Get)\n", cfg.L1DirectWorkers)
	fmt.Printf("  Payload Sizes: %v KB\n", payloadSizes)
	fmt.Printf("  Stats Interval: %v\n", cfg.StatsInterval)
	fmt.Printf("  Key Range: %d\n", cfg.KeyRange)
	fmt.Printf("  Miss Percentage: %d%%\n\n", cfg.MissPercentage)

	// Start periodic stats reporter
	var wg sync.WaitGroup

	// Periodic stats ticker
	statsTicker := time.NewTicker(cfg.StatsInterval)
	defer statsTicker.Stop()

	// Start stats reporter goroutine
	periodicDone := make(chan struct{})
	go func() {
		for {
			select {
			case <-statsTicker.C:
				printPeriodicStats(stats, cache, payloadSizes)
			case <-periodicDone:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	// Start write workers
	for i := 0; i < cfg.WriteWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			runWriteWorker(ctx, cache, stats, workerID, payloadSizes, cfg.KeyRange)
		}(i)
	}

	// Start read workers (concurrent with writes)
	for i := 0; i < cfg.ReadWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			runReadWorker(ctx, cache, stats, workerID, payloadSizes, cfg.KeyRange)
		}(i)
	}

	// Start miss test workers (tests cache misses in L0)
	for i := 0; i < cfg.MissWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			runMissWorker(ctx, cache, stats, workerID, payloadSizes, cfg.KeyRange, cfg.MissPercentage)
		}(i)
	}

	// Start verification workers (tests Set -> L1.Get)
	for i := 0; i < cfg.VerifyWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			runVerifyWorker(ctx, cache, stats, workerID, payloadSizes, cfg.KeyRange)
		}(i)
	}

	// Start L1 direct verification workers (tests L1.Set -> Get)
	for i := 0; i < cfg.L1DirectWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			runL1DirectVerifyWorker(ctx, cache, stats, workerID, payloadSizes, cfg.KeyRange)
		}(i)
	}

	// Wait for duration
	time.Sleep(cfg.Duration)

	// Cancel context to stop workers
	cancel()

	// Wait for all workers to finish
	wg.Wait()

	// Close periodic stats reporter
	close(periodicDone)

	// Print final stats
	printFinalStats(stats, cache, payloadSizes)

	return nil
}

func runWriteWorker(ctx context.Context, cache *tieredcache.TieredCache, stats *LoadTestStats, workerID int, payloadSizes []int, keyRange int) {
	r := rand.New(rand.NewSource(int64(workerID) + time.Now().UnixNano()))

	ticker := time.NewTicker(1 * time.Millisecond)
	defer ticker.Stop()

	writeCount := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Select random payload size
			size := payloadSizes[r.Intn(len(payloadSizes))]
			payload := GeneratePayload(size)

			// Select random key (some may already exist)
			keyIndex := r.Intn(keyRange)
			key := GenerateKey(keyIndex)

			// Time the write
			start := time.Now()
			err := cache.Set(ctx, key, payload, 0)
			latency := time.Since(start).Nanoseconds()

			if err != nil {
				stats.recordWriteError()
				fmt.Printf("Write error (worker %d): %v\n", workerID, err)
			} else {
				stats.recordWrite(latency, size)
			}
			writeCount++
		}
	}
}

func runReadWorker(ctx context.Context, cache *tieredcache.TieredCache, stats *LoadTestStats, workerID int, payloadSizes []int, keyRange int) {
	r := rand.New(rand.NewSource(int64(workerID+1000) + time.Now().UnixNano()))

	ticker := time.NewTicker(1 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Select random payload size
			size := payloadSizes[r.Intn(len(payloadSizes))]
			_ = size // Size is for reference, we just want to read

			// Select random key
			keyIndex := r.Intn(keyRange)
			key := GenerateKey(keyIndex)

			// Time the read
			start := time.Now()
			val, err := cache.Get(ctx, key)
			latency := time.Since(start).Nanoseconds()

			if err != nil {
				if err == common.ErrCodeNotFound {
					// Cache miss - record it
					stats.recordRead(latency, size, false)
				} else {
					stats.recordReadError()
					fmt.Printf("Read error (worker %d): %v\n", workerID, err)
				}
			} else {
				// Cache hit - record actual payload size
				actualSize := len(val) / 1024
				if actualSize == 0 && len(val) > 0 {
					actualSize = 1
				}
				stats.recordRead(latency, actualSize, true)
			}
		}
	}
}

func runMissWorker(ctx context.Context, cache *tieredcache.TieredCache, stats *LoadTestStats, workerID int, payloadSizes []int, keyRange int, missPercentage int) {
	r := rand.New(rand.NewSource(int64(workerID+2000) + time.Now().UnixNano()))

	ticker := time.NewTicker(1 * time.Millisecond)
	defer ticker.Stop()

	// Separate key range for miss testing (non-overlapping with write keys)
	missKeyStart := keyRange

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Check if we should trigger a miss
			shouldMiss := r.Intn(100) < missPercentage

			var key string
			if shouldMiss {
				// Use keys outside the normal range to force misses
				keyIndex := missKeyStart + r.Intn(keyRange)
				key = fmt.Sprintf("miss_key_%08d", keyIndex)
			} else {
				// Use a key that might exist
				keyIndex := r.Intn(keyRange)
				key = GenerateKey(keyIndex)
			}

			// Select random payload size for reference
			size := payloadSizes[r.Intn(len(payloadSizes))]
			_ = size

			// Time the read
			start := time.Now()
			val, err := cache.Get(ctx, key)
			latency := time.Since(start).Nanoseconds()

			if err != nil {
				if err == common.ErrCodeNotFound {
					// Cache miss - this is what we want for miss testing
					stats.recordRead(latency, 1, false)
				} else {
					stats.recordMissError()
				}
			} else {
				// Cache hit
				actualSize := len(val) / 1024
				if actualSize == 0 && len(val) > 0 {
					actualSize = 1
				}
				stats.recordRead(latency, actualSize, true)
			}
		}
	}
}

// runVerifyWorker tests Set followed by L1.Get() verification
// This verifies that data written to the cache is properly persisted to L1 (SSD)
func runVerifyWorker(ctx context.Context, cache *tieredcache.TieredCache, stats *LoadTestStats, workerID int, payloadSizes []int, keyRange int) {
	r := rand.New(rand.NewSource(int64(workerID+3000) + time.Now().UnixNano()))

	ticker := time.NewTicker(10 * time.Millisecond) // Slower than regular workers
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Select random payload size
			size := payloadSizes[r.Intn(len(payloadSizes))]
			payload := GeneratePayload(size)

			// Generate unique key for verification test
			key := fmt.Sprintf("verify_key_%08d_%d", r.Intn(keyRange), workerID)

			// Step 1: Set the key in the cache
			setStart := time.Now()
			if err := cache.Set(ctx, key, payload, 0); err != nil {
				atomic.AddUint64(&stats.VerificationFailure, 1)
				fmt.Printf("VerifyWorker %d: Set failed for %s: %v\n", workerID, key, err)
				continue
			}
			setLatency := time.Since(setStart).Nanoseconds()

			// Small delay to ensure write completes
			time.Sleep(1 * time.Millisecond)

			// Step 2: Verify by reading directly from L1 (SSD tier)
			l1GetStart := time.Now()
			l1Value, err := cache.GetFromL1(ctx, key)
			l1GetLatency := time.Since(l1GetStart).Nanoseconds()

			if err != nil {
				atomic.AddUint64(&stats.VerificationFailure, 1)
				fmt.Printf("VerifyWorker %d: L1.Get failed for %s: %v\n", workerID, key, err)
				continue
			}

			// Step 3: Verify data integrity
			if len(l1Value) != len(payload) {
				atomic.AddUint64(&stats.VerificationFailure, 1)
				fmt.Printf("VerifyWorker %d: Size mismatch for %s - expected %d, got %d\n",
					workerID, key, len(payload), len(l1Value))
				continue
			}

			// Compare byte-by-byte
			matches := true
			for i := 0; i < len(payload); i++ {
				if l1Value[i] != payload[i] {
					matches = false
					break
				}
			}

			if !matches {
				atomic.AddUint64(&stats.VerificationFailure, 1)
				fmt.Printf("VerifyWorker %d: Data mismatch for %s\n", workerID, key)
				continue
			}

			// Verification successful
			atomic.AddUint64(&stats.VerificationSuccess, 1)

			// Log occasionally
			if atomic.LoadUint64(&stats.VerificationSuccess)%100 == 0 {
				fmt.Printf("VerifyWorker %d: Verified %d keys - Set: %.2fus, L1.Get: %.2fus\n",
					workerID, stats.VerificationSuccess,
					float64(setLatency)/1000, float64(l1GetLatency)/1000)
			}
		}
	}
}

// runL1DirectVerifyWorker tests L1.Set followed by tieredcache.Get() verification
// This verifies that data written directly to L1 can be retrieved through the normal Get path
func runL1DirectVerifyWorker(ctx context.Context, cache *tieredcache.TieredCache, stats *LoadTestStats, workerID int, payloadSizes []int, keyRange int) {
	r := rand.New(rand.NewSource(int64(workerID+4000) + time.Now().UnixNano()))

	ticker := time.NewTicker(10 * time.Millisecond) // Slower than regular workers
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Select random payload size
			size := payloadSizes[r.Intn(len(payloadSizes))]
			payload := GeneratePayload(size)

			// Generate unique key for verification test
			key := fmt.Sprintf("l1direct_verify_%08d_%d", r.Intn(keyRange), workerID)

			// Step 1: Set the key directly in L1 (SSD tier), bypassing L0
			l1SetStart := time.Now()
			if err := cache.SetToL1(ctx, key, payload, 0); err != nil {
				atomic.AddUint64(&stats.L1DirectVerifyFailure, 1)
				fmt.Printf("L1DirectVerifyWorker %d: L1.Set failed for %s: %v\n", workerID, key, err)
				continue
			}
			l1SetLatency := time.Since(l1SetStart).Nanoseconds()

			// Small delay to ensure write completes
			time.Sleep(1 * time.Millisecond)

			// Step 2: Verify by reading through normal Get (which checks L0 first, then L1)
			getStart := time.Now()
			value, err := cache.Get(ctx, key)
			getLatency := time.Since(getStart).Nanoseconds()

			if err != nil {
				atomic.AddUint64(&stats.L1DirectVerifyFailure, 1)
				fmt.Printf("L1DirectVerifyWorker %d: Get failed for %s: %v\n", workerID, key, err)
				continue
			}

			// Step 3: Verify data integrity
			if len(value) != len(payload) {
				atomic.AddUint64(&stats.L1DirectVerifyFailure, 1)
				fmt.Printf("L1DirectVerifyWorker %d: Size mismatch for %s - expected %d, got %d\n",
					workerID, key, len(payload), len(value))
				continue
			}

			// Compare byte-by-byte
			matches := true
			for i := 0; i < len(payload); i++ {
				if value[i] != payload[i] {
					matches = false
					break
				}
			}

			if !matches {
				atomic.AddUint64(&stats.L1DirectVerifyFailure, 1)
				fmt.Printf("L1DirectVerifyWorker %d: Data mismatch for %s\n", workerID, key)
				continue
			}

			// Verification successful
			atomic.AddUint64(&stats.L1DirectVerifySuccess, 1)

			// Log occasionally
			if atomic.LoadUint64(&stats.L1DirectVerifySuccess)%100 == 0 {
				fmt.Printf("L1DirectVerifyWorker %d: Verified %d keys - L1.Set: %.2fus, Get: %.2fus\n",
					workerID, stats.L1DirectVerifySuccess,
					float64(l1SetLatency)/1000, float64(getLatency)/1000)
			}
		}
	}
}

func printPeriodicStats(stats *LoadTestStats, cache *tieredcache.TieredCache, payloadSizes []int) {
	cacheStats, _ := cache.Stats()

	reads := atomic.LoadUint64(&stats.TotalReads)
	misses := atomic.LoadUint64(&stats.TotalMisses)
	writes := atomic.LoadUint64(&stats.TotalWrites)
	writeErrors := atomic.LoadUint64(&stats.WriteErrors)
	readErrors := atomic.LoadUint64(&stats.ReadErrors)

	totalOps := reads + misses + writes
	hitRate := float64(0)
	if reads+misses > 0 {
		hitRate = float64(reads) / float64(reads+misses) * 100
	}

	elapsed := stats.GetElapsedTime()
	writeTPS := float64(writes) / elapsed.Seconds()
	readTPS := float64(reads) / elapsed.Seconds()

	fmt.Printf("\n=== Periodic Stats (L0 & L1) ===\n")
	fmt.Printf("Time: %v | Elapsed: %v\n", time.Now().Format("15:04:05"), elapsed.Round(time.Second))
	fmt.Printf("L0 Stats:\n")
	fmt.Printf("  Entries: %d\n", cacheStats.L0.Entries)
	fmt.Printf("  Memory Used: %d bytes (%.2f MB)\n", cacheStats.L0.MemoryUsed, float64(cacheStats.L0.MemoryUsed)/1024/1024)
	fmt.Printf("  Memory Limit: %d bytes (%.2f MB)\n", cacheStats.L0.MemoryLimit, float64(cacheStats.L0.MemoryLimit)/1024/1024)
	fmt.Printf("  Hit Rate: %.2f%%\n", cacheStats.L0.HitRate*100)
	fmt.Printf("  Hits: %d, Misses: %d\n", cacheStats.L0.Hits, cacheStats.L0.Misses)
	fmt.Printf("  Sets: %d, Evictions: %d\n", cacheStats.L0.Sets, cacheStats.L0.Evictions)
	fmt.Printf("L1 Stats:\n")
	fmt.Printf("  Disk Usage: %d bytes (%.2f MB)\n", cacheStats.L1.DiskUsage, float64(cacheStats.L1.DiskUsage)/1024/1024)
	fmt.Printf("  Hits: %d, Misses: %d\n", cacheStats.L1.Hits, cacheStats.L1.Misses)
	fmt.Printf("  Reads: %d, Writes: %d\n", cacheStats.L1.Reads, cacheStats.L1.Writes)
	fmt.Printf("\nLoad Test Progress:\n")
	fmt.Printf("  Total Writes: %d (errors: %d)\n", writes, writeErrors)
	fmt.Printf("  Total Reads: %d (errors: %d)\n", reads, readErrors)
	fmt.Printf("  Total Misses: %d\n", misses)
	fmt.Printf("  Total Ops: %d\n", totalOps)
	fmt.Printf("  Hit Rate: %.2f%%\n", hitRate)
	fmt.Printf("\nThroughput:\n")
	fmt.Printf("  Write TPS: %.2f\n", writeTPS)
	fmt.Printf("  Read TPS: %.2f\n\n", readTPS)
}

func printFinalStats(stats *LoadTestStats, cache *tieredcache.TieredCache, payloadSizes []int) {
	cacheStats, _ := cache.Stats()

	writes := atomic.LoadUint64(&stats.TotalWrites)
	reads := atomic.LoadUint64(&stats.TotalReads)
	misses := atomic.LoadUint64(&stats.TotalMisses)
	writeErrors := atomic.LoadUint64(&stats.WriteErrors)
	readErrors := atomic.LoadUint64(&stats.ReadErrors)

	totalOps := writes + reads + misses
	elapsed := stats.GetElapsedTime()

	// Calculate hit rate
	hitRate := float64(0)
	if reads+misses > 0 {
		hitRate = float64(reads) / float64(reads+misses) * 100
	}

	// Calculate overall TPS
	writeTPS := float64(writes) / elapsed.Seconds()
	readTPS := float64(reads) / elapsed.Seconds()

	fmt.Printf("\n")
	fmt.Printf("========================================\n")
	fmt.Printf("       FINAL LOAD TEST RESULTS        \n")
	fmt.Printf("========================================\n\n")

	fmt.Printf("--- L0 Cache Statistics ---\n")
	fmt.Printf("  Entries: %d\n", cacheStats.L0.Entries)
	fmt.Printf("  Memory Used: %d bytes (%.2f MB / %.2f%%)\n",
		cacheStats.L0.MemoryUsed,
		float64(cacheStats.L0.MemoryUsed)/1024/1024,
		float64(cacheStats.L0.MemoryUsed)/float64(cacheStats.L0.MemoryLimit)*100)
	fmt.Printf("  Hit Rate: %.2f%%\n", cacheStats.L0.HitRate*100)
	fmt.Printf("  Hits: %d\n", cacheStats.L0.Hits)
	fmt.Printf("  Misses: %d\n", cacheStats.L0.Misses)
	fmt.Printf("  Sets: %d\n", cacheStats.L0.Sets)
	fmt.Printf("  Evictions: %d\n", cacheStats.L0.Evictions)
	fmt.Printf("  Deletes: %d\n\n", cacheStats.L0.Deletes)

	fmt.Printf("--- L1 Cache Statistics ---\n")
	fmt.Printf("  Disk Usage: %d bytes (%.2f MB)\n", cacheStats.L1.DiskUsage, float64(cacheStats.L1.DiskUsage)/1024/1024)
	fmt.Printf("  Hit Rate: %.2f%%\n", float64(cacheStats.L1.Hits)/float64(cacheStats.L1.Hits+cacheStats.L1.Misses)*100)
	fmt.Printf("  Hits: %d\n", cacheStats.L1.Hits)
	fmt.Printf("  Misses: %d\n", cacheStats.L1.Misses)
	fmt.Printf("  Reads: %d\n", cacheStats.L1.Reads)
	fmt.Printf("  Writes: %d\n", cacheStats.L1.Writes)
	fmt.Printf("  Deletes: %d\n\n", cacheStats.L1.Deletes)

	fmt.Printf("--- Load Test Operations ---\n")
	fmt.Printf("  Total Writes: %d (errors: %d)\n", writes, writeErrors)
	fmt.Printf("  Total Reads: %d (errors: %d)\n", reads, readErrors)
	fmt.Printf("  Total Misses: %d\n", misses)
	fmt.Printf("  Total Operations: %d\n", totalOps)
	fmt.Printf("  Elapsed Time: %v\n\n", elapsed.Round(time.Millisecond))

	// Verification stats
	verifies := atomic.LoadUint64(&stats.VerificationSuccess)
	verifyFailures := atomic.LoadUint64(&stats.VerificationFailure)
	if verifies > 0 || verifyFailures > 0 {
		verifyRate := float64(0)
		if verifies+verifyFailures > 0 {
			verifyRate = float64(verifies) / float64(verifies+verifyFailures) * 100
		}
		fmt.Printf("--- L1 Verification (Set -> L1.Get) ---\n")
		fmt.Printf("  Successful Verifications: %d\n", verifies)
		fmt.Printf("  Failed Verifications: %d\n", verifyFailures)
		fmt.Printf("  Verification Rate: %.2f%%\n\n", verifyRate)
	}

	// L1 Direct Verification stats
	l1DirectVerifies := atomic.LoadUint64(&stats.L1DirectVerifySuccess)
	l1DirectFailures := atomic.LoadUint64(&stats.L1DirectVerifyFailure)
	if l1DirectVerifies > 0 || l1DirectFailures > 0 {
		l1DirectRate := float64(0)
		if l1DirectVerifies+l1DirectFailures > 0 {
			l1DirectRate = float64(l1DirectVerifies) / float64(l1DirectVerifies+l1DirectFailures) * 100
		}
		fmt.Printf("--- L1 Direct Verification (L1.Set -> Get) ---\n")
		fmt.Printf("  Successful Verifications: %d\n", l1DirectVerifies)
		fmt.Printf("  Failed Verifications: %d\n", l1DirectFailures)
		fmt.Printf("  Verification Rate: %.2f%%\n\n", l1DirectRate)
	}

	fmt.Printf("--- Hit Rate ---\n")
	fmt.Printf("  Read Hit Rate: %.2f%%\n\n", hitRate)

	fmt.Printf("--- Overall Throughput ---\n")
	fmt.Printf("  Write TPS: %.2f\n", writeTPS)
	fmt.Printf("  Read TPS: %.2f\n\n", readTPS)

	// Print breakdown by payload size
	fmt.Printf("--- Write Latency & TPS by Payload Size ---\n")
	fmt.Printf("%-8s | %-10s | %-12s | %-12s | %-12s | %-12s | %-10s\n",
		"Size", "Count", "P50 (ns)", "P90 (ns)", "P99 (ns)", "Avg (ns)", "TPS")
	fmt.Printf("%-8s-+-%-10s-+-%-12s-+-%-12s-+-%-12s-+-%-12s-+-%-10s\n",
		"--------", "----------", "------------", "------------", "------------", "------------", "----------")
	for _, size := range payloadSizes {
		if sizeStats, ok := stats.SizeStats[size]; ok {
			count, p50, p90, p99, avg := sizeStats.GetWriteStats()
			if count > 0 {
				tps := float64(count) / elapsed.Seconds()
				fmt.Printf("%-8d | %-10d | %-12d | %-12d | %-12d | %-12d | %-10.2f\n",
					size, count, p50, p90, p99, avg, tps)
			}
		}
	}
	fmt.Println()

	fmt.Printf("--- Read Latency & TPS by Payload Size ---\n")
	fmt.Printf("%-8s | %-10s | %-12s | %-12s | %-12s | %-12s | %-10s\n",
		"Size", "Count", "P50 (ns)", "P90 (ns)", "P99 (ns)", "Avg (ns)", "TPS")
	fmt.Printf("%-8s-+-%-10s-+-%-12s-+-%-12s-+-%-12s-+-%-12s-+-%-10s\n",
		"--------", "----------", "------------", "------------", "------------", "------------", "----------")
	for _, size := range payloadSizes {
		if sizeStats, ok := stats.SizeStats[size]; ok {
			count, p50, p90, p99, avg := sizeStats.GetReadStats()
			if count > 0 {
				tps := float64(count) / elapsed.Seconds()
				fmt.Printf("%-8d | %-10d | %-12d | %-12d | %-12d | %-12d | %-10.2f\n",
					size, count, p50, p90, p99, avg, tps)
			}
		}
	}
	fmt.Println()

	fmt.Printf("========================================\n")
}

func main() {
	cfg := LoadTestConfig{
		Duration:        30 * time.Second,
		WriteWorkers:    10,
		ReadWorkers:     10,
		MissWorkers:     5,
		VerifyWorkers:   2, // Verification workers (Set -> L1.Get)
		L1DirectWorkers: 2, // L1 direct workers (L1.Set -> Get)
		StatsInterval:   5 * time.Second,
		KeyRange:        100000,
		MissPercentage:  30,
		// PayloadSizes:   []int{1, 3, 5, 7, 9, 11, 13, 15, 16},
		PayloadSizes: []int{2, 4},
	}

	flag.DurationVar(&cfg.Duration, "duration", cfg.Duration, "Duration of the load test")
	flag.IntVar(&cfg.WriteWorkers, "write-workers", cfg.WriteWorkers, "Number of write workers")
	flag.IntVar(&cfg.ReadWorkers, "read-workers", cfg.ReadWorkers, "Number of read workers")
	flag.IntVar(&cfg.MissWorkers, "miss-workers", cfg.MissWorkers, "Number of miss test workers")
	flag.IntVar(&cfg.VerifyWorkers, "verify-workers", cfg.VerifyWorkers, "Number of verification workers (Set -> L1.Get)")
	flag.IntVar(&cfg.L1DirectWorkers, "l1-direct-workers", cfg.L1DirectWorkers, "Number of L1 direct workers (L1.Set -> Get)")
	flag.DurationVar(&cfg.StatsInterval, "stats-interval", cfg.StatsInterval, "Interval for periodic stats")
	flag.IntVar(&cfg.KeyRange, "key-range", cfg.KeyRange, "Range of keys to use")
	flag.IntVar(&cfg.MissPercentage, "miss-percentage", cfg.MissPercentage, "Percentage of misses to generate")

	flag.Parse()

	if err := runLoadTest(&cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error running load test: %v\n", err)
		os.Exit(1)
	}
}
