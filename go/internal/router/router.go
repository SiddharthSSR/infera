package router

import (
	"context"
	"errors"
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
	AffinityTTL      time.Duration
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
		AffinityTTL:      10 * time.Minute,
		RequestTimeoutMS: 30000,
		MaxRetries:       3,
	}
}

// Router is the main routing service.
type Router struct {
	config          Config
	registry        *registry.WorkerRegistry
	strategyEngine  *strategy.Engine
	batchManager    *batcher.Manager
	onBatchDispatch func(batch *types.BatchContext)
	trackers        map[string]*types.RequestTracker
	trackersMu      sync.RWMutex
	batchWaiters    map[string]chan batchRouteResult
	batchWaitersMu  sync.Mutex
	affinity        map[string]affinityBinding
	affinityMu      sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
}

// WorkerHealthTransition describes a registry-driven worker health event.
type WorkerHealthTransition = registry.HealthTransition

type batchRouteResult struct {
	routed *types.RoutedRequest
	err    error
}

type affinityBinding struct {
	WorkerID  string
	ExpiresAt time.Time
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
		trackers:     make(map[string]*types.RequestTracker),
		batchWaiters: make(map[string]chan batchRouteResult),
		affinity:     make(map[string]affinityBinding),
		ctx:          ctx,
		cancel:       cancel,
	}

	r.batchManager.OnBatchReady(r.onBatchReady)
	go r.registry.StartHealthChecker(ctx)

	return r
}

// Route routes an inference request to a worker.
func (r *Router) Route(ctx context.Context, request *types.InferenceRequest) (*types.RoutedRequest, error) {
	if request.RequestID == "" {
		request.RequestID = uuid.New().String()
	}

	tracker := r.createTracker(request.RequestID)
	tracker.Transition(types.RequestStateQueued, "validated")

	routed := &types.RoutedRequest{
		Request:     request,
		RoutedAt:    time.Now(),
		Attempt:     1,
		MaxAttempts: r.config.MaxRetries,
		Deadline:    time.Now().Add(time.Duration(r.config.RequestTimeoutMS) * time.Millisecond),
	}

	if r.batchManager.ShouldBatch(request) {
		if err := r.validateModelAvailability(request); err != nil {
			return nil, err
		}
		waiter := r.registerBatchWaiter(request.RequestID)
		defer r.unregisterBatchWaiter(request.RequestID, waiter)

		tracker.Transition(types.RequestStateBatched, "batched")
		r.batchManager.Enqueue(routed)

		select {
		case result := <-waiter:
			if result.err != nil {
				return nil, result.err
			}
			return result.routed, nil
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil, context.Canceled
			}
			return nil, types.NewInferaError(
				types.ErrorCodeTimeout,
				"request timed out while waiting for batch dispatch",
			).WithRequestID(request.RequestID)
		}
	}

	selection, err := r.selectWorker(request)
	if err != nil {
		return nil, err
	}

	routed.WorkerID = selection.Worker.WorkerID
	routed.RoutingDecision = selection.Decision
	tracker.CurrentWorker = selection.Worker.WorkerID
	return routed, nil
}

// HandleFailure processes a failed request, potentially retrying.
func (r *Router) HandleFailure(request *types.RoutedRequest, err error) (*types.RoutedRequest, error) {
	tracker := r.getTracker(request.Request.RequestID)
	if tracker != nil {
		tracker.Transition(types.RequestStateFailed, err.Error())
	}

	r.clearAffinityForFailure(request)

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
	selection, err := r.selectWorkerForModel(batch.ModelID)
	batchWait := time.Since(batch.CreatedAt)
	if batch.SealedAt != nil {
		batchWait = batch.SealedAt.Sub(batch.CreatedAt)
	}
	if err == nil && r.onBatchDispatch != nil {
		r.onBatchDispatch(batch)
	}
	for _, req := range batch.Requests {
		tracker := r.getTracker(req.Request.RequestID)
		if tracker != nil {
			tracker.CurrentBatch = batch.BatchID
			if err == nil {
				tracker.CurrentWorker = selection.Worker.WorkerID
				tracker.Transition(types.RequestStateProcessing, "batch dispatched")
			} else {
				tracker.Transition(types.RequestStateFailed, err.Error())
			}
		}

		if err != nil {
			r.completeBatchRoute(req.Request.RequestID, batchRouteResult{err: err})
			continue
		}

		req.WorkerID = selection.Worker.WorkerID
		req.BatchSize = batch.Size()
		req.BatchWaitMS = batchWait.Milliseconds()
		req.RoutingDecision = selection.Decision
		req.RoutedAt = time.Now()
		r.completeBatchRoute(req.Request.RequestID, batchRouteResult{routed: req})
	}
}

// OnBatchDispatch sets a callback invoked once per dispatched batch.
func (r *Router) OnBatchDispatch(callback func(batch *types.BatchContext)) {
	r.onBatchDispatch = callback
}

// OnWorkerHealthTransition sets a callback invoked when registry health checks
// mark workers unhealthy or remove them after missed heartbeats.
func (r *Router) OnWorkerHealthTransition(callback func(WorkerHealthTransition)) {
	r.registry.OnHealthTransition(callback)
}

// Stop shuts down the router.
func (r *Router) Stop() {
	r.cancel()
	r.batchManager.Stop()
}

func (r *Router) registerBatchWaiter(requestID string) chan batchRouteResult {
	waiter := make(chan batchRouteResult, 1)
	r.batchWaitersMu.Lock()
	r.batchWaiters[requestID] = waiter
	r.batchWaitersMu.Unlock()
	return waiter
}

func (r *Router) unregisterBatchWaiter(requestID string, waiter chan batchRouteResult) {
	r.batchWaitersMu.Lock()
	if existing, ok := r.batchWaiters[requestID]; ok && existing == waiter {
		delete(r.batchWaiters, requestID)
	}
	r.batchWaitersMu.Unlock()
}

func (r *Router) completeBatchRoute(requestID string, result batchRouteResult) {
	r.batchWaitersMu.Lock()
	waiter, ok := r.batchWaiters[requestID]
	if ok {
		delete(r.batchWaiters, requestID)
	}
	r.batchWaitersMu.Unlock()

	if !ok {
		return
	}

	waiter <- result
	close(waiter)
}

func (r *Router) validateModelAvailability(request *types.InferenceRequest) error {
	allWorkers := r.registry.GetWorkersForModel(request.ModelID)
	if len(allWorkers) == 0 {
		return types.NewInferaError(
			types.ErrorCodeModelNotFound,
			fmt.Sprintf("model %s not found", request.ModelID),
		).WithRequestID(request.RequestID)
	}

	if len(r.registry.GetHealthyWorkersForModel(request.ModelID)) == 0 {
		return types.NewInferaError(
			types.ErrorCodeModelOverloaded,
			fmt.Sprintf("all workers for model %s at capacity", request.ModelID),
		).WithRequestID(request.RequestID)
	}

	return nil
}

func (r *Router) selectWorker(request *types.InferenceRequest) (*strategy.Selection, error) {
	if err := r.validateModelAvailability(request); err != nil {
		return nil, err
	}

	if selection, ok := r.selectAffinityWorker(request); ok {
		return selection, nil
	}

	candidates := r.registry.GetHealthyWorkersForModel(request.ModelID)
	selection, err := r.strategyEngine.SelectWorker(request, candidates)
	if err != nil {
		return nil, types.NewInferaError(
			types.ErrorCodeInternalError,
			fmt.Sprintf("failed to select worker: %v", err),
		).WithRequestID(request.RequestID)
	}
	r.rememberAffinity(request, selection.Worker.WorkerID)
	return selection, nil
}

func (r *Router) selectWorkerForModel(modelID string) (*strategy.Selection, error) {
	req := &types.InferenceRequest{
		RequestID: uuid.New().String(),
		ModelID:   modelID,
		Priority:  types.PriorityNormal,
	}
	return r.selectWorker(req)
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

func (r *Router) affinityKey(request *types.InferenceRequest) string {
	if request == nil || request.Metadata == nil {
		return ""
	}
	return request.Metadata[types.MetadataAffinityKey]
}

func (r *Router) selectAffinityWorker(request *types.InferenceRequest) (*strategy.Selection, bool) {
	key := r.affinityKey(request)
	if key == "" || r.config.AffinityTTL <= 0 {
		return nil, false
	}

	r.affinityMu.RLock()
	binding, ok := r.affinity[key]
	r.affinityMu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Now().After(binding.ExpiresAt) {
		r.clearAffinity(key)
		return nil, false
	}

	worker, ok := r.registry.Get(binding.WorkerID)
	if !ok || !worker.IsHealthy() || !worker.HasCapacity() || !worker.HasModel(request.ModelID) {
		r.clearAffinity(key)
		return nil, false
	}

	score := 1.0 - worker.CurrentLoad()
	return &strategy.Selection{
		Worker: worker,
		Score:  score,
		Decision: types.RoutingDecision{
			Strategy:            types.StrategyAffinity,
			Reason:              "selected sticky worker for affinity key",
			CandidatesEvaluated: 1,
			SelectedWorkerScore: score,
		},
	}, true
}

func (r *Router) rememberAffinity(request *types.InferenceRequest, workerID string) {
	key := r.affinityKey(request)
	if key == "" || workerID == "" || r.config.AffinityTTL <= 0 {
		return
	}

	r.affinityMu.Lock()
	r.affinity[key] = affinityBinding{
		WorkerID:  workerID,
		ExpiresAt: time.Now().Add(r.config.AffinityTTL),
	}
	r.affinityMu.Unlock()
}

func (r *Router) clearAffinityForFailure(request *types.RoutedRequest) {
	if request == nil || request.Request == nil {
		return
	}
	key := r.affinityKey(request.Request)
	if key == "" {
		return
	}

	r.affinityMu.Lock()
	binding, ok := r.affinity[key]
	if ok && binding.WorkerID == request.WorkerID {
		delete(r.affinity, key)
	}
	r.affinityMu.Unlock()
}

func (r *Router) clearAffinity(key string) {
	if key == "" {
		return
	}
	r.affinityMu.Lock()
	delete(r.affinity, key)
	r.affinityMu.Unlock()
}
