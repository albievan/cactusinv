package smtp

import (
	"context"
	"errors"

	"github.com/albievan/cactusinv/internal/ports"
)

// Resolver returns the mail server to use for the next message; ok=false means "use the
// fallback".
type Resolver func(ctx context.Context) (cfg Config, ok bool, err error)

// Dynamic sends through the server configured in the app (Settings → E-mail server), read
// at send time so changes apply immediately, and otherwise through Fallback.
type Dynamic struct {
	Resolve  Resolver
	Fallback ports.Mailer
}

var _ ports.Mailer = Dynamic{}

// Send implements ports.Mailer.
func (d Dynamic) Send(ctx context.Context, m ports.Mail) error {
	cfg, ok, err := d.Resolve(ctx)
	if err != nil {
		return err
	}
	if !ok {
		if d.Fallback == nil {
			return errors.New("no e-mail server is configured: set one under Settings → E-mail server")
		}
		return d.Fallback.Send(ctx, m)
	}
	mailer, err := New(cfg)
	if err != nil {
		return err
	}
	return mailer.Send(ctx, m)
}
