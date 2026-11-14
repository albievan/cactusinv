package app_test

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

var resetLinkRe = regexp.MustCompile(`/reset-password/([A-Za-z0-9_-]{43})\n`)

// Your account: the user's own name and e-mail address (M43).
func TestProfile(t *testing.T) {
	h := newHarness(t)
	u := must(h.svc.CreateUser(h.ctx, "albie@example.com", "correct horse battery"))
	got := must(h.svc.UpdateProfile(h.ctx, u.ID, domain.Profile{FirstName: " Albie ", LastName: "van der Merwe", EmailAddress: "albie@home.example"}))
	if got.FirstName != "Albie" || got.LastName != "van der Merwe" || got.EmailAddress != "albie@home.example" || got.Email != "albie@example.com" || got.ResetAddress() != "albie@home.example" {
		t.Fatalf("profile = %+v", got)
	}
	if _, err := h.svc.UpdateProfile(h.ctx, u.ID, domain.Profile{EmailAddress: "nope"}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("bad address: %v", err)
	}
	if a := h.store.Audit(); a[len(a)-1].Action != "user.profile_updated" {
		t.Fatalf("audit = %+v", a[len(a)-1])
	}
}

// Forgot password (M43): a single-use link, valid for an hour, e-mailed only to a user who
// exists, is active and has an address; the answer is the same in every case.
func TestForgotPassword(t *testing.T) {
	h := newHarness(t).seed()
	u := must(h.svc.CreateUser(h.ctx, "albie@example.com", "correct horse battery"))
	other, _, err := h.svc.Login(h.ctx, "albie@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	count := func() int {
		_, n := must3(h.svc.ListEmails(h.ctx, ports.EmailFilter{Page: ports.Page{Limit: 100}}))
		return n
	}
	before := count()
	for _, login := range []string{"nobody@example.com", "admin"} { // unknown: no e-mail, no error
		if err := h.svc.RequestPasswordReset(h.ctx, login, "https://inv.example.com"); err != nil {
			t.Fatalf("%s: %v", login, err)
		}
	}
	if count() != before {
		t.Fatal("an e-mail for an unknown user")
	}
	if err := h.svc.RequestPasswordReset(h.ctx, " ", "https://inv.example.com"); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("empty user name: %v", err)
	}

	must0(t, h.svc.RequestPasswordReset(h.ctx, "ALBIE@example.com", "https://inv.example.com/"))
	list, _ := must3(h.svc.ListEmails(h.ctx, ports.EmailFilter{Page: ports.Page{Limit: 100}}))
	var m domain.EmailMessage
	for _, e := range list {
		if e.Kind == domain.EmailKindPasswordReset {
			m = e
		}
	}
	link := resetLinkRe.FindStringSubmatch(m.Body)
	if link == nil || m.To[0] != "albie@example.com" || !strings.Contains(m.Body, "https://inv.example.com/reset-password/") {
		t.Fatalf("reset e-mail = %+v", m)
	}
	token := link[1]
	// A second request within two minutes sends nothing more.
	must0(t, h.svc.RequestPasswordReset(h.ctx, "albie@example.com", "https://inv.example.com"))
	if count() != before+1 {
		t.Fatalf("e-mails = %d", count()-before)
	}

	// Delivered, then the link is cleared from the outbox.
	must(h.svc.DispatchEmails(h.ctx, 10))
	sent := h.mailer.Sent[len(h.mailer.Sent)-1]
	if !strings.Contains(sent.Body, token) || sent.HTMLBody != "" || len(sent.Attachments) != 0 {
		t.Fatalf("sent = %+v", sent)
	}
	stored := must(h.store.Repos().Emails.Get(h.ctx, m.ID))
	if strings.Contains(stored.Body, token) || stored.Status != domain.EmailSent {
		t.Fatalf("the outbox keeps the link: %+v", stored)
	}

	must0(t, h.svc.CheckPasswordReset(h.ctx, token))
	if err := h.svc.ResetPassword(h.ctx, token, "short"); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("short password: %v", err)
	}
	must0(t, h.svc.ResetPassword(h.ctx, token, "a brand new long password"))
	if _, _, err := h.svc.Login(h.ctx, "albie@example.com", "correct horse battery"); err == nil {
		t.Fatal("the old password still works")
	}
	if _, _, err := h.svc.Login(h.ctx, "albie@example.com", "a brand new long password"); err != nil {
		t.Fatalf("new password: %v", err)
	}
	if _, err := h.svc.Authenticate(h.ctx, other); err == nil {
		t.Fatal("an earlier session survived the reset")
	}
	if got := must(h.svc.GetProfile(h.ctx, u.ID)); got.MustChangePassword {
		t.Fatal("a password the user chose must not need changing")
	}
	// Used once.
	if err := h.svc.ResetPassword(h.ctx, token, "another long password!"); err == nil || !strings.Contains(err.Error(), "ask for a new one") {
		t.Fatalf("used twice: %v", err)
	}
	if err := h.svc.CheckPasswordReset(h.ctx, token); err == nil {
		t.Fatal("a used link checks out")
	}
}

func TestPasswordResetExpiresAndNeedsAnAddress(t *testing.T) {
	h := newHarness(t).seed()
	must(h.svc.CreateUser(h.ctx, "albie@example.com", "correct horse battery"))
	must0(t, h.svc.RequestPasswordReset(h.ctx, "albie@example.com", "https://inv.example.com"))
	list, _ := must3(h.svc.ListEmails(h.ctx, ports.EmailFilter{}))
	token := resetLinkRe.FindStringSubmatch(list[0].Body)[1]
	h.clock.Advance(time.Hour)
	if err := h.svc.ResetPassword(h.ctx, token, "a brand new long password"); err == nil {
		t.Fatal("an expired link worked")
	}
	for _, bad := range []string{"", "x", strings.Repeat("A", 43)} {
		if err := h.svc.CheckPasswordReset(h.ctx, bad); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%q: %v", bad, err)
		}
	}

	// The default administrator ("admin") has no address until they add one.
	h2 := newHarness(t).seed() // no users yet: the default administrator is created
	h = h2
	must(h.svc.EnsureDefaultAdmin(h.ctx, "admin", "first-password-123"))
	admin := must(h.store.Repos().Users.GetByEmail(h.ctx, "admin"))
	pending := func() []domain.EmailMessage {
		l, _ := must3(h.svc.ListEmails(h.ctx, ports.EmailFilter{Status: domain.EmailPending, Page: ports.Page{Limit: 100}}))
		return l
	}
	n0 := len(pending())
	must0(t, h.svc.RequestPasswordReset(h.ctx, "admin", "https://inv.example.com"))
	if a := h.store.Audit(); a[len(a)-1].Action != "auth.password_reset_refused" {
		t.Fatalf("audit = %+v", a[len(a)-1])
	}
	must(h.svc.UpdateProfile(h.ctx, admin.ID, domain.Profile{EmailAddress: "owner@example.com"}))
	must0(t, h.svc.RequestPasswordReset(h.ctx, "admin", "https://inv.example.com"))
	var to []string
	for _, m := range pending() {
		if m.Kind == domain.EmailKindPasswordReset {
			to = append(to, m.To...)
		}
	}
	if len(pending()) != n0+1 || !strings.Contains(strings.Join(to, ","), "owner@example.com") {
		t.Fatalf("reset e-mails to %v", to)
	}
	_ = app.ActorSystem
}
