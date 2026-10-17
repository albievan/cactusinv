package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// SystemView is System Settings as shown to an administrator: the saved settings and the
// deployment's folder mapping now.
type SystemView struct {
	Settings   domain.SystemSettings
	Deployment Deployment
}

// GetSystemSettings returns System Settings (defaults before first save).
func (s *Service) GetSystemSettings(ctx context.Context) (SystemView, error) {
	st, err := s.systemSettings(ctx, s.tx.Repos())
	if err != nil {
		return SystemView{}, wrap("app.GetSystemSettings", err)
	}
	return SystemView{Settings: st, Deployment: s.deploy}, nil
}

// UpdateSystemSettings saves System Settings. The session timeouts apply at once, to every
// session; a folder mapping takes effect at the next deploy (deploy.sh --install reads it
// with FolderMappings).
func (s *Service) UpdateSystemSettings(ctx context.Context, st domain.SystemSettings) (SystemView, error) {
	if err := st.Validate(); err != nil {
		return SystemView{}, wrap("app.UpdateSystemSettings", err)
	}
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		if err := r.SystemSettings.Save(ctx, st); err != nil {
			return err
		}
		return s.audit(ctx, r, "settings.system_updated", "settings", "system", systemSummary(st))
	})
	if err != nil {
		return SystemView{}, wrap("app.UpdateSystemSettings", err)
	}
	return s.GetSystemSettings(ctx)
}

func systemSummary(st domain.SystemSettings) string {
	idle := "no idle timeout"
	if st.SessionIdle > 0 {
		idle = "idle timeout " + st.SessionIdle.String()
	}
	dir := func(d string) string {
		if d == "" {
			return "the deployment's"
		}
		return d
	}
	return fmt.Sprintf("%s, sessions at most %s; backups → %s, invoices → %s", idle, st.SessionMax, dir(st.BackupHostDir), dir(st.InvoicesHostDir))
}

// FolderMappings returns the host folders System Settings map to the container's folders,
// as deploy.sh reads them: BACKUP_DIR and INVOICES_DIR, only those that are set.
func (s *Service) FolderMappings(ctx context.Context) (map[string]string, error) {
	st, err := s.systemSettings(ctx, s.tx.Repos())
	if err != nil {
		return nil, wrap("app.FolderMappings", err)
	}
	out := map[string]string{}
	if st.BackupHostDir != "" {
		out["BACKUP_DIR"] = st.BackupHostDir
	}
	if st.InvoicesHostDir != "" {
		out["INVOICES_DIR"] = st.InvoicesHostDir
	}
	return out, nil
}

// PendingFolders reports whether a saved mapping differs from what is mounted now (only
// known on Docker, where deploy.sh says what it mounted).
func (v SystemView) PendingFolders() bool {
	d := v.Deployment
	if d.Target != "dockerdesktop" {
		return false
	}
	abs := func(p string) string {
		if rel, ok := strings.CutPrefix(p, "./"); ok && d.ProjectDir != "" {
			p = strings.TrimRight(d.ProjectDir, "/") + "/" + rel
		}
		return strings.TrimRight(p, "/")
	}
	differs := func(want, now string) bool { return want != "" && abs(want) != abs(now) }
	return differs(v.Settings.BackupHostDir, d.BackupHostDir) || differs(v.Settings.InvoicesHostDir, d.InvoicesHostDir)
}

// systemSettings returns the saved settings, or the defaults (sessions of Deps.SessionTTL).
func (s *Service) systemSettings(ctx context.Context, r ports.Repos) (domain.SystemSettings, error) {
	st, err := r.SystemSettings.Get(ctx)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.DefaultSystemSettings(s.ttl), nil
	}
	return st, err
}
