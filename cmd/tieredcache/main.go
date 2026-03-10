package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tieredcache/pkg/common"
	"tieredcache/pkg/config"
	"tieredcache/pkg/tieredcache"
)

// Signal names for logging
var signalNames = map[os.Signal]string{
	syscall.SIGINT:  "SIGINT (Ctrl+C)",
	syscall.SIGTERM: "SIGTERM (kill)",
}

func main() {
	// Parse command line flags
	configPath := common.DefaultConfigPath
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

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)

	// Notify on all common termination signals (cross-platform)
	signal.Notify(sigChan,
		syscall.SIGINT,  // Ctrl+C (Interrupt from terminal)
		syscall.SIGTERM, // Termination request (kill default)
	)

	// Add platform-specific signals
	addPlatformSignals(sigChan)

	// Create cancellation context
	ctx, cancel := context.WithCancel(context.Background())

	// Handle signals in goroutine
	go func() {
		sig := <-sigChan
		fmt.Printf("\nReceived signal: %v\n", sig)

		// Log the signal type with descriptive name
		signalName := getSignalName(sig)
		fmt.Printf("%s - Initiating graceful shutdown...\n", signalName)

		// Cancel context to stop all operations
		cancel()
	}()

	// Example usage
	runExample(ctx, cache)

	// Wait for any background operations to complete
	fmt.Println("Waiting for background operations to complete...")
	time.Sleep(common.DefaultCloseWaitTime)

	// Print stats
	stats, err := cache.Stats()
	if err != nil {
		log.Printf("Failed to get stats: %v", err)
	} else {
		fmt.Printf("\nFinal Cache Stats:\n")
		fmt.Printf("  L0 - Hits: %d, Misses: %d, Entries: %d, Memory: %d bytes\n",
			stats.L0.Hits, stats.L0.Misses, stats.L0.Entries, stats.L0.MemoryUsed)
		fmt.Printf("  L1 - Reads: %d, Writes: %d, Disk: %d bytes\n",
			stats.L1.Reads, stats.L1.Writes, stats.L1.DiskUsage)
	}

	// Close cache with graceful shutdown
	fmt.Println("Closing cache...")
	if err := cache.Close(); err != nil {
		log.Printf("Error closing cache: %v", err)
	} else {
		fmt.Println("Cache closed successfully")
	}

	// Stop signal handling
	signal.Stop(sigChan)

	fmt.Println("Shutdown complete - Goodbye!")
}

// getSignalName returns a human-readable name for the signal
func getSignalName(sig os.Signal) string {
	if name, ok := signalNames[sig.(syscall.Signal)]; ok {
		return name
	}
	return fmt.Sprintf("Signal: %v", sig)
}

// addPlatformSignals adds platform-specific signals
// On Unix systems, adds SIGQUIT, SIGHUP, SIGUSR1, SIGUSR2
// On Windows, no additional signals are available
func addPlatformSignals(sigChan chan<- os.Signal) {
	// On Unix, we could add:
	// syscall.SIGQUIT, syscall.SIGHUP, syscall.SIGUSR1, syscall.SIGUSR2
	// But these are not available on Windows, so we keep it simple
	// for cross-platform compatibility

	// For Unix systems, uncomment the following:
	// signal.Notify(sigChan, syscall.SIGQUIT, syscall.SIGHUP)
	// Note: SIGUSR1/2 require syscall package and are platform-specific
	_ = sigChan // suppress unused warning
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
