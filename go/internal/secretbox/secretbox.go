// Package secretbox provides authenticated encryption for secrets persisted by
// Infera control-plane stores.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

const CiphertextPrefix = "enc:v1:"

// Box encrypts secrets with AES-256-GCM. Label is used only in errors so
// callers can retain domain-specific diagnostics.
type Box struct {
	aead  cipher.AEAD
	label string
}

func New(encodedKey, label string) (*Box, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		label = "secret"
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedKey))
	if err != nil {
		return nil, fmt.Errorf("%s encryption key must be base64 encoded: %w", label, err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("%s encryption key must decode to exactly 32 bytes", label)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize %s encryption: %w", label, err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize %s encryption: %w", label, err)
	}
	return &Box{aead: aead, label: label}, nil
}

func (b *Box) Encrypt(value string, aad []byte) (string, error) {
	if value == "" {
		return "", nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate %s nonce: %w", b.label, err)
	}
	ciphertext := b.aead.Seal(nonce, nonce, []byte(value), aad)
	return CiphertextPrefix + base64.RawStdEncoding.EncodeToString(ciphertext), nil
}

func (b *Box) Decrypt(value string, aad []byte) (string, error) {
	if value == "" {
		return "", nil
	}
	if !strings.HasPrefix(value, CiphertextPrefix) {
		return "", fmt.Errorf("%s is not encrypted", b.label)
	}
	payload, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, CiphertextPrefix))
	if err != nil {
		return "", fmt.Errorf("%s ciphertext is invalid: %w", b.label, err)
	}
	if len(payload) < b.aead.NonceSize() {
		return "", fmt.Errorf("%s ciphertext is truncated", b.label)
	}
	nonce, ciphertext := payload[:b.aead.NonceSize()], payload[b.aead.NonceSize():]
	plaintext, err := b.aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return "", fmt.Errorf("%s could not be decrypted", b.label)
	}
	return string(plaintext), nil
}
