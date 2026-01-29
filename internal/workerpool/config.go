package workerpool

import "time"

// Config contains configuration for creating a worker pool
type Config struct {
	Workers       int           // Number of concurrent workers
	JobBufferSize int           // Size of job channel buffer (0 = unbuffered)
	FlushInterval time.Duration // Interval for flushing results
	BatchSize     int           // Number of items per batch
}

// NewConfig creates a new pool configuration with the given parameters
func NewConfig(workers int, batchSize int) Config {
	return Config{
		Workers:       workers,
		BatchSize:     batchSize,
		JobBufferSize: 0,                      // Default: unbuffered
		FlushInterval: 500 * time.Millisecond, // Default: 500ms
	}
}

// NewConfigWithDefaults creates a config with optimal values calculated from job count
func NewConfigWithDefaults(jobCount int) Config {
	return Config{
		Workers:       CalculateOptimalWorkers(jobCount),
		BatchSize:     CalculateOptimalBatchSize(jobCount),
		JobBufferSize: 0,
		FlushInterval: 500 * time.Millisecond,
	}
}

// CalculateOptimalWorkers determines the appropriate number of workers based on job count
// This is optimized for Redis I/O-bound operations (network latency is the bottleneck)
func CalculateOptimalWorkers(jobCount int) int {
	switch {
	case jobCount < 50:
		return 2 // Very small workloads - minimal overhead
	case jobCount < 200:
		return 5 // Small workloads - good parallelism without overwhelming Redis
	case jobCount < 500:
		return 8 // Medium workloads - sweet spot for Redis operations
	case jobCount < 1000:
		return 10 // Large workloads - high parallelism
	case jobCount < 2000:
		return 12 // Very large workloads
	default:
		return 15 // Huge workloads - capped to prevent Redis connection pool exhaustion
	}
}

// CalculateOptimalBatchSize determines the appropriate batch size based on job count
// Balances between frequent progress updates (small batches) and efficiency (large batches)
func CalculateOptimalBatchSize(jobCount int) int {
	switch {
	case jobCount < 20:
		return 5 // Very small - send updates frequently for better UX
	case jobCount < 100:
		return 10 // Small - good balance
	case jobCount < 500:
		return 25 // Medium - reduce WebSocket message overhead
	case jobCount < 1000:
		return 50 // Large - efficient batching
	case jobCount < 5000:
		return 100 // Very large - minimize overhead
	default:
		return 200 // Huge - maximum efficiency for bulk operations
	}
}
