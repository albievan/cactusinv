package migrations

import (
	"context"
	"database/sql"
	"time"
	_ "time/tzdata" // the business's zone, whatever the server has installed

	"github.com/google/uuid"
)

type paidInvoice struct {
	id    string
	total int64
	at    time.Time
}

// paidWithoutPayments lists the paid invoices with a total and nothing recorded.
func paidWithoutPayments(ctx context.Context, tx *sql.Tx) ([]paidInvoice, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, total_minor, COALESCE(paid_at, updated_at) FROM invoices
		WHERE status = 'paid' AND amount_paid_minor = 0 AND total_minor > 0 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all []paidInvoice
	for rows.Next() {
		var p paidInvoice
		if err := rows.Scan(&p.id, &p.total, &p.at); err != nil {
			return nil, err
		}
		all = append(all, p)
	}
	return all, rows.Err()
}

// backfillPayments gives every paid invoice without payments one payment of its total,
// received on the day it was marked paid in the business's time zone (M58), so an upgraded
// install's balances, statements and paid-per-month figures stay as they were. It touches
// only paid invoices with nothing recorded, so running it again changes nothing.
func backfillPayments(ctx context.Context, tx *sql.Tx) error {
	loc := time.UTC
	var zone string
	if err := tx.QueryRowContext(ctx, `SELECT time_zone FROM business_profile LIMIT 1`).Scan(&zone); err == nil {
		if l, err := time.LoadLocation(zone); err == nil {
			loc = l
		}
	} else if err != sql.ErrNoRows {
		return err
	}
	all, err := paidWithoutPayments(ctx, tx)
	if err != nil {
		return err
	}
	for _, p := range all {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		on := p.at.UTC().In(loc).Format(time.DateOnly)
		if _, err := tx.ExecContext(ctx, `INSERT INTO invoice_payments (id, invoice_id, amount_minor, received_on, reference, created_at)
			VALUES (?, ?, ?, ?, '', ?)`, id.String(), p.id, p.total, on, p.at.UTC()); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE invoices SET amount_paid_minor = total_minor WHERE id = ?`, p.id); err != nil {
			return err
		}
	}
	return nil
}
