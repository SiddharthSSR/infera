package registry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/infera/infera/go/pkg/types"
)

// ErrWorkerNotFound distinguishes a missing registration from a registry
// backend failure. Callers may safely ask the worker to register again only
// for this error.
var ErrWorkerNotFound = errors.New("worker not found")

// WorkerRegistry maintains the pool of available workers.
type WorkerRegistry struct {
	workers            map[string]*types.WorkerInfo
	modelIndex         map[string]map[string]struct{}
	mu                 sync.RWMutex
	config             RegistryConfig
	onHealthTransition func(HealthTransition)
}

// HealthTransitionEvent identifies a registry-driven worker health event.
type HealthTransitionEvent string

const (
	HealthTransitionMarkedUnhealthy HealthTransitionEvent = "marked_unhealthy"
	HealthTransitionRemoved         HealthTransitionEvent = "removed"
)

// HealthTransition describes a worker health transition detected by the registry.
type HealthTransition struct {
	Event          HealthTransitionEvent
	WorkerID       string
	FromStatus     types.WorkerStatus
	ToStatus       types.WorkerStatus
	SinceHeartbeat time.Duration
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

// OnHealthTransition sets a callback invoked when the registry marks a worker
// unhealthy or removes it after missed heartbeats.
func (r *WorkerRegistry) OnHealthTransition(callback func(HealthTransition)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onHealthTransition = callback
}

// Register adds a worker to the registry.
func (r *WorkerRegistry) Register(ctx context.Context, worker *types.WorkerInfo) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	if worker.WorkerID == "" {
		return fmt.Errorf("worker ID is required")
	}
	worker.RegistrationID = uuid.NewString()

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
func (r *WorkerRegistry) Deregister(ctx context.Context, workerID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	worker, exists := r.workers[workerID]
	if !exists {
		return fmt.Errorf("%w: %s", ErrWorkerNotFound, workerID)
	}

	for _, model := range worker.LoadedModels {
		r.removeFromModelIndex(model.ModelID, workerID)
	}

	delete(r.workers, workerID)
	return nil
}

// Get returns a worker by ID.
func (r *WorkerRegistry) Get(ctx context.Context, workerID string) (*types.WorkerInfo, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	worker, exists := r.workers[workerID]
	if !exists {
		return nil, false, nil
	}
	return worker.Clone(), true, nil
}

// GetWorkersForModel returns all workers that have a specific model loaded.
func (r *WorkerRegistry) GetWorkersForModel(ctx context.Context, modelID string) ([]*types.WorkerInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	workerIDs, exists := r.modelIndex[modelID]
	if !exists {
		return nil, nil
	}

	workers := make([]*types.WorkerInfo, 0, len(workerIDs))
	for workerID := range workerIDs {
		if worker, exists := r.workers[workerID]; exists {
			workers = append(workers, worker.Clone())
		}
	}
	return workers, nil
}

// GetHealthyWorkersForModel returns healthy workers that have a model loaded.
func (r *WorkerRegistry) GetHealthyWorkersForModel(ctx context.Context, modelID string) ([]*types.WorkerInfo, error) {
	workers, err := r.GetWorkersForModel(ctx, modelID)
	if err != nil {
		return nil, err
	}
	healthy := make([]*types.WorkerInfo, 0, len(workers))
	for _, w := range workers {
		if w.IsHealthy() && w.HasCapacity() {
			healthy = append(healthy, w)
		}
	}
	return healthy, nil
}

// GetAllWorkers returns all registered workers.
func (r *WorkerRegistry) GetAllWorkers(ctx context.Context) ([]*types.WorkerInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	workers := make([]*types.WorkerInfo, 0, len(r.workers))
	for _, w := range r.workers {
		workers = append(workers, w.Clone())
	}
	return workers, nil
}

// Snapshot returns one consistent cloned view of the worker registry.
func (r *WorkerRegistry) Snapshot(ctx context.Context) ([]*types.WorkerInfo, error) {
	return r.GetAllWorkers(ctx)
}

// GetHealthyWorkers returns all healthy workers.
func (r *WorkerRegistry) GetHealthyWorkers(ctx context.Context) ([]*types.WorkerInfo, error) {
	workers, err := r.GetAllWorkers(ctx)
	if err != nil {
		return nil, err
	}
	healthy := make([]*types.WorkerInfo, 0, len(workers))
	for _, w := range workers {
		if w.IsHealthy() {
			healthy = append(healthy, w)
		}
	}
	return healthy, nil
}

// UpdateWorkerStats updates the stats for a worker.
func (r *WorkerRegistry) UpdateWorkerStats(ctx context.Context, workerID string, stats types.WorkerStats) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	worker, exists := r.workers[workerID]
	if !exists {
		return fmt.Errorf("%w: %s", ErrWorkerNotFound, workerID)
	}

	worker.UpdateStats(stats)
	if worker.Status == types.WorkerStatusUnhealthy || worker.Status == types.WorkerStatusOffline {
		worker.UpdateStatus(types.WorkerStatusHealthy)
	}
	return nil
}

// UpdateWorkerModels updates the loaded models for a worker.
func (r *WorkerRegistry) UpdateWorkerModels(ctx context.Context, workerID string, models []types.LoadedModel) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	worker, exists := r.workers[workerID]
	if !exists {
		return fmt.Errorf("%w: %s", ErrWorkerNotFound, workerID)
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

// Heartbeat atomically updates telemetry and, when supplied, loaded models.
func (r *WorkerRegistry) Heartbeat(ctx context.Context, _ string, workerID string, stats types.WorkerStats, models []types.LoadedModel, replaceModels bool) (*types.WorkerInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	worker, exists := r.workers[workerID]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrWorkerNotFound, workerID)
	}
	worker.UpdateStats(stats)
	if worker.Status == types.WorkerStatusUnhealthy || worker.Status == types.WorkerStatusOffline {
		worker.UpdateStatus(types.WorkerStatusHealthy)
	}
	if replaceModels {
		for _, model := range worker.LoadedModels {
			r.removeFromModelIndex(model.ModelID, workerID)
		}
		worker.LoadedModels = models
		for _, model := range models {
			r.addToModelIndex(model.ModelID, workerID)
		}
	}
	return worker.Clone(), nil
}

// Count returns the number of registered workers.
func (r *WorkerRegistry) Count(ctx context.Context) (int, error) {
	workers, err := r.Snapshot(ctx)
	return len(workers), err
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
	transitions := []HealthTransition{}

	for workerID, worker := range r.workers {
		timeSinceHeartbeat := now.Sub(worker.LastHealthCheck)

		if timeSinceHeartbeat > r.config.RemovalThreshold {
			toRemove = append(toRemove, workerID)
			transitions = append(transitions, HealthTransition{
				Event:          HealthTransitionRemoved,
				WorkerID:       workerID,
				FromStatus:     worker.Status,
				ToStatus:       types.WorkerStatusOffline,
				SinceHeartbeat: timeSinceHeartbeat,
			})
			logEntries = append(logEntries, registryLogEntry{
				message:          "removing worker after missed heartbeats",
				workerID:         workerID,
				sinceHeartbeat:   timeSinceHeartbeat,
				removalThreshold: r.config.RemovalThreshold,
			})
		} else if timeSinceHeartbeat > r.config.UnhealthyThreshold {
			if worker.Status != types.WorkerStatusUnhealthy {
				transitions = append(transitions, HealthTransition{
					Event:          HealthTransitionMarkedUnhealthy,
					WorkerID:       workerID,
					FromStatus:     worker.Status,
					ToStatus:       types.WorkerStatusUnhealthy,
					SinceHeartbeat: timeSinceHeartbeat,
				})
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
	onHealthTransition := r.onHealthTransition

	for _, workerID := range toRemove {
		worker := r.workers[workerID]
		for _, model := range worker.LoadedModels {
			r.removeFromModelIndex(model.ModelID, workerID)
		}
		delete(r.workers, workerID)
	}

	r.mu.Unlock()
	if onHealthTransition != nil {
		for _, transition := range transitions {
			onHealthTransition(transition)
		}
	}
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
