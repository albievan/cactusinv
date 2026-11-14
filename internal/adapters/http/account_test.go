package httpapi_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	httpapi "github.com/albievan/cactusinv/internal/adapters/http"
	"github.com/albievan/cactusinv/internal/ports"
)

// Your account (M43): the profile over HTTP.
func TestProfileOverHTTP(t *testing.T) {
	c, _, _, _ := newServer(t)
	c.login()
	var p httpapi.Profile
	c.ok(c.do("GET", "/api/v1/auth/profile", nil, &p), 200, "get")
	if p.UserName != "albie@example.com" || p.ResetAddress != "albie@example.com" || p.FirstName != "" {
		t.Fatalf("profile = %+v", p)
	}
	c.ok(c.do("PUT", "/api/v1/auth/profile", obj{"firstName": "Albie", "lastName": "van der Merwe", "emailAddress": "albie@home.example"}, &p), 200, "put")
	if p.FirstName != "Albie" || p.EmailAddress != "albie@home.example" || p.ResetAddress != "albie@home.example" {
		t.Fatalf("saved = %+v", p)
	}
	var prob httpapi.Problem
	c.ok(c.do("PUT", "/api/v1/auth/profile", obj{"firstName": "", "lastName": "", "emailAddress": "nope"}, &prob), 422, "bad address")
	c.second().ok(c.second().do("GET", "/api/v1/auth/profile", nil, &prob), 401, "needs a session")
}

// Forgot password over HTTP (M43): no session; the same answer for every user name; the
// link's text is hidden in the outbox; the link sets a new password once.
func TestForgotPasswordOverHTTP(t *testing.T) {
	c, store, _, svc := newServer(t)
	anon := c.second()
	anon.ok(anon.do("POST", "/api/v1/auth/password-reset", obj{"userName": "nobody@example.com", "linkBase": "https://inv.example.com"}, nil), 202, "unknown user")
	anon.ok(anon.do("POST", "/api/v1/auth/password-reset", obj{"userName": "albie@example.com", "linkBase": "https://inv.example.com"}, nil), 202, "real user")

	msgs, _, err := svc.ListEmails(context.Background(), ports.EmailFilter{})
	if err != nil || len(msgs) != 1 {
		t.Fatalf("outbox = %d %v", len(msgs), err)
	}
	stored, _ := store.Repos().Emails.Get(context.Background(), msgs[0].ID)
	token := regexp.MustCompile(`/reset-password/([A-Za-z0-9_-]{43})\n`).FindStringSubmatch(stored.Body)[1]

	// Staff who may read the outbox see that it is a reset, not the link.
	c.login()
	var list httpapi.EmailList
	c.ok(c.do("GET", "/api/v1/emails", nil, &list), 200, "outbox")
	if len(list.Items) != 1 || list.Items[0].Kind != "password_reset" || strings.Contains(list.Items[0].Body, token) {
		t.Fatalf("outbox shows %+v", list.Items)
	}

	var prob httpapi.Problem
	anon.ok(anon.do("GET", "/api/v1/auth/password-reset/"+strings.Repeat("A", 43), nil, &prob), 422, "unknown link")
	anon.ok(anon.do("GET", "/api/v1/auth/password-reset/"+token, nil, nil), 204, "the link works")
	anon.ok(anon.do("POST", "/api/v1/auth/password-reset/"+token, obj{"newPassword": "short"}, &prob), 422, "short password")
	anon.ok(anon.do("POST", "/api/v1/auth/password-reset/"+token, obj{"newPassword": "a brand new long password"}, nil), 204, "reset")
	anon.ok(anon.do("POST", "/api/v1/auth/password-reset/"+token, obj{"newPassword": "another long password!"}, &prob), 422, "used")
	if code := anon.loginAs("albie@example.com", "a brand new long password"); code != 204 {
		t.Fatalf("sign in with the new password = %d", code)
	}
	c.ok(c.do("GET", "/api/v1/auth/me", nil, &prob), 401, "the earlier session ended")
}

// Every reset request counts against the client's sign-in budget.
func TestForgotPasswordIsRateLimited(t *testing.T) {
	c, _, _, _ := newServer(t)
	codes := map[int]int{}
	for range 30 {
		codes[c.do("POST", "/api/v1/auth/password-reset", obj{"userName": "x@example.com"}, nil)]++
	}
	if codes[429] == 0 {
		t.Fatalf("no limit: %v", codes)
	}
}

// Users & groups (M44): edit and delete a user over HTTP, with the actor's password.
func TestEditAndDeleteUserOverHTTP(t *testing.T) {
	c, _, _, svc := newServer(t)
	c.login()
	other, err := svc.CreateUser(context.Background(), "other@example.com", "other password long")
	if err != nil {
		t.Fatal(err)
	}
	var u httpapi.User
	var prob httpapi.Problem
	c.ok(c.do("PUT", "/api/v1/users/"+other.ID, obj{"userName": "thandi@example.com", "firstName": "Thandi", "lastName": "Mokoena", "emailAddress": "", "currentPassword": "wrong password!!"}, &prob), 422, "wrong password")
	c.ok(c.do("PUT", "/api/v1/users/"+other.ID, obj{"userName": "thandi@example.com", "firstName": "Thandi", "lastName": "Mokoena", "emailAddress": "", "currentPassword": "correct horse battery"}, &u), 200, "edit")
	if u.Email != "thandi@example.com" || u.FirstName != "Thandi" || u.LastName != "Mokoena" {
		t.Fatalf("edited = %+v", u)
	}
	c.ok(c.do("POST", "/api/v1/users/"+other.ID+"/delete", obj{"currentPassword": "correct horse battery"}, nil), 204, "delete")
	var list httpapi.UserList
	c.ok(c.do("GET", "/api/v1/users", nil, &list), 200, "list")
	if len(list.Items) != 1 {
		t.Fatalf("users = %+v", list.Items)
	}
	var me httpapi.Me
	c.ok(c.do("GET", "/api/v1/auth/me", nil, &me), 200, "me")
	c.ok(c.do("POST", "/api/v1/users/"+me.Id+"/delete", obj{"currentPassword": "correct horse battery"}, &prob), 403, "delete yourself")
}
