package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tosha-tian/tieredcache/pkg/config"
	"github.com/tosha-tian/tieredcache/pkg/tieredcache"
)

func main() {
	// Parse command line flags
	configPath := "configs/config.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	// Load configuration
	cfg, err := config.LoadOrDefault(configPath)
	if err != nil {
		log.Printf("Failed to load config, using defaults: %v", err)
		cfg = config.DefaultConfig()
	}

	// Validate configuration
	if err := config.Validate(cfg); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	// Create tiered cache
	cache, err := tieredcache.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create cache: %v", err)
	}

	// Initialize cache
	if err := cache.Initialize(); err != nil {
		log.Fatalf("Failed to initialize cache: %v", err)
	}

	// Set up signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nShutting down...")
		cancel()
	}()

	// Example usage
	runExample(ctx, cache)

	// Print stats
	stats, err := cache.Stats()
	if err != nil {
		log.Printf("Failed to get stats: %v", err)
	} else {
		fmt.Printf("\nCache Stats:\n")
		fmt.Printf("  L0 - Hits: %d, Misses: %d, Entries: %d, Memory: %d bytes\n",
			stats.L0.Hits, stats.L0.Misses, stats.L0.Entries, stats.L0.MemoryUsed)
		fmt.Printf("  L1 - Reads: %d, Writes: %d, Disk: %d bytes\n",
			stats.L1.Reads, stats.L1.Writes, stats.L1.DiskUsage)
	}

	// Close cache
	if err := cache.Close(); err != nil {
		log.Fatalf("Failed to close cache: %v", err)
	}

	fmt.Println("Shutdown complete")
}

func runExample(ctx context.Context, cache *tieredcache.TieredCache) {
	fmt.Println("Running example operations...")

	// Write examples
	keys := []string{
		"user:1001",
		"session:abc123",
		"product:500",
		"cache:key1",
		"cache:key2",
	}

	for _, key := range keys {
		value := []byte(fmt.Sprintf("value_for_%s", key))

		if err := cache.Set(ctx, key, value, time.Hour); err != nil {
			log.Printf("Failed to set %s: %v", key, err)
		} else {
			fmt.Printf("Set: %s\n", key)
		}
	}

	// Read examples
	fmt.Println("\nReading keys:")
	for _, key := range keys {
		value, err := cache.Get(ctx, key)
		if err != nil {
			fmt.Printf("  %s: not found\n", key)
		} else {
			fmt.Printf("  %s: %s\n", key, string(value))
		}
	}

	// Delete example
	fmt.Println("\nDeleting key: session:abc123")
	if err := cache.Delete(ctx, "session:abc123"); err != nil {
		log.Printf("Failed to delete: %v", err)
	}

	// Verify deletion
	value, err := cache.Get(ctx, "session:abc123")
	if err != nil {
		fmt.Printf("  Confirmed: key deleted (error: %v)\n", err)
	} else {
		fmt.Printf("  Unexpected: got value: %s\n", string(value))
	}
}
