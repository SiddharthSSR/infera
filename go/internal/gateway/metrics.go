package gateway

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type GatewayMetrics struct {
	registry *prometheus.Registry

	httpRequests *prometheus.CounterVec
	httpDuration *prometheus.HistogramVec
	httpInFlight prometheus.Gauge

	inferenceRequests *prometheus.CounterVec
	inferenceDuration *prometheus.HistogramVec
	inferenceTokens   prometheus.Counter
}

func NewGatewayMetrics() *GatewayMetrics {
	registry := prometheus.NewRegistry()

	m := &GatewayMetrics{
		registry: registry,
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "infera_gateway_http_requests_total",
			Help: "Total number of HTTP requests handled by the gateway.",
		}, []string{"method", "path", "status"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "infera_gateway_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "path", "status"}),
		httpInFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "infera_gateway_http_in_flight_requests",
			Help: "Current number of in-flight HTTP requests.",
		}),
		inferenceRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "infera_gateway_inference_requests_total",
			Help: "Total number of inference requests.",
		}, []string{"stream", "status"}),
		inferenceDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "infera_gateway_inference_duration_seconds",
			Help:    "Inference request duration in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"stream", "status"}),
		inferenceTokens: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "infera_gateway_inference_tokens_total",
			Help: "Total number of tokens generated/used by inference requests.",
		}),
	}

	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.httpRequests,
		m.httpDuration,
		m.httpInFlight,
		m.inferenceRequests,
		m.inferenceDuration,
		m.inferenceTokens,
	)

	return m
}

func (m *GatewayMetrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *GatewayMetrics) HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		path := normalizeMetricPath(r.URL.Path)

		m.httpInFlight.Inc()
		defer m.httpInFlight.Dec()

		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)

		status := strconv.Itoa(rec.Status())
		duration := time.Since(start).Seconds()
		method := strings.ToUpper(r.Method)

		m.httpRequests.WithLabelValues(method, path, status).Inc()
		m.httpDuration.WithLabelValues(method, path, status).Observe(duration)
	})
}

func (m *GatewayMetrics) RecordInference(stream bool, status string, tokenCount int, duration time.Duration) {
	streamLabel := strconv.FormatBool(stream)
	statusLabel := strings.TrimSpace(status)
	if statusLabel == "" {
		statusLabel = "unknown"
	}
	m.inferenceRequests.WithLabelValues(streamLabel, statusLabel).Inc()
	m.inferenceDuration.WithLabelValues(streamLabel, statusLabel).Observe(duration.Seconds())
	if tokenCount > 0 {
		m.inferenceTokens.Add(float64(tokenCount))
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(p)
}

func (w *statusRecorder) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func normalizeMetricPath(path string) string {
	switch {
	case path == "/v1/chat/completions":
		return "/v1/chat/completions"
	case path == "/v1/models":
		return "/v1/models"
	case path == "/api/workers":
		return "/api/workers"
	case path == "/api/workers/register":
		return "/api/workers/register"
	case path == "/api/workers/heartbeat":
		return "/api/workers/heartbeat"
	case path == "/api/stats":
		return "/api/stats"
	case path == "/api/health":
		return "/api/health"
	case path == "/health":
		return "/health"
	case path == "/api/vault/models":
		return "/api/vault/models"
	case path == "/api/vault/models/families":
		return "/api/vault/models/families"
	case strings.HasPrefix(path, "/api/vault/models/"):
		return "/api/vault/models/:id"
	case path == "/api/vault/stats":
		return "/api/vault/stats"
	case strings.HasPrefix(path, "/api/instances"):
		return "/api/instances"
	case strings.HasPrefix(path, "/api/auth/"):
		return "/api/auth/*"
	case path == "/api/audit/usage":
		return "/api/audit/usage"
	case path == "/metrics":
		return "/metrics"
	default:
		return "/unknown"
	}
}
