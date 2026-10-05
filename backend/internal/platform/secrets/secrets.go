// Package secrets encrypts the secrets Provenly must read back (webhook signing secrets, connector tokens) before
// they are stored: AES-256-GCM with a random nonce per value, under the key in PROVENLY_SECRETS_KEY (MVP D4). Values
// are stored as "v1:" + base64(nonce || ciphertext); the version leaves room for key rotation. Secrets Provenly only
// verifies (passwords, API keys, invitations) are hashed elsewhere, never encrypted.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const prefix = "v1:"

// ErrUndecryptable is returned for a stored value that this key cannot open (another key, or tampered).
var ErrUndecryptable = errors.New("secret cannot be decrypted with the configured PROVENLY_SECRETS_KEY")

// Box encrypts and decrypts secrets with one key.
type Box struct{ aead cipher.AEAD }

// New builds a Box from a 32-byte key; nil generates a random one (secrets do not survive a restart).
func New(key []byte) (*Box, error) {
	if key == nil {
		key = make([]byte, 32)
		_, _ = rand.Read(key)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets key: %w", err)
	}
	aead, _ := cipher.NewGCM(block) // only fails for non-standard nonce or tag sizes
	return &Box{aead: aead}, nil
}

// Seal encrypts a secret for storage.
func (b *Box) Seal(plain string) string {
	nonce := make([]byte, b.aead.NonceSize())
	_, _ = rand.Read(nonce)
	return prefix + base64.StdEncoding.EncodeToString(b.aead.Seal(nonce, nonce, []byte(plain), nil))
}

// Open decrypts a stored secret.
func (b *Box) Open(stored string) (string, error) {
	raw, ok := strings.CutPrefix(stored, prefix)
	if !ok {
		return "", ErrUndecryptable
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(data) < b.aead.NonceSize() {
		return "", ErrUndecryptable
	}
	plain, err := b.aead.Open(nil, data[:b.aead.NonceSize()], data[b.aead.NonceSize():], nil)
	if err != nil {
		return "", ErrUndecryptable
	}
	return string(plain), nil
}
