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
	ext := st.ExternalURL
	if ext == "" {
		ext = "none"
	}
	hsts := "off"
	if st.HSTS {
		hsts = "on"
	}
	cf := "no"
	if st.BehindCloudflare {
		cf = "yes"
	}
	return fmt.Sprintf("%s, sessions at most %s; backups → %s, invoices → %s; external address %s; HTTPS only (HSTS) %s; behind Cloudflare %s",
		idle, st.SessionMax, dir(st.BackupHostDir), dir(st.InvoicesHostDir), ext, hsts, cf)
}

// linkBase is the address links in e-mails point at (M45): System Settings' external
// address, else the public address an older version kept under Settings → Quotes (M41),
// else requestBase — the address the action was taken from in the browser. "" means no
// address is known and no link is made.
func (s *Service) linkBase(ctx context.Context, r ports.Repos, requestBase string) (string, error) {
	st, err := s.systemSettings(ctx, r)
	if err != nil {
		return "", err
	}
	if st.ExternalURL != "" {
		return st.ExternalURL, nil
	}
	qs, err := s.quoteSettings(ctx, r)
	if err != nil {
		return "", err
	}
	if qs.PublicURL != "" {
		return qs.PublicURL, nil
	}
	return domain.ParsePublicURL(requestBase)
}

// WebSettings are the System Settings the web server applies to every request (M45).
type WebSettings struct {
	HSTS             bool // send Strict-Transport-Security on HTTPS
	BehindCloudflare bool // a visitor's address is Cloudflare's CF-Connecting-IP
}

// WebSettings returns them. A failure to read the settings answers the zero value: neither
// is ever switched on by an error.
func (s *Service) WebSettings(ctx context.Context) WebSettings {
	st, err := s.systemSettings(ctx, s.tx.Repos())
	if err != nil {
		return WebSettings{}
	}
	return WebSettings{HSTS: st.HSTS, BehindCloudflare: st.BehindCloudflare}
}

// HSTSEnabled reports whether System Settings ask browsers to use HTTPS only (M45).
func (s *Service) HSTSEnabled(ctx context.Context) bool { return s.WebSettings(ctx).HSTS }

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
// known on Docker and Podman, where deploy.sh says what it mounted).
func (v SystemView) PendingFolders() bool {
	d := v.Deployment
	if d.Target != "dockerdesktop" && d.Target != "podman" {
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
