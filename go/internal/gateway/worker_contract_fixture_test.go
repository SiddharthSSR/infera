package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/infera/infera/go/pkg/types"
)

func TestWorkerClientInferMatchesSharedContractFixture(t *testing.T) {
	requestFixture := loadWorkerInferRequestFixture(t, "infer_request_tool_calls.json")
	responseFixture := loadWorkerFixtureBytes(t, "infer_response_tool_calls.json")

	client := NewWorkerClient("http://localhost:8081")
	client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/infer" {
			t.Fatalf("expected /infer request, got %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		assertJSONEqual(t, requestFixture.raw, body)
		return jsonHTTPResponse(http.StatusOK, string(responseFixture)), nil
	})

	resp, err := client.InferWithContext(context.Background(), buildInferenceRequest(requestFixture.request))
	if err != nil {
		t.Fatalf("InferWithContext: %v", err)
	}

	if len(resp.Choices) != 1 {
		t.Fatalf("expected one choice, got %d", len(resp.Choices))
	}
	if len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected tool calls in response, got %+v", resp.Choices[0].Message.ToolCalls)
	}
	if resp.Choices[0].Message.ToolCalls[0].Function.Name != "web_search" {
		t.Fatalf("expected decoded tool call, got %+v", resp.Choices[0].Message.ToolCalls[0])
	}
	if resp.Choices[0].FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("expected tool_calls finish reason, got %s", resp.Choices[0].FinishReason)
	}
}

func TestWorkerClientInferStreamMatchesSharedContractFixture(t *testing.T) {
	requestFixture := loadWorkerInferRequestFixture(t, "infer_stream_request_tool_calls.json")
	chunkFixture := loadWorkerFixtureBytes(t, "infer_stream_chunk_tool_calls.json")

	client := NewWorkerClient("http://localhost:8081")
	client.streamingHTTPClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/infer/stream" {
			t.Fatalf("expected /infer/stream request, got %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		assertJSONEqual(t, requestFixture.raw, body)
		var chunk any
		if err := json.Unmarshal(chunkFixture, &chunk); err != nil {
			t.Fatalf("decode chunk fixture: %v", err)
		}
		compact, err := json.Marshal(chunk)
		if err != nil {
			t.Fatalf("compact chunk fixture: %v", err)
		}
		return jsonHTTPResponse(http.StatusOK, string(compact)+"\n"), nil
	})

	chunks, err := client.InferStream(context.Background(), buildInferenceRequest(requestFixture.request))
	if err != nil {
		t.Fatalf("InferStream: %v", err)
	}

	var got []*types.TokenChunk
	for chunk := range chunks {
		got = append(got, chunk)
	}
	if len(got) != 1 {
		t.Fatalf("expected one chunk, got %d", len(got))
	}
	if got[0].RequestID != requestFixture.request.RequestID {
		t.Fatalf("expected request_id %s, got %s", requestFixture.request.RequestID, got[0].RequestID)
	}
	if len(got[0].ToolCalls) != 1 || got[0].ToolCalls[0].Function.Name != "web_search" {
		t.Fatalf("expected decoded tool-call delta, got %+v", got[0].ToolCalls)
	}
	if got[0].FinishReason == nil || *got[0].FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("expected tool_calls finish reason, got %+v", got[0].FinishReason)
	}
	if got[0].Usage == nil || got[0].Usage.TotalTokens != 6 {
		t.Fatalf("expected usage in chunk, got %+v", got[0].Usage)
	}
}

type workerInferRequestFixture struct {
	raw     []byte
	request WorkerInferRequest
}

func loadWorkerInferRequestFixture(t *testing.T, name string) workerInferRequestFixture {
	t.Helper()

	data := loadWorkerFixtureBytes(t, name)
	var request WorkerInferRequest
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}

	return workerInferRequestFixture{raw: data, request: request}
}

func loadWorkerFixtureBytes(t *testing.T, name string) []byte {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "contracts", "worker_http", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func buildInferenceRequest(request WorkerInferRequest) *types.InferenceRequest {
	messages := make([]types.Message, len(request.Messages))
	for i, msg := range request.Messages {
		messages[i] = types.Message{
			Role:       types.Role(msg.Role),
			Content:    msg.Content,
			Name:       msg.Name,
			ToolCalls:  msg.ToolCalls,
			ToolCallID: msg.ToolCallID,
		}
	}

	return &types.InferenceRequest{
		RequestID:  request.RequestID,
		ModelID:    request.ModelID,
		Messages:   messages,
		Parameters: request.Parameters,
		Stream:     request.Stream,
		Tools:      request.Tools,
		ToolChoice: request.ToolChoice,
	}
}

func assertJSONEqual(t *testing.T, want, got []byte) {
	t.Helper()

	var wantValue any
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("decode expected JSON: %v", err)
	}
	var gotValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("decode actual JSON: %v", err)
	}

	if !reflect.DeepEqual(wantValue, gotValue) {
		t.Fatalf("json mismatch\nwant: %s\ngot: %s", strings.TrimSpace(string(want)), strings.TrimSpace(string(got)))
	}
}
