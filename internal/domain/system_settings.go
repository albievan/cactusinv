package domain

import (
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Session timeout limits (System Settings, M23).
const (
	MinSessionIdle = 5 * time.Minute
	MaxSessionIdle = 30 * 24 * time.Hour
	MinSessionMax  = time.Hour
	MaxSessionMax  = 365 * 24 * time.Hour
	// DefaultSessionMax is how long a sign-in lasts before System Settings are saved.
	DefaultSessionMax = 7 * 24 * time.Hour
	// MaxHostDirLength bounds a mapped host folder.
	MaxHostDirLength = 1024
)

// SystemSettings are the installation's own settings (never backed up or restored): how
// long a sign-in lasts, and which host folder the deployment maps to the container's backup
// and invoice folders. A host folder takes effect at the next deploy (deploy.sh --install);
// "" keeps the deployment's (.env).
type SystemSettings struct {
	// SessionIdle signs a user out after this long without a request; 0 = never.
	SessionIdle time.Duration
	// SessionMax ends every sign-in this long after it started, however active.
	SessionMax      time.Duration
	BackupHostDir   string
	InvoicesHostDir string
	Version         int
}

// DefaultSystemSettings are the settings before any are saved: no idle timeout, sessions of
// max (DefaultSessionMax when max is not positive), the deployment's folders.
func DefaultSystemSettings(max time.Duration) SystemSettings {
	if max <= 0 {
		max = DefaultSessionMax
	}
	return SystemSettings{SessionMax: max}
}

// Validate checks and normalises the settings.
func (s *SystemSettings) Validate() error {
	if s.SessionIdle != 0 && (s.SessionIdle < MinSessionIdle || s.SessionIdle > MaxSessionIdle) {
		return invalid("sessionIdleMinutes", "must be 0 (never) or between 5 minutes and 30 days")
	}
	if s.SessionMax < MinSessionMax || s.SessionMax > MaxSessionMax {
		return invalid("sessionMaxHours", "must be between 1 hour and 365 days")
	}
	if s.SessionIdle > s.SessionMax {
		return invalid("sessionIdleMinutes", "cannot be longer than the maximum session")
	}
	var err error
	if s.BackupHostDir, err = ValidateHostDir("backupHostDir", s.BackupHostDir); err != nil {
		return err
	}
	if s.InvoicesHostDir, err = ValidateHostDir("invoicesHostDir", s.InvoicesHostDir); err != nil {
		return err
	}
	return nil
}

// ValidateHostDir checks a host folder to map into the container and returns it trimmed,
// without a trailing slash: "" (the deployment's), an absolute path, or one relative to the
// project folder starting with "./". It may not contain "..", control characters, or the
// characters Docker Compose or a shell would read specially (: $ " ' ` \).
func ValidateHostDir(field, p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", nil
	}
	if len(p) > MaxHostDirLength {
		return "", invalid(field, "max "+strconv.Itoa(MaxHostDirLength)+" characters")
	}
	if !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "./") {
		return "", invalid(field, `must be an absolute folder (/…) or relative to the project folder (./…)`)
	}
	if strings.ContainsFunc(p, func(r rune) bool { return unicode.IsControl(r) || strings.ContainsRune(":$\"'`\\", r) }) {
		return "", invalid(field, "may not contain : $ quotes, backslashes or control characters")
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", invalid(field, `may not contain ".."`)
		}
	}
	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
	}
	if p == "/" || p == "." {
		return "", invalid(field, "must be a folder of its own, not the root or the project folder")
	}
	return p, nil
}

// SessionAlive reports whether a session may still be used at now: within the maximum from
// its start, and, with an idle timeout, used recently enough (lastSeen zero means never
// since it started).
func (s SystemSettings) SessionAlive(sess Session, now time.Time) bool {
	if !sess.ExpiresAt.After(now) || !sess.CreatedAt.Add(s.SessionMax).After(now) {
		return false
	}
	if s.SessionIdle <= 0 {
		return true
	}
	last := sess.LastSeenAt
	if last.Before(sess.CreatedAt) {
		last = sess.CreatedAt
	}
	return last.Add(s.SessionIdle).After(now)
}
