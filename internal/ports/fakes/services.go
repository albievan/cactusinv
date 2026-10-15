package fakes

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/albievan/cactusinv/internal/ports"
)

// Mailer records sent mail and can be told to fail.
type Mailer struct {
	mu   sync.Mutex
	Sent []ports.Mail
	Err  error
}

// Send implements ports.Mailer.
func (m *Mailer) Send(_ context.Context, mail ports.Mail) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return m.Err
	}
	m.Sent = append(m.Sent, mail)
	return nil
}

// Count returns how many messages were sent.
func (m *Mailer) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Sent)
}

// Renderer returns a tiny fake PDF naming the invoice number.
type Renderer struct {
	Err error
}

// RenderPDF implements ports.InvoiceRenderer.
func (r Renderer) RenderPDF(_ context.Context, doc ports.InvoiceDocument) ([]byte, error) {
	if r.Err != nil {
		return nil, r.Err
	}
	return []byte("%PDF-fake " + doc.Invoice.Number), nil
}

// Archive records saved invoice files.
type Archive struct {
	mu    sync.Mutex
	Files map[string][]byte
	Err   error
}

// Save implements ports.InvoiceArchive.
func (a *Archive) Save(_ context.Context, path string, data []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.Err != nil {
		return a.Err
	}
	if a.Files == nil {
		a.Files = map[string][]byte{}
	}
	a.Files[path] = data
	return nil
}

// RenderStatement implements ports.InvoiceRenderer.
func (r Renderer) RenderStatement(_ context.Context, doc ports.StatementDocument) ([]byte, error) {
	if r.Err != nil {
		return nil, r.Err
	}
	return []byte("%PDF-statement " + doc.Statement.Customer.Code), nil
}

// Hasher is a reversible fake password hasher ("hash:" + password).
type Hasher struct{}

// Hash implements ports.PasswordHasher.
func (Hasher) Hash(pw string) (string, error) { return "hash:" + pw, nil }

// Verify implements ports.PasswordHasher.
func (Hasher) Verify(hash, pw string) (bool, error) {
	if !strings.HasPrefix(hash, "hash:") {
		return false, errors.New("bad hash")
	}
	return hash == "hash:"+pw, nil
}

// Secrets is a map-backed secret store.
type Secrets map[string]string

// Get implements ports.Secrets.
func (s Secrets) Get(_ context.Context, key string) (string, error) {
	v, ok := s[key]
	if !ok {
		return "", errors.New("secret not found: " + key)
	}
	return v, nil
}

// Cipher is a reversible fake cipher ("enc:" prefix).
type Cipher struct{}

// Encrypt implements ports.Cipher.
func (Cipher) Encrypt(p []byte) ([]byte, error) { return append([]byte("enc:"), p...), nil }

// Decrypt implements ports.Cipher.
func (Cipher) Decrypt(c []byte) ([]byte, error) {
	if !strings.HasPrefix(string(c), "enc:") {
		return nil, errors.New("bad ciphertext")
	}
	return c[4:], nil
}
