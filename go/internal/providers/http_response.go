package providers

import (
	"fmt"
	"io"
)

const (
	MaxProviderResponseBytes      int64 = 8 << 20
	ProviderErrorResponseTooLarge       = "response_too_large"
)

func ReadResponseBody(provider ProviderType, body io.Reader) ([]byte, error) {
	payload, err := io.ReadAll(io.LimitReader(body, MaxProviderResponseBytes+1))
	if err != nil {
		return nil, &ProviderError{Provider: provider, Code: ProviderErrorRequestFailed, Message: "failed to read provider response"}
	}
	if int64(len(payload)) > MaxProviderResponseBytes {
		return nil, &ProviderError{
			Provider: provider,
			Code:     ProviderErrorResponseTooLarge,
			Message:  fmt.Sprintf("provider response exceeds %d bytes", MaxProviderResponseBytes),
		}
	}
	return payload, nil
}
