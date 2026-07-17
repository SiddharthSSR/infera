// Package gateway provides the HTTP API for Infera.
package gateway

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/infera/infera/go/internal/agents"
	"github.com/infera/infera/go/internal/audit"
	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/internal/providers"
	"github.com/infera/infera/go/internal/router"
	"github.com/infera/infera/go/internal/router/registry"
	"github.com/infera/infera/go/internal/vault"
	"github.com/infera/infera/go/pkg/types"
)

type workerPrincipalContextKey struct{}

type workerPrincipal struct{ InstanceID, WorkspaceID string }

// Gateway is the HTTP API server.
type Gateway struct {
	router     *router.Router
	config     Config
	httpServer *http.Server
	startedAt  time.Time

	// Instance management
	instanceManager  *providers.Manager
	instanceHandlers *InstanceHandlers

	// Auth
	authHandler *auth.Handler

	// Vault (model registry)
	vaultHandler *vault.Handler

	// Audit (inference usage tracking)
	auditStore auditUsageStore
	auditCh    chan auditWriteRequest
	auditWg    sync.WaitGroup

	// Deployments (shared deployment history)
	deploymentStore deploymentHistoryStore

	// Agents runtime
	agentRuntime   *agents.Runtime
	webSearcher    WebSearcher
	visionAnalyzer VisionAnalyzer

	// Rate limiting
	rateLimiter *RateLimiter

	// Metrics
	metrics *GatewayMetrics

	// Backpressure: track in-flight inference requests
	inFlightRequests   int64
	maxInFlightDefault int64
	quotaCache         *quotaCache

	// Structured logger
	log *slog.Logger

	// Worker clients for direct inference calls
	workerClients            map[string]*WorkerClient
	workerClientsMu          sync.RWMutex
	workerCredentialResolver func(string) (string, error)
}

// Config configures the gateway.
type Config struct {
	HTTPPort                     int
	ReadTimeout                  time.Duration
	WriteTimeout                 time.Duration
	InferenceTimeout             time.Duration
	EnableCORS                   bool
	AllowedOrigins               []string
	WorkerSharedToken            string
	ReleaseID                    string
	WorkerProtocolVersion        string
	RequireMatchingWorkerRelease bool
	RequestTimeoutMS             int
	RateLimiter                  *RateLimiterConfig
	MaxInFlight                  int64
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		HTTPPort:              8080,
		ReadTimeout:           30 * time.Second,
		WriteTimeout:          300 * time.Second,
		InferenceTimeout:      120 * time.Second,
		EnableCORS:            true,
		AllowedOrigins:        []string{"*"},
		WorkerSharedToken:     "",
		ReleaseID:             "",
		WorkerProtocolVersion: "",
		RequestTimeoutMS:      30000,
	}
}

// New creates a new gateway.
func New(config Config, r *router.Router, instanceMgr *providers.Manager) *Gateway {
	rateLimiter := NewRateLimiter(DefaultRateLimiterConfig())
	if config.RateLimiter != nil {
		if config.RateLimiter.RequestsPerMinute > 0 {
			rateLimiter = NewRateLimiter(*config.RateLimiter)
		} else {
			rateLimiter = nil
		}
	}

	maxInFlight := int64(100)
	if config.MaxInFlight > 0 {
		maxInFlight = config.MaxInFlight
	}

	gw := &Gateway{
		router:             r,
		config:             config,
		instanceManager:    instanceMgr,
		rateLimiter:        rateLimiter,
		metrics:            NewGatewayMetrics(),
		maxInFlightDefault: maxInFlight,
		quotaCache:         newQuotaCache(defaultQuotaRecordCacheTTL, defaultQuotaUsageCacheTTL),
		log:                NewLogger(),
		workerClients:      make(map[string]*WorkerClient),
		startedAt:          time.Now(),
		webSearcher:        newDuckDuckGoWebSearcher(),
		visionAnalyzer:     newScreenshotAnalyzer(),
	}

	if r != nil {
		r.OnBatchDispatch(func(batch *types.BatchContext) {
			if gw.metrics == nil {
				return
			}

			wait := time.Since(batch.CreatedAt)
			if batch.SealedAt != nil {
				wait = batch.SealedAt.Sub(batch.CreatedAt)
			}
			gw.metrics.RecordBatch(batch.ModelID, batch.Size(), wait)
		})
		r.OnWorkerHealthTransition(func(transition router.WorkerHealthTransition) {
			if gw.instanceManager != nil && (transition.ToStatus == types.WorkerStatusUnhealthy || transition.ToStatus == types.WorkerStatusOffline) {
				if _, err := gw.instanceManager.RecordWorkerUnhealthyWithError(transition.WorkerID, time.Now()); err != nil {
					gw.log.Error("worker.control_state_update_failed", slog.String("error", "control state unavailable"))
				}
			}
			if gw.metrics != nil {
				gw.metrics.RecordWorkerHealthTransition(string(transition.Event), string(transition.FromStatus), string(transition.ToStatus))
			}
		})
	}

	if instanceMgr != nil {
		gw.instanceHandlers = NewInstanceHandlers(instanceMgr)
	}

	return gw
}

// SetAuthHandler sets the authentication handler.
func (g *Gateway) SetAuthHandler(h *auth.Handler) {
	g.authHandler = h
}

// SetVaultHandler sets the vault model registry handler.
func (g *Gateway) SetVaultHandler(h *vault.Handler) {
	g.vaultHandler = h
}

// SetAuditStore sets the inference audit store and starts the serialized writer.
func (g *Gateway) SetAuditStore(s auditUsageStore) {
	g.auditStore = s
	g.auditCh = make(chan auditWriteRequest, 1024)
	g.auditWg.Add(1)
	go g.runAuditWriter()
}

// runAuditWriter serializes audit writes and acknowledges durable persistence.
func (g *Gateway) runAuditWriter() {
	defer g.auditWg.Done()
	for write := range g.auditCh {
		err := g.appendAuditRecordWithRetry(write.record)
		write.done <- err
		close(write.done)
	}
}

// SetDeploymentStore sets the shared deployment history store.
func (g *Gateway) SetDeploymentStore(s deploymentHistoryStore) {
	g.deploymentStore = s
	if g.instanceHandlers != nil {
		g.instanceHandlers.SetDeploymentStore(s)
	}
}

// SetAgentRuntime sets the shared agents runtime.
func (g *Gateway) SetAgentRuntime(runtime *agents.Runtime) {
	g.agentRuntime = runtime
}

func (g *Gateway) SetWebSearcher(searcher WebSearcher) {
	g.webSearcher = searcher
}

func (g *Gateway) SetVisionAnalyzer(analyzer VisionAnalyzer) {
	g.visionAnalyzer = analyzer
}

// Start starts the HTTP server.
func (g *Gateway) Start() error {
	mux := http.NewServeMux()

	// Helper: wrap with auth if auth handler is configured
	withAuth := func(h http.HandlerFunc) http.HandlerFunc {
		if g.authHandler != nil {
			return g.handleCORS(g.authHandler.RequireAuth(h))
		}
		return g.handleCORS(h)
	}
	// Rate limit wrapper for inference endpoints
	withRateLimit := RateLimitMiddleware(g.rateLimiter)

	// OpenAI-compatible endpoints (require auth + rate limit)
	mux.HandleFunc("/v1/chat/completions", withAuth(withRateLimit(g.handleChatCompletions)))
	mux.HandleFunc("/v1/models", withAuth(g.handleListModels))
	if g.metrics != nil {
		mux.HandleFunc("/metrics", g.handleMetrics)
	}

	// Public endpoints (no auth — workers need these, plus health checks)
	mux.HandleFunc("/api/workers/register", g.handleCORS(g.requireWorkerToken(g.handleRegisterWorker)))
	mux.HandleFunc("/api/workers/heartbeat", g.handleCORS(g.requireWorkerToken(g.handleWorkerHeartbeat)))
	mux.HandleFunc("/api/health", g.handleCORS(g.handleHealth))
	mux.HandleFunc("/health", g.handleHealth)
	mux.HandleFunc("/internal/prometheus/worker-targets", g.internalOnlyHandler(g.handlePrometheusWorkerTargets))

	// Protected internal API endpoints (require auth)
	mux.HandleFunc("/api/workers", withAuth(g.handleGetWorkers))
	mux.HandleFunc("/api/stats", withAuth(g.handleGetStats))
	if g.auditStore != nil {
		mux.HandleFunc("/api/audit/usage", withAuth(g.handleGetAuditUsage))
	}

	// Instance management endpoints (route-level auth, handler-level authorization)
	if g.instanceHandlers != nil {
		g.instanceHandlers.RegisterRoutes(mux, func(h http.HandlerFunc) http.HandlerFunc {
			return withAuth(h)
		})
	}

	// Vault (model registry) endpoints (route-level auth, handler-level authorization)
	if g.vaultHandler != nil {
		g.vaultHandler.RegisterRoutes(mux, func(h http.HandlerFunc) http.HandlerFunc {
			return withAuth(h)
		})
	}

	// Auth management endpoints (self-registers with admin-only middleware)
	if g.authHandler != nil {
		g.authHandler.RegisterRoutes(mux, g.handleCORS)
	}
	if g.agentRuntime != nil {
		mux.HandleFunc("/api/agents", withAuth(g.handleAgents))
		mux.HandleFunc("/api/agent-attachments", withAuth(g.handleAgentAttachments))
		mux.HandleFunc("/api/agents/runs", withAuth(g.handleAgentRuns))
		mux.HandleFunc("/api/agents/runs/", withAuth(g.handleAgentRunByID))
		mux.HandleFunc("/api/agents/definitions", withAuth(g.handleAgentDefinitions))
		mux.HandleFunc("/api/agents/definitions/", withAuth(g.handleAgentDefinitionByID))
		mux.HandleFunc("/api/agents/webhooks", withAuth(g.handleAgentWebhooks))
		mux.HandleFunc("/api/agents/webhooks/", withAuth(g.handleAgentWebhookByID))
		// External API: API-key authenticated, rate-limited (same policy as /v1/chat/completions).
		mux.HandleFunc("/v1/agents/runs", withAuth(withRateLimit(g.handleExternalAgentRun)))
	}

	// Apply middleware chain: recovery → request ID → body size limit.
	// Note: we intentionally do NOT apply http.TimeoutHandler globally
	// because streaming endpoints need long-lived connections. The
	// inference timeout is enforced per-request in handleChatCompletions.
	handler := chainMiddleware(
		mux,
		recoveryMiddleware(g.log),
		requestIDMiddleware,
		traceparentMiddleware,
		bodySizeLimitMiddleware(maxRequestBodyBytes),
	)
	if g.metrics != nil {
		handler = g.metrics.HTTPMiddleware(handler)
	}

	g.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", g.config.HTTPPort),
		Handler:      handler,
		ReadTimeout:  g.config.ReadTimeout,
		WriteTimeout: g.config.WriteTimeout,
	}

	return g.httpServer.ListenAndServe()
}

func (g *Gateway) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if g.metrics == nil {
		http.NotFound(w, r)
		return
	}
	g.recordWorkerCountMetrics(r.Context())
	g.metrics.Handler().ServeHTTP(w, r)
}

func (g *Gateway) recordWorkerCountMetrics(ctx context.Context) {
	if g.metrics == nil || g.router == nil {
		return
	}
	workers, err := g.router.GetWorkers(ctx, "", false)
	if err != nil {
		return
	}
	total := len(workers)
	healthy := len(filterHealthyWorkers(workers))
	g.metrics.RecordWorkerCounts(total, healthy)
}

// Stop gracefully stops the server.
func (g *Gateway) Stop(ctx context.Context) error {
	if g.rateLimiter != nil {
		g.rateLimiter.Stop()
	}
	if g.httpServer != nil {
		if err := g.httpServer.Shutdown(ctx); err != nil {
			return err
		}
	}
	// Drain the audit channel only after HTTP shutdown has completed so
	// request handlers can no longer enqueue records into the channel.
	if g.auditCh != nil {
		close(g.auditCh)
		g.auditWg.Wait()
		g.auditCh = nil
	}
	return nil
}

// CORS middleware
func (g *Gateway) handleCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if g.config.EnableCORS {
			origin := r.Header.Get("Origin")
			if origin != "" {
				if !g.isOriginAllowed(origin) {
					http.Error(w, "origin not allowed", http.StatusForbidden)
					return
				}

				if g.hasWildcardOrigin() {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				} else {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Credentials", "true")
				}
				w.Header().Set("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Worker-Token, X-API-Key")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
		}
		next(w, r)
	}
}

func (g *Gateway) isOriginAllowed(origin string) bool {
	for _, allowed := range g.config.AllowedOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}
	return false
}

func (g *Gateway) hasWildcardOrigin() bool {
	for _, allowed := range g.config.AllowedOrigins {
		if allowed == "*" {
			return true
		}
	}
	return false
}

func (g *Gateway) requireWorkerToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(r.Header.Get("X-Worker-Token"))
		if token == "" {
			auth := strings.TrimSpace(r.Header.Get("Authorization"))
			if strings.HasPrefix(auth, "Bearer ") {
				token = strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
			}
		}

		if g.instanceManager != nil {
			instance, ok, err := g.instanceManager.AuthenticateWorkerToken(token)
			if err != nil {
				g.writeError(w, http.StatusServiceUnavailable, "worker_auth_unavailable", "Worker authentication is temporarily unavailable")
				return
			}
			if !ok {
				g.writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid worker token")
				return
			}
			principal := workerPrincipal{InstanceID: instance.ID, WorkspaceID: normalizeWorkspaceIDForGateway(instance.WorkspaceID)}
			next(w, r.WithContext(context.WithValue(r.Context(), workerPrincipalContextKey{}, principal)))
			return
		}

		expected := strings.TrimSpace(g.config.WorkerSharedToken)
		if token == "" || expected == "" || subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
			g.writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid worker token")
			return
		}

		next(w, r)
	}
}

func hashPrompt(messages []ChatMessage) string {
	if len(messages) == 0 {
		return ""
	}
	hasher := sha256.New()
	for _, msg := range messages {
		_, _ = hasher.Write([]byte(msg.Role))
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write([]byte(msg.Name))
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write([]byte(msg.Content))
		_, _ = hasher.Write([]byte{0})
	}
	sum := hex.EncodeToString(hasher.Sum(nil))
	if len(sum) > 16 {
		return sum[:16]
	}
	return sum
}

func hashPromptPrefix(messages []ChatMessage, maxBytes int) string {
	if len(messages) == 0 || maxBytes <= 0 {
		return ""
	}

	hasher := sha256.New()
	remaining := maxBytes
	for _, msg := range messages {
		parts := []string{msg.Role, msg.Name, msg.Content}
		for _, part := range parts {
			if remaining <= 0 {
				break
			}
			chunk := part
			if len(chunk) > remaining {
				chunk = chunk[:remaining]
			}
			_, _ = hasher.Write([]byte(chunk))
			_, _ = hasher.Write([]byte{0})
			remaining -= len(chunk)
		}
		if remaining <= 0 {
			break
		}
	}

	sum := hex.EncodeToString(hasher.Sum(nil))
	if len(sum) > 16 {
		return sum[:16]
	}
	return sum
}

func (g *Gateway) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is allowed")
		return
	}

	var req ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, OpenAIChatErrorTypeInvalidRequest, "Invalid JSON: "+err.Error())
		return
	}
	// Validate
	if req.Model == "" {
		g.writeError(w, http.StatusBadRequest, OpenAIChatErrorTypeInvalidRequest, "model is required")
		return
	}
	if len(req.Messages) == 0 {
		g.writeError(w, http.StatusBadRequest, OpenAIChatErrorTypeInvalidRequest, "messages is required")
		return
	}

	if req.Stream {
		g.handleStreamingChatCompletion(w, r, &req)
		return
	}

	inferenceReq := g.toInferenceRequest(r, &req)

	result, err := g.executeNonStreamingInference(r.Context(), auth.KeyFromContext(r.Context()), inferenceReq)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		if inferaErr, ok := err.(*types.InferaError); ok {
			setFailedRouteDecisionHeader(w, r, inferenceReq, inferaErr.Message)
			status := g.errorCodeToStatus(inferaErr.Code)
			if inferaErr.Code == types.ErrorCode("overloaded") {
				w.Header().Set("Retry-After", "5")
			}
			if inferaErr.Code == types.ErrorCodeNoWorkersAvailable {
				g.writeRetryableError(w, status, string(inferaErr.Code), "service_unavailable", inferaErr.Message)
				return
			}
			g.writeError(w, status, string(inferaErr.Code), inferaErr.Message)
			return
		}
		g.writeError(w, http.StatusInternalServerError, OpenAIChatErrorTypeInferenceError, err.Error())
		return
	}
	setRouteDecisionHeader(w, r, result.RoutingDecision)
	g.writeChatCompletionResponse(w, inferenceReq.RequestID, req.Model, inferenceReq, result.Response)
}

func (g *Gateway) handleStreamingChatCompletion(w http.ResponseWriter, r *http.Request, req *ChatCompletionRequest) {
	current := atomic.AddInt64(&g.inFlightRequests, 1)
	defer atomic.AddInt64(&g.inFlightRequests, -1)
	if current > g.maxInFlightDefault {
		w.Header().Set("Retry-After", "5")
		if g.metrics != nil {
			g.metrics.RecordInferenceRejected("overloaded")
		}
		g.writeError(w, http.StatusServiceUnavailable, "overloaded", "Server is overloaded. Please retry shortly.")
		return
	}

	requestStart := time.Now()
	inferenceReq := g.toInferenceRequest(r, req)
	requestID := inferenceReq.RequestID
	clientRequestID := inferenceReq.ClientRequestID
	keyID := ""
	workspaceID := ""
	if record := auth.KeyFromContext(r.Context()); record != nil {
		keyID = record.KeyPrefix
		workspaceID = record.WorkspaceID
	}
	promptHash := hashPrompt(req.Messages)
	auditStatus := "unknown_error"
	auditTokenCount := 0
	auditUsage := usageMeasurement{TokenSource: audit.TokenSourceUnknown}
	auditWorkerID := ""
	auditRoutingDecision := types.RoutingDecision{}
	auditErrorCode := ""
	sloModel := ""
	sloStrategy := ""
	defer func() {
		elapsed := time.Since(requestStart)
		latencyMS := elapsed.Milliseconds()
		attrs := []any{
			slog.String("request_id", requestID),
			slog.String("key_id", keyID),
			slog.String("model", req.Model),
			slog.String("worker_id", auditWorkerID),
			slog.Bool("stream", true),
			slog.Int("message_count", len(req.Messages)),
			slog.Int("token_count", auditTokenCount),
			slog.String("token_source", auditUsage.TokenSource),
			slog.String("prompt_hash", promptHash),
			slog.String("status", auditStatus),
			slog.Int64("latency_ms", latencyMS),
		}
		if auditErrorCode != "" {
			attrs = append(attrs, slog.String("error_code", auditErrorCode))
		}
		g.log.Info("inference.audit", attrs...)

		if g.auditCh != nil {
			rec := audit.InferenceAuditRecord{
				Timestamp:        requestStart.UTC(),
				RequestID:        requestID,
				ClientRequestID:  clientRequestID,
				KeyID:            keyID,
				WorkspaceID:      workspaceID,
				Model:            req.Model,
				WorkerID:         auditWorkerID,
				Stream:           true,
				MessageCount:     len(req.Messages),
				PromptTokens:     auditUsage.PromptTokens,
				CompletionTokens: auditUsage.CompletionTokens,
				TokenCount:       auditTokenCount,
				TokenSource:      auditUsage.TokenSource,
				PromptHash:       promptHash,
				Status:           auditStatus,
				ErrorCode:        auditErrorCode,
				LatencyMS:        latencyMS,
				Cost:             g.requestCostAttribution(auditWorkerID, auditRoutingDecision, elapsed),
			}
			if err := g.enqueueAuditRecord(rec); err != nil {
				g.log.Error("inference.audit_persist_failed", slog.String("request_id", requestID), slog.String("error", err.Error()))
			}
		}
		if g.metrics != nil {
			g.metrics.RecordInference(true, auditStatus, auditTokenCount, time.Since(requestStart))
			g.metrics.RecordSLORequest(sloModel, sloStrategy, true, auditStatus, time.Since(requestStart))
		}
	}()

	ctx, cancel := context.WithTimeout(r.Context(), g.config.InferenceTimeout)
	defer cancel()

	if err := g.enforceWorkspaceQuotaForKey(auth.KeyFromContext(r.Context()), inferenceReq); err != nil {
		auditStatus = "failed"
		auditErrorCode = string(err.Code)
		g.writeError(w, g.errorCodeToStatus(err.Code), string(err.Code), err.Message)
		return
	}

	routed, err := g.router.Route(ctx, inferenceReq)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			auditStatus = "client_canceled"
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			auditStatus = "failed"
			auditErrorCode = "inference_timeout"
			g.logRouteDecisionFailed(inferenceReq, "inference_timeout", "Inference request timed out")
			setFailedRouteDecisionHeader(w, r, inferenceReq, "Inference request timed out")
			g.writeError(w, http.StatusGatewayTimeout, "inference_timeout", "Inference request timed out")
			return
		}
		if inferaErr, ok := err.(*types.InferaError); ok {
			auditStatus = "failed"
			auditErrorCode = string(inferaErr.Code)
			g.logRouteDecisionFailed(inferenceReq, string(inferaErr.Code), inferaErr.Message)
			setFailedRouteDecisionHeader(w, r, inferenceReq, inferaErr.Message)
			status := g.errorCodeToStatus(inferaErr.Code)
			if inferaErr.Code == types.ErrorCodeNoWorkersAvailable {
				g.writeRetryableError(w, status, string(inferaErr.Code), "service_unavailable", inferaErr.Message)
				return
			}
			g.writeError(w, status, string(inferaErr.Code), inferaErr.Message)
			return
		}
		auditStatus = "failed"
		auditErrorCode = "no_workers"
		g.logRouteDecisionFailed(inferenceReq, "no_workers", err.Error())
		setFailedRouteDecisionHeader(w, r, inferenceReq, "No healthy workers available for model: "+req.Model)
		g.writeError(w, http.StatusServiceUnavailable, "no_workers", "No healthy workers available for model: "+req.Model)
		return
	}
	g.logRouteDecision(routed.RoutingDecision)
	sloModel = req.Model
	sloStrategy = string(routed.RoutingDecision.Strategy)
	auditRoutingDecision = routed.RoutingDecision
	setRouteDecisionHeader(w, r, routed.RoutingDecision)

	auditWorkerID = routed.WorkerID
	client, err := g.getWorkerClient(ctx, routed.WorkerID)
	if err != nil {
		auditStatus = "failed"
		if errors.Is(err, context.Canceled) {
			auditStatus = "client_canceled"
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			auditErrorCode = "inference_timeout"
			g.writeError(w, http.StatusGatewayTimeout, "inference_timeout", "Inference request timed out")
			return
		}
		if errors.Is(err, errWorkerRegistryUnavailable) {
			auditErrorCode = string(types.ErrorCodeWorkerRegistryUnavailable)
			g.writeWorkerRegistryUnavailable(w)
			return
		}
		auditErrorCode = "worker_unavailable"
		g.writeError(w, http.StatusServiceUnavailable, "worker_unavailable", err.Error())
		return
	}

	streamResult := g.handleStreamingInference(w, r.WithContext(ctx), client, inferenceReq, req.Model, sloStrategy, requestStart)
	auditUsage = streamResult.Usage
	auditTokenCount = auditUsage.TotalTokens
	auditStatus = streamResult.Status
	if auditStatus != "success" {
		auditErrorCode = auditStatus
	}
}

func (g *Gateway) handlePrometheusWorkerTargets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed")
		return
	}

	type targetGroup struct {
		Targets []string          `json:"targets"`
		Labels  map[string]string `json:"labels,omitempty"`
	}

	if g.router == nil {
		g.writeJSON(w, http.StatusOK, []targetGroup{})
		return
	}

	workers, err := g.router.GetWorkers(r.Context(), "", true)
	if err != nil {
		if g.writeRequestContextError(w, err) {
			return
		}
		g.writeWorkerRegistryUnavailable(w)
		return
	}
	targets := make([]targetGroup, 0, len(workers))
	for _, worker := range workers {
		address := strings.TrimSpace(worker.Address)
		if address == "" {
			continue
		}

		labels := map[string]string{
			"job":        "infera_worker",
			"service":    "worker",
			"env":        inferaEnv(),
			"worker_id":  worker.WorkerID,
			"status":     string(worker.Status),
			"__scheme__": workerMetricsScheme(address),
		}
		for _, key := range []string{"provider", "engine", "version", "env"} {
			if value := strings.TrimSpace(worker.Tags[key]); value != "" {
				labels[key] = value
			}
		}

		targets = append(targets, targetGroup{
			Targets: []string{address},
			Labels:  labels,
		})
	}

	g.writeJSON(w, http.StatusOK, targets)
}

func workerMetricsScheme(address string) string {
	switch {
	case strings.Contains(address, ".proxy.runpod.net"), strings.Contains(address, ".runpod."):
		return "https"
	default:
		return "http"
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (g *Gateway) internalOnlyHandler(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
		if err != nil {
			host = strings.TrimSpace(r.RemoteAddr)
		}
		ip := net.ParseIP(host)
		if ip == nil || !(ip.IsLoopback() || ip.IsPrivate()) {
			g.writeError(w, http.StatusForbidden, "forbidden", "Internal endpoint")
			return
		}
		next(w, r)
	}
}

func (g *Gateway) handleListModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed")
		return
	}
	models, err := g.listModelEntries(r.Context())
	if err != nil {
		if g.writeRequestContextError(w, err) {
			return
		}
		if errors.Is(err, errWorkerRegistryUnavailable) {
			g.writeWorkerRegistryUnavailable(w)
			return
		}
		g.writeError(w, http.StatusInternalServerError, "models_unavailable", "Models are temporarily unavailable")
		return
	}

	g.writeJSON(w, http.StatusOK, map[string]interface{}{
		"object": "list",
		"data":   models,
	})
}

func (g *Gateway) requirePermission(w http.ResponseWriter, r *http.Request, permission, message string) bool {
	record := auth.KeyFromContext(r.Context())
	if !auth.HasPermission(record, permission) {
		g.writeError(w, http.StatusForbidden, "forbidden", message)
		return false
	}
	return true
}

// ============================================================================
// Internal API Endpoints
// ============================================================================

func (g *Gateway) handleGetWorkers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed")
		return
	}
	if !g.requirePermission(w, r, auth.PermissionViewInfrastructure, "Infrastructure view access required") {
		return
	}

	response, err := g.listWorkerEntries(r.Context(), currentWorkspaceID(r))
	if err != nil {
		if g.writeRequestContextError(w, err) {
			return
		}
		g.writeWorkerRegistryUnavailable(w)
		return
	}
	g.writeJSON(w, http.StatusOK, map[string]interface{}{
		"workers": response,
		"total":   len(response),
	})
}

func (g *Gateway) handleRegisterWorker(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is allowed")
		return
	}

	var req struct {
		WorkerID        string            `json:"worker_id"`
		Address         string            `json:"address"`
		Status          string            `json:"status"`
		Tags            map[string]string `json:"tags"`
		ReleaseID       string            `json:"release_id"`
		ProtocolVersion string            `json:"protocol_version"`
		LoadedModels    []struct {
			ModelID           string `json:"model_id"`
			Version           string `json:"version"`
			MemoryBytes       int64  `json:"memory_bytes"`
			MaxBatchSize      int    `json:"max_batch_size"`
			MaxSequenceLength int    `json:"max_sequence_length"`
		} `json:"loaded_models"`
		Stats struct {
			QueueDepth        int     `json:"queue_depth"`
			ActiveRequests    int     `json:"active_requests"`
			GPUUtilization    float64 `json:"gpu_utilization"`
			MemoryUsedBytes   int64   `json:"memory_used_bytes"`
			MemoryTotalBytes  int64   `json:"memory_total_bytes"`
			RequestsPerSecond float64 `json:"requests_per_second"`
			AvgLatencyMS      float64 `json:"avg_latency_ms"`
			ErrorRate         float64 `json:"error_rate"`
		} `json:"stats"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON")
		return
	}
	if strings.TrimSpace(req.WorkerID) == "" || strings.TrimSpace(req.Address) == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "worker_id and address are required")
		return
	}
	if expected := strings.TrimSpace(g.config.WorkerProtocolVersion); expected != "" && strings.TrimSpace(req.ProtocolVersion) != expected {
		g.writeError(w, http.StatusConflict, "worker_protocol_mismatch", "Worker protocol is incompatible with this gateway release")
		return
	}
	if g.config.RequireMatchingWorkerRelease {
		expected := strings.TrimSpace(g.config.ReleaseID)
		if expected == "" || strings.TrimSpace(req.ReleaseID) != expected {
			g.writeError(w, http.StatusConflict, "worker_release_mismatch", "Worker release does not match the gateway release")
			return
		}
	}
	principal, managed := r.Context().Value(workerPrincipalContextKey{}).(workerPrincipal)
	if managed {
		instance, found, err := g.instanceManager.GetInstanceWithError(principal.InstanceID)
		if err != nil {
			g.writeError(w, http.StatusServiceUnavailable, "worker_control_state_unavailable", "Worker control state is temporarily unavailable")
			return
		}
		if !found {
			g.writeError(w, http.StatusServiceUnavailable, "worker_control_state_unavailable", "Worker control state is temporarily unavailable")
			return
		}
		address, err := trustedWorkerAddress(instance, req.Address)
		if err != nil {
			g.writeError(w, http.StatusForbidden, "worker_address_mismatch", err.Error())
			return
		}
		req.Address = address
		if err := g.instanceManager.LinkWorker(principal.InstanceID, req.WorkerID); err != nil {
			g.writeWorkerControlError(w, err)
			return
		}
	}

	// Convert to WorkerInfo
	loadedModels := make([]types.LoadedModel, len(req.LoadedModels))
	for i, m := range req.LoadedModels {
		loadedModels[i] = types.LoadedModel{
			ModelID:           m.ModelID,
			Version:           m.Version,
			MemoryBytes:       m.MemoryBytes,
			MaxBatchSize:      m.MaxBatchSize,
			MaxSequenceLength: m.MaxSequenceLength,
			LoadedAt:          time.Now(),
		}
	}

	workerInfo := &types.WorkerInfo{
		WorkerID:     req.WorkerID,
		WorkspaceID:  principal.WorkspaceID,
		Address:      req.Address,
		Status:       types.WorkerStatus(req.Status),
		LoadedModels: loadedModels,
		Stats: types.WorkerStats{
			QueueDepth:        req.Stats.QueueDepth,
			ActiveRequests:    req.Stats.ActiveRequests,
			GPUUtilization:    req.Stats.GPUUtilization,
			MemoryUsedBytes:   req.Stats.MemoryUsedBytes,
			MemoryTotalBytes:  req.Stats.MemoryTotalBytes,
			RequestsPerSecond: req.Stats.RequestsPerSecond,
			AvgLatencyMS:      req.Stats.AvgLatencyMS,
			ErrorRate:         req.Stats.ErrorRate,
			UpdatedAt:         time.Now(),
		},
		LastHealthCheck: time.Now(),
		RegisteredAt:    time.Now(),
		Tags:            req.Tags,
	}
	workerToken, err := g.workerCredentialForWorker(req.WorkerID)
	if err != nil {
		g.writeError(w, http.StatusServiceUnavailable, "worker_credential_unavailable", "Worker credential is temporarily unavailable")
		return
	}

	if err := g.router.RegisterWorker(r.Context(), workerInfo); err != nil {
		if g.writeRequestContextError(w, err) {
			return
		}
		g.writeWorkerRegistryUnavailable(w)
		return
	}

	if !managed {
		if err := g.linkWorkerToInstance(workerInfo); err != nil {
			g.writeError(w, http.StatusServiceUnavailable, "worker_control_state_unavailable", "Worker control state is temporarily unavailable")
			return
		}
	}

	// Register worker client
	g.registerWorkerClient(req.WorkerID, req.Address, workerToken, workerInfo.RegistrationID)

	g.writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"worker_id": req.WorkerID,
		"message":   "Worker registered successfully",
	})
}

func trustedWorkerAddress(instance *providers.Instance, presented string) (string, error) {
	if instance == nil {
		return "", errors.New("worker instance is not available")
	}
	if instance.Provider == providers.ProviderRunPod && strings.TrimSpace(instance.ProviderID) != "" {
		expected := fmt.Sprintf("%s-8081.proxy.runpod.net", strings.TrimSpace(instance.ProviderID))
		raw := strings.TrimSpace(presented)
		if !strings.Contains(raw, "://") {
			raw = "https://" + raw
		}
		u, err := validatedWorkerURL(raw, "/")
		if err != nil || !strings.EqualFold(u.Host, expected) {
			return "", errors.New("worker address does not match the provisioned RunPod endpoint")
		}
		return "https://" + expected, nil
	}
	u, err := validatedWorkerURL(presented, "/")
	if err != nil {
		return "", err
	}
	host := u.Hostname()
	if ip := strings.TrimSpace(instance.PublicIP); ip != "" && host != ip {
		return "", errors.New("worker address does not match the provisioned instance address")
	}
	if instance.HTTPPort > 0 && u.Port() != fmt.Sprint(instance.HTTPPort) {
		return "", errors.New("worker address does not match the provisioned instance port")
	}
	return strings.TrimSuffix(u.String(), "/"), nil
}

func (g *Gateway) handleWorkerHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is allowed")
		return
	}

	var req struct {
		WorkerID string `json:"worker_id"`
		Stats    struct {
			QueueDepth        int     `json:"queue_depth"`
			ActiveRequests    int     `json:"active_requests"`
			GPUUtilization    float64 `json:"gpu_utilization"`
			MemoryUsedBytes   int64   `json:"memory_used_bytes"`
			MemoryTotalBytes  int64   `json:"memory_total_bytes"`
			RequestsPerSecond float64 `json:"requests_per_second"`
			AvgLatencyMS      float64 `json:"avg_latency_ms"`
			P50LatencyMS      float64 `json:"p50_latency_ms"`
			P99LatencyMS      float64 `json:"p99_latency_ms"`
			ErrorRate         float64 `json:"error_rate"`
		} `json:"stats"`
		LoadedModels []struct {
			ModelID string `json:"model_id"`
			Version string `json:"version"`
		} `json:"loaded_models"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON")
		return
	}
	if principal, managed := r.Context().Value(workerPrincipalContextKey{}).(workerPrincipal); managed {
		if err := g.instanceManager.AuthorizeWorkerBinding(principal.InstanceID, req.WorkerID); err != nil {
			g.writeWorkerControlError(w, err)
			return
		}
	}

	stats := types.WorkerStats{
		QueueDepth:        req.Stats.QueueDepth,
		ActiveRequests:    req.Stats.ActiveRequests,
		GPUUtilization:    req.Stats.GPUUtilization,
		MemoryUsedBytes:   req.Stats.MemoryUsedBytes,
		MemoryTotalBytes:  req.Stats.MemoryTotalBytes,
		RequestsPerSecond: req.Stats.RequestsPerSecond,
		AvgLatencyMS:      req.Stats.AvgLatencyMS,
		P50LatencyMS:      req.Stats.P50LatencyMS,
		P99LatencyMS:      req.Stats.P99LatencyMS,
		ErrorRate:         req.Stats.ErrorRate,
		UpdatedAt:         time.Now(),
	}

	if err := g.router.UpdateWorkerStats(r.Context(), req.WorkerID, stats); err != nil {
		if g.writeRequestContextError(w, err) {
			return
		}
		if errors.Is(err, registry.ErrWorkerNotFound) {
			g.writeJSON(w, http.StatusOK, map[string]interface{}{
				"acknowledged": false,
				"message":      "Worker not registered",
			})
			return
		}
		g.writeWorkerRegistryUnavailable(w)
		return
	}

	if g.metrics != nil {
		g.metrics.RecordWorkerQueueDepth(req.WorkerID, req.Stats.QueueDepth)
	}

	worker, found, err := g.router.GetWorker(r.Context(), req.WorkerID)
	if err != nil {
		if g.writeRequestContextError(w, err) {
			return
		}
		g.writeWorkerRegistryUnavailable(w)
		return
	}
	if !found {
		g.writeJSON(w, http.StatusOK, map[string]interface{}{
			"acknowledged": false,
			"message":      "Worker not registered",
		})
		return
	}
	if found {
		if err := g.linkWorkerToInstance(worker); err != nil {
			g.writeError(w, http.StatusServiceUnavailable, "worker_control_state_unavailable", "Worker control state is temporarily unavailable")
			return
		}
	}

	// Sync loaded models from heartbeat (self-healing if registration missed them)
	if len(req.LoadedModels) > 0 {
		models := make([]types.LoadedModel, len(req.LoadedModels))
		for i, m := range req.LoadedModels {
			models[i] = types.LoadedModel{
				ModelID:  m.ModelID,
				Version:  m.Version,
				LoadedAt: time.Now(),
			}
		}
		if err := g.router.UpdateWorkerModels(r.Context(), req.WorkerID, models); err != nil {
			if g.writeRequestContextError(w, err) {
				return
			}
			if errors.Is(err, registry.ErrWorkerNotFound) {
				g.writeJSON(w, http.StatusOK, map[string]interface{}{
					"acknowledged": false,
					"message":      "Worker not registered",
				})
				return
			}
			g.writeWorkerRegistryUnavailable(w)
			return
		}
	}

	g.writeJSON(w, http.StatusOK, map[string]interface{}{
		"acknowledged": true,
	})
}

func (g *Gateway) linkWorkerToInstance(worker *types.WorkerInfo) error {
	if g == nil || g.instanceManager == nil || worker == nil {
		return nil
	}

	if instance, found, err := g.instanceManager.GetInstanceByWorkerWithError(worker.WorkerID); err != nil {
		return err
	} else if found && instance != nil {
		_, err := g.instanceManager.RecordWorkerHeartbeatWithError(worker.WorkerID, worker.LastHealthCheck)
		return err
	}

	instanceID := strings.TrimSpace(worker.Tags["instance_id"])
	if instanceID == "" {
		instanceID = strings.TrimSpace(worker.Tags["provider_id"])
	}

	if instanceID != "" {
		if inst, found, err := g.instanceManager.GetInstanceWithError(instanceID); err != nil {
			return err
		} else if found && inst != nil {
			if err := g.instanceManager.LinkWorker(inst.ID, worker.WorkerID); err != nil {
				return err
			}
			g.log.Info("worker.linked_via_tag",
				slog.String("worker_id", worker.WorkerID),
				slog.String("instance_id", inst.ID),
			)
			return nil
		}
	}

	providerID, providerType, method, ok := inferWorkerProviderRef(worker)
	if !ok {
		g.log.Debug("worker.link_skipped",
			slog.String("worker_id", worker.WorkerID),
			slog.String("address", worker.Address),
			slog.String("reason", "no provider ref resolvable from tags or hostname"),
		)
		return nil
	}

	if inst, found, err := g.instanceManager.GetInstanceByProviderRefWithError(providerType, providerID); err != nil {
		return err
	} else if found && inst != nil {
		if err := g.instanceManager.LinkWorker(inst.ID, worker.WorkerID); err != nil {
			return err
		}
		g.log.Info("worker.linked_via_provider_ref",
			slog.String("worker_id", worker.WorkerID),
			slog.String("instance_id", inst.ID),
			slog.String("provider", string(providerType)),
			slog.String("provider_id", providerID),
			slog.String("method", method),
		)
	} else {
		g.log.Warn("worker.link_no_instance",
			slog.String("worker_id", worker.WorkerID),
			slog.String("provider", string(providerType)),
			slog.String("provider_id", providerID),
			slog.String("method", method),
		)
	}
	return nil
}

func (g *Gateway) writeWorkerControlError(w http.ResponseWriter, err error) {
	if errors.Is(err, providers.ErrWorkerIdentityConflict) {
		g.writeError(w, http.StatusForbidden, "worker_identity_mismatch", "Worker identity does not match the provisioned instance")
		return
	}
	g.writeError(w, http.StatusServiceUnavailable, "worker_control_state_unavailable", "Worker control state is temporarily unavailable")
}

func inferWorkerProviderRef(worker *types.WorkerInfo) (providerID string, providerType providers.ProviderType, method string, ok bool) {
	if worker == nil {
		return "", "", "", false
	}

	if tagProviderID := strings.TrimSpace(worker.Tags["provider_id"]); tagProviderID != "" {
		provider := providers.ProviderType(strings.TrimSpace(worker.Tags["provider"]))
		if provider != "" {
			return tagProviderID, provider, "tags", true
		}
	}

	address := strings.TrimSpace(worker.Address)
	if address == "" {
		return "", "", "", false
	}

	host := address
	if parsedHost, _, err := net.SplitHostPort(address); err == nil {
		host = parsedHost
	}
	host = strings.ToLower(strings.TrimSpace(host))

	if strings.Contains(host, ".proxy.runpod.net") {
		firstLabel := host
		if idx := strings.Index(host, "."); idx >= 0 {
			firstLabel = host[:idx]
		}
		if dash := strings.Index(firstLabel, "-"); dash > 0 {
			return firstLabel[:dash], providers.ProviderRunPod, "runpod_hostname", true
		}
	}

	return "", "", "", false
}

func (g *Gateway) handleGetStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed")
		return
	}
	if !g.requirePermission(w, r, auth.PermissionViewInfrastructure, "Infrastructure view access required") {
		return
	}

	payload, err := g.statsPayload(r.Context(), currentWorkspaceID(r))
	if err != nil {
		if g.writeRequestContextError(w, err) {
			return
		}
		g.writeWorkerRegistryUnavailable(w)
		return
	}
	g.writeJSON(w, http.StatusOK, payload)
}

func (g *Gateway) handleGetAuditUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed")
		return
	}
	if !g.requirePermission(w, r, auth.PermissionViewUsage, "Usage access required") {
		return
	}
	if g.auditStore == nil {
		g.writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "Audit store is not configured")
		return
	}

	now := time.Now().UTC()
	start := now.Add(-24 * time.Hour)
	end := now

	if rawStart := strings.TrimSpace(r.URL.Query().Get("start")); rawStart != "" {
		parsed, err := time.Parse(time.RFC3339, rawStart)
		if err != nil {
			g.writeError(w, http.StatusBadRequest, "invalid_request", "start must be RFC3339")
			return
		}
		start = parsed.UTC()
	}
	if rawEnd := strings.TrimSpace(r.URL.Query().Get("end")); rawEnd != "" {
		parsed, err := time.Parse(time.RFC3339, rawEnd)
		if err != nil {
			g.writeError(w, http.StatusBadRequest, "invalid_request", "end must be RFC3339")
			return
		}
		end = parsed.UTC()
	}
	if !start.Before(end) {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "start must be before end")
		return
	}

	bucket := strings.TrimSpace(r.URL.Query().Get("bucket"))
	if bucket == "" {
		bucket = "day"
	}
	if bucket != "day" && bucket != "hour" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "bucket must be 'day' or 'hour'")
		return
	}
	currentKey := auth.KeyFromContext(r.Context())
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" {
		if currentKey != nil {
			workspaceID = currentKey.WorkspaceID
		}
	}
	if currentKey != nil && currentKey.WorkspaceID != auth.DefaultWorkspaceID && workspaceID != "" && workspaceID != currentKey.WorkspaceID {
		g.writeError(w, http.StatusForbidden, "forbidden", "Workspace-scoped identities can only query audit usage in their own workspace")
		return
	}

	rows, err := g.auditStore.UsageByKey(audit.UsageQuery{
		Start:       start,
		End:         end,
		Bucket:      bucket,
		KeyID:       strings.TrimSpace(r.URL.Query().Get("key_id")),
		WorkspaceID: workspaceID,
		Model:       strings.TrimSpace(r.URL.Query().Get("model")),
	})
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "audit_query_failed", err.Error())
		return
	}

	type usageRow struct {
		BucketStart       string      `json:"bucket_start"`
		WorkspaceID       string      `json:"workspace_id"`
		KeyID             string      `json:"key_id"`
		Attempts          int64       `json:"attempts"`
		Requests          int64       `json:"requests"`
		Tokens            int64       `json:"tokens"`
		ExactRequests     int64       `json:"exact_requests"`
		EstimatedRequests int64       `json:"estimated_requests"`
		ExactTokens       int64       `json:"exact_tokens"`
		EstimatedTokens   int64       `json:"estimated_tokens"`
		Successes         int64       `json:"successes"`
		Errors            int64       `json:"errors"`
		Cost              costMetrics `json:"cost"`
	}

	out := make([]usageRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, usageRow{
			BucketStart:       time.UnixMilli(row.BucketStartMS).UTC().Format(time.RFC3339),
			WorkspaceID:       row.WorkspaceID,
			KeyID:             row.KeyID,
			Attempts:          row.AttemptCount,
			Requests:          row.RequestCount,
			Tokens:            row.TokenCount,
			ExactRequests:     row.ExactRequestCount,
			EstimatedRequests: row.EstimatedRequestCount,
			ExactTokens:       row.ExactTokenCount,
			EstimatedTokens:   row.EstimatedTokenCount,
			Successes:         row.SuccessCount,
			Errors:            row.ErrorCount,
			Cost:              buildCostMetrics(row.CostNano, row.CostedTokenCount, row.ExactCostCount, row.EstimatedCostCount, row.UnavailableCostCount),
		})
	}

	g.writeJSON(w, http.StatusOK, map[string]interface{}{
		"bucket":         bucket,
		"start":          start.Format(time.RFC3339),
		"end":            end.Format(time.RFC3339),
		"rows":           out,
		"reconciliation": reconcileUsageRows(rows),
	})
}

func (g *Gateway) handleHealth(w http.ResponseWriter, r *http.Request) {
	stats, err := g.router.GetStats(r.Context())
	if err != nil {
		if g.writeRequestContextError(w, err) {
			return
		}
		g.writeWorkerRegistryUnavailable(w)
		return
	}

	status := "healthy"
	if stats.HealthyWorkers == 0 {
		status = "degraded"
	}

	g.writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":                  status,
		"version":                 "0.1.0",
		"release_id":              g.config.ReleaseID,
		"worker_protocol_version": g.config.WorkerProtocolVersion,
		"uptime_seconds":          int64(time.Since(g.startedAt).Seconds()),
		"workers":                 stats.TotalWorkers,
		"healthy_workers":         stats.HealthyWorkers,
	})
}

func (g *Gateway) getWorkerClient(ctx context.Context, workerID string) (*WorkerClient, error) {
	// Resolve the current registration before consulting the process-local
	// client cache. A cached transport must not hide a registry outage or a
	// worker that was removed after routing.
	worker, found, err := g.router.GetWorker(ctx, workerID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("%w", errWorkerRegistryUnavailable)
	}
	if !found {
		return nil, fmt.Errorf("worker %s not found", workerID)
	}

	g.workerClientsMu.RLock()
	observedClient, exists := g.workerClients[workerID]
	if exists && workerClientMatches(observedClient, worker) {
		g.workerClientsMu.RUnlock()
		return observedClient, nil
	}
	g.workerClientsMu.RUnlock()

	// The registry issues an opaque identity on every registration. Fetch the
	// credential only on a cache miss or identity change, never on the
	// unchanged-registration fast path.
	workerToken, err := g.workerCredentialForWorker(workerID)
	if err != nil {
		return nil, err
	}

	confirmed, found, err := g.router.GetWorker(ctx, workerID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("%w", errWorkerRegistryUnavailable)
	}
	if !found || confirmed.RegistrationID != worker.RegistrationID || confirmed.Address != worker.Address {
		return nil, errors.New("worker registration changed while resolving client")
	}

	replacement := newRegisteredWorkerClient(worker.Address, workerToken, worker.RegistrationID)
	currentClient, installed := g.installWorkerClientIfUnchanged(workerID, observedClient, replacement)
	if !installed {
		return nil, errors.New("worker registration changed while resolving client")
	}
	if currentClient != nil {
		currentClient.closeIdleConnections()
	}
	return replacement, nil
}

func workerClientMatches(client *WorkerClient, worker *types.WorkerInfo) bool {
	if client == nil || worker == nil || client.address != worker.Address {
		return false
	}
	// Generationless clients are supported for explicitly injected transports
	// (for example, tests and embedders). Gateway-created clients always carry
	// a registration generation and therefore use the stricter comparison.
	return client.registrationID == "" || client.registrationID == worker.RegistrationID
}

func (g *Gateway) installWorkerClientIfUnchanged(workerID string, observed, replacement *WorkerClient) (*WorkerClient, bool) {
	g.workerClientsMu.Lock()
	defer g.workerClientsMu.Unlock()
	current := g.workerClients[workerID]
	if current != observed {
		return current, false
	}
	g.workerClients[workerID] = replacement
	return current, true
}

func (g *Gateway) writeWorkerRegistryUnavailable(w http.ResponseWriter) {
	g.writeError(w, http.StatusServiceUnavailable, string(types.ErrorCodeWorkerRegistryUnavailable), "Worker registry is temporarily unavailable")
}

func (g *Gateway) writeRequestContextError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, context.Canceled):
		return true
	case errors.Is(err, context.DeadlineExceeded):
		g.writeError(w, http.StatusGatewayTimeout, "request_timeout", "Request timed out")
		return true
	default:
		return false
	}
}

func filterHealthyWorkers(workers []*types.WorkerInfo) []*types.WorkerInfo {
	healthy := make([]*types.WorkerInfo, 0, len(workers))
	for _, worker := range workers {
		if worker.IsHealthy() {
			healthy = append(healthy, worker)
		}
	}
	return healthy
}

func (g *Gateway) workerCredentialForWorker(workerID string) (string, error) {
	if g.workerCredentialResolver != nil {
		return g.workerCredentialResolver(workerID)
	}
	if g.instanceManager == nil {
		credential := strings.TrimSpace(g.config.WorkerSharedToken)
		if credential == "" {
			return "", errors.New("worker credential is not configured")
		}
		return credential, nil
	}
	credential, found, err := g.instanceManager.WorkerCredentialForWorker(workerID)
	if err != nil {
		if errors.Is(err, providers.ErrControlStateUnavailable) || errors.Is(err, providers.ErrWorkerCredentialIntegrity) {
			return "", errors.New("worker control state is temporarily unavailable")
		}
		return "", errors.New("worker credential is unavailable")
	}
	if !found {
		return "", fmt.Errorf("deployment-bound credential for worker %s is unavailable", workerID)
	}
	return credential, nil
}

// RegisterWorkerClient registers a worker client (called when worker connects).
func (g *Gateway) RegisterWorkerClient(workerID, address string) error {
	worker, found, err := g.router.GetWorker(context.Background(), workerID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("worker %s not found", workerID)
	}
	workerToken, err := g.workerCredentialForWorker(workerID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(address) != strings.TrimSpace(worker.Address) {
		return fmt.Errorf("worker %s address does not match its registration", workerID)
	}
	g.registerWorkerClient(workerID, worker.Address, workerToken, worker.RegistrationID)
	return nil
}

func (g *Gateway) registerWorkerClient(workerID, address, workerToken, registrationID string) {
	g.workerClientsMu.Lock()
	previous := g.workerClients[workerID]
	g.workerClients[workerID] = newRegisteredWorkerClient(address, workerToken, registrationID)
	g.workerClientsMu.Unlock()
	if previous != nil {
		previous.closeIdleConnections()
	}
}

// RemoveWorkerClient removes a worker client.
func (g *Gateway) RemoveWorkerClient(workerID string) {
	g.workerClientsMu.Lock()
	previous := g.workerClients[workerID]
	delete(g.workerClients, workerID)
	g.workerClientsMu.Unlock()
	if previous != nil {
		previous.closeIdleConnections()
	}
}

func (g *Gateway) errorCodeToStatus(code types.ErrorCode) int {
	switch code {
	case types.ErrorCodeInvalidRequest:
		return http.StatusBadRequest
	case types.ErrorCodeModelNotFound:
		return http.StatusNotFound
	case types.ErrorCodeRateLimited:
		return http.StatusTooManyRequests
	case types.ErrorCodeModelOverloaded, types.ErrorCodeNoWorkersAvailable:
		return http.StatusServiceUnavailable
	case types.ErrorCodeWorkerRegistryUnavailable:
		return http.StatusServiceUnavailable
	case types.ErrorCodeTimeout:
		return http.StatusGatewayTimeout
	case types.ErrorCode("quota_exceeded"):
		return http.StatusForbidden
	case types.ErrorCode("quota_unavailable"):
		return http.StatusServiceUnavailable
	case types.ErrorCode("no_workers"):
		return http.StatusServiceUnavailable
	case types.ErrorCode("worker_unavailable"):
		return http.StatusServiceUnavailable
	case types.ErrorCode("inference_timeout"):
		return http.StatusGatewayTimeout
	case types.ErrorCode("overloaded"):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
