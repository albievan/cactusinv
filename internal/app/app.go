// Package app holds cactusInv's use cases. It depends only on the domain and on ports;
// every use case that changes state runs inside one ports.TxManager transaction.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// Deps are the ports the application needs.
type Deps struct {
	Tx       ports.TxManager
	Clock    ports.Clock
	IDs      ports.IDGenerator
	Mailer   ports.Mailer
	Renderer ports.InvoiceRenderer
	Hasher   ports.PasswordHasher
	Archive  ports.InvoiceArchive
	Cipher   ports.Cipher // encrypts the SMTP password; nil disables storing one
	Logger   *slog.Logger
	// SessionTTL is how long a login lasts (default 7 days).
	SessionTTL time.Duration
}

// Service exposes every use case. It is safe for concurrent use.
type Service struct {
	tx       ports.TxManager
	clock    ports.Clock
	ids      ports.IDGenerator
	mailer   ports.Mailer
	renderer ports.InvoiceRenderer
	hasher   ports.PasswordHasher
	archive  ports.InvoiceArchive
	cipher   ports.Cipher
	log      *slog.Logger
	ttl      time.Duration
}

// New builds a Service.
func New(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	if d.SessionTTL <= 0 {
		d.SessionTTL = 7 * 24 * time.Hour
	}
	return &Service{
		tx: d.Tx, clock: d.Clock, ids: d.IDs, mailer: d.Mailer, renderer: d.Renderer,
		hasher: d.Hasher, archive: d.Archive, cipher: d.Cipher, log: d.Logger, ttl: d.SessionTTL,
	}
}

// now is the current UTC time at the database's microsecond precision.
func (s *Service) now() time.Time { return s.clock.Now().UTC().Truncate(time.Microsecond) }

// business returns the stored profile or sensible defaults before first save.
func business(ctx context.Context, r ports.Repos) (domain.BusinessProfile, error) {
	b, err := r.Business.Get(ctx)
	if errors.Is(err, domain.ErrNotFound) {
		b = domain.BusinessProfile{Name: "My Business"}
		_ = b.Validate()
		return b, nil
	}
	return b, err
}

// today is the current date in the business time zone.
func (s *Service) today(b domain.BusinessProfile) domain.Date {
	return domain.DateOf(s.now().In(b.Location()))
}

// event records an invoice action in the audit trail.
func (s *Service) event(ctx context.Context, r ports.Repos, invoiceID, typ, detail string) error {
	return s.audit(ctx, r, "invoice."+typ, "invoice", invoiceID, detail)
}

// wrap adds the use-case name to an error, keeping errors.Is working.
func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, err)
}

// stopping reports whether ctx is cancelled: workers finish quietly on shutdown.
func stopping(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

func activeText(active bool) string {
	if active {
		return "active"
	}
	return "inactive"
}
