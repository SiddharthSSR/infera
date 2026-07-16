package gateway

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/infera/infera/go/pkg/types"
)

func TestValidatedWorkerURLRequiresTLSOutsideLoopback(t *testing.T) {
	for _, address := range []string{"203.0.113.10:8081", "http://worker.example:8081", "http://[2001:db8::1]:8081"} {
		if _, err := validatedWorkerURL(address, "/infer"); err == nil {
			t.Fatalf("expected insecure address %q to be rejected", address)
		}
	}
	for _, address := range []string{"http://localhost:8081", "http://127.0.0.1:8081", "http://[::1]:8081", "https://worker.example"} {
		if _, err := validatedWorkerURL(address, "/infer"); err != nil {
			t.Fatalf("expected address %q to be accepted: %v", address, err)
		}
	}
}

func TestWorkerClientBoundsErrorAndSuccessBodies(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		client := NewWorkerClient("http://localhost:8081")
		client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			return jsonHTTPResponse(http.StatusBadGateway, strings.Repeat("x", int(maxWorkerErrorBytes)+1)), nil
		})
		_, err := client.InferWithContext(context.Background(), &types.InferenceRequest{RequestID: "r", ModelID: "m"})
		if err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("expected bounded error, got %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		client := NewWorkerClient("http://localhost:8081")
		client.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			return jsonHTTPResponse(http.StatusOK, strings.Repeat(" ", int(maxWorkerResponseBytes)+1)), nil
		})
		_, err := client.InferWithContext(context.Background(), &types.InferenceRequest{RequestID: "r", ModelID: "m"})
		if err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("expected bounded response error, got %v", err)
		}
	})
}

func TestWorkerClientBoundsStreamingValue(t *testing.T) {
	client := NewWorkerClient("http://localhost:8081")
	client.streamingHTTPClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonHTTPResponse(http.StatusOK, `{"delta":"`+strings.Repeat("x", maxWorkerStreamValue)+`"}`+"\n"), nil
	})
	chunks, err := client.InferStream(context.Background(), &types.InferenceRequest{RequestID: "r", ModelID: "m"})
	if err != nil {
		t.Fatalf("InferStream: %v", err)
	}
	if chunk, ok := <-chunks; ok || chunk != nil {
		t.Fatalf("expected oversized stream value to emit no chunks")
	}
}
