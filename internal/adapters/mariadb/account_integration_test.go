//go:build integration

package mariadb_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// Your account and forgot password on MariaDB 11 (M43).
func TestProfileAndPasswordResetPersistence(t *testing.T) {
	e := setup(t)
	u := must(e.svc.CreateUser(e.ctx, "albie@example.com", "correct horse battery"))
	got := must(e.svc.UpdateProfile(e.ctx, u.ID, domain.Profile{FirstName: "Albie", LastName: "van der Merwe", EmailAddress: "albie@home.example"}))
	if got.FirstName != "Albie" || got.EmailAddress != "albie@home.example" {
		t.Fatalf("profile = %+v", got)
	}
	must(e.svc.UpdateProfile(e.ctx, u.ID, domain.Profile{FirstName: "Albie", LastName: "van der Merwe", EmailAddress: "albie@home.example"})) // unchanged is fine

	if err := e.svc.RequestPasswordReset(e.ctx, "albie@example.com", "https://inv.example.com"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RequestPasswordReset(e.ctx, "albie@example.com", "https://inv.example.com"); err != nil { // throttled
		t.Fatal(err)
	}
	list, n := must3(e.svc.ListEmails(e.ctx, ports.EmailFilter{}))
	if n != 1 || list[0].Kind != domain.EmailKindPasswordReset || list[0].To[0] != "albie@home.example" {
		t.Fatalf("outbox = %+v", list)
	}
	token := regexp.MustCompile(`/reset-password/([A-Za-z0-9_-]{43})\n`).FindStringSubmatch(list[0].Body)[1]
	if res := must(e.svc.DispatchEmails(e.ctx, 5)); res.Sent != 1 {
		t.Fatalf("dispatch = %+v", res)
	}
	if m := must(e.store.Repos().Emails.Get(e.ctx, list[0].ID)); strings.Contains(m.Body, token) {
		t.Fatal("the sent link stays in the outbox")
	}
	if err := e.svc.ResetPassword(e.ctx, token, "a brand new long password"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.ResetPassword(e.ctx, token, "another long password!"); err == nil {
		t.Fatal("a link worked twice")
	}
	if _, _, err := e.svc.Login(e.ctx, "albie@example.com", "a brand new long password"); err != nil {
		t.Fatal(err)
	}
}

// Users & groups on MariaDB 11 (M44): a user in a group is deleted (memberships first),
// their sessions and reset links go with them, and the audit log keeps their name.
func TestEditAndDeleteUserPersistence(t *testing.T) {
	e := setup(t)
	admin := must(e.svc.CreateUser(e.ctx, "admin@example.com", "admin password long"))
	other := must(e.svc.CreateUser(e.ctx, "other@example.com", "other password long")) // in Administrators
	ctx := app.WithActor(e.ctx, app.Actor{ID: admin.ID, Name: admin.Email})
	u := must(e.svc.UpdateUser(ctx, other.ID, app.UserEdit{Login: "thandi@example.com", Profile: domain.Profile{FirstName: "Thandi"}}, "admin password long"))
	if u.Email != "thandi@example.com" || u.FirstName != "Thandi" {
		t.Fatalf("edited = %+v", u)
	}
	if _, err := e.svc.UpdateUser(ctx, other.ID, app.UserEdit{Login: "ADMIN@example.com"}, "admin password long"); err == nil {
		t.Fatal("a taken user name in another letter case")
	}
	if _, _, err := e.svc.Login(e.ctx, "thandi@example.com", "other password long"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RequestPasswordReset(e.ctx, "thandi@example.com", "https://inv.example.com"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.DeleteUser(ctx, other.ID, "admin password long"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.Repos().Users.Get(e.ctx, other.ID); err == nil {
		t.Fatal("not deleted")
	}
	es, _ := must3(e.svc.ListAudit(e.ctx, ports.AuditFilter{Action: "user.deleted"}))
	if len(es) != 1 || !strings.Contains(es[0].Summary, "thandi@example.com (Thandi)") || es[0].ActorName != "admin@example.com" {
		t.Fatalf("audit = %+v", es)
	}
}
