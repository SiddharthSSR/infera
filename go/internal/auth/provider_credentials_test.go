package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"testing"
)

func TestProviderCredentialCipherDecryptsLegacyCiphertext(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	encodedKey := base64.StdEncoding.EncodeToString(key)
	workspaceID, provider, field := "ws-legacy", "runpod", "api_key"
	plaintext := "legacy-provider-key"

	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, aead.NonceSize())
	for i := range nonce {
		nonce[i] = byte(i + 1)
	}
	legacyPayload := aead.Seal(nonce, nonce, []byte(plaintext), providerCredentialAAD(workspaceID, provider, field))
	legacyCiphertext := providerCredentialCiphertextPrefix + base64.RawStdEncoding.EncodeToString(legacyPayload)

	cipher, err := newProviderCredentialCipher(encodedKey)
	if err != nil {
		t.Fatalf("new provider credential cipher: %v", err)
	}
	decrypted, err := cipher.decrypt(legacyCiphertext, workspaceID, provider, field)
	if err != nil {
		t.Fatalf("decrypt legacy provider credential: %v", err)
	}
	if decrypted != plaintext {
		t.Fatalf("legacy provider credential changed: got %q want %q", decrypted, plaintext)
	}
}
