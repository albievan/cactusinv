// Package argon2 implements ports.PasswordHasher with argon2id (PHC string format).
package argon2

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"

	"github.com/albievan/simpleinvoice/internal/ports"
)

// Hasher hashes with argon2id. The zero value uses OWASP-recommended parameters.
type Hasher struct {
	Memory  uint32 // KiB
	Time    uint32
	Threads uint8
}

var _ ports.PasswordHasher = Hasher{}

// keyLen is the derived key length in bytes.
const keyLen = 32

func (h Hasher) params() (uint32, uint32, uint8) {
	m, t, p := h.Memory, h.Time, h.Threads
	if m == 0 {
		m = 64 * 1024
	}
	if t == 0 {
		t = 3
	}
	if p == 0 {
		p = 2
	}
	return m, t, p
}

// Hash implements ports.PasswordHasher.
func (h Hasher) Hash(password string) (string, error) {
	m, t, p := h.params()
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("argon2.Hash: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, t, m, p, keyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, m, t, p, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// Verify implements ports.PasswordHasher in constant time.
func (Hasher) Verify(hash, password string) (bool, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("argon2.Verify: unsupported hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errors.New("argon2.Verify: unsupported version")
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil || m == 0 || t == 0 || p == 0 || m > 1<<21 || t > 20 {
		return false, errors.New("argon2.Verify: bad parameters")
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil {
		return false, errors.New("argon2.Verify: bad salt")
	}
	want, err := enc.DecodeString(parts[5])
	if err != nil || len(want) != keyLen {
		return false, errors.New("argon2.Verify: bad key")
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, keyLen)
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
