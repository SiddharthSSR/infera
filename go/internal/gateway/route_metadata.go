package gateway

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/infera/infera/go/pkg/types"
)

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
