// Package gateway provides the HTTP API for Infera.
package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
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

	// Vault (model registry)
	vaultHandler *vault.Handler

	// Worker clients for direct inference calls
	workerClients   map[string]*WorkerClient
	workerClientsMu sync.RWMutex
}

// Config configures the gateway.
type Config struct {
	HTTPPort         int
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration
	EnableCORS       bool
	AllowedOrigins   []string
	RequestTimeoutMS int
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		HTTPPort:         8080,
		ReadTimeout:      30 * time.Second,
		WriteTimeout:     60 * time.Second,
		EnableCORS:       true,
		AllowedOrigins:   []string{"*"},
		RequestTimeoutMS: 30000,
	}
}

// New creates a new gateway.
func New(config Config, r *router.Router, instanceMgr *providers.Manager) *Gateway {
	gw := &Gateway{
		router:          r,
		config:          config,
		instanceManager: instanceMgr,
		workerClients:   make(map[string]*WorkerClient),
		startedAt:       time.Now(),
	}

	if instanceMgr != nil {
		gw.instanceHandlers = NewInstanceHandlers(instanceMgr)
	}

	return gw
}

// SetVaultHandler sets the vault model registry handler.
func (g *Gateway) SetVaultHandler(h *vault.Handler) {
	g.vaultHandler = h
}

// Start starts the HTTP server.
func (g *Gateway) Start() error {
	mux := http.NewServeMux()

	// OpenAI-compatible endpoints
	mux.HandleFunc("/v1/chat/completions", g.handleCORS(g.handleChatCompletions))
	mux.HandleFunc("/v1/models", g.handleCORS(g.handleListModels))

	// Internal API endpoints
	mux.HandleFunc("/api/workers", g.handleCORS(g.handleGetWorkers))
	mux.HandleFunc("/api/workers/register", g.handleCORS(g.handleRegisterWorker))
	mux.HandleFunc("/api/workers/heartbeat", g.handleCORS(g.handleWorkerHeartbeat))
	mux.HandleFunc("/api/stats", g.handleCORS(g.handleGetStats))
	mux.HandleFunc("/api/health", g.handleCORS(g.handleHealth))

	// Instance management endpoints
	if g.instanceHandlers != nil {
		g.instanceHandlers.RegisterRoutes(mux, g.handleCORS)
	}

	// Vault (model registry) endpoints
	if g.vaultHandler != nil {
		g.vaultHandler.RegisterRoutes(mux, g.handleCORS)
	}

	// Health check
	mux.HandleFunc("/health", g.handleHealth)

	g.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", g.config.HTTPPort),
		Handler:      mux,
		ReadTimeout:  g.config.ReadTimeout,
		WriteTimeout: g.config.WriteTimeout,
	}

	return g.httpServer.ListenAndServe()
}

// Stop gracefully stops the server.
func (g *Gateway) Stop(ctx context.Context) error {
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
			if origin == "" {
				origin = "*"
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Allow-Credentials", "true")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
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
	Stop             []string      `json:"stop,omitempty"`
	Stream           bool          `json:"stream,omitempty"`
	Seed             *int64        `json:"seed,omitempty"`
	PresencePenalty  *float64      `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64      `json:"frequency_penalty,omitempty"`
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

func (g *Gateway) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is allowed")
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

	// Convert to internal format
	inferenceReq := g.toInferenceRequest(&req)

	// Route the request
	routed, err := g.router.Route(inferenceReq)
	if err != nil {
		if inferaErr, ok := err.(*types.InferaError); ok {
			status := g.errorCodeToStatus(inferaErr.Code)
			g.writeError(w, status, string(inferaErr.Code), inferaErr.Message)
			return
		}
		g.writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	// Get worker client
	client, err := g.getWorkerClient(routed.WorkerID)
	if err != nil {
		g.writeError(w, http.StatusServiceUnavailable, "worker_unavailable", err.Error())
		return
	}

	if req.Stream {
		g.handleStreamingInference(w, r, client, inferenceReq, req.Model)
	} else {
		g.handleNonStreamingInference(w, client, inferenceReq, req.Model)
	}
}

func (g *Gateway) handleNonStreamingInference(w http.ResponseWriter, client *WorkerClient, req *types.InferenceRequest, model string) {
	// Call worker
	resp, err := client.Infer(req)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, "inference_error", err.Error())
		return
	}

	// Convert to OpenAI format
	openAIResp := ChatCompletionResponse{
		ID:      "chatcmpl-" + req.RequestID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: make([]ChatChoice, len(resp.Choices)),
		Usage: Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
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
}

func (g *Gateway) handleStreamingInference(w http.ResponseWriter, r *http.Request, client *WorkerClient, req *types.InferenceRequest, model string) {
	// First, try to get the stream from worker
	// This validates the request before we commit to SSE
	chunks, err := client.InferStream(r.Context(), req)
	if err != nil {
		// Return regular error response (not SSE) since we haven't committed to streaming yet
		g.writeError(w, http.StatusInternalServerError, "inference_error", err.Error())
		return
	}

	// Now commit to SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // For nginx proxies

	flusher, ok := w.(http.Flusher)
	if !ok {
		g.writeError(w, http.StatusInternalServerError, "streaming_not_supported", "Streaming not supported")
		return
	}

	requestID := "chatcmpl-" + req.RequestID
	created := time.Now().Unix()

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

	for chunk := range chunks {
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

// ============================================================================
// Internal API Endpoints
// ============================================================================

func (g *Gateway) handleGetWorkers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed")
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
		WorkerID     string `json:"worker_id"`
		Address      string `json:"address"`
		Status       string `json:"status"`
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
	}

	if err := g.router.RegisterWorker(workerInfo); err != nil {
		g.writeError(w, http.StatusInternalServerError, "registration_failed", err.Error())
		return
	}

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

func (g *Gateway) handleGetStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		g.writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed")
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
		params.StopSequences = req.Stop
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
