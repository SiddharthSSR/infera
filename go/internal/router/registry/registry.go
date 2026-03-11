package registry

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/infera/infera/go/pkg/types"
)

// WorkerRegistry maintains the pool of available workers.
type WorkerRegistry struct {
	workers    map[string]*types.WorkerInfo
	modelIndex map[string]map[string]struct{}
	mu         sync.RWMutex
	config     RegistryConfig
}

// RegistryConfig configures the registry behavior.
type RegistryConfig struct {
	HealthCheckInterval time.Duration
	UnhealthyThreshold  time.Duration
	RemovalThreshold    time.Duration
}

// DefaultRegistryConfig returns sensible defaults.
func DefaultRegistryConfig() RegistryConfig {
	return RegistryConfig{
		HealthCheckInterval: 5 * time.Second,
		UnhealthyThreshold:  10 * time.Second,
		RemovalThreshold:    30 * time.Second,
	}
}

// NewWorkerRegistry creates a new registry.
func NewWorkerRegistry(config RegistryConfig) *WorkerRegistry {
	return &WorkerRegistry{
		workers:    make(map[string]*types.WorkerInfo),
		modelIndex: make(map[string]map[string]struct{}),
		config:     config,
	}
}

// Register adds a worker to the registry.
func (r *WorkerRegistry) Register(worker *types.WorkerInfo) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if worker.WorkerID == "" {
		return fmt.Errorf("worker ID is required")
	}

	// If replacing an existing worker entry, remove old model index entries first.
	if existing, exists := r.workers[worker.WorkerID]; exists {
		for _, model := range existing.LoadedModels {
			r.removeFromModelIndex(model.ModelID, worker.WorkerID)
		}
	}

	r.workers[worker.WorkerID] = worker

	for _, model := range worker.LoadedModels {
		r.addToModelIndex(model.ModelID, worker.WorkerID)
	}

	return nil
}

// Deregister removes a worker from the registry.
func (r *WorkerRegistry) Deregister(workerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	worker, exists := r.workers[workerID]
	if !exists {
		return fmt.Errorf("worker %s not found", workerID)
	}

	for _, model := range worker.LoadedModels {
		r.removeFromModelIndex(model.ModelID, workerID)
	}

	delete(r.workers, workerID)
	return nil
}

// Get returns a worker by ID.
func (r *WorkerRegistry) Get(workerID string) (*types.WorkerInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	worker, exists := r.workers[workerID]
	if !exists {
		return nil, false
	}
	return worker.Clone(), true
}

// GetWorkersForModel returns all workers that have a specific model loaded.
func (r *WorkerRegistry) GetWorkersForModel(modelID string) []*types.WorkerInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	workerIDs, exists := r.modelIndex[modelID]
	if !exists {
		return nil
	}

	workers := make([]*types.WorkerInfo, 0, len(workerIDs))
	for workerID := range workerIDs {
		if worker, exists := r.workers[workerID]; exists {
			workers = append(workers, worker.Clone())
		}
	}
	return workers
}

// GetHealthyWorkersForModel returns healthy workers that have a model loaded.
func (r *WorkerRegistry) GetHealthyWorkersForModel(modelID string) []*types.WorkerInfo {
	workers := r.GetWorkersForModel(modelID)
	healthy := make([]*types.WorkerInfo, 0, len(workers))
	for _, w := range workers {
		if w.IsHealthy() && w.HasCapacity() {
			healthy = append(healthy, w)
		}
	}
	return healthy
}

// GetAllWorkers returns all registered workers.
func (r *WorkerRegistry) GetAllWorkers() []*types.WorkerInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	workers := make([]*types.WorkerInfo, 0, len(r.workers))
	for _, w := range r.workers {
		workers = append(workers, w.Clone())
	}
	return workers
}

// GetHealthyWorkers returns all healthy workers.
func (r *WorkerRegistry) GetHealthyWorkers() []*types.WorkerInfo {
	workers := r.GetAllWorkers()
	healthy := make([]*types.WorkerInfo, 0, len(workers))
	for _, w := range workers {
		if w.IsHealthy() {
			healthy = append(healthy, w)
		}
	}
	return healthy
}

// UpdateWorkerStats updates the stats for a worker.
func (r *WorkerRegistry) UpdateWorkerStats(workerID string, stats types.WorkerStats) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	worker, exists := r.workers[workerID]
	if !exists {
		return fmt.Errorf("worker %s not found", workerID)
	}

	worker.UpdateStats(stats)
	return nil
}

// UpdateWorkerModels updates the loaded models for a worker.
func (r *WorkerRegistry) UpdateWorkerModels(workerID string, models []types.LoadedModel) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	worker, exists := r.workers[workerID]
	if !exists {
		return fmt.Errorf("worker %s not found", workerID)
	}

	for _, model := range worker.LoadedModels {
		r.removeFromModelIndex(model.ModelID, workerID)
	}

	worker.LoadedModels = models

	for _, model := range models {
		r.addToModelIndex(model.ModelID, workerID)
	}

	return nil
}

// Count returns the number of registered workers.
func (r *WorkerRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.workers)
}

// StartHealthChecker starts a background goroutine that checks worker health.
func (r *WorkerRegistry) StartHealthChecker(ctx context.Context) {
	ticker := time.NewTicker(r.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.checkWorkerHealth()
		}
	}
}

func (r *WorkerRegistry) checkWorkerHealth() {
	r.mu.Lock()

	now := time.Now()
	toRemove := []string{}
	type registryLogEntry struct {
		message            string
		workerID           string
		sinceHeartbeat     time.Duration
		unhealthyThreshold time.Duration
		removalThreshold   time.Duration
	}
	logEntries := []registryLogEntry{}

	for workerID, worker := range r.workers {
		timeSinceHeartbeat := now.Sub(worker.LastHealthCheck)

		if timeSinceHeartbeat > r.config.RemovalThreshold {
			toRemove = append(toRemove, workerID)
			logEntries = append(logEntries, registryLogEntry{
				message:          "removing worker after missed heartbeats",
				workerID:         workerID,
				sinceHeartbeat:   timeSinceHeartbeat,
				removalThreshold: r.config.RemovalThreshold,
			})
		} else if timeSinceHeartbeat > r.config.UnhealthyThreshold {
			if worker.Status != types.WorkerStatusUnhealthy {
				logEntries = append(logEntries, registryLogEntry{
					message:            "marking worker unhealthy after missed heartbeats",
					workerID:           workerID,
					sinceHeartbeat:     timeSinceHeartbeat,
					unhealthyThreshold: r.config.UnhealthyThreshold,
				})
			}
			worker.UpdateStatus(types.WorkerStatusUnhealthy)
		}
	}

	for _, workerID := range toRemove {
		worker := r.workers[workerID]
		for _, model := range worker.LoadedModels {
			r.removeFromModelIndex(model.ModelID, workerID)
		}
		delete(r.workers, workerID)
	}

	r.mu.Unlock()
	for _, entry := range logEntries {
		attrs := []any{
			slog.String("worker_id", entry.workerID),
			slog.Duration("since_heartbeat", entry.sinceHeartbeat.Round(time.Second)),
		}
		if entry.unhealthyThreshold > 0 {
			attrs = append(attrs, slog.Duration("unhealthy_threshold", entry.unhealthyThreshold))
		}
		if entry.removalThreshold > 0 {
			attrs = append(attrs, slog.Duration("removal_threshold", entry.removalThreshold))
		}
		slog.Info(entry.message, attrs...)
	}
}

func (r *WorkerRegistry) addToModelIndex(modelID, workerID string) {
	if _, exists := r.modelIndex[modelID]; !exists {
		r.modelIndex[modelID] = make(map[string]struct{})
	}
	r.modelIndex[modelID][workerID] = struct{}{}
}

func (r *WorkerRegistry) removeFromModelIndex(modelID, workerID string) {
	if workers, exists := r.modelIndex[modelID]; exists {
		delete(workers, workerID)
		if len(workers) == 0 {
			delete(r.modelIndex, modelID)
		}
	}
}
