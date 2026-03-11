package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
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
	WriteWorkers int
	ReadWorkers  int
	MissWorkers  int

	// Payload sizes (in KB) - 1KB, 3KB, 5KB, 7KB, 9KB, 11KB, 13KB, 15KB, 16KB (max)
	PayloadSizes []int

	// Stats interval
	StatsInterval time.Duration

	// Key range
	KeyRange int

	// Cache miss percentage
	MissPercentage int
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

	// Latency (in nanoseconds)
	WriteLatencyMin   uint64
	WriteLatencyMax   uint64
	WriteLatencyTotal uint64
	ReadLatencyMin    uint64
	ReadLatencyMax    uint64
	ReadLatencyTotal  uint64
	MissLatencyMin    uint64
	MissLatencyMax    uint64
	MissLatencyTotal  uint64

	// By payload size - using sync.Map for atomic updates
	WriteBySize *sync.Map
	ReadBySize  *sync.Map

	mu sync.Mutex
}

func newLoadTestStats() *LoadTestStats {
	return &LoadTestStats{
		WriteBySize:     &sync.Map{},
		ReadBySize:      &sync.Map{},
		WriteLatencyMin: ^uint64(0),
		ReadLatencyMin:  ^uint64(0),
		MissLatencyMin:  ^uint64(0),
	}
}

func (s *LoadTestStats) recordWrite(latency int64, size int) {
	atomic.AddUint64(&s.TotalWrites, 1)

	// Use sync.Map for atomic updates
	if val, ok := s.WriteBySize.Load(size); ok {
		s.WriteBySize.Store(size, val.(uint64)+1)
	} else {
		s.WriteBySize.Store(size, uint64(1))
	}

	s.mu.Lock()
	if uint64(latency) < s.WriteLatencyMin {
		s.WriteLatencyMin = uint64(latency)
	}
	if uint64(latency) > s.WriteLatencyMax {
		s.WriteLatencyMax = uint64(latency)
	}
	s.WriteLatencyTotal += uint64(latency)
	s.mu.Unlock()
}

func (s *LoadTestStats) recordRead(latency int64, size int, found bool) {
	if found {
		atomic.AddUint64(&s.TotalReads, 1)
	} else {
		atomic.AddUint64(&s.TotalMisses, 1)
	}

	s.mu.Lock()
	if found {
		if uint64(latency) < s.ReadLatencyMin {
			s.ReadLatencyMin = uint64(latency)
		}
		if uint64(latency) > s.ReadLatencyMax {
			s.ReadLatencyMax = uint64(latency)
		}
		s.ReadLatencyTotal += uint64(latency)

		// Use sync.Map for atomic updates
		if val, ok := s.ReadBySize.Load(size); ok {
			s.ReadBySize.Store(size, val.(uint64)+1)
		} else {
			s.ReadBySize.Store(size, uint64(1))
		}
	} else {
		if uint64(latency) < s.MissLatencyMin {
			s.MissLatencyMin = uint64(latency)
		}
		if uint64(latency) > s.MissLatencyMax {
			s.MissLatencyMax = uint64(latency)
		}
		s.MissLatencyTotal += uint64(latency)
	}
	s.mu.Unlock()
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

	stats := newLoadTestStats()

	// Payload sizes: 1KB, 3KB, 5KB, 7KB, 9KB, 11KB, 13KB, 15KB, 16KB (max)
	payloadSizes := []int{1, 3, 5, 7, 9, 11, 13, 15, 16}

	fmt.Printf("Starting load test with configuration:\n")
	fmt.Printf("  Duration: %v\n", cfg.Duration)
	fmt.Printf("  Write Workers: %d\n", cfg.WriteWorkers)
	fmt.Printf("  Read Workers: %d\n", cfg.ReadWorkers)
	fmt.Printf("  Miss Workers: %d\n", cfg.MissWorkers)
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

func printPeriodicStats(stats *LoadTestStats, cache *tieredcache.TieredCache, payloadSizes []int) {
	l0Stats, _ := cache.Stats()

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

	fmt.Printf("\n=== Periodic Stats (L0) ===\n")
	fmt.Printf("Time: %v\n", time.Now().Format("15:04:05"))
	fmt.Printf("L0 Stats:\n")
	fmt.Printf("  Entries: %d\n", l0Stats.L0.Entries)
	fmt.Printf("  Memory Used: %d bytes (%.2f MB)\n", l0Stats.L0.MemoryUsed, float64(l0Stats.L0.MemoryUsed)/1024/1024)
	fmt.Printf("  Memory Limit: %d bytes (%.2f MB)\n", l0Stats.L0.MemoryLimit, float64(l0Stats.L0.MemoryLimit)/1024/1024)
	fmt.Printf("  Hit Rate: %.2f%%\n", l0Stats.L0.HitRate)
	fmt.Printf("  Hits: %d, Misses: %d\n", l0Stats.L0.Hits, l0Stats.L0.Misses)
	fmt.Printf("  Sets: %d, Evictions: %d\n", l0Stats.L0.Sets, l0Stats.L0.Evictions)
	fmt.Printf("\nLoad Test Progress:\n")
	fmt.Printf("  Total Writes: %d (errors: %d)\n", writes, writeErrors)
	fmt.Printf("  Total Reads: %d (errors: %d)\n", reads, readErrors)
	fmt.Printf("  Total Misses: %d\n", misses)
	fmt.Printf("  Total Ops: %d\n", totalOps)
	fmt.Printf("  Hit Rate: %.2f%%\n\n", hitRate)
}

func printFinalStats(stats *LoadTestStats, cache *tieredcache.TieredCache, payloadSizes []int) {
	l0Stats, _ := cache.Stats()

	writes := atomic.LoadUint64(&stats.TotalWrites)
	reads := atomic.LoadUint64(&stats.TotalReads)
	misses := atomic.LoadUint64(&stats.TotalMisses)
	writeErrors := atomic.LoadUint64(&stats.WriteErrors)
	readErrors := atomic.LoadUint64(&stats.ReadErrors)

	totalOps := writes + reads + misses

	// Calculate hit rate
	hitRate := float64(0)
	if reads+misses > 0 {
		hitRate = float64(reads) / float64(reads+misses) * 100
	}

	// Calculate average latencies
	writeAvgLatency := uint64(0)
	readAvgLatency := uint64(0)
	missAvgLatency := uint64(0)

	if writes > 0 {
		stats.mu.Lock()
		writeAvgLatency = stats.WriteLatencyTotal / writes
		stats.mu.Unlock()
	}

	if reads > 0 {
		stats.mu.Lock()
		readAvgLatency = stats.ReadLatencyTotal / reads
		stats.mu.Unlock()
	}

	if misses > 0 {
		stats.mu.Lock()
		missAvgLatency = stats.MissLatencyTotal / misses
		stats.mu.Unlock()
	}

	fmt.Printf("\n")
	fmt.Printf("========================================\n")
	fmt.Printf("       FINAL LOAD TEST RESULTS        \n")
	fmt.Printf("========================================\n\n")

	fmt.Printf("--- L0 Cache Statistics ---\n")
	fmt.Printf("  Entries: %d\n", l0Stats.L0.Entries)
	fmt.Printf("  Memory Used: %d bytes (%.2f MB / %.2f%%)\n",
		l0Stats.L0.MemoryUsed,
		float64(l0Stats.L0.MemoryUsed)/1024/1024,
		float64(l0Stats.L0.MemoryUsed)/float64(l0Stats.L0.MemoryLimit)*100)
	fmt.Printf("  Hit Rate: %.2f%%\n", l0Stats.L0.HitRate)
	fmt.Printf("  Hits: %d\n", l0Stats.L0.Hits)
	fmt.Printf("  Misses: %d\n", l0Stats.L0.Misses)
	fmt.Printf("  Sets: %d\n", l0Stats.L0.Sets)
	fmt.Printf("  Evictions: %d\n", l0Stats.L0.Evictions)
	fmt.Printf("  Deletes: %d\n\n", l0Stats.L0.Deletes)

	fmt.Printf("--- Load Test Operations ---\n")
	fmt.Printf("  Total Writes: %d (errors: %d)\n", writes, writeErrors)
	fmt.Printf("  Total Reads: %d (errors: %d)\n", reads, readErrors)
	fmt.Printf("  Total Misses: %d\n", misses)
	fmt.Printf("  Total Operations: %d\n\n", totalOps)

	fmt.Printf("--- Hit Rate ---\n")
	fmt.Printf("  Read Hit Rate: %.2f%%\n\n", hitRate)

	fmt.Printf("--- Write Latency (ns) ---\n")
	fmt.Printf("  Min: %d\n", stats.WriteLatencyMin)
	fmt.Printf("  Avg: %d\n", writeAvgLatency)
	fmt.Printf("  Max: %d\n\n", stats.WriteLatencyMax)

	fmt.Printf("--- Read Latency (ns) ---\n")
	fmt.Printf("  Min: %d\n", stats.ReadLatencyMin)
	fmt.Printf("  Avg: %d\n", readAvgLatency)
	fmt.Printf("  Max: %d\n\n", stats.ReadLatencyMax)

	fmt.Printf("--- Miss Latency (ns) ---\n")
	fmt.Printf("  Min: %d\n", stats.MissLatencyMin)
	fmt.Printf("  Avg: %d\n", missAvgLatency)
	fmt.Printf("  Max: %d\n\n", stats.MissLatencyMax)

	// Print breakdown by payload size
	fmt.Printf("--- Writes by Payload Size ---\n")
	for _, size := range payloadSizes {
		if val, ok := stats.WriteBySize.Load(size); ok {
			count := val.(uint64)
			if count > 0 {
				fmt.Printf("  %d KB: %d writes\n", size, count)
			}
		}
	}
	fmt.Println()

	fmt.Printf("--- Reads by Payload Size ---\n")
	for _, size := range payloadSizes {
		if val, ok := stats.ReadBySize.Load(size); ok {
			count := val.(uint64)
			if count > 0 {
				fmt.Printf("  %d KB: %d reads\n", size, count)
			}
		}
	}
	fmt.Println()

	fmt.Printf("========================================\n")
}

func main() {
	cfg := LoadTestConfig{
		Duration:       30 * time.Second,
		WriteWorkers:   10,
		ReadWorkers:    10,
		MissWorkers:    5,
		StatsInterval:  5 * time.Second,
		KeyRange:       10000,
		MissPercentage: 30,
		PayloadSizes:   []int{1, 3, 5, 7, 9, 11, 13, 15, 16},
	}

	flag.DurationVar(&cfg.Duration, "duration", cfg.Duration, "Duration of the load test")
	flag.IntVar(&cfg.WriteWorkers, "write-workers", cfg.WriteWorkers, "Number of write workers")
	flag.IntVar(&cfg.ReadWorkers, "read-workers", cfg.ReadWorkers, "Number of read workers")
	flag.IntVar(&cfg.MissWorkers, "miss-workers", cfg.MissWorkers, "Number of miss test workers")
	flag.DurationVar(&cfg.StatsInterval, "stats-interval", cfg.StatsInterval, "Interval for periodic stats")
	flag.IntVar(&cfg.KeyRange, "key-range", cfg.KeyRange, "Range of keys to use")
	flag.IntVar(&cfg.MissPercentage, "miss-percentage", cfg.MissPercentage, "Percentage of misses to generate")

	flag.Parse()

	if err := runLoadTest(&cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error running load test: %v\n", err)
		os.Exit(1)
	}
}
