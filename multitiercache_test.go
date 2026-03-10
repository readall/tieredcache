package multitiercache

import (
	"context"
	"fmt"
	"time"
)

// Example usage with all features + parallel L2s
func ExampleUsage() {
	cfg := DefaultConfig("/data/cache")
	cfg.EnableSink = true

	store, err := NewShardedCachedStore(cfg)
	if err != nil {
		panic(err)
	}
	defer store.Close()

	// Register parallel L2s (after New, before StartSink)
	kafkaTier, _ := NewKafkaTier([]string{"localhost:9092"}, "cold-events")
	minioTier, _ := NewMinioTier("minio:9000", "access", "secret", "archive", false)
	pgTier, _ := NewPostgresTier("postgres://user:pass@localhost/db", "cold_archive")

	store.sinkManager = NewMultiSinkManager(store.shards, []TierConfig{
		{Tier: kafkaTier, Policy: func(i TierItem) bool { return i.Meta.AccessCount < 10 }, Workers: 2, Rate: 500},
		{Tier: minioTier, Policy: func(i TierItem) bool { return time.Since(time.Unix(int64(i.Meta.LastAccessUnix), 0)) > 30*24*time.Hour }, Workers: 4, Rate: 200},
		{Tier: pgTier, Policy: func(i TierItem) bool { return true }, Workers: 1, Rate: 100},
	})
	store.sinkManager.Start(context.Background())

	// Your app logic
	ctx := context.Background()
	_ = store.Set(ctx, []byte("user:123"), []byte("payload..."))
	val, _ := store.Get(ctx, []byte("user:123"))

	// Wait for readiness in production (Kubernetes probe)
	for !store.Ready() {
		time.Sleep(500 * time.Millisecond)
	}
	fmt.Println("Cache ready with value:", string(val))
}
