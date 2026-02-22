package router

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/infera/infera/go/internal/router/batcher"
	"github.com/infera/infera/go/internal/router/registry"
	"github.com/infera/infera/go/internal/router/strategy"
	"github.com/infera/infera/go/pkg/types"
)

// Config configures the router.
type Config struct {
	DefaultStrategy  types.StrategyType
	EnableBatching   bool
	MaxBatchSize     int
	MaxBatchWaitMS   int
	RequestTimeoutMS int
	MaxRetries       int
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		DefaultStrategy:  types.StrategyLeastLoaded,
		EnableBatching:   true,
		MaxBatchSize:     8,
		MaxBatchWaitMS:   50,
		RequestTimeoutMS: 30000,
		MaxRetries:       3,
	}
}

// Router is the main routing service.
type Router struct {
	config         Config
	registry       *registry.WorkerRegistry
	strategyEngine *strategy.Engine
	batchManager   *batcher.Manager
	trackers       map[string]*types.RequestTracker
	trackersMu     sync.RWMutex
	ctx            context.Context
	cancel         context.CancelFunc
}

// New creates a new router.
func New(config Config) *Router {
	ctx, cancel := context.WithCancel(context.Background())

	r := &Router{
		config:         config,
		registry:       registry.NewWorkerRegistry(registry.DefaultRegistryConfig()),
		strategyEngine: strategy.NewEngine(config.DefaultStrategy),
		batchManager: batcher.NewManager(batcher.Config{
			MaxBatchSize:      config.MaxBatchSize,
			MaxWaitMS:         config.MaxBatchWaitMS,
			MaxTokensPerBatch: 4096,
			Enabled:           config.EnableBatching,
		}),
		trackers: make(map[string]*types.RequestTracker),
		ctx:      ctx,
		cancel:   cancel,
	}

	r.batchManager.OnBatchReady(r.onBatchReady)
	go r.registry.StartHealthChecker(ctx)

	return r
}

// Route routes an inference request to a worker.
func (r *Router) Route(request *types.InferenceRequest) (*types.RoutedRequest, error) {
	if request.RequestID == "" {
		request.RequestID = uuid.New().String()
	}

	tracker := r.createTracker(request.RequestID)
	tracker.Transition(types.RequestStateQueued, "validated")

	candidates := r.registry.GetHealthyWorkersForModel(request.ModelID)
	if len(candidates) == 0 {
		allWorkers := r.registry.GetWorkersForModel(request.ModelID)
		if len(allWorkers) == 0 {
			return nil, types.NewInferaError(types.ErrorCodeModelNotFound,
				fmt.Sprintf("model %s not found", request.ModelID)).WithRequestID(request.RequestID)
		}
		return nil, types.NewInferaError(types.ErrorCodeModelOverloaded,
			fmt.Sprintf("all workers for model %s at capacity", request.ModelID)).WithRequestID(request.RequestID)
	}

	selection, err := r.strategyEngine.SelectWorker(request, candidates)
	if err != nil {
		return nil, types.NewInferaError(types.ErrorCodeInternalError,
			fmt.Sprintf("failed to select worker: %v", err)).WithRequestID(request.RequestID)
	}

	routed := &types.RoutedRequest{
		Request:         request,
		WorkerID:        selection.Worker.WorkerID,
		RoutingDecision: selection.Decision,
		RoutedAt:        time.Now(),
		Attempt:         1,
		MaxAttempts:     r.config.MaxRetries,
		Deadline:        time.Now().Add(time.Duration(r.config.RequestTimeoutMS) * time.Millisecond),
	}

	tracker.CurrentWorker = selection.Worker.WorkerID

	if r.batchManager.ShouldBatch(request) {
		tracker.Transition(types.RequestStateBatched, "batched")
		r.batchManager.Enqueue(routed)
	}

	return routed, nil
}

// HandleFailure processes a failed request, potentially retrying.
func (r *Router) HandleFailure(request *types.RoutedRequest, err error) (*types.RoutedRequest, error) {
	tracker := r.getTracker(request.Request.RequestID)
	if tracker != nil {
		tracker.Transition(types.RequestStateFailed, err.Error())
	}

	if !request.IsRetriable() || request.IsExpired() {
		return nil, types.NewInferaError(types.ErrorCodeInternalError,
			fmt.Sprintf("request failed: %v", err)).WithRequestID(request.Request.RequestID)
	}

	retry := request.WithRetry()

	candidates := r.registry.GetHealthyWorkersForModel(request.Request.ModelID)
	filtered := make([]*types.WorkerInfo, 0, len(candidates))
	for _, w := range candidates {
		if w.WorkerID != request.WorkerID {
			filtered = append(filtered, w)
		}
	}

	if len(filtered) == 0 {
		filtered = candidates
	}

	if len(filtered) == 0 {
		return nil, types.NewInferaError(types.ErrorCodeModelOverloaded, "no workers for retry")
	}

	selection, err := r.strategyEngine.SelectWorker(request.Request, filtered)
	if err != nil {
		return nil, err
	}

	retry.WorkerID = selection.Worker.WorkerID
	retry.RoutingDecision = selection.Decision
	retry.RoutedAt = time.Now()

	return retry, nil
}

// RegisterWorker registers a worker with the router.
func (r *Router) RegisterWorker(worker *types.WorkerInfo) error {
	return r.registry.Register(worker)
}

// DeregisterWorker removes a worker from the router.
func (r *Router) DeregisterWorker(workerID string) error {
	return r.registry.Deregister(workerID)
}

// UpdateWorkerStats updates stats for a worker.
func (r *Router) UpdateWorkerStats(workerID string, stats types.WorkerStats) error {
	return r.registry.UpdateWorkerStats(workerID, stats)
}

// UpdateWorkerModels updates loaded models for a worker.
func (r *Router) UpdateWorkerModels(workerID string, models []types.LoadedModel) error {
	return r.registry.UpdateWorkerModels(workerID, models)
}

// GetWorker returns a worker by ID.
func (r *Router) GetWorker(workerID string) (*types.WorkerInfo, bool) {
	return r.registry.Get(workerID)
}

// GetWorkers returns all workers, optionally filtered.
func (r *Router) GetWorkers(modelID string, healthyOnly bool) []*types.WorkerInfo {
	if modelID != "" {
		if healthyOnly {
			return r.registry.GetHealthyWorkersForModel(modelID)
		}
		return r.registry.GetWorkersForModel(modelID)
	}
	if healthyOnly {
		return r.registry.GetHealthyWorkers()
	}
	return r.registry.GetAllWorkers()
}

// WorkerCount returns the number of registered workers.
func (r *Router) WorkerCount() int {
	return r.registry.Count()
}

// GetQueueDepth returns the total queue depth.
func (r *Router) GetQueueDepth() int {
	return r.batchManager.GetQueueDepth()
}

// GetQueueDepthForModel returns the queue depth for a model.
func (r *Router) GetQueueDepthForModel(modelID string) int {
	return r.batchManager.GetQueueDepthForModel(modelID)
}

func (r *Router) createTracker(requestID string) *types.RequestTracker {
	tracker := &types.RequestTracker{RequestID: requestID, State: types.RequestStateReceived}
	r.trackersMu.Lock()
	r.trackers[requestID] = tracker
	r.trackersMu.Unlock()
	return tracker
}

func (r *Router) getTracker(requestID string) *types.RequestTracker {
	r.trackersMu.RLock()
	defer r.trackersMu.RUnlock()
	return r.trackers[requestID]
}

func (r *Router) onBatchReady(batch *types.BatchContext) {
	for _, req := range batch.Requests {
		tracker := r.getTracker(req.Request.RequestID)
		if tracker != nil {
			tracker.CurrentBatch = batch.BatchID
			tracker.Transition(types.RequestStateProcessing, "batch dispatched")
		}
	}
}

// Stop shuts down the router.
func (r *Router) Stop() {
	r.cancel()
	r.batchManager.Stop()
}

// Stats contains router statistics.
type Stats struct {
	TotalWorkers    int
	HealthyWorkers  int
	TotalQueueDepth int
	ModelsAvailable int
}

// GetStats returns current router statistics.
func (r *Router) GetStats() Stats {
	allWorkers := r.registry.GetAllWorkers()
	healthyWorkers := r.registry.GetHealthyWorkers()

	modelSet := make(map[string]struct{})
	for _, w := range allWorkers {
		for _, m := range w.LoadedModels {
			modelSet[m.ModelID] = struct{}{}
		}
	}

	return Stats{
		TotalWorkers:    len(allWorkers),
		HealthyWorkers:  len(healthyWorkers),
		TotalQueueDepth: r.GetQueueDepth(),
		ModelsAvailable: len(modelSet),
	}
}
