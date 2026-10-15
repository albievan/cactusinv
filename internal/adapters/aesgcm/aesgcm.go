// Package aesgcm implements ports.Cipher with AES-256-GCM, used to keep the SMTP password
// encrypted at rest. The key is the app_key secret (64 hex characters).
package aesgcm

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/albievan/cactusinv/internal/ports"
)

// Cipher encrypts with one 256-bit key.
type Cipher struct {
	aead cipher.AEAD
}

var _ ports.Cipher = (*Cipher)(nil)

// New parses a hex-encoded 32-byte key.
func New(hexKey string) (*Cipher, error) {
	key, err := hex.DecodeString(strings.TrimSpace(hexKey))
	if err != nil || len(key) != 32 {
		return nil, errors.New("aesgcm.New: app_key must be 64 hex characters (32 bytes)")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aesgcm.New: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("aesgcm.New: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt returns nonce || ciphertext.
func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt reverses Encrypt.
func (c *Cipher) Decrypt(ciphertext []byte) ([]byte, error) {
	n := c.aead.NonceSize()
	if len(ciphertext) < n {
		return nil, errors.New("aesgcm.Decrypt: ciphertext too short")
	}
	out, err := c.aead.Open(nil, ciphertext[:n], ciphertext[n:], nil)
	if err != nil {
		return nil, errors.New("aesgcm.Decrypt: wrong key or corrupted data")
	}
	return out, nil
}
