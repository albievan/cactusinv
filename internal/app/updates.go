package app

import (
	"context"
	"errors"
	"sync"

	"github.com/albievan/cactusinv/internal/domain"
)

// UpdateCheck answers "is there a newer version?" (System Settings; M27). A standalone
// binary can install it in place (M31); a container cannot (it has no Docker or cluster
// rights, and its image would bring the old version back): there it is ./deploy.sh --upgrade.
type UpdateCheck struct {
	// Current is the running version; Latest the newest published ("" when not read).
	Current, Latest string
	Available       bool
	// Problem says why the check could not be made ("" when it was).
	Problem string
	// CanInstall: InstallUpdate can replace this installation; else InstallProblem says why.
	CanInstall     bool
	InstallProblem string
}

// CheckForUpdate compares the running version with the newest published one. It only runs
// when asked: nothing is fetched in the background.
func (s *Service) CheckForUpdate(ctx context.Context) UpdateCheck {
	out := UpdateCheck{Current: s.version}
	if s.releases == nil {
		out.Problem = "Checking for updates is switched off on this installation (CACTUSINV_UPDATE_URL=off)."
		return out
	}
	cur, err := domain.ParseVersion(s.version)
	if err != nil {
		out.Problem = "This is a development build (" + s.version + "), which cannot be compared with a release."
		return out
	}
	published, err := s.releases.LatestVersion(ctx)
	if err != nil {
		if s.log != nil {
			s.log.WarnContext(ctx, "update check failed", "err", err)
		}
		out.Problem = "The newest version could not be read. Check that the server can reach the internet, then try again."
		return out
	}
	latest, err := domain.ParseVersion(published)
	if err != nil {
		out.Problem = "The published version could not be understood."
		return out
	}
	out.Latest, out.Available = latest.String(), cur.Less(latest)
	out.InstallProblem = s.installProblem()
	out.CanInstall = out.InstallProblem == ""
	return out
}

func (s *Service) installProblem() string {
	if s.upgrader == nil {
		return "This installation cannot replace itself."
	}
	return s.upgrader.CanInstall()
}

// Upgrade states, in order; a failed install leaves the running version in place.
const (
	UpgradeBackingUp   = "backing-up"
	UpgradeDownloading = "downloading"
	UpgradeInstalling  = "installing"
	UpgradeRestarting  = "restarting"
	UpgradeFailed      = "failed"
)

// UpgradeJob is the progress of InstallUpdate; State "" = none started since this start.
type UpgradeJob struct {
	State, From, To string
	Done, Total     int64
	Error           string
	Backup          string // the backup taken first ("" when backups are not set up)
}

type upgradeState struct {
	mu      sync.Mutex
	job     UpgradeJob
	restart func()
}

// SetRestart names what restarts the process after an install (cmd/cactusinv serve: a
// graceful shutdown, then the new binary in place of this one).
func (s *Service) SetRestart(restart func()) {
	s.upgrade.mu.Lock()
	defer s.upgrade.mu.Unlock()
	s.upgrade.restart = restart
}

// UpgradeStatus is the current (or last) install's progress.
func (s *Service) UpgradeStatus() UpgradeJob {
	s.upgrade.mu.Lock()
	defer s.upgrade.mu.Unlock()
	return s.upgrade.job
}

func (s *Service) setUpgrade(f func(*UpgradeJob)) {
	s.upgrade.mu.Lock()
	defer s.upgrade.mu.Unlock()
	f(&s.upgrade.job)
}

// errUpgrade is why InstallUpdate cannot start; its text is shown as it is.
func errUpgrade(reason string) error {
	return &domain.ValidationError{Field: "update", Reason: reason}
}

// InstallUpdate replaces this standalone installation with the newest release and restarts
// it (cactusDB's mechanism, M31): a backup first (when backups are set up), then the
// download, checked against the published SHA-256 and by running it, then the swap; the
// new version migrates the database as it starts. It returns at once; UpgradeStatus follows
// the work, and a failure leaves this version running.
func (s *Service) InstallUpdate(ctx context.Context) (UpgradeJob, error) {
	check := s.CheckForUpdate(ctx)
	switch {
	case check.Problem != "":
		return UpgradeJob{}, wrap("app.InstallUpdate", errUpgrade(check.Problem))
	case !check.Available:
		return UpgradeJob{}, wrap("app.InstallUpdate", errUpgrade("You have the newest version ("+check.Current+")."))
	case !check.CanInstall:
		return UpgradeJob{}, wrap("app.InstallUpdate", errUpgrade(check.InstallProblem))
	}
	s.upgrade.mu.Lock()
	if st := s.upgrade.job.State; st != "" && st != UpgradeFailed {
		s.upgrade.mu.Unlock()
		return UpgradeJob{}, wrap("app.InstallUpdate", domain.ErrConflict)
	}
	s.upgrade.job = UpgradeJob{State: UpgradeBackingUp, From: check.Current, To: check.Latest}
	job, restart := s.upgrade.job, s.upgrade.restart
	s.upgrade.mu.Unlock()
	// The work outlives the request; it keeps the request's actor for the audit trail.
	go s.runUpgrade(context.WithoutCancel(ctx), job, restart)
	return job, nil
}

func (s *Service) runUpgrade(ctx context.Context, job UpgradeJob, restart func()) {
	fail := func(stage string, err error) {
		if s.log != nil {
			s.log.ErrorContext(ctx, "update failed", "stage", stage, "to", job.To, "err", err)
		}
		s.setUpgrade(func(j *UpgradeJob) { j.State, j.Error = UpgradeFailed, stage+": "+err.Error() })
		_ = s.auditNow(ctx, "system.update_failed", "system", "update", "update to "+job.To+" failed while "+stage+"; "+job.From+" keeps running")
	}
	// A backup first, unless backups are not set up (no backup key): then the update goes
	// on without one, as an upgrade by hand would.
	b, err := s.CreateBackup(ctx, domain.BackupManual)
	switch {
	case err == nil:
		s.setUpgrade(func(j *UpgradeJob) { j.Backup = b.Name })
	case errors.Is(err, domain.ErrNotConfigured):
		if s.log != nil {
			s.log.WarnContext(ctx, "updating without a backup: backups are not set up", "err", err)
		}
	default:
		fail("taking the backup", err)
		return
	}
	s.setUpgrade(func(j *UpgradeJob) { j.State = UpgradeDownloading })
	err = s.upgrader.Install(ctx, job.To, func(done, total int64) {
		s.setUpgrade(func(j *UpgradeJob) { j.Done, j.Total = done, total })
	})
	if err != nil {
		fail("installing", err)
		return
	}
	s.setUpgrade(func(j *UpgradeJob) { j.State = UpgradeInstalling })
	_ = s.auditNow(ctx, "system.update_installed", "system", "update", "installed "+job.To+" in place of "+job.From+"; restarting")
	s.setUpgrade(func(j *UpgradeJob) { j.State = UpgradeRestarting })
	if restart != nil {
		restart()
	}
}
