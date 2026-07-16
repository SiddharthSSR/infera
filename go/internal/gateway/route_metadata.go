package gateway

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/infera/infera/go/pkg/types"
)

func (g *Gateway) logRouteDecision(decision types.RoutingDecision) {
	if g.metrics != nil {
		g.metrics.RecordRouteDecision(string(decision.Strategy), "success", decision.CandidatesEvaluated)
	}
	attrs := []any{
		slog.String("request_id", decision.RequestID), slog.String("model", decision.Model),
		slog.String("strategy", string(decision.Strategy)), slog.String("selected_worker", decision.SelectedWorker),
		slog.Int("candidates_evaluated", decision.CandidatesEvaluated), slog.String("reason", decision.Reason),
		slog.Float64("selected_worker_score", decision.SelectedWorkerScore),
	}
	if decision.SelectedProvider != "" {
		attrs = append(attrs, slog.String("selected_provider", decision.SelectedProvider))
	}
	if decision.SelectedGPUType != "" {
		attrs = append(attrs, slog.String("selected_gpu_type", decision.SelectedGPUType))
	}
	if decision.WorkerQueueDepth != nil {
		attrs = append(attrs, slog.Int("worker_queue_depth", *decision.WorkerQueueDepth))
	}
	if decision.WorkerActiveRequests != nil {
		attrs = append(attrs, slog.Int("worker_active_requests", *decision.WorkerActiveRequests))
	}
	if decision.WorkerP50LatencyMS != nil {
		attrs = append(attrs, slog.Float64("worker_p50_latency_ms", *decision.WorkerP50LatencyMS))
	}
	if decision.WorkerP95LatencyMS != nil {
		attrs = append(attrs, slog.Float64("worker_p95_latency_ms", *decision.WorkerP95LatencyMS))
	}
	if decision.WorkerP99LatencyMS != nil {
		attrs = append(attrs, slog.Float64("worker_p99_latency_ms", *decision.WorkerP99LatencyMS))
	}
	if decision.WorkerLoad != nil {
		attrs = append(attrs, slog.Float64("worker_load", *decision.WorkerLoad))
	}
	if !decision.DecisionTimestamp.IsZero() {
		attrs = append(attrs, slog.Time("decision_timestamp", decision.DecisionTimestamp))
	}
	g.log.Info("route_decision", attrs...)
}

func (g *Gateway) logRouteDecisionFailed(req *types.InferenceRequest, errorCode, reason string) {
	model, requestID, healthyWorkers := "", "", 0
	if req != nil {
		model, requestID = req.ModelID, req.RequestID
	}
	if g.router != nil {
		healthyWorkers = len(g.router.GetWorkers("", true))
	}
	if g.metrics != nil {
		g.metrics.RecordRouteDecision("", "failure", -1)
	}
	g.log.Warn("route_decision_failed",
		slog.String("request_id", requestID),
		slog.String("model", model),
		slog.String("error_code", strings.TrimSpace(errorCode)),
		slog.String("reason", strings.TrimSpace(reason)),
		slog.Int("healthy_workers", healthyWorkers),
	)
}

const (
	headerDebugRouteDecision = "X-Infera-Debug-Route"
	headerRouteDecision      = "X-Infera-Route-Decision"
)

type safeRouteDecisionMetadata struct {
	RequestID            string             `json:"request_id,omitempty"`
	Model                string             `json:"model,omitempty"`
	Strategy             types.StrategyType `json:"strategy,omitempty"`
	SelectedWorker       string             `json:"selected_worker,omitempty"`
	SelectedProvider     string             `json:"selected_provider,omitempty"`
	SelectedGPUType      string             `json:"selected_gpu_type,omitempty"`
	Reason               string             `json:"reason,omitempty"`
	CandidatesEvaluated  *int               `json:"candidates_evaluated,omitempty"`
	WorkerQueueDepth     *int               `json:"worker_queue_depth,omitempty"`
	WorkerActiveRequests *int               `json:"worker_active_requests,omitempty"`
	WorkerP50LatencyMS   *float64           `json:"worker_p50_latency_ms,omitempty"`
	WorkerP95LatencyMS   *float64           `json:"worker_p95_latency_ms,omitempty"`
	WorkerP99LatencyMS   *float64           `json:"worker_p99_latency_ms,omitempty"`
	WorkerLoad           *float64           `json:"worker_load,omitempty"`
	DecisionTimestamp    *time.Time         `json:"decision_timestamp,omitempty"`
}

func routeDecisionMetadataRequested(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.Header.Get(headerDebugRouteDecision)), "true")
}

func setRouteDecisionHeader(w http.ResponseWriter, r *http.Request, decision types.RoutingDecision) {
	if !routeDecisionMetadataRequested(r) {
		return
	}
	candidatesEvaluated := decision.CandidatesEvaluated
	metadata := safeRouteDecisionMetadata{
		RequestID:            decision.RequestID,
		Model:                decision.Model,
		Strategy:             decision.Strategy,
		SelectedWorker:       decision.SelectedWorker,
		SelectedProvider:     decision.SelectedProvider,
		SelectedGPUType:      decision.SelectedGPUType,
		Reason:               decision.Reason,
		CandidatesEvaluated:  &candidatesEvaluated,
		WorkerQueueDepth:     decision.WorkerQueueDepth,
		WorkerActiveRequests: decision.WorkerActiveRequests,
		WorkerP50LatencyMS:   decision.WorkerP50LatencyMS,
		WorkerP95LatencyMS:   decision.WorkerP95LatencyMS,
		WorkerP99LatencyMS:   decision.WorkerP99LatencyMS,
		WorkerLoad:           decision.WorkerLoad,
	}
	if !decision.DecisionTimestamp.IsZero() {
		ts := decision.DecisionTimestamp.UTC()
		metadata.DecisionTimestamp = &ts
	}
	setEncodedRouteDecisionHeader(w, metadata)
}

func setFailedRouteDecisionHeader(w http.ResponseWriter, r *http.Request, req *types.InferenceRequest, reason string) {
	if !routeDecisionMetadataRequested(r) {
		return
	}
	metadata := safeRouteDecisionMetadata{
		Reason: strings.TrimSpace(reason),
	}
	candidatesEvaluated := 0
	metadata.CandidatesEvaluated = &candidatesEvaluated
	if req != nil {
		metadata.RequestID = req.RequestID
		metadata.Model = req.ModelID
	}
	setEncodedRouteDecisionHeader(w, metadata)
}

func setEncodedRouteDecisionHeader(w http.ResponseWriter, metadata safeRouteDecisionMetadata) {
	data, err := json.Marshal(metadata)
	if err != nil {
		return
	}
	w.Header().Set(headerRouteDecision, base64.RawURLEncoding.EncodeToString(data))
}
