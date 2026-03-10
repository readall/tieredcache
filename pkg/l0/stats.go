package l0

import "sync/atomic"

// Stats represents cache statistics
type Stats struct {
	Hits        uint64  `json:"hits"`
	Misses      uint64  `json:"misses"`
	Sets        uint64  `json:"sets"`
	Deletes     uint64  `json:"deletes"`
	Evictions   uint64  `json:"evictions"`
	Entries     int     `json:"entries"`
	TotalWeight int     `json:"total_weight"`
	MemoryUsed  uint64  `json:"memory_used"`
	MemoryLimit uint64  `json:"memory_limit"`
	HitRate     float64 `json:"hit_rate"`
}

// Add increments stats atomically
func (s *Stats) Add(other Stats) {
	atomic.AddUint64(&s.Hits, other.Hits)
	atomic.AddUint64(&s.Misses, other.Misses)
	atomic.AddUint64(&s.Sets, other.Sets)
	atomic.AddUint64(&s.Deletes, other.Deletes)
	atomic.AddUint64(&s.Evictions, other.Evictions)
}

// Reset resets all stats to zero
func (s *Stats) Reset() {
	atomic.StoreUint64(&s.Hits, 0)
	atomic.StoreUint64(&s.Misses, 0)
	atomic.StoreUint64(&s.Sets, 0)
	atomic.StoreUint64(&s.Deletes, 0)
	atomic.StoreUint64(&s.Evictions, 0)
}
