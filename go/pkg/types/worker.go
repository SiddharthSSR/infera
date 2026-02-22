package types

import (
	"sync"
	"time"
)

// WorkerStatus represents the health state of a worker.
type WorkerStatus string

const (
	WorkerStatusHealthy   WorkerStatus = "healthy"
	WorkerStatusDegraded  WorkerStatus = "degraded"
	WorkerStatusUnhealthy WorkerStatus = "unhealthy"
	WorkerStatusDraining  WorkerStatus = "draining"
	WorkerStatusOffline   WorkerStatus = "offline"
)

// LoadedModel represents a model loaded on a worker.
type LoadedModel struct {
	ModelID           string    `json:"model_id"`
	Version           string    `json:"version"`
	LoadedAt          time.Time `json:"loaded_at"`
	MemoryBytes       int64     `json:"memory_bytes"`
	MaxBatchSize      int       `json:"max_batch_size"`
	MaxSequenceLength int       `json:"max_sequence_length"`
}

// WorkerStats contains real-time metrics from a worker.
type WorkerStats struct {
	QueueDepth        int       `json:"queue_depth"`
	ActiveRequests    int       `json:"active_requests"`
	GPUUtilization    float64   `json:"gpu_utilization"`
	MemoryUsedBytes   int64     `json:"memory_used_bytes"`
	MemoryTotalBytes  int64     `json:"memory_total_bytes"`
	RequestsPerSecond float64   `json:"requests_per_second"`
	AvgLatencyMS      float64   `json:"avg_latency_ms"`
	P50LatencyMS      float64   `json:"p50_latency_ms"`
	P99LatencyMS      float64   `json:"p99_latency_ms"`
	ErrorRate         float64   `json:"error_rate"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// CurrentLoad returns a normalized load score (0.0 - 1.0).
func (s *WorkerStats) CurrentLoad() float64 {
	gpuLoad := s.GPUUtilization * 0.5
	queueLoad := min(float64(s.QueueDepth)/100.0, 1.0) * 0.3
	memoryLoad := 0.0
	if s.MemoryTotalBytes > 0 {
		memoryLoad = float64(s.MemoryUsedBytes) / float64(s.MemoryTotalBytes) * 0.2
	}
	return gpuLoad + queueLoad + memoryLoad
}

// IsOverloaded returns true if the worker is at capacity.
func (s *WorkerStats) IsOverloaded() bool {
	return s.CurrentLoad() > 0.9 || s.ErrorRate > 0.1
}

// WorkerInfo contains everything the router knows about a worker.
type WorkerInfo struct {
	WorkerID        string            `json:"worker_id"`
	Address         string            `json:"address"`
	Status          WorkerStatus      `json:"status"`
	LoadedModels    []LoadedModel     `json:"loaded_models"`
	Stats           WorkerStats       `json:"stats"`
	LastHealthCheck time.Time         `json:"last_health_check"`
	RegisteredAt    time.Time         `json:"registered_at"`
	Tags            map[string]string `json:"tags"`
	mu              sync.RWMutex
}

// HasModel checks if the worker has a specific model loaded.
func (w *WorkerInfo) HasModel(modelID string) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	for _, m := range w.LoadedModels {
		if m.ModelID == modelID {
			return true
		}
	}
	return false
}

// HasCapacity checks if the worker can accept more requests.
func (w *WorkerInfo) HasCapacity() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return !w.Stats.IsOverloaded() && w.Status == WorkerStatusHealthy
}

// IsHealthy returns true if the worker is healthy.
func (w *WorkerInfo) IsHealthy() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.Status == WorkerStatusHealthy || w.Status == WorkerStatusDegraded
}

// CurrentLoad returns the current load score.
func (w *WorkerInfo) CurrentLoad() float64 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.Stats.CurrentLoad()
}

// UpdateStats updates the worker stats.
func (w *WorkerInfo) UpdateStats(stats WorkerStats) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Stats = stats
	w.LastHealthCheck = time.Now()
}

// UpdateStatus updates the worker status.
func (w *WorkerInfo) UpdateStatus(status WorkerStatus) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Status = status
}

// Clone creates a copy of WorkerInfo.
func (w *WorkerInfo) Clone() *WorkerInfo {
	w.mu.RLock()
	defer w.mu.RUnlock()

	models := make([]LoadedModel, len(w.LoadedModels))
	copy(models, w.LoadedModels)

	tags := make(map[string]string)
	for k, v := range w.Tags {
		tags[k] = v
	}

	return &WorkerInfo{
		WorkerID:        w.WorkerID,
		Address:         w.Address,
		Status:          w.Status,
		LoadedModels:    models,
		Stats:           w.Stats,
		LastHealthCheck: w.LastHealthCheck,
		RegisteredAt:    w.RegisteredAt,
		Tags:            tags,
	}
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
