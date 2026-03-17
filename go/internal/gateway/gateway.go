// Package gateway provides the HTTP API for Infera.
package gateway

import (
	"context"
	"crypto/sha256"
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

	"github.com/google/uuid"
	"github.com/infera/infera/go/internal/audit"
	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/internal/deployments"
	"github.com/infera/infera/go/internal/providers"
	"github.com/infera/infera/go/internal/router"
	"github.com/infera/infera/go/internal/vault"
	"github.com/infera/infera/go/pkg/types"
)

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
	auditStore *audit.Store

	// Deployments (shared deployment history)
	deploymentStore *deployments.Store

	// Rate limiting
	rateLimiter *RateLimiter

	// Metrics
	metrics *GatewayMetrics

	// Backpressure: track in-flight inference requests
	inFlightRequests   int64
	maxInFlightDefault int64

	// Structured logger
	log *slog.Logger

	// Worker clients for direct inference calls
	workerClients   map[string]*WorkerClient
	workerClientsMu sync.RWMutex
}

// Config configures the gateway.
type Config struct {
	HTTPPort          int
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	InferenceTimeout  time.Duration
	EnableCORS        bool
	AllowedOrigins    []string
	WorkerSharedToken string
	RequestTimeoutMS  int
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		HTTPPort:          8080,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      300 * time.Second,
		InferenceTimeout:  120 * time.Second,
		EnableCORS:        true,
		AllowedOrigins:    []string{"*"},
		WorkerSharedToken: "",
		RequestTimeoutMS:  30000,
	}
}

// New creates a new gateway.
func New(config Config, r *router.Router, instanceMgr *providers.Manager) *Gateway {
	gw := &Gateway{
		router:             r,
		config:             config,
		instanceManager:    instanceMgr,
		rateLimiter:        NewRateLimiter(DefaultRateLimiterConfig()),
		metrics:            NewGatewayMetrics(),
		maxInFlightDefault: 100,
		log:                NewLogger(),
		workerClients:      make(map[string]*WorkerClient),
		startedAt:          time.Now(),
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

// SetAuditStore sets the inference audit store.
func (g *Gateway) SetAuditStore(s *audit.Store) {
	g.auditStore = s
}

// SetDeploymentStore sets the shared deployment history store.
func (g *Gateway) SetDeploymentStore(s *deployments.Store) {
	g.deploymentStore = s
	if g.instanceHandlers != nil {
		g.instanceHandlers.SetDeploymentStore(s)
	}
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
		mux.Handle("/metrics", g.metrics.Handler())
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

	// Apply middleware chain: recovery → request ID → body size limit.
	// Note: we intentionally do NOT apply http.TimeoutHandler globally
	// because streaming endpoints need long-lived connections. The
	// inference timeout is enforced per-request in handleChatCompletions.
	handler := chainMiddleware(
		mux,
		recoveryMiddleware(g.log),
		requestIDMiddleware,
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

// Stop gracefully stops the server.
func (g *Gateway) Stop(ctx context.Context) error {
	if g.rateLimiter != nil {
		g.rateLimiter.Stop()
	}
	if g.httpServer != nil {
		return g.httpServer.Shutdown(ctx)
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
		expected := strings.TrimSpace(g.config.WorkerSharedToken)
		if expected == "" {
			next(w, r)
			return
		}

		token := strings.TrimSpace(r.Header.Get("X-Worker-Token"))
		if token == "" {
			auth := strings.TrimSpace(r.Header.Get("Authorization"))
			if strings.HasPrefix(auth, "Bearer ") {
				token = strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
			}
		}

		if token == "" || token != expected {
			g.writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid worker token")
			return
		}

		next(w, r)
	}
}

// ============================================================================
// OpenAI-Compatible Endpoints
// ============================================================================

// ChatCompletionRequest is the OpenAI-compatible request format.
type ChatCompletionRequest struct {
	Model            string        `json:"model"`
	Messages         []ChatMessage `json:"messages"`
	Temperature      *float64      `json:"temperature,omitempty"`
	TopP             *float64      `json:"top_p,omitempty"`
	MaxTokens        *int          `json:"max_tokens,omitempty"`
	Stop             StopSequences `json:"stop,omitempty"`
	Stream           bool          `json:"stream,omitempty"`
	Seed             *int64        `json:"seed,omitempty"`
	PresencePenalty  *float64      `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64      `json:"frequency_penalty,omitempty"`
}

// StopSequences accepts either a single stop string or a list of stop strings.
type StopSequences []string

func (s *StopSequences) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*s = nil
		return nil
	}

	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*s = StopSequences{single}
		return nil
	}

	var many []string
	if err := json.Unmarshal(data, &many); err == nil {
		*s = StopSequences(many)
		return nil
	}

	return fmt.Errorf("stop must be a string or array of strings")
}

// ChatMessage is a single message.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

// ChatCompletionResponse is the OpenAI-compatible response format.
type ChatCompletionResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []ChatChoice `json:"choices"`
	Usage   Usage        `json:"usage"`
}

// ChatChoice is a single completion choice.
type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// Usage tracks token usage.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatCompletionChunk is a streaming response chunk.
type ChatCompletionChunk struct {
	ID      string            `json:"id"`
	Object  string            `json:"object"`
	Created int64             `json:"created"`
	Model   string            `json:"model"`
	Choices []ChatChunkChoice `json:"choices"`
}

// ChatChunkChoice is a streaming choice.
type ChatChunkChoice struct {
	Index        int       `json:"index"`
	Delta        ChatDelta `json:"delta"`
	FinishReason *string   `json:"finish_reason"`
}

// ChatDelta is the delta content in streaming.
type ChatDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
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

func (g *Gateway) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is allowed")
		return
	}

	// Backpressure: reject if too many in-flight requests
	current := atomic.AddInt64(&g.inFlightRequests, 1)
	defer atomic.AddInt64(&g.inFlightRequests, -1)

	if current > g.maxInFlightDefault {
		w.Header().Set("Retry-After", "5")
		g.writeError(w, http.StatusServiceUnavailable, "overloaded", "Server is overloaded. Please retry shortly.")
		return
	}

	// Parse request
	var req ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON: "+err.Error())
		return
	}

	// Validate
	if req.Model == "" {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "model is required")
		return
	}
	if len(req.Messages) == 0 {
		g.writeError(w, http.StatusBadRequest, "invalid_request", "messages is required")
		return
	}

	// Audit log fields
	requestStart := time.Now()
	requestID := r.Header.Get(HeaderRequestID)
	keyID := ""
	workspaceID := ""
	if record := auth.KeyFromContext(r.Context()); record != nil {
		keyID = record.KeyPrefix
		workspaceID = record.WorkspaceID
	}
	promptHash := hashPrompt(req.Messages)
	auditStatus := "unknown_error"
	auditTokenCount := 0
	auditWorkerID := ""
	auditErrorCode := ""
	defer func() {
		latencyMS := time.Since(requestStart).Milliseconds()
		attrs := []any{
			slog.String("request_id", requestID),
			slog.String("key_id", keyID),
			slog.String("model", req.Model),
			slog.String("worker_id", auditWorkerID),
			slog.Bool("stream", req.Stream),
			slog.Int("message_count", len(req.Messages)),
			slog.Int("token_count", auditTokenCount),
			slog.String("prompt_hash", promptHash),
			slog.String("status", auditStatus),
			slog.Int64("latency_ms", latencyMS),
		}
		if auditErrorCode != "" {
			attrs = append(attrs, slog.String("error_code", auditErrorCode))
		}
		g.log.Info("inference.audit", attrs...)

		if g.auditStore != nil {
			err := g.auditStore.AppendInference(audit.InferenceAuditRecord{
				Timestamp:    requestStart.UTC(),
				RequestID:    requestID,
				KeyID:        keyID,
				WorkspaceID:  workspaceID,
				Model:        req.Model,
				WorkerID:     auditWorkerID,
				Stream:       req.Stream,
				MessageCount: len(req.Messages),
				TokenCount:   auditTokenCount,
				PromptHash:   promptHash,
				Status:       auditStatus,
				ErrorCode:    auditErrorCode,
				LatencyMS:    latencyMS,
			})
			if err != nil {
				g.log.Warn("inference.audit_persist_failed",
					slog.String("request_id", requestID),
					slog.String("error", err.Error()),
				)
			}
		}

		if g.metrics != nil {
			g.metrics.RecordInference(req.Stream, auditStatus, auditTokenCount, time.Since(requestStart))
		}
	}()

	// Apply inference timeout — cancel if worker takes too long
	ctx, cancel := context.WithTimeout(r.Context(), g.config.InferenceTimeout)
	defer cancel()
	r = r.WithContext(ctx)

	// Convert to internal format
	inferenceReq := g.toInferenceRequest(&req)
	if !g.enforceWorkspaceQuota(w, r, inferenceReq) {
		return
	}

	// Route the request
	routed, err := g.router.Route(ctx, inferenceReq)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			auditStatus = "client_canceled"
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			auditStatus = "failed"
			auditErrorCode = "inference_timeout"
			g.writeError(w, http.StatusGatewayTimeout, "inference_timeout", "Inference request timed out")
			return
		}
		g.log.Warn("inference.route_failed",
			slog.String("request_id", requestID),
			slog.String("key_id", keyID),
			slog.String("model", req.Model),
			slog.String("error", err.Error()),
			slog.Int64("latency_ms", time.Since(requestStart).Milliseconds()),
		)
		if inferaErr, ok := err.(*types.InferaError); ok {
			auditStatus = "failed"
			auditErrorCode = string(inferaErr.Code)
			status := g.errorCodeToStatus(inferaErr.Code)
			g.writeError(w, status, string(inferaErr.Code), inferaErr.Message)
			return
		}
		// No healthy workers → 503
		auditStatus = "failed"
		auditErrorCode = "no_workers"
		g.writeError(w, http.StatusServiceUnavailable, "no_workers", "No healthy workers available for model: "+req.Model)
		return
	}

	// Get worker client
	auditWorkerID = routed.WorkerID
	client, err := g.getWorkerClient(routed.WorkerID)
	if err != nil {
		g.log.Warn("inference.worker_unavailable",
			slog.String("request_id", requestID),
			slog.String("key_id", keyID),
			slog.String("model", req.Model),
			slog.String("worker_id", routed.WorkerID),
			slog.String("error", err.Error()),
			slog.Int64("latency_ms", time.Since(requestStart).Milliseconds()),
		)
		auditStatus = "failed"
		auditErrorCode = "worker_unavailable"
		g.writeError(w, http.StatusServiceUnavailable, "worker_unavailable", err.Error())
		return
	}

	if req.Stream {
		auditTokenCount, auditStatus = g.handleStreamingInference(w, r, client, inferenceReq, req.Model)
	} else {
		auditTokenCount, auditStatus = g.handleNonStreamingInference(w, ctx, client, inferenceReq, req.Model)
	}
	if auditStatus != "success" {
		auditErrorCode = auditStatus
	}
}

func (g *Gateway) handleNonStreamingInference(w http.ResponseWriter, ctx context.Context, client *WorkerClient, req *types.InferenceRequest, model string) (int, string) {
	// Call worker
	resp, err := client.InferWithContext(ctx, req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			g.writeError(w, http.StatusGatewayTimeout, "inference_timeout", "Inference request timed out")
			return 0, "inference_timeout"
		}
		if errors.Is(err, context.Canceled) {
			return 0, "client_canceled"
		}
		g.writeError(w, http.StatusInternalServerError, "inference_error", err.Error())
		return 0, "inference_error"
	}

	// Convert to OpenAI format
	promptTokens := resp.Usage.PromptTokens
	if promptTokens == 0 {
		promptTokens = req.TokenEstimate()
	}
	completionTokens := resp.Usage.CompletionTokens
	if completionTokens == 0 {
		completionTokens = estimateCompletionTokens(resp)
	}
	if g.metrics != nil {
		g.recordNonStreamingLatencyMetrics(model, resp, completionTokens)
	}
	totalTokens := usageTotalTokens(
		promptTokens,
		completionTokens,
		resp.Usage.TotalTokens,
	)

	openAIResp := ChatCompletionResponse{
		ID:      "chatcmpl-" + req.RequestID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: make([]ChatChoice, len(resp.Choices)),
		Usage: Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
		},
	}

	for i, choice := range resp.Choices {
		openAIResp.Choices[i] = ChatChoice{
			Index: choice.Index,
			Message: ChatMessage{
				Role:    string(choice.Message.Role),
				Content: choice.Message.Content,
			},
			FinishReason: string(choice.FinishReason),
		}
	}

	g.writeJSON(w, http.StatusOK, openAIResp)
	return openAIResp.Usage.TotalTokens, "success"
}

func (g *Gateway) enforceWorkspaceQuota(w http.ResponseWriter, r *http.Request, req *types.InferenceRequest) bool {
	if g.authHandler == nil || g.auditStore == nil {
		return true
	}
	key := auth.KeyFromContext(r.Context())
	if key == nil || strings.TrimSpace(key.WorkspaceID) == "" {
		return true
	}

	quota, err := g.authHandler.Store().GetWorkspaceQuota(key.WorkspaceID)
	if err != nil {
		g.log.Warn("workspace.quota_lookup_failed",
			slog.String("workspace_id", key.WorkspaceID),
			slog.String("error", err.Error()),
		)
		return true
	}
	if quota == nil {
		return true
	}
	if quota.MonthlyRequestLimit == nil && quota.MonthlyTokenLimit == nil {
		return true
	}
	if !quota.EnforceHardLimits {
		return true
	}

	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	usage, err := g.auditStore.UsageSummary(audit.UsageSummaryQuery{
		Start:       monthStart,
		End:         now,
		WorkspaceID: key.WorkspaceID,
	})
	if err != nil {
		g.log.Warn("workspace.quota_usage_failed",
			slog.String("workspace_id", key.WorkspaceID),
			slog.String("error", err.Error()),
		)
		return true
	}

	projectedRequests := usage.RequestCount + 1
	projectedTokens := usage.TokenCount + int64(req.TokenEstimate()+req.Parameters.MaxTokens)

	if quota.MonthlyRequestLimit != nil && projectedRequests > *quota.MonthlyRequestLimit {
		g.writeError(w, http.StatusForbidden, "quota_exceeded",
			fmt.Sprintf("Workspace request quota exceeded for %s. Limit: %d requests/month.", key.WorkspaceName, *quota.MonthlyRequestLimit))
		return false
	}
	if quota.MonthlyTokenLimit != nil && projectedTokens > *quota.MonthlyTokenLimit {
		g.writeError(w, http.StatusForbidden, "quota_exceeded",
			fmt.Sprintf("Workspace token quota exceeded for %s. Limit: %d tokens/month.", key.WorkspaceName, *quota.MonthlyTokenLimit))
		return false
	}
	return true
}

func (g *Gateway) handleStreamingInference(w http.ResponseWriter, r *http.Request, client *WorkerClient, req *types.InferenceRequest, model string) (int, string) {
	// First, try to get the stream from worker
	// This validates the request before we commit to SSE
	chunks, err := client.InferStream(r.Context(), req)
	if err != nil {
		// Return regular error response (not SSE) since we haven't committed to streaming yet
		g.writeError(w, http.StatusInternalServerError, "inference_error", err.Error())
		return 0, "inference_error"
	}

	// Now commit to SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // For nginx proxies

	flusher, ok := w.(http.Flusher)
	if !ok {
		g.writeError(w, http.StatusInternalServerError, "streaming_not_supported", "Streaming not supported")
		return 0, "streaming_not_supported"
	}

	requestID := "chatcmpl-" + req.RequestID
	created := time.Now().Unix()
	streamStart := time.Now()

	// Send initial role chunk (OpenAI format)
	initialChunk := ChatCompletionChunk{
		ID:      requestID,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []ChatChunkChoice{
			{
				Index: 0,
				Delta: ChatDelta{
					Role: "assistant",
				},
			},
		},
	}
	data, _ := json.Marshal(initialChunk)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()

	tokenCount := 0
	generatedChars := 0
	bestPromptTokens := 0
	bestCompletionTokens := 0
	bestTotalTokens := 0
	firstChunkObserved := false
	var previousChunkAt time.Time

	for chunk := range chunks {
		now := time.Now()
		if !firstChunkObserved {
			firstChunkObserved = true
			if g.metrics != nil {
				g.metrics.RecordTTFT(model, true, now.Sub(streamStart))
			}
		} else if g.metrics != nil {
			g.metrics.RecordTPOT(model, true, now.Sub(previousChunkAt))
		}
		previousChunkAt = now

		generatedChars += len(chunk.Delta)
		if chunk.Usage != nil {
			bestPromptTokens = maxInt(bestPromptTokens, chunk.Usage.PromptTokens)
			bestCompletionTokens = maxInt(bestCompletionTokens, chunk.Usage.CompletionTokens)
			bestTotalTokens = maxInt(bestTotalTokens, chunk.Usage.TotalTokens)
		}

		openAIChunk := ChatCompletionChunk{
			ID:      requestID,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
			Choices: []ChatChunkChoice{
				{
					Index: chunk.Index,
					Delta: ChatDelta{
						Content: chunk.Delta,
					},
				},
			},
		}

		if chunk.FinishReason != nil {
			reason := string(*chunk.FinishReason)
			openAIChunk.Choices[0].FinishReason = &reason
		}

		data, _ := json.Marshal(openAIChunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	// Send [DONE]
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()

	if bestPromptTokens == 0 {
		bestPromptTokens = req.TokenEstimate()
	}
	if bestCompletionTokens == 0 {
		bestCompletionTokens = estimateCompletionChars(generatedChars)
	}
	tokenCount = maxInt(tokenCount, usageTotalTokens(
		bestPromptTokens,
		bestCompletionTokens,
		bestTotalTokens,
	))

	return tokenCount, "success"
}

func (g *Gateway) recordNonStreamingLatencyMetrics(model string, resp *types.InferenceResponse, completionTokens int) {
	ttft := time.Duration(resp.Latency.TimeToFirstTokenMS) * time.Millisecond
	g.metrics.RecordTTFT(model, false, ttft)

	if completionTokens <= 1 {
		return
	}

	total := time.Duration(resp.Latency.TotalMS) * time.Millisecond
	if total <= ttft {
		return
	}

	g.metrics.RecordTPOT(model, false, (total-ttft)/time.Duration(completionTokens-1))
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

	workers := g.router.GetWorkers("", true)
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

func usageTotalTokens(promptTokens, completionTokens, totalTokens int) int {
	if totalTokens > 0 {
		return totalTokens
	}
	sum := promptTokens + completionTokens
	if sum > 0 {
		return sum
	}
	return 0
}

func estimateCompletionTokens(resp *types.InferenceResponse) int {
	totalChars := 0
	for _, choice := range resp.Choices {
		totalChars += len(choice.Message.Content)
	}
	return estimateCompletionChars(totalChars)
}

func estimateCompletionChars(chars int) int {
	if chars <= 0 {
		return 0
	}
	estimate := chars / 4
	if estimate == 0 {
		return 1
	}
	return estimate
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

	// Get unique models from all workers
	workers := g.router.GetWorkers("", false)
	loadedSet := make(map[string]bool)

	for _, worker := range workers {
		for _, model := range worker.LoadedModels {
			loadedSet[model.ModelID] = true
		}
	}

	// If vault is not configured, fall back to existing behavior
	if g.vaultHandler == nil {
		models := make([]map[string]interface{}, 0, len(loadedSet))
		for modelID := range loadedSet {
			models = append(models, map[string]interface{}{
				"id":       modelID,
				"object":   "model",
				"created":  time.Now().Unix(),
				"owned_by": "infera",
			})
		}
		g.writeJSON(w, http.StatusOK, map[string]interface{}{
			"object": "list",
			"data":   models,
		})
		return
	}

	// Query vault for all models
	vaultModels, err := g.vaultHandler.Store().List(&vault.ModelFilter{})
	if err != nil {
		// Fall back to worker-only models on vault error
		models := make([]map[string]interface{}, 0, len(loadedSet))
		for modelID := range loadedSet {
			models = append(models, map[string]interface{}{
				"id":       modelID,
				"object":   "model",
				"created":  time.Now().Unix(),
				"owned_by": "infera",
			})
		}
		g.writeJSON(w, http.StatusOK, map[string]interface{}{
			"object": "list",
			"data":   models,
		})
		return
	}

	// Track which worker models are covered by vault entries
	coveredByVault := make(map[string]bool)
	now := time.Now().Unix()

	models := make([]map[string]interface{}, 0, len(vaultModels)+len(loadedSet))

	// Add vault models with loaded status
	for _, vm := range vaultModels {
		loaded := loadedSet[vm.SourceURI]
		if loaded {
			coveredByVault[vm.SourceURI] = true
		}

		entry := map[string]interface{}{
			"id":            vm.SourceURI,
			"object":        "model",
			"created":       now,
			"owned_by":      "infera",
			"loaded":        loaded,
			"family":        vm.Family,
			"parameters":    vm.Parameters,
			"quantization":  vm.Quantization,
			"vram_required": vm.VRAMRequired,
			"max_context":   vm.MaxContext,
			"tags":          vm.Tags,
			"vault_status":  vm.Status,
		}
		models = append(models, entry)
	}

	// Add worker models not in vault
	for modelID := range loadedSet {
		if !coveredByVault[modelID] {
			models = append(models, map[string]interface{}{
				"id":       modelID,
				"object":   "model",
				"created":  now,
				"owned_by": "infera",
				"loaded":   true,
			})
		}
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

	workers := g.router.GetWorkers("", false)

	response := make([]map[string]interface{}, 0, len(workers))
	for _, worker := range workers {
		models := make([]string, 0, len(worker.LoadedModels))
		for _, m := range worker.LoadedModels {
			models = append(models, m.ModelID)
		}

		response = append(response, map[string]interface{}{
			"worker_id":        worker.WorkerID,
			"address":          worker.Address,
			"status":           worker.Status,
			"models":           models,
			"gpu_utilization":  worker.Stats.GPUUtilization,
			"memory_used":      worker.Stats.MemoryUsedBytes,
			"memory_total":     worker.Stats.MemoryTotalBytes,
			"queue_depth":      worker.Stats.QueueDepth,
			"requests_per_sec": worker.Stats.RequestsPerSecond,
			"avg_latency_ms":   worker.Stats.AvgLatencyMS,
			"p50_latency_ms":   worker.Stats.P50LatencyMS,
			"p99_latency_ms":   worker.Stats.P99LatencyMS,
			"error_rate":       worker.Stats.ErrorRate,
			"last_heartbeat":   worker.LastHealthCheck,
		})
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
		WorkerID     string            `json:"worker_id"`
		Address      string            `json:"address"`
		Status       string            `json:"status"`
		Tags         map[string]string `json:"tags"`
		LoadedModels []struct {
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

	if err := g.router.RegisterWorker(workerInfo); err != nil {
		g.writeError(w, http.StatusInternalServerError, "registration_failed", err.Error())
		return
	}

	g.linkWorkerToInstance(workerInfo)

	// Register worker client
	g.RegisterWorkerClient(req.WorkerID, req.Address)

	g.writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"worker_id": req.WorkerID,
		"message":   "Worker registered successfully",
	})
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

	if err := g.router.UpdateWorkerStats(req.WorkerID, stats); err != nil {
		// Worker might not be registered yet, ignore
		g.writeJSON(w, http.StatusOK, map[string]interface{}{
			"acknowledged": false,
			"message":      "Worker not registered",
		})
		return
	}

	if worker, found := g.router.GetWorker(req.WorkerID); found {
		g.linkWorkerToInstance(worker)
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
		_ = g.router.UpdateWorkerModels(req.WorkerID, models)
	}

	g.writeJSON(w, http.StatusOK, map[string]interface{}{
		"acknowledged": true,
	})
}

func (g *Gateway) linkWorkerToInstance(worker *types.WorkerInfo) {
	if g == nil || g.instanceManager == nil || worker == nil {
		return
	}

	if instance, found := g.instanceManager.GetInstanceByWorker(worker.WorkerID); found && instance != nil {
		return
	}

	instanceID := strings.TrimSpace(worker.Tags["instance_id"])
	if instanceID == "" {
		instanceID = strings.TrimSpace(worker.Tags["provider_id"])
	}

	if instanceID != "" {
		if inst, found := g.instanceManager.GetInstance(instanceID); found && inst != nil {
			_ = g.instanceManager.LinkWorker(inst.ID, worker.WorkerID)
			return
		}
	}

	providerID, providerType, ok := inferWorkerProviderRef(worker)
	if !ok {
		return
	}

	if inst, found := g.instanceManager.GetInstanceByProviderRef(providerType, providerID); found && inst != nil {
		_ = g.instanceManager.LinkWorker(inst.ID, worker.WorkerID)
	}
}

func inferWorkerProviderRef(worker *types.WorkerInfo) (providerID string, providerType providers.ProviderType, ok bool) {
	if worker == nil {
		return "", "", false
	}

	if tagProviderID := strings.TrimSpace(worker.Tags["provider_id"]); tagProviderID != "" {
		provider := providers.ProviderType(strings.TrimSpace(worker.Tags["provider"]))
		if provider != "" {
			return tagProviderID, provider, true
		}
	}

	address := strings.TrimSpace(worker.Address)
	if address == "" {
		return "", "", false
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
			return firstLabel[:dash], providers.ProviderRunPod, true
		}
	}

	return "", "", false
}

func (g *Gateway) handleGetStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed")
		return
	}
	if !g.requirePermission(w, r, auth.PermissionViewInfrastructure, "Infrastructure view access required") {
		return
	}

	stats := g.router.GetStats()
	workers := g.router.GetWorkers("", false)

	// Aggregate worker stats
	var totalRPS float64
	var totalMemoryUsed, totalMemoryTotal int64
	var totalGPUUtil float64
	healthyCount := 0

	// For weighted average latency: weight by each worker's RPS
	// so high-throughput workers contribute more to the average
	var weightedLatencySum, totalWeight float64

	for _, w := range workers {
		totalRPS += w.Stats.RequestsPerSecond
		totalMemoryUsed += w.Stats.MemoryUsedBytes
		totalMemoryTotal += w.Stats.MemoryTotalBytes
		totalGPUUtil += w.Stats.GPUUtilization
		if w.IsHealthy() {
			healthyCount++
		}

		// Use RPS as weight; if a worker has 0 RPS, use equal weight of 1
		weight := w.Stats.RequestsPerSecond
		if weight == 0 {
			weight = 1
		}
		weightedLatencySum += w.Stats.AvgLatencyMS * weight
		totalWeight += weight
	}

	avgLatency := 0.0
	if totalWeight > 0 {
		avgLatency = weightedLatencySum / totalWeight
	}

	avgGPUUtil := 0.0
	if len(workers) > 0 {
		avgGPUUtil = totalGPUUtil / float64(len(workers))
	}

	g.writeJSON(w, http.StatusOK, map[string]interface{}{
		"workers": map[string]interface{}{
			"total":   stats.TotalWorkers,
			"healthy": healthyCount,
		},
		"models": map[string]interface{}{
			"available": stats.ModelsAvailable,
		},
		"requests": map[string]interface{}{
			"per_second":  totalRPS,
			"queue_depth": stats.TotalQueueDepth,
		},
		"latency": map[string]interface{}{
			"avg_ms": avgLatency,
		},
		"gpu": map[string]interface{}{
			"avg_utilization": avgGPUUtil,
		},
		"memory": map[string]interface{}{
			"used_bytes":  totalMemoryUsed,
			"total_bytes": totalMemoryTotal,
		},
		"uptime_seconds": int64(time.Since(g.startedAt).Seconds()),
	})
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
		BucketStart string `json:"bucket_start"`
		WorkspaceID string `json:"workspace_id"`
		KeyID       string `json:"key_id"`
		Requests    int64  `json:"requests"`
		Tokens      int64  `json:"tokens"`
		Successes   int64  `json:"successes"`
		Errors      int64  `json:"errors"`
	}

	out := make([]usageRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, usageRow{
			BucketStart: time.UnixMilli(row.BucketStartMS).UTC().Format(time.RFC3339),
			WorkspaceID: row.WorkspaceID,
			KeyID:       row.KeyID,
			Requests:    row.RequestCount,
			Tokens:      row.TokenCount,
			Successes:   row.SuccessCount,
			Errors:      row.ErrorCount,
		})
	}

	g.writeJSON(w, http.StatusOK, map[string]interface{}{
		"bucket": bucket,
		"start":  start.Format(time.RFC3339),
		"end":    end.Format(time.RFC3339),
		"rows":   out,
	})
}

func (g *Gateway) handleHealth(w http.ResponseWriter, r *http.Request) {
	stats := g.router.GetStats()

	status := "healthy"
	if stats.HealthyWorkers == 0 {
		status = "degraded"
	}

	g.writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":          status,
		"version":         "0.1.0",
		"uptime_seconds":  int64(time.Since(g.startedAt).Seconds()),
		"workers":         stats.TotalWorkers,
		"healthy_workers": stats.HealthyWorkers,
	})
}

// ============================================================================
// Helper Methods
// ============================================================================

func (g *Gateway) toInferenceRequest(req *ChatCompletionRequest) *types.InferenceRequest {
	messages := make([]types.Message, len(req.Messages))
	for i, msg := range req.Messages {
		messages[i] = types.Message{
			Role:    types.Role(msg.Role),
			Content: msg.Content,
			Name:    msg.Name,
		}
	}

	params := types.DefaultInferenceParameters()
	if req.Temperature != nil {
		params.Temperature = *req.Temperature
	}
	if req.TopP != nil {
		params.TopP = *req.TopP
	}
	if req.MaxTokens != nil {
		params.MaxTokens = *req.MaxTokens
	}
	if req.Stop != nil {
		params.StopSequences = []string(req.Stop)
	}
	if req.Seed != nil {
		params.Seed = req.Seed
	}
	if req.PresencePenalty != nil {
		params.PresencePenalty = *req.PresencePenalty
	}
	if req.FrequencyPenalty != nil {
		params.FrequencyPenalty = *req.FrequencyPenalty
	}

	return &types.InferenceRequest{
		RequestID:  uuid.New().String(),
		ModelID:    req.Model,
		Messages:   messages,
		Parameters: params,
		Stream:     req.Stream,
		Priority:   types.PriorityNormal,
		CreatedAt:  time.Now(),
	}
}

func (g *Gateway) getWorkerClient(workerID string) (*WorkerClient, error) {
	g.workerClientsMu.RLock()
	client, exists := g.workerClients[workerID]
	g.workerClientsMu.RUnlock()

	if exists {
		return client, nil
	}

	// Get worker info
	worker, found := g.router.GetWorker(workerID)
	if !found {
		return nil, fmt.Errorf("worker %s not found", workerID)
	}

	// Create new client
	g.workerClientsMu.Lock()
	defer g.workerClientsMu.Unlock()

	// Double-check after acquiring write lock
	if client, exists = g.workerClients[workerID]; exists {
		return client, nil
	}

	client = NewWorkerClient(worker.Address)
	g.workerClients[workerID] = client
	return client, nil
}

// RegisterWorkerClient registers a worker client (called when worker connects).
func (g *Gateway) RegisterWorkerClient(workerID, address string) {
	g.workerClientsMu.Lock()
	defer g.workerClientsMu.Unlock()
	g.workerClients[workerID] = NewWorkerClient(address)
}

// RemoveWorkerClient removes a worker client.
func (g *Gateway) RemoveWorkerClient(workerID string) {
	g.workerClientsMu.Lock()
	defer g.workerClientsMu.Unlock()
	delete(g.workerClients, workerID)
}

func (g *Gateway) errorCodeToStatus(code types.ErrorCode) int {
	switch code {
	case types.ErrorCodeInvalidRequest:
		return http.StatusBadRequest
	case types.ErrorCodeModelNotFound:
		return http.StatusNotFound
	case types.ErrorCodeRateLimited:
		return http.StatusTooManyRequests
	case types.ErrorCodeModelOverloaded:
		return http.StatusServiceUnavailable
	case types.ErrorCodeTimeout:
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}

func (g *Gateway) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (g *Gateway) writeError(w http.ResponseWriter, status int, errType, message string) {
	g.writeJSON(w, status, map[string]interface{}{
		"error": map[string]interface{}{
			"type":    errType,
			"message": message,
		},
	})
}

func (g *Gateway) writeSSEError(w http.ResponseWriter, flusher http.Flusher, message string) {
	errData, _ := json.Marshal(map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
		},
	})
	fmt.Fprintf(w, "data: %s\n\n", errData)
	flusher.Flush()
}
