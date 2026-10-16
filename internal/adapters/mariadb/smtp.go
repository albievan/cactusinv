package mariadb

import (
	"context"
	"database/sql"
	"time"

	"github.com/albievan/cactusinv/internal/adapters/mariadb/sqlcgen"
	"github.com/albievan/cactusinv/internal/domain"
)

type smtpRepo struct{ q *sqlcgen.Queries }

// blob carries ciphertext through sqlc's NullString mapping of VARBINARY (bytes unchanged).
func blob(b []byte) sql.NullString { return sql.NullString{String: string(b), Valid: len(b) > 0} }

func (r smtpRepo) Get(ctx context.Context) (domain.SMTPSettings, []byte, error) {
	row, err := r.q.GetSMTPSettings(ctx)
	if err != nil {
		return domain.SMTPSettings{}, nil, mapErr(err)
	}
	var enc []byte // nil when no password is stored (callers test for nil)
	if row.PasswordEnc.Valid && row.PasswordEnc.String != "" {
		enc = []byte(row.PasswordEnc.String)
	}
	return domain.SMTPSettings{
		Enabled: row.Enabled, Host: row.Host, Port: int(row.Port), Username: row.Username, From: row.FromAddress,
		TLS: row.TlsMode, Version: int(row.Version),
	}, enc, nil
}

func (r smtpRepo) Save(ctx context.Context, s domain.SMTPSettings, passwordEnc []byte) error {
	now := time.Now().UTC()
	if s.Version == 0 {
		return mapErr(r.q.InsertSMTPSettings(ctx, sqlcgen.InsertSMTPSettingsParams{
			Enabled: s.Enabled, Host: s.Host, Port: i32(s.Port), Username: s.Username, PasswordEnc: blob(passwordEnc),
			FromAddress: s.From, TlsMode: s.TLS, UpdatedAt: now,
		}))
	}
	n, err := r.q.UpdateSMTPSettings(ctx, sqlcgen.UpdateSMTPSettingsParams{
		Enabled: s.Enabled, Host: s.Host, Port: i32(s.Port), Username: s.Username, PasswordEnc: blob(passwordEnc),
		FromAddress: s.From, TlsMode: s.TLS, UpdatedAt: now, Version: i32(s.Version),
	})
	return affected(n, err, domain.ErrConflict)
}
