package types

import "time"

// StrategyType identifies the routing algorithm used.
type StrategyType string

const (
	StrategyLeastLoaded            StrategyType = "least_loaded"
	StrategyRoundRobin             StrategyType = "round_robin"
	StrategyLatencyBased           StrategyType = "latency_based"
	StrategyMinCostUnderLatencySLO StrategyType = "min_cost_under_latency_slo"
	StrategyAffinity               StrategyType = "affinity"
)

// RoutingDecision captures why a worker was selected.
type RoutingDecision struct {
	RequestID                 string       `json:"request_id,omitempty"`
	Model                     string       `json:"model,omitempty"`
	Strategy                  StrategyType `json:"strategy"`
	SelectedWorker            string       `json:"selected_worker,omitempty"`
	SelectedProvider          string       `json:"selected_provider,omitempty"`
	SelectedGPUType           string       `json:"selected_gpu_type,omitempty"`
	Reason                    string       `json:"reason"`
	CandidatesEvaluated       int          `json:"candidates_evaluated"`
	WorkerQueueDepth          *int         `json:"worker_queue_depth,omitempty"`
	WorkerActiveRequests      *int         `json:"worker_active_requests,omitempty"`
	WorkerP50LatencyMS        *float64     `json:"worker_p50_latency_ms,omitempty"`
	WorkerP95LatencyMS        *float64     `json:"worker_p95_latency_ms,omitempty"`
	WorkerP99LatencyMS        *float64     `json:"worker_p99_latency_ms,omitempty"`
	WorkerLoad                *float64     `json:"worker_load,omitempty"`
	DecisionTimestamp         time.Time    `json:"decision_timestamp,omitempty"`
	SelectedWorkerScore       float64      `json:"selected_worker_score"`
	LatencySLOMS              *float64     `json:"latency_slo_ms,omitempty"`
	SelectedCostNanoPerHour   *int64       `json:"selected_cost_nano_per_hour,omitempty"`
	CostSLOEligibleCandidates *int         `json:"cost_slo_eligible_candidates,omitempty"`
	FallbackReason            string       `json:"fallback_reason,omitempty"`
}

// RoutedRequest wraps an InferenceRequest with routing metadata.
type RoutedRequest struct {
	Request         *InferenceRequest `json:"request"`
	WorkerID        string            `json:"worker_id"`
	BatchID         string            `json:"batch_id,omitempty"`
	BatchSize       int               `json:"batch_size,omitempty"`
	BatchWaitMS     int64             `json:"batch_wait_ms,omitempty"`
	RoutingDecision RoutingDecision   `json:"routing_decision"`
	RoutedAt        time.Time         `json:"routed_at"`
	Attempt         int               `json:"attempt"`
	MaxAttempts     int               `json:"max_attempts"`
	Deadline        time.Time         `json:"deadline"`
}

// IsBatched returns true if this request is part of a batch.
func (r *RoutedRequest) IsBatched() bool {
	return r.BatchID != ""
}

// IsRetriable returns true if the request can be retried.
func (r *RoutedRequest) IsRetriable() bool {
	return r.Attempt < r.MaxAttempts
}

// IsExpired returns true if the request has exceeded its deadline.
func (r *RoutedRequest) IsExpired() bool {
	return time.Now().After(r.Deadline)
}

// WithRetry creates a new RoutedRequest with incremented attempt.
func (r *RoutedRequest) WithRetry() *RoutedRequest {
	return &RoutedRequest{
		Request:     r.Request,
		WorkerID:    r.WorkerID,
		BatchID:     r.BatchID,
		BatchSize:   r.BatchSize,
		BatchWaitMS: r.BatchWaitMS,
		Attempt:     r.Attempt + 1,
		MaxAttempts: r.MaxAttempts,
		Deadline:    r.Deadline,
	}
}

// BatchContext groups requests for batched inference.
type BatchContext struct {
	BatchID   string           `json:"batch_id"`
	Requests  []*RoutedRequest `json:"requests"`
	ModelID   string           `json:"model_id"`
	CreatedAt time.Time        `json:"created_at"`
	SealedAt  *time.Time       `json:"sealed_at,omitempty"`
	MaxSize   int              `json:"max_size"`
	MaxWaitMS int              `json:"max_wait_ms"`
}

// Add adds a request to the batch if there's room.
func (b *BatchContext) Add(req *RoutedRequest) bool {
	if b.IsFull() || b.IsSealed() {
		return false
	}
	b.Requests = append(b.Requests, req)
	return true
}

// IsFull returns true if the batch is at capacity.
func (b *BatchContext) IsFull() bool {
	return len(b.Requests) >= b.MaxSize
}

// IsExpired returns true if the batch has waited too long.
func (b *BatchContext) IsExpired() bool {
	maxWait := time.Duration(b.MaxWaitMS) * time.Millisecond
	return time.Since(b.CreatedAt) > maxWait
}

// IsSealed returns true if the batch is closed for new requests.
func (b *BatchContext) IsSealed() bool {
	return b.SealedAt != nil
}

// Seal closes the batch for new requests.
func (b *BatchContext) Seal() {
	now := time.Now()
	b.SealedAt = &now
}

// Size returns the number of requests in the batch.
func (b *BatchContext) Size() int {
	return len(b.Requests)
}

// RequestState represents the lifecycle state of a request.
type RequestState string

const (
	RequestStateReceived   RequestState = "received"
	RequestStateQueued     RequestState = "queued"
	RequestStateBatched    RequestState = "batched"
	RequestStateProcessing RequestState = "processing"
	RequestStateCompleted  RequestState = "completed"
	RequestStateFailed     RequestState = "failed"
)

// RequestTracker tracks the lifecycle of a request.
type RequestTracker struct {
	RequestID     string       `json:"request_id"`
	State         RequestState `json:"state"`
	CurrentWorker string       `json:"current_worker,omitempty"`
	CurrentBatch  string       `json:"current_batch,omitempty"`
}

// Transition moves the request to a new state.
func (t *RequestTracker) Transition(newState RequestState, reason string) {
	t.State = newState
}
