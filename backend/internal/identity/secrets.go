package identity

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// secretBox encrypts small secrets (TOTP shared secrets) at rest with AES-256-GCM.
// The database never holds a usable TOTP secret on its own: an attacker who
// dumps the table also needs the key from the secret store.
type secretBox struct {
	aead cipher.AEAD
}

func newSecretBox(key []byte) (*secretBox, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("identity: encryption key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("identity: build cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("identity: build aead: %w", err)
	}
	return &secretBox{aead: aead}, nil
}

func (b *secretBox) seal(plain []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("identity: read nonce: %w", err)
	}
	return b.aead.Seal(nonce, nonce, plain, nil), nil
}

func (b *secretBox) open(sealed []byte) ([]byte, error) {
	nonceSize := b.aead.NonceSize()
	if len(sealed) < nonceSize+1 {
		return nil, errors.New("identity: encrypted secret is truncated")
	}
	plain, err := b.aead.Open(nil, sealed[:nonceSize], sealed[nonceSize:], nil)
	if err != nil {
		return nil, errors.New("identity: encrypted secret failed authentication")
	}
	return plain, nil
}
