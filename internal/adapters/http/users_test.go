package httpapi_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"testing"

	httpapi "github.com/albievan/cactusinv/internal/adapters/http"
	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
)

const albiePW = "correct horse battery"

// second is another browser on the same server.
func (c *client) second() *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: c.t, srv: c.srv, http: &http.Client{Jar: jar}}
}

func (c *client) loginAs(login, pw string) int {
	c.t.Helper()
	return c.do("POST", "/api/v1/auth/login", obj{"email": login, "password": pw}, nil)
}

func problemType(code string) string { return "https://cactusinv/problems/" + code }

func TestUserRoutes(t *testing.T) {
	c, _, _, _ := newServer(t)
	c.login()
	var me httpapi.Me
	c.ok(c.do("GET", "/api/v1/auth/me", nil, &me), 200, "me")
	if me.MustChangePassword {
		t.Fatal("a user made on the server need not change the password")
	}

	var p httpapi.Problem
	c.ok(c.do("POST", "/api/v1/users", obj{"email": "bob@example.com", "password": "first password b", "currentPassword": "guess"}, &p), 422, "wrong current password")
	if p.Field != "currentPassword" {
		t.Fatalf("problem = %+v", p)
	}
	c.ok(c.do("POST", "/api/v1/users", obj{"email": "bob", "password": "first password b", "currentPassword": albiePW}, &p), 422, "not an e-mail address")
	if p.Field != "email" {
		t.Fatalf("problem = %+v", p)
	}
	var bob httpapi.User
	c.ok(c.do("POST", "/api/v1/users", obj{"email": "bob@example.com", "password": "first password b", "currentPassword": albiePW}, &bob), 201, "add user")
	if !bob.Active || !bob.MustChangePassword || bob.Email != "bob@example.com" || bob.LastLoginAt != nil || bob.DisabledAt != nil {
		t.Fatalf("bob = %+v", bob)
	}
	c.ok(c.do("POST", "/api/v1/users", obj{"email": "BOB@example.com", "password": "first password b", "currentPassword": albiePW}, &p), 409, "duplicate login")

	var list httpapi.UserList
	c.ok(c.do("GET", "/api/v1/users", nil, &list), 200, "list")
	if len(list.Items) != 2 || list.Items[0].Email != "albie@example.com" || list.Items[0].LastLoginAt == nil {
		t.Fatalf("list = %+v", list)
	}

	bobC := c.second()
	c.ok(bobC.loginAs("bob@example.com", "first password b"), 204, "bob signs in")

	c.ok(c.do("POST", "/api/v1/users/"+me.Id+"/reset-password", obj{"password": "a temporary pass", "currentPassword": albiePW}, &p), 403, "reset yourself")
	if p.Type != problemType("forbidden") {
		t.Fatalf("problem = %+v", p)
	}
	c.ok(c.do("POST", "/api/v1/users/nobody/reset-password", obj{"password": "a temporary pass", "currentPassword": albiePW}, &p), 404, "reset unknown")
	c.ok(c.do("POST", "/api/v1/users/"+bob.Id+"/reset-password", obj{"password": "a temporary pass", "currentPassword": albiePW}, &bob), 200, "reset bob")
	if !bob.MustChangePassword {
		t.Fatal("reset → must change")
	}
	c.ok(bobC.do("GET", "/api/v1/auth/me", nil, &p), 401, "bob was signed out by the reset")
	c.ok(bobC.loginAs("bob@example.com", "a temporary pass"), 204, "bob signs in again")

	c.ok(c.do("POST", "/api/v1/users/"+me.Id+"/disable", obj{"currentPassword": albiePW}, &p), 403, "disable yourself")
	c.ok(c.do("POST", "/api/v1/users/"+bob.Id+"/disable", obj{"currentPassword": albiePW}, &bob), 200, "disable bob")
	if bob.Active || bob.DisabledAt == nil {
		t.Fatalf("bob = %+v", bob)
	}
	c.ok(bobC.do("GET", "/api/v1/auth/me", nil, &p), 401, "bob was signed out by the disable")
	c.ok(bobC.loginAs("bob@example.com", "a temporary pass"), 401, "a disabled user cannot sign in")
	c.ok(c.do("POST", "/api/v1/users/"+bob.Id+"/enable", obj{"currentPassword": albiePW}, &bob), 200, "enable bob")
	if !bob.Active {
		t.Fatal("enabled")
	}
	c.ok(bobC.loginAs("bob@example.com", "a temporary pass"), 204, "bob signs in once enabled")
}

func TestMustChangePasswordGate(t *testing.T) {
	c, _, _, svc := newServer(t)
	albie := c
	albie.login()
	var me httpapi.Me
	albie.ok(albie.do("GET", "/api/v1/auth/me", nil, &me), 200, "me")
	actor := app.WithActor(context.Background(), app.Actor{ID: me.Id, Name: me.Email})
	if _, err := svc.AddUser(actor, "carol@example.com", "chosen by albie", albiePW); err != nil {
		t.Fatal(err)
	}
	carol := c.second()
	carol.ok(carol.loginAs("carol@example.com", "chosen by albie"), 204, "carol signs in")
	carol.ok(carol.do("GET", "/api/v1/auth/me", nil, &me), 200, "me is allowed")
	if !me.MustChangePassword {
		t.Fatal("carol must change her password")
	}
	var p httpapi.Problem
	for _, r := range []struct{ method, path string }{
		{"GET", "/api/v1/invoices"},
		{"GET", "/api/v1/users"},
		{"GET", "/api/v1/dashboard"},
		{"GET", "/api/v1/backups"},
		{"POST", "/api/v1/backups/uploads"},
		{"POST", "/api/v1/backups/export"},
		{"POST", "/api/v1/restores/x/apply"},
		{"POST", "/api/v1/automation/resume"},
	} {
		var body any
		if r.method == "POST" {
			body = obj{"passphrase": "a passphrase of twenty", "currentPassword": "x", "confirmation": "RESTORE"}
		}
		carol.ok(carol.do(r.method, r.path, body, &p), 403, r.method+" "+r.path)
		if p.Type != problemType("password-change-required") {
			t.Fatalf("%s %s: problem = %+v", r.method, r.path, p)
		}
	}
	// A refused mutation does not use up its Idempotency-Key.
	cur := obj{"code": "ZAR", "name": "South African Rand", "symbol": "R", "minorUnits": 2, "active": true}
	carol.ok(carol.do("POST", "/api/v1/currencies", cur, &p, "Idempotency-Key", "k1"), 403, "refused currency")
	carol.ok(carol.do("POST", "/api/v1/auth/password", obj{"currentPassword": "chosen by albie", "newPassword": "chosen by albie"}, &p), 422, "the same password")
	carol.ok(carol.do("POST", "/api/v1/auth/password", obj{"currentPassword": "chosen by albie", "newPassword": "carol's own password"}, nil), 204, "change")
	carol.ok(carol.do("GET", "/api/v1/auth/me", nil, &me), 200, "still signed in")
	if me.MustChangePassword {
		t.Fatal("the flag is cleared")
	}
	carol.ok(carol.do("POST", "/api/v1/currencies", cur, nil, "Idempotency-Key", "k1"), 201, "the same key runs now")
	carol.ok(carol.do("GET", "/api/v1/invoices", nil, nil), 200, "invoices")
	carol.ok(carol.do("POST", "/api/v1/auth/logout", nil, nil), 204, "logout")
}

func TestOwnPasswordChangeSignsOutOtherSessions(t *testing.T) {
	c, _, _, _ := newServer(t)
	c.login()
	phone := c.second()
	phone.login()
	c.ok(c.do("POST", "/api/v1/auth/password", obj{"currentPassword": albiePW, "newPassword": "a fresh long password"}, nil), 204, "change")
	c.ok(c.do("GET", "/api/v1/auth/me", nil, nil), 200, "this session stays")
	c.ok(phone.do("GET", "/api/v1/auth/me", nil, nil), 401, "the other session ends")
}

func TestReauthAttemptsAreRateLimited(t *testing.T) {
	c, _, _, _ := newServer(t)
	c.login() // a success does not count
	codes := map[int]int{}
	for range 11 {
		codes[c.do("POST", "/api/v1/users", obj{"email": "x@example.com", "password": "first password x", "currentPassword": "guess"}, nil)]++
	}
	if codes[422] != 10 || codes[429] != 1 {
		t.Fatalf("codes = %v", codes)
	}
	c.ok(c.do("POST", "/api/v1/users/nobody/disable", obj{"currentPassword": albiePW}, nil), 429, "disable shares the limit")
	c.ok(c.do("GET", "/api/v1/users", nil, nil), 200, "reading is not limited")
}

// TestDefaultAdminMustChangeOverHTTP: the default administrator (as made on a new
// installation, or flagged by the upgrade to v1.6.0) signs in, is held at the password
// change, changes it and continues.
func TestDefaultAdminMustChangeOverHTTP(t *testing.T) {
	c, store, _, _ := newServer(t)
	if err := store.Repos().Users.Create(context.Background(), domain.User{ID: "adm", Email: "admin", PasswordHash: "hash:admin", MustChangePassword: true}); err != nil {
		t.Fatal(err)
	}
	c.ok(c.loginAs("admin", "admin"), 204, "admin/admin signs in")
	var me httpapi.Me
	c.ok(c.do("GET", "/api/v1/auth/me", nil, &me), 200, "me")
	if !me.MustChangePassword || me.Email != "admin" {
		t.Fatalf("me = %+v", me)
	}
	c.ok(c.do("GET", "/api/v1/settings/business", nil, nil), 403, "nothing else")
	c.ok(c.do("POST", "/api/v1/auth/password", obj{"currentPassword": "admin", "newPassword": "admin's new password"}, nil), 204, "change")
	c.ok(c.do("GET", "/api/v1/settings/business", nil, nil), 200, "continues")
}

// TestRefusalsBeforeAPasswordCheckDoNotCount: requests that never checked a password (no
// session, a bad e-mail address, an unknown user) do not use up the limit.
func TestRefusalsBeforeAPasswordCheckDoNotCount(t *testing.T) {
	c, _, _, _ := newServer(t)
	for range 15 {
		c.ok(c.do("POST", "/api/v1/users", obj{"email": "x@example.com", "password": "first password x", "currentPassword": albiePW}, nil), 401, "signed out")
	}
	c.login()
	for range 15 {
		c.ok(c.do("POST", "/api/v1/users", obj{"email": "not an address", "password": "first password x", "currentPassword": albiePW}, nil), 422, "bad e-mail")
		c.ok(c.do("POST", "/api/v1/users/nobody/disable", obj{"currentPassword": albiePW}, nil), 404, "unknown user")
	}
	c.ok(c.do("POST", "/api/v1/users", obj{"email": "x@example.com", "password": "first password x", "currentPassword": albiePW}, nil), 201, "still allowed")
}
