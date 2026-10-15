package mariadb

import (
	"context"
	"fmt"

	"github.com/albievan/cactusinv/internal/adapters/mariadb/sqlcgen"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

type auditRepo struct{ q *sqlcgen.Queries }

func toAudit(r sqlcgen.AuditLog) domain.AuditEntry {
	return domain.AuditEntry{
		Seq: r.Seq, At: r.At.UTC(), ActorID: r.ActorID, ActorName: r.ActorName, Action: r.Action,
		EntityType: r.EntityType, EntityID: r.EntityID, Summary: r.Summary, PrevHash: r.PrevHash, Hash: r.Hash,
	}
}

func toAudits(rows []sqlcgen.AuditLog) []domain.AuditEntry {
	out := make([]domain.AuditEntry, len(rows))
	for i, r := range rows {
		out[i] = toAudit(r)
	}
	return out
}

// Append locks the chain head (serialising writers), seals the entry onto it, inserts
// it and advances the head — all in the caller's transaction.
func (r auditRepo) Append(ctx context.Context, e *domain.AuditEntry) error {
	prev, err := r.q.LockAuditHead(ctx)
	if err != nil {
		return mapErr(err)
	}
	seq, err := r.q.NextAuditSeq(ctx)
	if err != nil {
		return mapErr(err)
	}
	e.Seal(prev)
	e.Seq = seq
	return r.insert(ctx, *e)
}

// AppendVerbatim inserts an already sealed entry, refusing one that does not chain from
// the locked head.
func (r auditRepo) AppendVerbatim(ctx context.Context, e domain.AuditEntry) error {
	prev, err := r.q.LockAuditHead(ctx)
	if err != nil {
		return mapErr(err)
	}
	seq, err := r.q.NextAuditSeq(ctx)
	if err != nil {
		return mapErr(err)
	}
	if e.Seq != seq || e.PrevHash != prev || e.ComputeHash(prev) != e.Hash {
		return fmt.Errorf("%w: audit entry #%d does not chain from the head (#%d)", domain.ErrConflict, e.Seq, seq-1)
	}
	return r.insert(ctx, e)
}

func (r auditRepo) insert(ctx context.Context, e domain.AuditEntry) error {
	if err := r.q.InsertAudit(ctx, sqlcgen.InsertAuditParams{
		Seq: e.Seq, At: e.At.UTC(), ActorID: e.ActorID, ActorName: e.ActorName, Action: e.Action, EntityType: e.EntityType,
		EntityID: e.EntityID, Summary: e.Summary, PrevHash: e.PrevHash, Hash: e.Hash,
	}); err != nil {
		return mapErr(err)
	}
	return mapErr(r.q.SetAuditHead(ctx, e.Hash))
}

func (r auditRepo) List(ctx context.Context, f ports.AuditFilter) ([]domain.AuditEntry, int, error) {
	p := f.Page.Normalize()
	rows, err := r.q.ListAudit(ctx, sqlcgen.ListAuditParams{
		EntityType: f.EntityType, EntityID: f.EntityID, Actor: f.Actor, Action: f.Action, Limit: i32(p.Limit), Offset: i32(p.Offset),
	})
	if err != nil {
		return nil, 0, mapErr(err)
	}
	n, err := r.q.CountAudit(ctx, sqlcgen.CountAuditParams{EntityType: f.EntityType, EntityID: f.EntityID, Actor: f.Actor, Action: f.Action})
	if err != nil {
		return nil, 0, mapErr(err)
	}
	return toAudits(rows), int(n), nil
}

func (r auditRepo) ForEntity(ctx context.Context, entityType, entityID string) ([]domain.AuditEntry, error) {
	rows, err := r.q.ListAuditForEntity(ctx, sqlcgen.ListAuditForEntityParams{EntityType: entityType, EntityID: entityID})
	return toAudits(rows), mapErr(err)
}

func (r auditRepo) Chain(ctx context.Context, after int64, limit int) ([]domain.AuditEntry, error) {
	rows, err := r.q.ListAuditChain(ctx, sqlcgen.ListAuditChainParams{Seq: after, Limit: i32(limit)})
	return toAudits(rows), mapErr(err)
}
