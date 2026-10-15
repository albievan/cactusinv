package app

import (
	"context"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// Actor is who performs an action: a signed-in user or a background worker.
type Actor struct {
	ID   string
	Name string
}

// System actors used by the background workers.
var (
	ActorScheduler  = Actor{ID: "system:scheduler", Name: "Scheduler"}
	ActorDispatcher = Actor{ID: "system:dispatcher", Name: "E-mail dispatcher"}
	ActorExporter   = Actor{ID: "system:exporter", Name: "Invoice archive"}
	ActorSystem     = Actor{ID: "system", Name: "System"}
)

type actorKey struct{}

// WithActor returns ctx carrying the actor that every audit entry made with it records.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

// ActorFrom returns the actor in ctx, or ActorSystem.
func ActorFrom(ctx context.Context) Actor {
	if a, ok := ctx.Value(actorKey{}).(Actor); ok && a.Name != "" {
		return a
	}
	return ActorSystem
}

// audit appends to the tamper-evident audit trail inside the caller's transaction, so the
// record exists if and only if the change does.
func (s *Service) audit(ctx context.Context, r ports.Repos, action, entityType, entityID, summary string) error {
	a := ActorFrom(ctx)
	return r.Audit.Append(ctx, &domain.AuditEntry{
		At: s.now(), ActorID: a.ID, ActorName: a.Name, Action: action,
		EntityType: entityType, EntityID: entityID, Summary: summary,
	})
}

// auditNow records an action that changes nothing else (e.g. viewing a PDF).
func (s *Service) auditNow(ctx context.Context, action, entityType, entityID, summary string) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		return s.audit(ctx, r, action, entityType, entityID, summary)
	})
}

// ListAudit returns the audit trail, newest first.
func (s *Service) ListAudit(ctx context.Context, f ports.AuditFilter) ([]domain.AuditEntry, int, error) {
	f.Page = f.Page.Normalize()
	out, n, err := s.tx.Repos().Audit.List(ctx, f)
	return out, n, wrap("app.ListAudit", err)
}

// VerifyAudit walks the whole hash chain and reports whether it is intact.
func (s *Service) VerifyAudit(ctx context.Context) (domain.AuditVerification, error) {
	var all []domain.AuditEntry
	var after int64
	for {
		batch, err := s.tx.Repos().Audit.Chain(ctx, after, 5000)
		if err != nil {
			return domain.AuditVerification{}, wrap("app.VerifyAudit", err)
		}
		if len(batch) == 0 {
			break
		}
		all = append(all, batch...)
		after = batch[len(batch)-1].Seq
	}
	return domain.VerifyAuditChain(all), nil
}
