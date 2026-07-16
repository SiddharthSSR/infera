package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/infera/infera/go/pkg/types"
)

func (g *Gateway) writeChatCompletionResponse(w http.ResponseWriter, requestID, model string, req *types.InferenceRequest, resp *types.InferenceResponse) {
	openAIResp, _ := buildChatCompletionResponse(requestID, model, req, resp)
	g.writeJSON(w, http.StatusOK, openAIResp)
}

func buildChatCompletionResponse(requestID, model string, req *types.InferenceRequest, resp *types.InferenceResponse) (ChatCompletionResponse, int) {
	promptTokens := resp.Usage.PromptTokens
	if promptTokens == 0 {
		promptTokens = req.TokenEstimate()
	}
	completionTokens := resp.Usage.CompletionTokens
	if completionTokens == 0 {
		completionTokens = estimateCompletionTokens(resp)
	}
	totalTokens := usageTotalTokens(
		promptTokens,
		completionTokens,
		resp.Usage.TotalTokens,
	)

	openAIResp := ChatCompletionResponse{
		ID:      "chatcmpl-" + requestID,
		Object:  OpenAIChatCompletionObject,
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
				Role:      string(choice.Message.Role),
				Content:   choice.Message.Content,
				ToolCalls: marshalToolCalls(choice.Message.ToolCalls),
			},
			FinishReason: string(choice.FinishReason),
		}
	}

	return openAIResp, completionTokens
}

func buildInitialChatCompletionChunk(requestID string, created int64, model string) ChatCompletionChunk {
	return ChatCompletionChunk{
		ID:      requestID,
		Object:  OpenAIChatCompletionChunkObject,
		Created: created,
		Model:   model,
		Choices: []ChatChunkChoice{{
			Index: 0,
			Delta: ChatDelta{
				Role: "assistant",
			},
		}},
	}
}

func buildStreamingChatCompletionChunk(requestID string, created int64, model string, chunk *types.TokenChunk) ChatCompletionChunk {
	openAIChunk := ChatCompletionChunk{
		ID:      requestID,
		Object:  OpenAIChatCompletionChunkObject,
		Created: created,
		Model:   model,
		Choices: []ChatChunkChoice{{
			Index: chunk.Index,
			Delta: ChatDelta{
				Content:   chunk.Delta,
				ToolCalls: marshalToolCallChunkDeltas(chunk.ToolCalls),
			},
		}},
	}

	if chunk.FinishReason != nil {
		reason := string(*chunk.FinishReason)
		openAIChunk.Choices[0].FinishReason = &reason
	}

	return openAIChunk
}

func (g *Gateway) handleStreamingInference(w http.ResponseWriter, r *http.Request, client *WorkerClient, req *types.InferenceRequest, model string) (int, string) {
	chunks, err := client.InferStream(r.Context(), req)
	if err != nil {
		g.writeError(w, http.StatusInternalServerError, OpenAIChatErrorTypeInferenceError, err.Error())
		return 0, OpenAIChatErrorTypeInferenceError
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		g.writeError(w, http.StatusInternalServerError, "streaming_not_supported", "Streaming not supported")
		return 0, "streaming_not_supported"
	}

	requestID := "chatcmpl-" + req.RequestID
	created := time.Now().Unix()
	streamStart := time.Now()

	initialChunk := buildInitialChatCompletionChunk(requestID, created, model)
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
	prevCompletionTokens := 0

	for chunk := range chunks {
		now := time.Now()
		if !firstChunkObserved {
			firstChunkObserved = true
			if g.metrics != nil {
				g.metrics.RecordTTFT(model, true, now.Sub(streamStart))
			}
		} else if g.metrics != nil {
			elapsed := now.Sub(previousChunkAt)
			tokensInChunk := 1
			if chunk.Usage != nil && chunk.Usage.CompletionTokens > prevCompletionTokens {
				tokensInChunk = chunk.Usage.CompletionTokens - prevCompletionTokens
			}
			perToken := elapsed / time.Duration(tokensInChunk)
			for i := 0; i < tokensInChunk; i++ {
				g.metrics.RecordTPOT(model, true, perToken)
			}
		}
		if chunk.Usage != nil && chunk.Usage.CompletionTokens > 0 {
			prevCompletionTokens = chunk.Usage.CompletionTokens
		}
		previousChunkAt = now

		generatedChars += len(chunk.Delta)
		if chunk.Usage != nil {
			bestPromptTokens = maxInt(bestPromptTokens, chunk.Usage.PromptTokens)
			bestCompletionTokens = maxInt(bestCompletionTokens, chunk.Usage.CompletionTokens)
			bestTotalTokens = maxInt(bestTotalTokens, chunk.Usage.TotalTokens)
		}

		openAIChunk := buildStreamingChatCompletionChunk(requestID, created, model, chunk)

		data, _ := json.Marshal(openAIChunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

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
