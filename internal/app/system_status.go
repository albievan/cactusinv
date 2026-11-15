package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// The Database & containers page, its secrets and the dashboard's health (M57). Everything
// here is read from inside the application's container: the app has no rights over Docker
// or the cluster, so the other containers are seen through their connections.

// SystemDeps are the probes and the configuration the status pages show.
type SystemDeps struct {
	Database ports.DatabaseInspector
	Host     ports.HostProbe
	Requests ports.RequestStatsSource
	// Secrets reads the secrets named in SecretKeys (nil: none can be viewed).
	Secrets    ports.Secrets
	SecretKeys []SecretInfo
	// DBHost, DBPort, DBName and DBUser are where the application connects.
	DBHost         string
	DBPort         int
	DBName, DBUser string
	// MailHost and MailPort are the deployment's mail server, used while none is saved in
	// Settings → E-mail server; MailpitHost and MailpitPort the test mailbox deploy.sh
	// starts beside the app ("" when there is none).
	MailHost    string
	MailPort    int
	MailpitHost string
	MailpitPort int
	// Build and Container describe the running release.
	Build     string
	Container bool
	// Configuration is the application's own configuration, without secrets.
	Configuration []ports.Setting
}

// SecretInfo names a secret the application holds and says what it is for.
type SecretInfo struct {
	Key, Description string
}

// SecretStatus is a secret on the status page: never its value.
type SecretStatus struct {
	SecretInfo
	Present bool
}

// Service states.
const (
	StateRunning     = "running"
	StateUnreachable = "unreachable"
	StateNotSet      = "not configured"
)

// ServiceStatus is one container or service as the application reaches it.
type ServiceStatus struct {
	Name, Role, State, Detail string
	Latency                   time.Duration
}

// SystemStatus is the Database & containers page.
type SystemStatus struct {
	Target, Version, Build string
	Container              bool
	Runtime                ports.RuntimeStats
	Uptime                 time.Duration
	DBHost                 string
	DBPort                 int
	DBName, DBUser         string
	Database               ports.DatabaseStatus
	// DatabaseError says why the database could not be read ("" when it could).
	DatabaseError string
	Services      []ServiceStatus
	Configuration []ports.Setting
	Secrets       []SecretStatus
}

// reachTimeout bounds each network check.
const reachTimeout = 2 * time.Second

// SystemStatus reads the deployment, the database and the services the app talks to.
func (s *Service) SystemStatus(ctx context.Context) (SystemStatus, error) {
	sys := s.system
	out := SystemStatus{
		Target: s.deploy.Target, Version: s.version, Build: sys.Build, Container: sys.Container,
		DBHost: sys.DBHost, DBPort: sys.DBPort, DBName: sys.DBName, DBUser: sys.DBUser,
		Configuration: sys.Configuration,
	}
	if sys.Host != nil {
		out.Runtime = sys.Host.Runtime()
		if !out.Runtime.StartedAt.IsZero() {
			out.Uptime = s.clock.Now().Sub(out.Runtime.StartedAt)
		}
	}
	out.Database, out.DatabaseError = s.inspectDatabase(ctx)
	out.Services = s.services(ctx, out)
	for _, k := range sys.SecretKeys {
		st := SecretStatus{SecretInfo: k}
		if sys.Secrets != nil {
			v, err := sys.Secrets.Get(ctx, k.Key)
			st.Present = err == nil && v != ""
		}
		out.Secrets = append(out.Secrets, st)
	}
	return out, nil
}

func (s *Service) inspectDatabase(ctx context.Context) (ports.DatabaseStatus, string) {
	if s.system.Database == nil {
		return ports.DatabaseStatus{}, "not available here"
	}
	st, err := s.system.Database.Inspect(ctx)
	if err != nil {
		s.log.WarnContext(ctx, "database status", "err", err)
		return st, "the database did not answer"
	}
	return st, ""
}

// services lists this application, the database and the mail servers as the app sees them.
func (s *Service) services(ctx context.Context, st SystemStatus) []ServiceStatus {
	app := ServiceStatus{Name: "cactusInv", Role: "This application", State: StateRunning, Detail: fmt.Sprintf("version %s (build %s)", s.version, st.Build)}
	if st.Runtime.Hostname != "" {
		app.Detail += " on " + st.Runtime.Hostname
	}
	out := []ServiceStatus{app}
	db := ServiceStatus{Name: "MariaDB", Role: "Database", State: StateRunning, Latency: st.Database.Ping}
	if st.DatabaseError != "" {
		db.State, db.Detail = StateUnreachable, st.DatabaseError
	} else {
		db.Detail = fmt.Sprintf("%s, up %s, %d of %d connections", st.Database.Version, roundDuration(st.Database.Uptime), st.Database.Connections, st.Database.MaxConnections)
	}
	out = append(out, db)
	host, port, from := s.mailServer(ctx)
	mail := ServiceStatus{Name: "E-mail server", Role: "Sends invoices, quotes and statements", State: StateNotSet, Detail: "none is set up"}
	if host != "" {
		mail = s.reach(ctx, mail, host, port)
		mail.Detail = fmt.Sprintf("%s (%s): %s", net.JoinHostPort(host, strconv.Itoa(port)), from, mail.Detail)
	}
	out = append(out, mail)
	if s.system.MailpitHost != "" {
		mp := s.reach(ctx, ServiceStatus{Name: "Mailpit", Role: "Test mailbox (catches e-mails while no real server is set)"}, s.system.MailpitHost, s.system.MailpitPort)
		mp.Detail = net.JoinHostPort(s.system.MailpitHost, strconv.Itoa(s.system.MailpitPort)) + ": " + mp.Detail
		out = append(out, mp)
	}
	return out
}

// mailServer is the SMTP server e-mails go through: the one saved in Settings, else the
// deployment's.
func (s *Service) mailServer(ctx context.Context) (host string, port int, from string) {
	if st, _, ok, err := s.SMTPServer(ctx); err == nil && ok {
		return st.Host, st.Port, "set in Settings → E-mail server"
	}
	if s.system.MailHost != "" {
		return s.system.MailHost, s.system.MailPort, "set by the deployment"
	}
	return "", 0, ""
}

func (s *Service) reach(ctx context.Context, st ServiceStatus, host string, port int) ServiceStatus {
	if s.system.Host == nil {
		st.State, st.Detail = StateUnreachable, "cannot be checked here"
		return st
	}
	ctx, cancel := context.WithTimeout(ctx, reachTimeout)
	defer cancel()
	d, err := s.system.Host.Reach(ctx, host, port)
	if err != nil {
		st.State, st.Detail = StateUnreachable, "does not answer"
		return st
	}
	st.State, st.Latency, st.Detail = StateRunning, d, "answers"
	return st
}

func roundDuration(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
	case d >= time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
}

// RevealSecret returns a secret's value to someone allowed to see the secrets, after their
// own current password; the view is audited with the secret's name only.
func (s *Service) RevealSecret(ctx context.Context, key, currentPassword string) (string, error) {
	known := false
	for _, k := range s.system.SecretKeys {
		known = known || k.Key == key
	}
	if !known || s.system.Secrets == nil {
		return "", wrap("app.RevealSecret", &domain.ValidationError{Field: "key", Reason: "no such secret"})
	}
	if err := s.reauthUser(ctx, "viewing the secret "+key, currentPassword); err != nil {
		return "", wrap("app.RevealSecret", err)
	}
	v, err := s.system.Secrets.Get(ctx, key)
	if err != nil || v == "" {
		return "", wrap("app.RevealSecret", fmt.Errorf("%w: the secret %s is not set", domain.ErrNotFound, key))
	}
	if err := s.auditNow(ctx, "secret.viewed", "secret", key, "viewed the secret "+key); err != nil {
		return "", wrap("app.RevealSecret", err)
	}
	return v, nil
}

// Health states, worst last.
const (
	HealthOK      = "ok"
	HealthWarning = "warning"
	HealthFailed  = "failed"
)

// HealthCheck is one line of the dashboard's health.
type HealthCheck struct {
	Name, State, Detail string
}

// SystemHealth is the dashboard's health and performance.
type SystemHealth struct {
	State    string
	Checks   []HealthCheck
	Uptime   time.Duration
	Runtime  ports.RuntimeStats
	Requests ports.RequestStats
	DBPing   time.Duration
	Pool     ports.DBPool
	// OutboxPending and OutboxFailed count e-mails waiting and given up.
	OutboxPending, OutboxFailed int
}

// lowDisk and fullDisk are the free space below which a data folder is a warning and a
// failure. Absolute, not a share: the backups and PDFs are small, and a large disk that is
// 95 % full still has room for years of them.
const (
	lowDisk  = 2 << 30
	fullDisk = 200 << 20
)

// SystemHealth checks the database, the mail server, the outbox, backups, free disk and
// automation, and reports the application's performance.
func (s *Service) SystemHealth(ctx context.Context) (SystemHealth, error) {
	sys := s.system
	var h SystemHealth
	add := func(name, state, detail string) { h.Checks = append(h.Checks, HealthCheck{name, state, detail}) }
	if sys.Host != nil {
		h.Runtime = sys.Host.Runtime()
		if !h.Runtime.StartedAt.IsZero() {
			h.Uptime = s.clock.Now().Sub(h.Runtime.StartedAt)
		}
	}
	if sys.Requests != nil {
		h.Requests = sys.Requests.RequestStats()
	}

	db, dbErr := s.inspectDatabase(ctx)
	h.DBPing, h.Pool = db.Ping, db.Pool
	if dbErr != "" {
		add("Database", HealthFailed, dbErr)
	} else {
		add("Database", HealthOK, fmt.Sprintf("answers in %d ms, %d of %d connections", db.Ping.Milliseconds(), db.Connections, db.MaxConnections))
	}

	if host, port, _ := s.mailServer(ctx); host == "" {
		add("E-mail server", HealthWarning, "none is set up: e-mails wait in the outbox")
	} else if r := s.reach(ctx, ServiceStatus{}, host, port); r.State != StateRunning {
		add("E-mail server", HealthWarning, net.JoinHostPort(host, strconv.Itoa(port))+" does not answer")
	} else {
		add("E-mail server", HealthOK, fmt.Sprintf("answers in %d ms", r.Latency.Milliseconds()))
	}

	if dbErr == "" {
		s.outboxHealth(ctx, &h, add)
		s.automationHealth(ctx, add)
	}
	s.backupHealth(ctx, add)
	s.diskHealth(add)

	h.State = HealthOK
	for _, c := range h.Checks {
		if c.State == HealthFailed || (c.State == HealthWarning && h.State == HealthOK) {
			h.State = c.State
		}
	}
	return h, nil
}

func (s *Service) outboxHealth(ctx context.Context, h *SystemHealth, add func(name, state, detail string)) {
	r := s.tx.Repos()
	st, err := r.Emails.Stats(ctx)
	if err != nil {
		add("Outbox", HealthWarning, "could not be read")
		return
	}
	h.OutboxPending = st.Pending
	_, failed, err := r.Emails.List(ctx, ports.EmailFilter{Status: domain.EmailFailed, Page: ports.Page{Limit: 1}})
	if err == nil {
		h.OutboxFailed = failed
	}
	switch {
	case failed > 0:
		add("Outbox", HealthWarning, fmt.Sprintf("%d e-mail(s) failed; see the Outbox", failed))
	case st.Pending > 0 && s.clock.Now().Sub(st.OldestPending) > 15*time.Minute:
		add("Outbox", HealthWarning, fmt.Sprintf("%d e-mail(s) waiting, the oldest for %s", st.Pending, roundDuration(s.clock.Now().Sub(st.OldestPending))))
	default:
		add("Outbox", HealthOK, fmt.Sprintf("%d waiting", st.Pending))
	}
}

func (s *Service) automationHealth(ctx context.Context, add func(name, state, detail string)) {
	st, err := s.tx.Repos().System.Get(ctx)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return
	}
	if st.AutomationHeld {
		add("Automatic sending", HealthWarning, "paused after a restore until an administrator resumes it")
		return
	}
	add("Automatic sending", HealthOK, "running")
}

func (s *Service) backupHealth(ctx context.Context, add func(name, state, detail string)) {
	ov, err := s.ListBackups(ctx)
	switch {
	case err != nil:
		add("Backups", HealthWarning, "could not be read")
	case !ov.KeyConfigured:
		add("Backups", HealthWarning, "no server backup key: nothing is backed up automatically")
	case ov.LastRun != nil && ov.LastRun.Result == domain.BackupRunFailed:
		add("Backups", HealthFailed, "the last scheduled backup failed")
	case len(ov.Files) == 0:
		add("Backups", HealthWarning, "no backup is stored yet")
	case s.clock.Now().Sub(ov.Files[0].CreatedAt) > 48*time.Hour:
		add("Backups", HealthWarning, "the newest backup is older than two days")
	default:
		add("Backups", HealthOK, fmt.Sprintf("%d stored, the newest %s ago", len(ov.Files), roundDuration(s.clock.Now().Sub(ov.Files[0].CreatedAt))))
	}
}

func (s *Service) diskHealth(add func(name, state, detail string)) {
	if s.system.Host == nil {
		return
	}
	for _, d := range []struct{ name, path string }{{"Disk: backups", s.deploy.BackupDir}, {"Disk: invoice PDFs", s.deploy.InvoicesDir}} {
		if d.path == "" {
			continue
		}
		u, err := s.system.Host.Disk(d.path)
		if err != nil {
			continue
		}
		state := HealthOK
		switch {
		case u.FreeBytes < fullDisk:
			state = HealthFailed
		case u.FreeBytes < lowDisk:
			state = HealthWarning
		}
		add(d.name, state, fmt.Sprintf("%s free of %s", bytesText(u.FreeBytes), bytesText(u.TotalBytes)))
	}
}

func bytesText(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
