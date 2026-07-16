package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

const providerCredentialCiphertextPrefix = "enc:v1:"

type providerCredentialCipher struct {
	aead cipher.AEAD
}

func newProviderCredentialCipher(encodedKey string) (*providerCredentialCipher, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedKey))
	if err != nil {
		return nil, fmt.Errorf("provider credential encryption key must be base64 encoded: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("provider credential encryption key must decode to exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize provider credential encryption: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize provider credential encryption: %w", err)
	}
	return &providerCredentialCipher{aead: aead}, nil
}

func (c *providerCredentialCipher) encrypt(value, workspaceID, provider, field string) (string, error) {
	if value == "" {
		return "", nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate provider credential nonce: %w", err)
	}
	ciphertext := c.aead.Seal(nonce, nonce, []byte(value), providerCredentialAAD(workspaceID, provider, field))
	return providerCredentialCiphertextPrefix + base64.RawStdEncoding.EncodeToString(ciphertext), nil
}

func (c *providerCredentialCipher) decrypt(value, workspaceID, provider, field string) (string, error) {
	if value == "" {
		return "", nil
	}
	if !strings.HasPrefix(value, providerCredentialCiphertextPrefix) {
		return "", fmt.Errorf("provider credential is not encrypted")
	}
	payload, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, providerCredentialCiphertextPrefix))
	if err != nil {
		return "", fmt.Errorf("provider credential ciphertext is invalid: %w", err)
	}
	if len(payload) < c.aead.NonceSize() {
		return "", fmt.Errorf("provider credential ciphertext is truncated")
	}
	nonce, ciphertext := payload[:c.aead.NonceSize()], payload[c.aead.NonceSize():]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, providerCredentialAAD(workspaceID, provider, field))
	if err != nil {
		return "", fmt.Errorf("provider credential could not be decrypted")
	}
	return string(plaintext), nil
}

func providerCredentialAAD(workspaceID, provider, field string) []byte {
	return []byte(workspaceID + "\x00" + provider + "\x00" + field)
}
