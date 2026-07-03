package gateway

import (
	"bufio"
	"net"
	"net/http"
	"os"
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
	gatewayInfo  *prometheus.GaugeVec

	inferenceRequests *prometheus.CounterVec
	inferenceDuration *prometheus.HistogramVec
	inferenceTTFT     *prometheus.HistogramVec
	inferenceTPOT     *prometheus.HistogramVec
	batchSize         *prometheus.HistogramVec
	batchWait         *prometheus.HistogramVec
	inferenceTokens   prometheus.Counter
	inferenceRejected *prometheus.CounterVec

	workerQueueDepth        *prometheus.GaugeVec
	workersTotal            prometheus.Gauge
	healthyWorkersTotal     prometheus.Gauge
	unhealthyWorkersTotal   prometheus.Gauge
	workerHealthTransitions *prometheus.CounterVec
}

var inferenceDurationBuckets = []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 20, 30, 60}
var inferenceTTFTBuckets = []float64{0.05, 0.1, 0.2, 0.5, 1, 2, 5, 10}
var inferenceTPOTBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5}
var batchSizeBuckets = []float64{1, 2, 4, 8, 16, 32}
var batchWaitBuckets = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1}

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
		gatewayInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "infera_gateway_info",
			Help: "Static gateway deployment metadata.",
		}, []string{"service", "env", "version"}),
		inferenceRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "infera_gateway_inference_requests_total",
			Help: "Total number of inference requests.",
		}, []string{"stream", "status"}),
		inferenceDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "infera_gateway_inference_duration_seconds",
			Help:    "Inference request duration in seconds.",
			Buckets: inferenceDurationBuckets,
		}, []string{"stream", "status"}),
		inferenceTTFT: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "infera_gateway_inference_ttft_seconds",
			Help:    "Time to first token for inference requests.",
			Buckets: inferenceTTFTBuckets,
		}, []string{"model", "stream"}),
		inferenceTPOT: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "infera_gateway_inference_tpot_seconds",
			Help:    "Time per output token after the first token.",
			Buckets: inferenceTPOTBuckets,
		}, []string{"model", "stream"}),
		batchSize: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "infera_gateway_batch_size",
			Help:    "Observed dispatched batch size for batched requests.",
			Buckets: batchSizeBuckets,
		}, []string{"model"}),
		batchWait: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "infera_gateway_batch_wait_seconds",
			Help:    "Time requests waited in the batch queue before dispatch.",
			Buckets: batchWaitBuckets,
		}, []string{"model"}),
		inferenceTokens: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "infera_gateway_inference_tokens_total",
			Help: "Total number of tokens generated/used by inference requests.",
		}),
		inferenceRejected: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "infera_gateway_inference_rejected_total",
			Help: "Total number of inference requests rejected before worker dispatch.",
		}, []string{"reason"}),
		workerQueueDepth: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "infera_gateway_worker_queue_depth",
			Help: "Current request queue depth per worker, as reported by the last heartbeat.",
		}, []string{"worker_id"}),
		workersTotal: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "infera_workers_total",
			Help: "Current number of workers registered with the gateway.",
		}),
		healthyWorkersTotal: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "infera_healthy_workers_total",
			Help: "Current number of healthy workers registered with the gateway.",
		}),
		unhealthyWorkersTotal: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "infera_unhealthy_workers_total",
			Help: "Current number of registered workers that are not healthy.",
		}),
		workerHealthTransitions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "infera_gateway_worker_health_transitions_total",
			Help: "Total number of registry-driven worker health transitions.",
		}, []string{"event", "from_status", "to_status"}),
	}

	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.httpRequests,
		m.httpDuration,
		m.httpInFlight,
		m.gatewayInfo,
		m.inferenceRequests,
		m.inferenceDuration,
		m.inferenceTTFT,
		m.inferenceTPOT,
		m.batchSize,
		m.batchWait,
		m.inferenceTokens,
		m.inferenceRejected,
		m.workerQueueDepth,
		m.workersTotal,
		m.healthyWorkersTotal,
		m.unhealthyWorkersTotal,
		m.workerHealthTransitions,
	)
	m.gatewayInfo.WithLabelValues("gateway", inferaEnv(), inferaVersion()).Set(1)

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

func (m *GatewayMetrics) RecordTTFT(model string, stream bool, duration time.Duration) {
	if duration <= 0 {
		return
	}
	m.inferenceTTFT.WithLabelValues(strings.TrimSpace(model), strconv.FormatBool(stream)).Observe(duration.Seconds())
}

func (m *GatewayMetrics) RecordTPOT(model string, stream bool, duration time.Duration) {
	if duration <= 0 {
		return
	}
	m.inferenceTPOT.WithLabelValues(strings.TrimSpace(model), strconv.FormatBool(stream)).Observe(duration.Seconds())
}

func (m *GatewayMetrics) RecordInferenceRejected(reason string) {
	reasonLabel := strings.TrimSpace(reason)
	if reasonLabel == "" {
		reasonLabel = "unknown"
	}
	m.inferenceRejected.WithLabelValues(reasonLabel).Inc()
}

func (m *GatewayMetrics) RecordWorkerQueueDepth(workerID string, depth int) {
	m.workerQueueDepth.WithLabelValues(strings.TrimSpace(workerID)).Set(float64(depth))
}

func (m *GatewayMetrics) RecordWorkerCounts(total int, healthy int) {
	if total < 0 {
		total = 0
	}
	if healthy < 0 {
		healthy = 0
	}
	if healthy > total {
		healthy = total
	}
	m.workersTotal.Set(float64(total))
	m.healthyWorkersTotal.Set(float64(healthy))
	m.unhealthyWorkersTotal.Set(float64(total - healthy))
}

func (m *GatewayMetrics) RecordWorkerHealthTransition(event string, fromStatus string, toStatus string) {
	m.workerHealthTransitions.WithLabelValues(
		strings.TrimSpace(event),
		strings.TrimSpace(fromStatus),
		strings.TrimSpace(toStatus),
	).Inc()
}

func (m *GatewayMetrics) RecordBatch(model string, size int, wait time.Duration) {
	if size > 0 {
		m.batchSize.WithLabelValues(strings.TrimSpace(model)).Observe(float64(size))
	}
	if wait > 0 {
		m.batchWait.WithLabelValues(strings.TrimSpace(model)).Observe(wait.Seconds())
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
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

func (w *statusRecorder) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func (w *statusRecorder) Push(target string, opts *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
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
	case path == "/internal/prometheus/worker-targets":
		return "/internal/prometheus/worker-targets"
	case path == "/metrics":
		return "/metrics"
	default:
		return "/unknown"
	}
}

func inferaEnv() string {
	if env := strings.TrimSpace(os.Getenv("INFERA_ENV")); env != "" {
		return env
	}
	return "production"
}

func inferaVersion() string {
	if version := strings.TrimSpace(os.Getenv("INFERA_VERSION")); version != "" {
		return version
	}
	return "dev"
}
