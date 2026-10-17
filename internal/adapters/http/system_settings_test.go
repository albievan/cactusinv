package httpapi_test

import (
	"testing"

	httpapi "github.com/albievan/cactusinv/internal/adapters/http"
)

// System Settings over HTTP (M23): defaults, a save in hours and minutes, a bad folder
// named, and administrators only.
func TestSystemSettingsOverHTTP(t *testing.T) {
	albie, _, _, _ := newServer(t)
	albie.login()
	var s httpapi.SystemSettings
	albie.ok(albie.do("GET", "/api/v1/settings/system", nil, &s), 200, "get")
	if s.SessionMaxHours != 168 || s.SessionIdleMinutes != 0 || s.Version != 0 || s.PendingFolders {
		t.Fatalf("defaults = %+v", s)
	}
	body := obj{"sessionIdleMinutes": 45, "sessionMaxHours": 12, "backupHostDir": "/Volumes/NAS/backups", "invoicesHostDir": "", "version": 0}
	albie.ok(albie.do("PUT", "/api/v1/settings/system", body, &s), 200, "save")
	if s.SessionIdleMinutes != 45 || s.SessionMaxHours != 12 || s.BackupHostDir != "/Volumes/NAS/backups" || s.Version != 1 {
		t.Fatalf("saved = %+v", s)
	}
	var p httpapi.Problem
	body["version"], body["invoicesHostDir"] = 1, "data/invoices"
	albie.ok(albie.do("PUT", "/api/v1/settings/system", body, &p), 422, "relative folder")
	if p.Field != "invoicesHostDir" {
		t.Fatalf("problem = %+v", p)
	}
	var carol httpapi.User
	albie.ok(albie.do("POST", "/api/v1/users", obj{"email": "carol@example.com", "password": "chosen by albie", "currentPassword": albiePW}, &carol), 201, "add carol")
	cc := albie.second()
	cc.ok(cc.loginAs("carol@example.com", "chosen by albie"), 204, "carol signs in")
	cc.ok(cc.do("POST", "/api/v1/auth/password", obj{"currentPassword": "chosen by albie", "newPassword": "carol's own password"}, nil), 204, "carol's password")
	cc.ok(cc.do("GET", "/api/v1/settings/system", nil, &p), 403, "not an administrator")
}
