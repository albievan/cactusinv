package mariadb

import (
	"context"
	"time"

	"github.com/albievan/cactusinv/internal/adapters/mariadb/sqlcgen"
	"github.com/albievan/cactusinv/internal/domain"
)

type systemSettingsRepo struct{ q *sqlcgen.Queries }

func (r systemSettingsRepo) Get(ctx context.Context) (domain.SystemSettings, error) {
	row, err := r.q.GetSystemSettings(ctx)
	if err != nil {
		return domain.SystemSettings{}, mapErr(err)
	}
	return domain.SystemSettings{
		SessionIdle: time.Duration(row.SessionIdleMinutes) * time.Minute, SessionMax: time.Duration(row.SessionMaxMinutes) * time.Minute,
		BackupHostDir: row.BackupHostDir, InvoicesHostDir: row.InvoicesHostDir, Version: int(row.Version),
	}, nil
}

func (r systemSettingsRepo) Save(ctx context.Context, s domain.SystemSettings) error {
	now := time.Now().UTC()
	idle, max := i32(int(s.SessionIdle/time.Minute)), i32(int(s.SessionMax/time.Minute))
	if s.Version == 0 {
		return mapErr(r.q.InsertSystemSettings(ctx, sqlcgen.InsertSystemSettingsParams{
			SessionIdleMinutes: idle, SessionMaxMinutes: max, BackupHostDir: s.BackupHostDir, InvoicesHostDir: s.InvoicesHostDir, UpdatedAt: now,
		}))
	}
	n, err := r.q.UpdateSystemSettings(ctx, sqlcgen.UpdateSystemSettingsParams{
		SessionIdleMinutes: idle, SessionMaxMinutes: max, BackupHostDir: s.BackupHostDir, InvoicesHostDir: s.InvoicesHostDir,
		UpdatedAt: now, Version: i32(s.Version),
	})
	return affected(n, err, domain.ErrConflict)
}
