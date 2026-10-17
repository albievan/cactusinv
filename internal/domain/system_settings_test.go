package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
)

func TestSystemSettingsValidate(t *testing.T) {
	ok := domain.SystemSettings{SessionIdle: 30 * time.Minute, SessionMax: 8 * time.Hour, BackupHostDir: " /Volumes/NAS/backups/ ", InvoicesHostDir: "./data/archive"}
	if err := ok.Validate(); err != nil || ok.BackupHostDir != "/Volumes/NAS/backups" || ok.InvoicesHostDir != "./data/archive" {
		t.Fatalf("valid: %v %+v", err, ok)
	}
	bad := []struct {
		s     domain.SystemSettings
		field string
	}{
		{domain.SystemSettings{SessionIdle: time.Minute, SessionMax: time.Hour}, "sessionIdleMinutes"},
		{domain.SystemSettings{SessionIdle: 2 * time.Hour, SessionMax: time.Hour}, "sessionIdleMinutes"},
		{domain.SystemSettings{SessionMax: 30 * time.Minute}, "sessionMaxHours"},
		{domain.SystemSettings{SessionMax: 400 * 24 * time.Hour}, "sessionMaxHours"},
		{domain.SystemSettings{SessionMax: time.Hour, BackupHostDir: "relative/dir"}, "backupHostDir"},
		{domain.SystemSettings{SessionMax: time.Hour, BackupHostDir: "/a/../etc"}, "backupHostDir"},
		{domain.SystemSettings{SessionMax: time.Hour, BackupHostDir: "/"}, "backupHostDir"},
		{domain.SystemSettings{SessionMax: time.Hour, InvoicesHostDir: "./"}, "invoicesHostDir"},
		{domain.SystemSettings{SessionMax: time.Hour, InvoicesHostDir: "/x:/y"}, "invoicesHostDir"},
		{domain.SystemSettings{SessionMax: time.Hour, InvoicesHostDir: "/home/$USER"}, "invoicesHostDir"},
		{domain.SystemSettings{SessionMax: time.Hour, InvoicesHostDir: "/a\nb"}, "invoicesHostDir"},
		{domain.SystemSettings{SessionMax: time.Hour, InvoicesHostDir: "/" + strings.Repeat("a", 1024)}, "invoicesHostDir"},
	}
	for _, b := range bad {
		err := b.s.Validate()
		var ve *domain.ValidationError
		if !errors.As(err, &ve) || ve.Field != b.field {
			t.Errorf("%+v: %v, want %s", b.s, err, b.field)
		}
	}
	if d := domain.DefaultSystemSettings(0); d.SessionMax != domain.DefaultSessionMax || d.SessionIdle != 0 {
		t.Fatalf("defaults %+v", d)
	}
}

func TestSessionAlive(t *testing.T) {
	start := time.Date(2026, 10, 17, 8, 0, 0, 0, time.UTC)
	sess := domain.Session{CreatedAt: start, ExpiresAt: start.Add(7 * 24 * time.Hour)}
	st := domain.SystemSettings{SessionIdle: 30 * time.Minute, SessionMax: 8 * time.Hour}
	at := start.Add
	if !st.SessionAlive(sess, at(29*time.Minute)) || st.SessionAlive(sess, at(30*time.Minute)) {
		t.Fatal("idle from the start")
	}
	sess.LastSeenAt = at(2 * time.Hour)
	if !st.SessionAlive(sess, at(2*time.Hour+29*time.Minute)) || st.SessionAlive(sess, at(2*time.Hour+31*time.Minute)) {
		t.Fatal("idle from the last use")
	}
	sess.LastSeenAt = at(7*time.Hour + 50*time.Minute)
	if st.SessionAlive(sess, at(8*time.Hour)) {
		t.Fatal("the maximum ends an active session")
	}
	st.SessionIdle, st.SessionMax = 0, 30*24*time.Hour
	if st.SessionAlive(sess, at(7*24*time.Hour)) {
		t.Fatal("the session's own expiry still applies")
	}
	if !st.SessionAlive(sess, at(6*24*time.Hour)) {
		t.Fatal("no idle timeout")
	}
}
