package providers

import (
	"errors"
	"strings"
	"testing"
)

func TestReadResponseBodyEnforcesLimitPlusOne(t *testing.T) {
	exact := strings.Repeat("x", int(MaxProviderResponseBytes))
	payload, err := ReadResponseBody(ProviderRunPod, strings.NewReader(exact))
	if err != nil {
		t.Fatalf("exact limit should remain compatible: %v", err)
	}
	if len(payload) != len(exact) {
		t.Fatalf("expected %d bytes, got %d", len(exact), len(payload))
	}

	_, err = ReadResponseBody(ProviderRunPod, strings.NewReader(exact+"sensitive-marker"))
	if err == nil {
		t.Fatal("expected oversized response error")
	}
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != ProviderErrorResponseTooLarge {
		t.Fatalf("expected %q, got %q", ProviderErrorResponseTooLarge, providerErr.Code)
	}
	if strings.Contains(providerErr.Message, "sensitive-marker") {
		t.Fatalf("oversized response leaked into error: %q", providerErr.Message)
	}
}
