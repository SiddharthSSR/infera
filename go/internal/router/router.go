package router

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	registry        workerRegistry
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

// workerRegistry defines the worker-state surface the router needs.
// The default implementation remains the in-memory registry, but this
// interface lets future shared-state implementations plug in without
// changing routing logic.
type workerRegistry interface {
	Register(ctx context.Context, worker *types.WorkerInfo) error
	Deregister(ctx context.Context, workerID string) error
	UpdateWorkerStats(ctx context.Context, workerID string, stats types.WorkerStats) error
	UpdateWorkerModels(ctx context.Context, workerID string, models []types.LoadedModel) error
	Snapshot(ctx context.Context) ([]*types.WorkerInfo, error)
	StartHealthChecker(ctx context.Context)
}

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
	return NewWithRegistry(config, registry.NewWorkerRegistry(registry.DefaultRegistryConfig()))
}

// NewWithRegistry creates a router with an explicit worker registry.
func NewWithRegistry(config Config, workerState workerRegistry) *Router {
	ctx, cancel := context.WithCancel(context.Background())
	if workerState == nil {
		workerState = registry.NewWorkerRegistry(registry.DefaultRegistryConfig())
	}

	r := &Router{
		config:         config,
		registry:       workerState,
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

	workers, err := r.registry.Snapshot(ctx)
	if err != nil {
		return nil, classifyRegistrySnapshotError(err, request.RequestID)
	}

	if r.batchManager.ShouldBatch(request) {
		if err := r.validateModelAvailability(request, workers); err != nil {
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

	selection, err := r.selectWorker(request, workers)
	if err != nil {
		return nil, err
	}

	routed.WorkerID = selection.Worker.WorkerID
	routed.RoutingDecision = r.enrichRoutingDecision(request, selection)
	tracker.CurrentWorker = selection.Worker.WorkerID
	return routed, nil
}

// HandleFailure processes a failed request, potentially retrying.
func (r *Router) HandleFailure(ctx context.Context, request *types.RoutedRequest, err error) (*types.RoutedRequest, error) {
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

	workers, registryErr := r.registry.Snapshot(ctx)
	if registryErr != nil {
		return nil, classifyRegistrySnapshotError(registryErr, request.Request.RequestID)
	}
	candidates := healthyWorkersForModel(workersForWorkspace(workers, request.Request.WorkspaceID), request.Request.ModelID)
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
	retry.RoutingDecision = r.enrichRoutingDecision(request.Request, selection)
	retry.RoutedAt = time.Now()

	return retry, nil
}

// RegisterWorker registers a worker with the router.
func (r *Router) RegisterWorker(ctx context.Context, worker *types.WorkerInfo) error {
	return r.registry.Register(ctx, worker)
}

// DeregisterWorker removes a worker from the router.
func (r *Router) DeregisterWorker(ctx context.Context, workerID string) error {
	return r.registry.Deregister(ctx, workerID)
}

// UpdateWorkerStats updates stats for a worker.
func (r *Router) UpdateWorkerStats(ctx context.Context, workerID string, stats types.WorkerStats) error {
	return r.registry.UpdateWorkerStats(ctx, workerID, stats)
}

// UpdateWorkerModels updates loaded models for a worker.
func (r *Router) UpdateWorkerModels(ctx context.Context, workerID string, models []types.LoadedModel) error {
	return r.registry.UpdateWorkerModels(ctx, workerID, models)
}

// GetWorker returns a worker by ID.
func (r *Router) GetWorker(ctx context.Context, workerID string) (*types.WorkerInfo, bool, error) {
	workers, err := r.registry.Snapshot(ctx)
	if err != nil {
		return nil, false, err
	}
	for _, worker := range workers {
		if worker.WorkerID == workerID {
			return worker, true, nil
		}
	}
	return nil, false, nil
}

// GetWorkers returns all workers, optionally filtered.
func (r *Router) GetWorkers(ctx context.Context, modelID string, healthyOnly bool) ([]*types.WorkerInfo, error) {
	workers, err := r.registry.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	return filterWorkers(workers, modelID, healthyOnly), nil
}

// WorkerCount returns the number of registered workers.
func (r *Router) WorkerCount(ctx context.Context) (int, error) {
	workers, err := r.registry.Snapshot(ctx)
	return len(workers), err
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
	requestTimeout := time.Duration(r.config.RequestTimeoutMS) * time.Millisecond
	if requestTimeout <= 0 {
		requestTimeout = 30 * time.Second
	}
	dispatchCtx, cancel := context.WithTimeout(r.ctx, requestTimeout)
	selection, err := r.selectWorkerForModel(dispatchCtx, batch.ModelID)
	cancel()
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
		req.RoutingDecision = r.enrichRoutingDecision(req.Request, selection)
		req.RoutedAt = time.Now()
		r.completeBatchRoute(req.Request.RequestID, batchRouteResult{routed: req})
	}
}

// OnBatchDispatch sets a callback invoked once per dispatched batch.
func (r *Router) OnBatchDispatch(callback func(batch *types.BatchContext)) {
	r.onBatchDispatch = callback
}

// OnWorkerHealthTransition sets a callback for registry health transitions.
func (r *Router) OnWorkerHealthTransition(callback func(WorkerHealthTransition)) {
	if source, ok := r.registry.(interface {
		OnHealthTransition(func(registry.HealthTransition))
	}); ok {
		source.OnHealthTransition(callback)
	}
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

func (r *Router) validateModelAvailability(request *types.InferenceRequest, snapshot []*types.WorkerInfo) error {
	workers := workersForWorkspace(snapshot, request.WorkspaceID)
	if len(filterWorkers(workers, "", true)) == 0 {
		return types.NewInferaError(
			types.ErrorCodeNoWorkersAvailable,
			"No healthy workers are currently available to serve the requested model.",
		).WithRequestID(request.RequestID)
	}

	allWorkers := filterWorkers(workers, request.ModelID, false)
	if len(allWorkers) == 0 {
		return types.NewInferaError(
			types.ErrorCodeModelNotFound,
			fmt.Sprintf("model %s not found", request.ModelID),
		).WithRequestID(request.RequestID)
	}

	if len(filterWorkers(workers, request.ModelID, true)) == 0 {
		return types.NewInferaError(
			types.ErrorCodeModelOverloaded,
			fmt.Sprintf("all workers for model %s at capacity", request.ModelID),
		).WithRequestID(request.RequestID)
	}

	return nil
}

func (r *Router) selectWorker(request *types.InferenceRequest, snapshot []*types.WorkerInfo) (*strategy.Selection, error) {
	if err := r.validateModelAvailability(request, snapshot); err != nil {
		return nil, err
	}

	if selection, ok := r.selectAffinityWorker(request, snapshot); ok {
		return selection, nil
	}

	candidates := filterWorkers(workersForWorkspace(snapshot, request.WorkspaceID), request.ModelID, true)
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

func (r *Router) selectWorkerForModel(ctx context.Context, modelID string) (*strategy.Selection, error) {
	req := &types.InferenceRequest{
		RequestID: uuid.New().String(),
		ModelID:   modelID,
		Priority:  types.PriorityNormal,
	}
	workers, err := r.registry.Snapshot(ctx)
	if err != nil {
		return nil, classifyRegistrySnapshotError(err, req.RequestID)
	}
	return r.selectWorker(req, workers)
}

func (r *Router) enrichRoutingDecision(request *types.InferenceRequest, selection *strategy.Selection) types.RoutingDecision {
	decision := selection.Decision
	if request != nil {
		decision.RequestID = request.RequestID
		decision.Model = request.ModelID
	}
	decision.DecisionTimestamp = time.Now().UTC()

	if selection.Worker == nil {
		return decision
	}

	worker := selection.Worker.Clone()
	decision.SelectedWorker = worker.WorkerID
	decision.SelectedProvider = strings.TrimSpace(worker.Tags["provider"])
	decision.SelectedGPUType = firstNonEmptyTag(worker.Tags, "gpu_type", "gpu", "provider_gpu_type")

	queueDepth := worker.Stats.QueueDepth
	activeRequests := worker.Stats.ActiveRequests
	p50Latency := worker.Stats.P50LatencyMS
	p99Latency := worker.Stats.P99LatencyMS
	load := worker.Stats.CurrentLoad()

	decision.WorkerQueueDepth = &queueDepth
	decision.WorkerActiveRequests = &activeRequests
	if p50Latency > 0 {
		decision.WorkerP50LatencyMS = &p50Latency
	}
	if p99Latency > 0 {
		decision.WorkerP99LatencyMS = &p99Latency
	}
	decision.WorkerLoad = &load
	return decision
}

func firstNonEmptyTag(tags map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(tags[key]); value != "" {
			return value
		}
	}
	return ""
}

// Stats contains router statistics.
type Stats struct {
	TotalWorkers    int
	HealthyWorkers  int
	TotalQueueDepth int
	ModelsAvailable int
}

// GetStats returns current router statistics.
func (r *Router) GetStats(ctx context.Context) (Stats, error) {
	allWorkers, err := r.registry.Snapshot(ctx)
	if err != nil {
		return Stats{}, err
	}
	healthyWorkers := filterWorkers(allWorkers, "", true)

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
	}, nil
}

func (r *Router) affinityKey(request *types.InferenceRequest) string {
	if request == nil || request.Metadata == nil {
		return ""
	}
	return request.Metadata[types.MetadataAffinityKey]
}

func (r *Router) selectAffinityWorker(request *types.InferenceRequest, snapshot []*types.WorkerInfo) (*strategy.Selection, bool) {
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

	worker, ok := workerByID(snapshot, binding.WorkerID)
	if !ok || len(workersForWorkspace([]*types.WorkerInfo{worker}, request.WorkspaceID)) != 1 || !worker.IsHealthy() || !worker.HasCapacity() || !worker.HasModel(request.ModelID) {
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

func workerRegistryUnavailable(requestID string) error {
	return types.NewInferaError(
		types.ErrorCodeWorkerRegistryUnavailable,
		"Worker registry is temporarily unavailable.",
	).WithRequestID(requestID)
}

func classifyRegistrySnapshotError(err error, requestID string) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return workerRegistryUnavailable(requestID)
}

func workerByID(workers []*types.WorkerInfo, workerID string) (*types.WorkerInfo, bool) {
	for _, worker := range workers {
		if worker.WorkerID == workerID {
			return worker, true
		}
	}
	return nil, false
}

func healthyWorkersForModel(workers []*types.WorkerInfo, modelID string) []*types.WorkerInfo {
	return filterWorkers(workers, modelID, true)
}

func filterWorkers(workers []*types.WorkerInfo, modelID string, healthyOnly bool) []*types.WorkerInfo {
	filtered := make([]*types.WorkerInfo, 0, len(workers))
	for _, worker := range workers {
		if modelID != "" && !worker.HasModel(modelID) {
			continue
		}
		if healthyOnly && (!worker.IsHealthy() || (modelID != "" && !worker.HasCapacity())) {
			continue
		}
		filtered = append(filtered, worker)
	}
	return filtered
}

func workersForWorkspace(workers []*types.WorkerInfo, workspaceID string) []*types.WorkerInfo {
	if strings.TrimSpace(workspaceID) == "" {
		workspaceID = "ws_default"
	}
	filtered := make([]*types.WorkerInfo, 0, len(workers))
	for _, worker := range workers {
		workerWorkspaceID := strings.TrimSpace(worker.WorkspaceID)
		if workerWorkspaceID == "" {
			workerWorkspaceID = "ws_default"
		}
		if worker.SharedPool || workerWorkspaceID == workspaceID {
			filtered = append(filtered, worker)
		}
	}
	return filtered
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
