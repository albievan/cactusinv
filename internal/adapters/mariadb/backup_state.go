package mariadb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/albievan/cactusinv/internal/adapters/mariadb/sqlcgen"
	"github.com/albievan/cactusinv/internal/domain"
)

type systemRepo struct{ q *sqlcgen.Queries }

func toSystemState(r sqlcgen.SystemState) (domain.SystemState, error) {
	s := domain.SystemState{
		AutomationHeld: r.AutomationHeld, HeldAt: fromNullTime(r.HeldAt), HeldBy: r.HeldBy, HeldReason: r.HeldReason, HeldRestoreID: r.HeldRestoreID.String,
		RestoreID: r.RestoreID.String, RestoreExpiresAt: fromNullTime(r.RestoreExpiresAt), RestoreSource: r.RestoreSource,
		RestoreSHA256: r.RestoreSha256, RestoreStagedBy: r.RestoreStagedBy,
	}
	if r.RestoreHeader.Valid && r.RestoreHeader.String != "" {
		h, err := domain.ParseBackupHeader([]byte(r.RestoreHeader.String))
		if err != nil {
			return domain.SystemState{}, fmt.Errorf("mariadb.systemRepo: staged header: %w", err)
		}
		s.RestoreHeader = h
	}
	return s, nil
}

func (r systemRepo) Get(ctx context.Context) (domain.SystemState, error) {
	row, err := r.q.GetSystemState(ctx)
	if err != nil {
		return domain.SystemState{}, mapErr(err)
	}
	return toSystemState(row)
}

func (r systemRepo) Lock(ctx context.Context) (domain.SystemState, error) {
	row, err := r.q.LockSystemState(ctx)
	if err != nil {
		return domain.SystemState{}, mapErr(err)
	}
	return toSystemState(row)
}

func (r systemRepo) Save(ctx context.Context, s domain.SystemState) error {
	header := sql.NullString{}
	if s.RestoreID != "" {
		data, err := json.Marshal(s.RestoreHeader)
		if err != nil {
			return fmt.Errorf("mariadb.systemRepo.Save: %w", err)
		}
		header = sql.NullString{String: string(data), Valid: true}
	}
	n, err := r.q.SaveSystemState(ctx, sqlcgen.SaveSystemStateParams{
		AutomationHeld: s.AutomationHeld, HeldAt: nullTime(s.HeldAt), HeldBy: s.HeldBy, HeldReason: s.HeldReason, HeldRestoreID: nullString(s.HeldRestoreID),
		RestoreID: nullString(s.RestoreID), RestoreExpiresAt: nullTime(s.RestoreExpiresAt), RestoreSource: s.RestoreSource,
		RestoreSha256: s.RestoreSHA256, RestoreStagedBy: s.RestoreStagedBy, RestoreHeader: header,
	})
	return affected(n, err, domain.ErrNotFound)
}

type backupRunRepo struct{ q *sqlcgen.Queries }

func (r backupRunRepo) Start(ctx context.Context, run domain.BackupRun) error {
	return mapErr(r.q.InsertBackupRun(ctx, sqlcgen.InsertBackupRunParams{
		Slot: run.Slot.UTC(), Name: run.Name, Result: string(run.Result), Error: run.Error,
		StartedAt: run.StartedAt.UTC(), FinishedAt: nullTime(run.FinishedAt),
	}))
}

func (r backupRunRepo) Finish(ctx context.Context, run domain.BackupRun) error {
	n, err := r.q.FinishBackupRun(ctx, sqlcgen.FinishBackupRunParams{
		Name: run.Name, Result: string(run.Result), Error: run.Error, FinishedAt: nullTime(run.FinishedAt), Slot: run.Slot.UTC(),
	})
	return affected(n, err, domain.ErrNotFound)
}

func (r backupRunRepo) Latest(ctx context.Context) (domain.BackupRun, error) {
	row, err := r.q.LatestBackupRun(ctx)
	if err != nil {
		return domain.BackupRun{}, mapErr(err)
	}
	return domain.BackupRun{
		Slot: row.Slot.UTC(), Name: row.Name, Result: domain.BackupRunResult(row.Result), Error: row.Error,
		StartedAt: row.StartedAt.UTC(), FinishedAt: fromNullTime(row.FinishedAt),
	}, nil
}
