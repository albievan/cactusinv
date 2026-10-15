// Package agecrypt implements ports.BackupSealer with age (filippo.io/age, ADR-010):
// stored backups are encrypted to the server key (an age identity in the backup_key
// secret), downloads with an scrypt passphrase. Either way the payload is authenticated,
// so damage, truncation and a wrong key or passphrase are all domain.ErrBackupInvalid.
package agecrypt

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"filippo.io/age"

	"github.com/albievan/cactusinv/internal/domain"
)

// DefaultWorkFactor is the scrypt work factor (2^18, about 256 MiB and a second) used for
// passphrase-encrypted backups; it is also the most Open accepts, so an uploaded file
// cannot make the server spend more.
const DefaultWorkFactor = 18

// Sealer encrypts and decrypts backups.
type Sealer struct {
	identity   age.Identity
	recipient  age.Recipient
	workFactor int
}

// Option configures a Sealer.
type Option func(*Sealer) error

// WithWorkFactor sets the scrypt work factor (1–DefaultWorkFactor) for sealing; tests use
// a small one to stay fast.
func WithWorkFactor(logN int) Option {
	return func(s *Sealer) error {
		if logN < 1 || logN > DefaultWorkFactor {
			return fmt.Errorf("agecrypt: work factor %d out of range 1-%d", logN, DefaultWorkFactor)
		}
		s.workFactor = logN
		return nil
	}
}

// New builds a Sealer. key is the contents of the backup_key secret: one age identity
// (AGE-SECRET-KEY-1… or a post-quantum AGE-SECRET-KEY-PQ-1…); empty means no server key
// (stored backups off, passphrase downloads still work).
func New(key string, opts ...Option) (*Sealer, error) {
	s := &Sealer{workFactor: DefaultWorkFactor}
	for _, o := range opts {
		if err := o(s); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(key) == "" {
		return s, nil
	}
	ids, err := age.ParseIdentities(strings.NewReader(key))
	if err != nil {
		return nil, errors.New("agecrypt: backup_key is not an age identity")
	}
	if len(ids) != 1 {
		return nil, fmt.Errorf("agecrypt: backup_key must hold one identity, found %d", len(ids))
	}
	switch id := ids[0].(type) {
	case *age.X25519Identity:
		s.identity, s.recipient = id, id.Recipient()
	case *age.HybridIdentity:
		s.identity, s.recipient = id, id.Recipient()
	default:
		return nil, errors.New("agecrypt: unsupported identity type in backup_key")
	}
	return s, nil
}

// GenerateKey returns a new X25519 identity for the backup_key secret. X25519 keys open
// with every age release (`age -d -i backup_key FILE`).
func GenerateKey() (string, error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return "", fmt.Errorf("agecrypt.GenerateKey: %w", err)
	}
	return id.String() + "\n", nil
}

// Recipient returns the public key backups are encrypted to ("" without a key).
func (s *Sealer) Recipient() string {
	if r, ok := s.recipient.(fmt.Stringer); ok {
		return r.String()
	}
	return ""
}

// KeyConfigured implements ports.BackupSealer.
func (s *Sealer) KeyConfigured() bool { return s.recipient != nil }

// SealWithKey implements ports.BackupSealer.
func (s *Sealer) SealWithKey(w io.Writer) (io.WriteCloser, error) {
	if s.recipient == nil {
		return nil, fmt.Errorf("agecrypt: %w: backup_key", domain.ErrNotConfigured)
	}
	wc, err := age.Encrypt(w, s.recipient)
	if err != nil {
		return nil, fmt.Errorf("agecrypt.SealWithKey: %w", err)
	}
	return wc, nil
}

// SealWithPassphrase implements ports.BackupSealer.
func (s *Sealer) SealWithPassphrase(w io.Writer, passphrase string) (io.WriteCloser, error) {
	if err := domain.ValidateBackupPassphrase(passphrase); err != nil {
		return nil, err
	}
	r, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, fmt.Errorf("agecrypt.SealWithPassphrase: %w", err)
	}
	r.SetWorkFactor(s.workFactor)
	wc, err := age.Encrypt(w, r)
	if err != nil {
		return nil, fmt.Errorf("agecrypt.SealWithPassphrase: %w", err)
	}
	return wc, nil
}

// Open implements ports.BackupSealer. With a passphrase it accepts passphrase-encrypted
// files (and files encrypted to the server key); without one, only the server key.
func (s *Sealer) Open(r io.Reader, passphrase string) (io.Reader, error) {
	var ids []age.Identity
	if passphrase != "" {
		si, err := age.NewScryptIdentity(passphrase)
		if err != nil {
			return nil, fmt.Errorf("agecrypt.Open: %w", domain.ErrBackupInvalid)
		}
		si.SetMaxWorkFactor(DefaultWorkFactor)
		ids = append(ids, si)
	}
	if s.identity != nil {
		ids = append(ids, s.identity)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("agecrypt.Open: %w: backup_key (or give the passphrase)", domain.ErrNotConfigured)
	}
	plain, err := age.Decrypt(r, ids...)
	if err != nil {
		return nil, fmt.Errorf("agecrypt.Open: %w", domain.ErrBackupInvalid)
	}
	return invalidOnError{plain}, nil
}

// invalidOnError reports every failure while reading the payload (authentication,
// truncation) as domain.ErrBackupInvalid.
type invalidOnError struct{ r io.Reader }

func (i invalidOnError) Read(p []byte) (int, error) {
	n, err := i.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		return n, fmt.Errorf("agecrypt: %w", domain.ErrBackupInvalid)
	}
	return n, err
}
