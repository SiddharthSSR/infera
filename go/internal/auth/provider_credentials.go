package auth

import (
	"github.com/infera/infera/go/internal/secretbox"
)

const providerCredentialCiphertextPrefix = secretbox.CiphertextPrefix

type providerCredentialCipher struct {
	box *secretbox.Box
}

func newProviderCredentialCipher(encodedKey string) (*providerCredentialCipher, error) {
	box, err := secretbox.New(encodedKey, "provider credential")
	if err != nil {
		return nil, err
	}
	return &providerCredentialCipher{box: box}, nil
}

func (c *providerCredentialCipher) encrypt(value, workspaceID, provider, field string) (string, error) {
	return c.box.Encrypt(value, providerCredentialAAD(workspaceID, provider, field))
}

func (c *providerCredentialCipher) decrypt(value, workspaceID, provider, field string) (string, error) {
	return c.box.Decrypt(value, providerCredentialAAD(workspaceID, provider, field))
}

func providerCredentialAAD(workspaceID, provider, field string) []byte {
	return []byte(workspaceID + "\x00" + provider + "\x00" + field)
}
