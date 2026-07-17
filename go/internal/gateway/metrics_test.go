package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
)

func TestNormalizeMetricPath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "/v1/chat/completions", want: "/v1/chat/completions"},
		{in: "/api/vault/models/families", want: "/api/vault/models/families"},
		{in: "/api/vault/models/model_123", want: "/api/vault/models/:id"},
		{in: "/api/auth/login", want: "/api/auth/*"},
		{in: "/internal/prometheus/worker-targets", want: "/internal/prometheus/worker-targets"},
		{in: "/random-path", want: "/unknown"},
	}

	for _, tc := range cases {
		got := normalizeMetricPath(tc.in)
		if got != tc.want {
			t.Fatalf("normalizeMetricPath(%q): expected %q, got %q", tc.in, tc.want, got)
		}
	}
}

func TestGatewayMetricsHTTPMiddleware(t *testing.T) {
	m := NewGatewayMetrics()
	handler := m.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/vault/models/model_1", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rec.Code)
	}

	requests := testutil.ToFloat64(m.httpRequests.WithLabelValues("GET", "/api/vault/models/:id", "204"))
	if requests != 1 {
		t.Fatalf("expected requests counter=1, got %v", requests)
	}

	inFlight := testutil.ToFloat64(m.httpInFlight)
	if inFlight != 0 {
		t.Fatalf("expected in-flight gauge to return to 0, got %v", inFlight)
	}
}

func TestGatewayMetricsRecordInference(t *testing.T) {
	m := NewGatewayMetrics()
	m.RecordInference(true, "success", 128, 150*time.Millisecond)
	m.RecordInference(false, "inference_timeout", 0, 300*time.Millisecond)
	m.RecordTTFT("model-1", "least_loaded", false, sloMeasurementDerived, 250*time.Millisecond)
	m.RecordTPOT("model-1", "least_loaded", false, sloMeasurementDerived, 25*time.Millisecond)
	m.RecordSLOMeasurement("ttft", "model-1", "least_loaded", false, sloMeasurementDerived)
	m.RecordSLOMeasurement("tpot", "model-1", "least_loaded", false, sloMeasurementDerived)
	m.RecordSLORequest("model-1", "least_loaded", false, "success", 150*time.Millisecond)
	m.RecordSLORequest("", "attacker-controlled", true, "failed", 300*time.Millisecond)
	m.RecordBatch("model-1", 4, 40*time.Millisecond)
	m.RecordInferenceRejected("overloaded")
	m.RecordRouteDecision("least_loaded", "success", 3)
	m.RecordRouteDecision("", "failure", 0)
	m.RecordWorkerCounts(3, 2)
	m.RecordWorkerHealthTransition("marked_unhealthy", "healthy", "unhealthy")

	successCount := testutil.ToFloat64(m.inferenceRequests.WithLabelValues("true", "success"))
	if successCount != 1 {
		t.Fatalf("expected success inference count=1, got %v", successCount)
	}

	timeoutCount := testutil.ToFloat64(m.inferenceRequests.WithLabelValues("false", "inference_timeout"))
	if timeoutCount != 1 {
		t.Fatalf("expected timeout inference count=1, got %v", timeoutCount)
	}

	tokens := testutil.ToFloat64(m.inferenceTokens)
	if tokens != 128 {
		t.Fatalf("expected tokens counter=128, got %v", tokens)
	}

	if got := histogramCountForLabels(t, m, "infera_gateway_inference_ttft_seconds", map[string]string{
		"model":  "model-1",
		"stream": "false",
	}); got != 1 {
		t.Fatalf("expected ttft histogram count=1, got %d", got)
	}

	if got := histogramCountForLabels(t, m, "infera_gateway_inference_tpot_seconds", map[string]string{
		"model":  "model-1",
		"stream": "false",
	}); got != 1 {
		t.Fatalf("expected tpot histogram count=1, got %d", got)
	}

	if got := histogramCountForLabels(t, m, "infera_gateway_slo_v1_ttft_seconds", map[string]string{
		"measurement":      "derived",
		"model":            "model-1",
		"routing_strategy": "least_loaded",
		"stream":           "false",
	}); got != 1 {
		t.Fatalf("expected SLO ttft histogram count=1, got %d", got)
	}

	if got := testutil.ToFloat64(m.sloRequests.WithLabelValues("unknown", "unknown", "true", "error")); got != 1 {
		t.Fatalf("expected pre-route error in bounded unknown labels, got %v", got)
	}
	if got := testutil.ToFloat64(m.sloMeasurements.WithLabelValues("ttft", "model-1", "least_loaded", "false", "derived")); got != 1 {
		t.Fatalf("expected derived TTFT availability count=1, got %v", got)
	}

	if got := histogramCountForLabels(t, m, "infera_gateway_batch_size", map[string]string{
		"model": "model-1",
	}); got != 1 {
		t.Fatalf("expected batch size histogram count=1, got %d", got)
	}

	if got := histogramCountForLabels(t, m, "infera_gateway_batch_wait_seconds", map[string]string{
		"model": "model-1",
	}); got != 1 {
		t.Fatalf("expected batch wait histogram count=1, got %d", got)
	}

	rejected := testutil.ToFloat64(m.inferenceRejected.WithLabelValues("overloaded"))
	if rejected != 1 {
		t.Fatalf("expected rejected inference count=1, got %v", rejected)
	}

	routeSuccess := testutil.ToFloat64(m.routeDecisions.WithLabelValues("least_loaded", "success"))
	if routeSuccess != 1 {
		t.Fatalf("expected route success count=1, got %v", routeSuccess)
	}

	routeFailure := testutil.ToFloat64(m.routeDecisions.WithLabelValues("unknown", "failure"))
	if routeFailure != 1 {
		t.Fatalf("expected route failure count=1, got %v", routeFailure)
	}

	if workersTotal := testutil.ToFloat64(m.workersTotal); workersTotal != 3 {
		t.Fatalf("expected workers total=3, got %v", workersTotal)
	}

	if healthyWorkersTotal := testutil.ToFloat64(m.healthyWorkersTotal); healthyWorkersTotal != 2 {
		t.Fatalf("expected healthy workers total=2, got %v", healthyWorkersTotal)
	}

	if unhealthyWorkersTotal := testutil.ToFloat64(m.unhealthyWorkersTotal); unhealthyWorkersTotal != 1 {
		t.Fatalf("expected unhealthy workers total=1, got %v", unhealthyWorkersTotal)
	}

	healthTransitions := testutil.ToFloat64(m.workerHealthTransitions.WithLabelValues("marked_unhealthy", "healthy", "unhealthy"))
	if healthTransitions != 1 {
		t.Fatalf("expected worker health transition count=1, got %v", healthTransitions)
	}
}

func TestGatewayMetricsHandler(t *testing.T) {
	m := NewGatewayMetrics()

	// Emit samples so metric families are present in exposition.
	httpHandler := m.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	httpReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	httpRec := httptest.NewRecorder()
	httpHandler.ServeHTTP(httpRec, httpReq)

	m.RecordInference(false, "success", 42, 120*time.Millisecond)
	m.RecordTTFT("model-1", "least_loaded", false, sloMeasurementDerived, 200*time.Millisecond)
	m.RecordTPOT("model-1", "least_loaded", false, sloMeasurementDerived, 20*time.Millisecond)
	m.RecordSLOMeasurement("ttft", "model-1", "least_loaded", false, sloMeasurementDerived)
	m.RecordSLOMeasurement("tpot", "model-1", "least_loaded", false, sloMeasurementDerived)
	m.RecordSLORequest("model-1", "least_loaded", false, "success", 120*time.Millisecond)
	m.RecordBatch("model-1", 2, 30*time.Millisecond)
	m.RecordInferenceRejected("overloaded")
	m.RecordRouteDecision("least_loaded", "success", 2)
	m.RecordWorkerCounts(2, 1)
	m.RecordWorkerHealthTransition("removed", "unhealthy", "offline")

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected /metrics status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	for _, metric := range []string{
		"infera_gateway_http_requests_total",
		"infera_gateway_http_request_duration_seconds",
		"infera_gateway_http_in_flight_requests",
		"infera_gateway_info",
		"infera_gateway_inference_requests_total",
		"infera_gateway_inference_duration_seconds",
		"infera_gateway_inference_ttft_seconds",
		"infera_gateway_inference_tpot_seconds",
		"infera_gateway_batch_size",
		"infera_gateway_batch_wait_seconds",
		"infera_gateway_inference_tokens_total",
		"infera_gateway_inference_rejected_total",
		"infera_gateway_slo_v1_requests_total",
		"infera_gateway_slo_v1_end_to_end_seconds",
		"infera_gateway_slo_v1_ttft_seconds",
		"infera_gateway_slo_v1_tpot_seconds",
		"infera_gateway_slo_v1_latency_measurements_total",
		"infera_route_decisions_total",
		"infera_route_candidates_evaluated",
		"infera_workers_total",
		"infera_healthy_workers_total",
		"infera_unhealthy_workers_total",
		"infera_gateway_worker_health_transitions_total",
	} {
		if !strings.Contains(body, metric) {
			t.Fatalf("expected /metrics output to contain %q", metric)
		}
	}
}

type flusherRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (r *flusherRecorder) Flush() {
	r.flushed = true
}

func TestStatusRecorderPreservesFlusher(t *testing.T) {
	m := NewGatewayMetrics()

	handler := m.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatalf("expected wrapped response writer to implement http.Flusher")
		}
		flusher.Flush()
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	rec := &flusherRecorder{ResponseRecorder: httptest.NewRecorder()}
	handler.ServeHTTP(rec, req)

	if !rec.flushed {
		t.Fatalf("expected underlying flusher to be invoked")
	}
}

func histogramCountForLabels(t *testing.T, m *GatewayMetrics, name string, wantLabels map[string]string) uint64 {
	t.Helper()

	families, err := m.registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if labelsMatch(metric, wantLabels) && metric.Histogram != nil {
				return metric.GetHistogram().GetSampleCount()
			}
		}
	}

	return 0
}

func labelsMatch(metric *dto.Metric, want map[string]string) bool {
	if len(want) == 0 {
		return true
	}

	got := make(map[string]string, len(metric.GetLabel()))
	for _, label := range metric.GetLabel() {
		got[label.GetName()] = label.GetValue()
	}

	for key, value := range want {
		if got[key] != value {
			return false
		}
	}

	return true
}
