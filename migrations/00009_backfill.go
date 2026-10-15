package migrations

import (
	"context"
	"database/sql"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/albievan/cactusinv/internal/domain"
)

// backfillAudit copies the pre-audit invoice history (invoice_events) into the hash-chained
// audit_log, oldest first, attributed to "unknown" because the actor was not recorded then.
func backfillAudit(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT invoice_id, type, detail, at FROM invoice_events ORDER BY at, id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var entries []domain.AuditEntry
	for rows.Next() {
		var invoiceID, typ, detail string
		var at time.Time
		if err := rows.Scan(&invoiceID, &typ, &detail, &at); err != nil {
			return err
		}
		entries = append(entries, domain.AuditEntry{
			At: at, ActorID: "", ActorName: "unknown (before the audit log)", Action: "invoice." + typ,
			EntityType: "invoice", EntityID: invoiceID, Summary: detail,
		})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	prev := ""
	if err := tx.QueryRowContext(ctx, `SELECT last_hash FROM audit_head WHERE id = 1 FOR UPDATE`).Scan(&prev); err != nil {
		return err
	}
	for i := range entries {
		e := &entries[i]
		e.Seal(prev)
		if _, err := tx.ExecContext(ctx, `INSERT INTO audit_log (at, actor_id, actor_name, action, entity_type, entity_id, summary, prev_hash, hash)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, e.At, e.ActorID, e.ActorName, e.Action, e.EntityType, e.EntityID, e.Summary, e.PrevHash, e.Hash); err != nil {
			return err
		}
		prev = e.Hash
	}
	_, err = tx.ExecContext(ctx, `UPDATE audit_head SET last_hash = ? WHERE id = 1`, prev)
	return err
}

// GoMigrations are the migrations written in Go; pass them to the goose provider.
func GoMigrations() []*goose.Migration {
	return []*goose.Migration{
		goose.NewGoMigration(9, &goose.GoFunc{RunTx: backfillAudit}, &goose.GoFunc{RunTx: func(context.Context, *sql.Tx) error { return nil }}),
	}
}
