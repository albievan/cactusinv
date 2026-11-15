package httpapi_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	httpapi "github.com/albievan/cactusinv/internal/adapters/http"
	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports/fakes"
)

type secretFiles map[string]string

func (s secretFiles) Get(_ context.Context, key string) (string, error) {
	if v, ok := s[key]; ok {
		return v, nil
	}
	return "", domain.ErrNotFound
}

// TestSystemRoutes (M57): the status and health for those who may see the database page,
// a secret for Administrators and Admin settings after their own password; never for others.
func TestSystemRoutes(t *testing.T) {
	store := fakes.NewStore()
	clock := &fakes.Clock{T: time.Date(2026, 11, 16, 6, 0, 0, 0, time.UTC)}
	stats := httpapi.NewRequestStats()
	svc := app.New(app.Deps{
		Tx: store, Clock: clock, IDs: &fakes.IDs{}, Mailer: &fakes.Mailer{}, Renderer: fakes.Renderer{}, Hasher: fakes.Hasher{}, Version: "1.34.0",
		System: app.SystemDeps{Build: "61", Requests: stats, Secrets: secretFiles{"db_password": "pw-db"}, SecretKeys: []app.SecretInfo{{Key: "db_password", Description: "db"}}},
	})
	if _, err := svc.CreateUser(context.Background(), "albie@example.com", albiePW); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(httpapi.NewHandler(httpapi.Options{Service: svc, Clock: clock, Stats: stats}))
	t.Cleanup(srv.Close)
	newClient := func() *client {
		jar, _ := cookiejar.New(nil)
		return &client{t: t, srv: srv, http: &http.Client{Jar: jar}}
	}
	albie := newClient()
	albie.login()

	var st httpapi.SystemStatus
	albie.ok(albie.do("GET", "/api/v1/system/status", nil, &st), 200, "status")
	if st.Version != "1.34.0" || st.Build != "61" || len(st.Services) < 3 || st.Services[0].Name != "cactusInv" || len(st.Secrets) != 1 || !st.Secrets[0].Present {
		t.Fatalf("status = %+v", st)
	}
	var hl httpapi.SystemHealth
	albie.ok(albie.do("GET", "/api/v1/system/health", nil, &hl), 200, "health")
	if hl.Requests.Total < 2 || len(hl.Checks) == 0 {
		t.Fatalf("health = %+v", hl)
	}
	var p httpapi.Problem
	albie.ok(albie.do("POST", "/api/v1/system/secrets/reveal", obj{"key": "db_password", "currentPassword": "wrong"}, &p), 422, "wrong password")
	var v httpapi.SecretValue
	albie.ok(albie.do("POST", "/api/v1/system/secrets/reveal", obj{"key": "db_password", "currentPassword": albiePW}, &v, "Idempotency-Key", "r1"), 200, "reveal")
	if v.Value != "pw-db" {
		t.Fatalf("value %+v", v)
	}

	// Carol in no group sees nothing; in Admin settings she sees the page and the secrets.
	var carol httpapi.User
	albie.ok(albie.do("POST", "/api/v1/users", obj{"email": "carol@example.com", "password": "chosen by albie", "currentPassword": albiePW}, &carol), 201, "add carol")
	cc := newClient()
	cc.ok(cc.loginAs("carol@example.com", "chosen by albie"), 204, "carol signs in")
	cc.ok(cc.do("POST", "/api/v1/auth/password", obj{"currentPassword": "chosen by albie", "newPassword": "carol's own password"}, nil), 204, "carol's password")
	for _, path := range []string{"/api/v1/system/status", "/api/v1/system/health"} {
		cc.ok(cc.do("GET", path, nil, &p), 403, "carol "+path)
	}
	cc.ok(cc.do("POST", "/api/v1/system/secrets/reveal", obj{"key": "db_password", "currentPassword": "carol's own password"}, &p), 403, "carol reveal")
	albie.ok(albie.do("PUT", "/api/v1/users/"+carol.Id+"/groups", obj{"groupIds": []string{"admin-settings"}, "currentPassword": albiePW}, nil), 200, "admin settings")
	cc.ok(cc.do("GET", "/api/v1/system/status", nil, &st), 200, "admin settings status")
	cc.ok(cc.do("POST", "/api/v1/system/secrets/reveal", obj{"key": "db_password", "currentPassword": "carol's own password"}, &v), 200, "admin settings reveal")
	cc.ok(cc.do("GET", "/api/v1/users", nil, &p), 403, "admin settings is not user management")
}
