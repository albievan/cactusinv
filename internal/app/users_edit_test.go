package app_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// An administrator edits a user's user name, name and e-mail address (M44).
func TestUpdateUser(t *testing.T) {
	h := newHarness(t)
	ctx, admin, other := h.users()
	edit := app.UserEdit{Login: " thandi@example.com ", Profile: domain.Profile{FirstName: "Thandi", LastName: "Mokoena", EmailAddress: "thandi@home.example"}}
	if _, err := h.svc.UpdateUser(ctx, other.ID, edit, "wrong password!!"); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("wrong password: %v", err)
	}
	u := must(h.svc.UpdateUser(ctx, other.ID, edit, adminPW))
	if u.Email != "thandi@example.com" || u.FirstName != "Thandi" || u.LastName != "Mokoena" || u.EmailAddress != "thandi@home.example" {
		t.Fatalf("edited = %+v", u)
	}
	if e := h.audited("user.updated"); e.ActorName != "admin@example.com" || !strings.Contains(e.Summary, "other@example.com → thandi@example.com") {
		t.Fatalf("audit = %+v", e)
	}
	// The new user name signs in; the old one no longer does.
	if _, _, err := h.svc.Login(h.ctx, "thandi@example.com", otherPW); err != nil {
		t.Fatalf("new user name: %v", err)
	}
	if _, _, err := h.svc.Login(h.ctx, "other@example.com", otherPW); err == nil {
		t.Fatal("old user name signs in")
	}
	for name, bad := range map[string]app.UserEdit{
		"taken (any case)": {Login: "ADMIN@example.com"},
		"not an address":   {Login: "thandi"},
		"bad e-mail":       {Login: "thandi@example.com", Profile: domain.Profile{EmailAddress: "nope"}},
	} {
		if _, err := h.svc.UpdateUser(ctx, other.ID, bad, adminPW); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// An unchanged legacy user name ("admin") is kept although it is no address.
	legacy := newHarness(t)
	must(legacy.svc.EnsureDefaultAdmin(legacy.ctx, "admin", adminPW))
	a := must(legacy.store.Repos().Users.GetByEmail(legacy.ctx, "admin"))
	actx := app.WithActor(legacy.ctx, app.Actor{ID: a.ID, Name: a.Email})
	if got := must(legacy.svc.UpdateUser(actx, a.ID, app.UserEdit{Login: "admin", Profile: domain.Profile{FirstName: "Albie"}}, adminPW)); got.Email != "admin" || got.FirstName != "Albie" {
		t.Fatalf("legacy = %+v", got)
	}
	_ = admin
}

// An administrator deletes a user (M44); the audit log keeps their user name.
func TestDeleteUser(t *testing.T) {
	h := newHarness(t)
	ctx, admin, other := h.users()
	// Something the user did, to check the log keeps their name.
	octx := app.WithActor(h.ctx, app.Actor{ID: other.ID, Name: other.Email})
	must(h.svc.UpdateProfile(octx, other.ID, domain.Profile{FirstName: "Other"}))
	token, _, err := h.svc.Login(h.ctx, "other@example.com", otherPW)
	if err != nil {
		t.Fatal(err)
	}

	if err := h.svc.DeleteUser(ctx, admin.ID, adminPW); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("delete yourself: %v", err)
	}
	if err := h.svc.DeleteUser(ctx, other.ID, "wrong password!!"); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("wrong password: %v", err)
	}
	must0(t, h.svc.DeleteUser(ctx, other.ID, adminPW))
	if _, err := h.store.Repos().Users.Get(h.ctx, other.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("still there: %v", err)
	}
	if _, err := h.svc.Authenticate(h.ctx, token); err == nil {
		t.Fatal("the deleted user's session still works")
	}
	if _, _, err := h.svc.Login(h.ctx, "other@example.com", otherPW); err == nil {
		t.Fatal("the deleted user signs in")
	}
	if e := h.audited("user.deleted"); e.ActorName != "admin@example.com" || !strings.Contains(e.Summary, "deleted user other@example.com (Other)") {
		t.Fatalf("audit = %+v", e)
	}
	// What they did before still names them.
	es, _ := must3(h.svc.ListAudit(h.ctx, ports.AuditFilter{Action: "user.profile_updated"}))
	if len(es) == 0 || es[0].ActorName != "other@example.com" {
		t.Fatalf("their own entries = %+v", es)
	}
	// The user name is free again.
	must(h.svc.CreateUser(h.ctx, "other@example.com", otherPW))
}
