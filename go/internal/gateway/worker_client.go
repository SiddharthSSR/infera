// Package gateway provides the HTTP API for Infera.
package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/infera/infera/go/pkg/types"
)

// WorkerClient communicates with a worker via HTTP.
// In production, this would use gRPC, but HTTP is simpler for the vertical slice.
type WorkerClient struct {
	address             string
	httpClient          *http.Client
	streamingHTTPClient *http.Client
	breaker             *CircuitBreaker
}

// NewWorkerClient creates a new worker client.
func NewWorkerClient(address string) *WorkerClient {
	return &WorkerClient{
		address: address,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
		streamingHTTPClient: &http.Client{},
		breaker:             NewCircuitBreaker(),
	}
}

func shouldRecordFailure(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return false
	}
	return true
}

// WorkerInferRequest is the request format for the worker.
type WorkerInferRequest struct {
	RequestID  string                    `json:"request_id"`
	ModelID    string                    `json:"model_id"`
	Messages   []WorkerMessage           `json:"messages"`
	Parameters types.InferenceParameters `json:"parameters"`
	Stream     bool                      `json:"stream"`
}

// WorkerMessage is a message for the worker.
type WorkerMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

// WorkerInferResponse is the response from the worker.
type WorkerInferResponse struct {
	RequestID string `json:"request_id"`
	ModelID   string `json:"model_id"`
	Choices   []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Latency struct {
		QueueMS            int64 `json:"queue_ms"`
		InferenceMS        int64 `json:"inference_ms"`
		TotalMS            int64 `json:"total_ms"`
		TimeToFirstTokenMS int64 `json:"time_to_first_token_ms"`
	} `json:"latency"`
}

// Infer sends an inference request to the worker.
func (c *WorkerClient) Infer(req *types.InferenceRequest) (*types.InferenceResponse, error) {
	return c.InferWithContext(context.Background(), req)
}

// InferWithContext sends an inference request with context propagation.
func (c *WorkerClient) InferWithContext(ctx context.Context, req *types.InferenceRequest) (*types.InferenceResponse, error) {
	if !c.breaker.Allow() {
		return nil, ErrCircuitOpen
	}

	// Convert to worker format
	workerReq := WorkerInferRequest{
		RequestID:  req.RequestID,
		ModelID:    req.ModelID,
		Messages:   make([]WorkerMessage, len(req.Messages)),
		Parameters: req.Parameters,
		Stream:     false,
	}

	for i, msg := range req.Messages {
		workerReq.Messages[i] = WorkerMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
			Name:    msg.Name,
		}
	}

	// Make request
	body, err := json.Marshal(workerReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Use HTTPS for RunPod proxy URLs, HTTP for localhost
	protocol := "http"
	if strings.Contains(c.address, ".proxy.runpod.net") || strings.Contains(c.address, ".runpod.") {
		protocol = "https"
	}
	url := fmt.Sprintf("%s://%s/infer", protocol, c.address)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(HeaderRequestID, req.RequestID)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if shouldRecordFailure(err) {
			c.breaker.RecordFailure()
		}
		return nil, fmt.Errorf("failed to call worker: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.breaker.RecordFailure()
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("worker error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse response
	var workerResp WorkerInferResponse
	if err := json.NewDecoder(resp.Body).Decode(&workerResp); err != nil {
		if shouldRecordFailure(err) {
			c.breaker.RecordFailure()
		}
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	c.breaker.RecordSuccess()

	// Convert to internal format
	choices := make([]types.Choice, len(workerResp.Choices))
	for i, c := range workerResp.Choices {
		choices[i] = types.Choice{
			Index: c.Index,
			Message: types.Message{
				Role:    types.Role(c.Message.Role),
				Content: c.Message.Content,
			},
			FinishReason: types.FinishReason(c.FinishReason),
		}
	}

	return &types.InferenceResponse{
		RequestID: workerResp.RequestID,
		ModelID:   workerResp.ModelID,
		Choices:   choices,
		Usage: types.UsageStats{
			PromptTokens:     workerResp.Usage.PromptTokens,
			CompletionTokens: workerResp.Usage.CompletionTokens,
			TotalTokens:      workerResp.Usage.TotalTokens,
		},
		Latency: types.LatencyStats{
			QueueMS:            workerResp.Latency.QueueMS,
			InferenceMS:        workerResp.Latency.InferenceMS,
			TotalMS:            workerResp.Latency.TotalMS,
			TimeToFirstTokenMS: workerResp.Latency.TimeToFirstTokenMS,
		},
		CreatedAt: time.Now(),
	}, nil
}

// InferStream sends a streaming inference request to the worker.
func (c *WorkerClient) InferStream(ctx context.Context, req *types.InferenceRequest) (<-chan *types.TokenChunk, error) {
	if !c.breaker.Allow() {
		return nil, ErrCircuitOpen
	}

	// Convert to worker format
	workerReq := WorkerInferRequest{
		RequestID:  req.RequestID,
		ModelID:    req.ModelID,
		Messages:   make([]WorkerMessage, len(req.Messages)),
		Parameters: req.Parameters,
		Stream:     true,
	}

	for i, msg := range req.Messages {
		workerReq.Messages[i] = WorkerMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
			Name:    msg.Name,
		}
	}

	// Make request
	body, err := json.Marshal(workerReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Use HTTPS for RunPod proxy URLs, HTTP for localhost
	protocol := "http"
	if strings.Contains(c.address, ".proxy.runpod.net") || strings.Contains(c.address, ".runpod.") {
		protocol = "https"
	}
	url := fmt.Sprintf("%s://%s/infer/stream", protocol, c.address)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set(HeaderRequestID, req.RequestID)

	resp, err := c.streamingHTTPClient.Do(httpReq)
	if err != nil {
		if shouldRecordFailure(err) {
			c.breaker.RecordFailure()
		}
		return nil, fmt.Errorf("failed to call worker: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		c.breaker.RecordFailure()
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("worker error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	// Create channel for chunks
	chunks := make(chan *types.TokenChunk, 100)

	go func() {
		defer close(chunks)
		defer resp.Body.Close()

		decoder := json.NewDecoder(resp.Body)
		index := 0
		recordedSuccess := false

		for {
			select {
			case <-ctx.Done():
				return
			default:
				var chunk struct {
					Delta        string  `json:"delta"`
					FinishReason *string `json:"finish_reason"`
					Usage        *struct {
						PromptTokens     int `json:"prompt_tokens"`
						CompletionTokens int `json:"completion_tokens"`
						TotalTokens      int `json:"total_tokens"`
					} `json:"usage"`
				}

				if err := decoder.Decode(&chunk); err != nil {
					if err == io.EOF {
						if !recordedSuccess {
							c.breaker.RecordFailure()
						}
						return
					}
					if !recordedSuccess && shouldRecordFailure(err) {
						c.breaker.RecordFailure()
						return
					}
					slog.Debug("worker stream decode error after stream start",
						slog.String("worker_address", c.address),
						slog.String("request_id", req.RequestID),
						slog.Int("chunk_index", index),
						slog.String("error", err.Error()),
					)
					// Try to continue on parse errors after the stream has already started.
					continue
				}

				if !recordedSuccess {
					c.breaker.RecordSuccess()
					recordedSuccess = true
				}

				tokenChunk := &types.TokenChunk{
					RequestID: req.RequestID,
					Index:     index,
					Delta:     chunk.Delta,
					CreatedAt: time.Now(),
				}

				if chunk.FinishReason != nil {
					reason := types.FinishReason(*chunk.FinishReason)
					tokenChunk.FinishReason = &reason
				}

				if chunk.Usage != nil {
					tokenChunk.Usage = &types.UsageStats{
						PromptTokens:     chunk.Usage.PromptTokens,
						CompletionTokens: chunk.Usage.CompletionTokens,
						TotalTokens:      chunk.Usage.TotalTokens,
					}
				}

				chunks <- tokenChunk
				index++

				if chunk.FinishReason != nil {
					return
				}
			}
		}
	}()

	return chunks, nil
}

// HealthCheck checks if the worker is healthy.
func (c *WorkerClient) HealthCheck() error {
	// Use HTTPS for RunPod proxy URLs, HTTP for localhost
	protocol := "http"
	if strings.Contains(c.address, ".proxy.runpod.net") || strings.Contains(c.address, ".runpod.") {
		protocol = "https"
	}
	url := fmt.Sprintf("%s://%s/health", protocol, c.address)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("worker unhealthy: status %d", resp.StatusCode)
	}

	return nil
}
