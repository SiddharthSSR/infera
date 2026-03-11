package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
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
		"infera_gateway_inference_requests_total",
		"infera_gateway_inference_duration_seconds",
		"infera_gateway_inference_tokens_total",
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
