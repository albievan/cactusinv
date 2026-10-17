package app_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
	"github.com/albievan/cactusinv/internal/ports/fakes"
)

const (
	adminPW = "admin password long"
	otherPW = "other password long"
)

// users seeds two active users and returns a context acting as the first (the admin).
func (h *harness) users() (ctx context.Context, admin, other domain.User) {
	h.t.Helper()
	admin = must(h.svc.CreateUser(h.ctx, "admin@example.com", adminPW))
	other = must(h.svc.CreateUser(h.ctx, "other@example.com", otherPW))
	return app.WithActor(h.ctx, app.Actor{ID: admin.ID, Name: admin.Email}), admin, other
}

// audited returns the newest audit entry whose action is action, or fails.
func (h *harness) audited(action string) domain.AuditEntry {
	h.t.Helper()
	es, _ := must3(h.svc.ListAudit(h.ctx, ports.AuditFilter{Action: action}))
	for _, e := range es {
		if e.Action == action {
			return e
		}
	}
	h.t.Fatalf("no %s audit entry", action)
	return domain.AuditEntry{}
}

func (h *harness) auditCount(action string) int {
	es, _ := must3(h.svc.ListAudit(h.ctx, ports.AuditFilter{Action: action}))
	n := 0
	for _, e := range es {
		if e.Action == action {
			n++
		}
	}
	return n
}

func (h *harness) user(id string) domain.User {
	h.t.Helper()
	return must(h.store.Repos().Users.Get(h.ctx, id))
}

func (h *harness) login(login, pw string) string {
	h.t.Helper()
	tok, _, err := h.svc.Login(h.ctx, login, pw)
	if err != nil {
		h.t.Fatalf("login %s: %v", login, err)
	}
	return tok
}

func (h *harness) signedIn(tok string) bool {
	_, err := h.svc.Authenticate(h.ctx, tok)
	return err == nil
}

func fieldOf(err error) string {
	var ve *domain.ValidationError
	if errors.As(err, &ve) {
		return ve.Field
	}
	return ""
}

func TestAddUser(t *testing.T) {
	tests := []struct {
		name, login, pw, current string
		want                     error
		field                    string
	}{
		{"added", "new@example.com", "a new user password", adminPW, nil, ""},
		{"trimmed", "  new@example.com ", "a new user password", adminPW, nil, ""},
		{"login is not an e-mail address", "newbie", "a new user password", adminPW, domain.ErrValidation, "email"},
		{"short password", "new@example.com", "short", adminPW, domain.ErrValidation, "password"},
		{"wrong current password", "new@example.com", "a new user password", "guess", domain.ErrValidation, "currentPassword"},
		{"login exists in another case", "OTHER@example.com", "a new user password", adminPW, domain.ErrConflict, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			ctx, admin, _ := h.users()
			u, err := h.svc.AddUser(ctx, tt.login, tt.pw, tt.current, nil)
			if !errors.Is(err, tt.want) || (tt.want == nil && err != nil) || fieldOf(err) != tt.field {
				t.Fatalf("AddUser = %v (field %q), want %v (%q)", err, fieldOf(err), tt.want, tt.field)
			}
			if tt.field == "currentPassword" {
				if e := h.audited("user.reauth_failed"); e.ActorID != admin.ID || !strings.Contains(e.Summary, "adding new@example.com") {
					t.Fatalf("reauth audit = %+v", e)
				}
			}
			if err != nil {
				if n := len(must(h.svc.ListUsers(h.ctx))); n != 2 {
					t.Fatalf("users after a refusal = %d", n)
				}
				return
			}
			got := h.user(u.ID)
			if got.Email != "new@example.com" || !got.MustChangePassword || !got.Active() {
				t.Fatalf("added = %+v", got)
			}
			if e := h.audited("user.created"); e.EntityID != u.ID || e.ActorID != admin.ID {
				t.Fatalf("audit = %+v", e)
			}
			// The new user signs in, and can do nothing but change the password.
			tok := h.login("new@example.com", "a new user password")
			me := must(h.svc.Authenticate(h.ctx, tok))
			if err := app.SessionAllows(me, "listInvoices"); !errors.Is(err, domain.ErrPasswordChangeRequired) {
				t.Fatalf("must-change session reaches invoices: %v", err)
			}
		})
	}
}

func TestAddUserByADisabledActor(t *testing.T) {
	h := newHarness(t)
	_, admin, _ := h.users()
	if err := h.store.Repos().Users.SetDisabled(h.ctx, admin.ID, start); err != nil {
		t.Fatal(err)
	}
	ctx := app.WithActor(h.ctx, app.Actor{ID: admin.ID, Name: admin.Email})
	if _, err := h.svc.AddUser(ctx, "new@example.com", "a new user password", adminPW, nil); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("disabled actor: %v", err)
	}
}

func TestResetUserPassword(t *testing.T) {
	tests := []struct {
		name, target, pw, current string
		want                      error
		field                     string
	}{
		{"reset", "other", "a temporary password", adminPW, nil, ""},
		{"yourself", "admin", "a temporary password", adminPW, domain.ErrForbidden, ""},
		{"unknown user", "nobody", "a temporary password", adminPW, domain.ErrNotFound, ""},
		{"short password", "other", "short", adminPW, domain.ErrValidation, "password"},
		{"wrong current password", "other", "a temporary password", otherPW, domain.ErrValidation, "currentPassword"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			ctx, admin, other := h.users()
			ids := map[string]string{"admin": admin.ID, "other": other.ID, "nobody": "nobody"}
			adminTok := h.login(admin.Email, adminPW)
			otherTok := h.login(other.Email, otherPW)
			_, err := h.svc.ResetUserPassword(ctx, ids[tt.target], tt.pw, tt.current)
			if !errors.Is(err, tt.want) || (tt.want == nil && err != nil) || fieldOf(err) != tt.field {
				t.Fatalf("ResetUserPassword = %v, want %v (%q)", err, tt.want, tt.field)
			}
			if tt.field == "currentPassword" && h.auditCount("user.reauth_failed") != 1 {
				t.Fatal("a wrong current password is audited")
			}
			if !h.signedIn(adminTok) {
				t.Fatal("the actor stays signed in")
			}
			if err != nil {
				if !h.signedIn(otherTok) || h.user(other.ID).MustChangePassword {
					t.Fatal("a refused reset changes nothing")
				}
				return
			}
			if h.signedIn(otherTok) {
				t.Fatal("the reset user's sessions end")
			}
			if !h.user(other.ID).MustChangePassword {
				t.Fatal("a reset password must be changed")
			}
			if _, _, err := h.svc.Login(h.ctx, other.Email, otherPW); err == nil {
				t.Fatal("the old password no longer works")
			}
			h.login(other.Email, "a temporary password")
			if e := h.audited("user.password_reset"); e.EntityID != other.ID || e.ActorID != admin.ID {
				t.Fatalf("audit = %+v", e)
			}
		})
	}
}

func TestDisableUser(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		current string
		setup   func(h *harness, admin, other domain.User)
		want    error
		field   string
	}{
		{name: "disabled", target: "other", current: adminPW},
		{name: "yourself", target: "admin", current: adminPW, want: domain.ErrForbidden},
		{name: "unknown user", target: "nobody", current: adminPW, want: domain.ErrNotFound},
		{name: "wrong current password", target: "other", current: "guess", want: domain.ErrValidation, field: "currentPassword"},
		{
			name: "already disabled", target: "other", current: adminPW,
			setup: func(h *harness, _, other domain.User) {
				_ = h.store.Repos().Users.SetDisabled(h.ctx, other.ID, start.Add(-time.Hour))
			},
		},
		{
			name: "the actor was disabled meanwhile", target: "other", current: adminPW, want: domain.ErrUnauthenticated,
			setup: func(h *harness, admin, _ domain.User) {
				_ = h.store.Repos().Users.SetDisabled(h.ctx, admin.ID, start.Add(-time.Hour))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			ctx, admin, other := h.users()
			ids := map[string]string{"admin": admin.ID, "other": other.ID, "nobody": "nobody"}
			otherTok := h.login(other.Email, otherPW)
			if tt.setup != nil {
				tt.setup(h, admin, other)
			}
			before := h.auditCount("user.disabled")
			u, err := h.svc.DisableUser(ctx, ids[tt.target], tt.current)
			if !errors.Is(err, tt.want) || (tt.want == nil && err != nil) || fieldOf(err) != tt.field {
				t.Fatalf("DisableUser = %v, want %v (%q)", err, tt.want, tt.field)
			}
			if err != nil {
				if h.auditCount("user.disabled") != before {
					t.Fatal("a refusal is not recorded as a disable")
				}
				return
			}
			if u.Active() || h.user(other.ID).Active() {
				t.Fatal("the user is disabled")
			}
			if h.signedIn(otherTok) {
				t.Fatal("the disabled user's sessions end")
			}
			if tt.setup == nil && h.auditCount("user.disabled") != 1 {
				t.Fatal("disabling is audited once")
			}
			if tt.setup != nil && h.auditCount("user.disabled") != 0 {
				t.Fatal("disabling a disabled user changes nothing")
			}
		})
	}
}

func TestTheLastActiveUserIsNeverDisabled(t *testing.T) {
	h := newHarness(t)
	ctx, admin, other := h.users()
	must(h.svc.DisableUser(ctx, other.ID, adminPW))
	// The only active user cannot disable themselves.
	if _, err := h.svc.DisableUser(ctx, admin.ID, adminPW); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("self: %v", err)
	}
	third := must(h.svc.CreateUser(h.ctx, "third@example.com", "third password long"))
	thirdCtx := app.WithActor(h.ctx, app.Actor{ID: third.ID, Name: third.Email})
	must(h.svc.DisableUser(thirdCtx, admin.ID, "third password long"))
	if _, err := h.svc.DisableUser(ctx, third.ID, adminPW); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("a disabled actor: %v", err)
	}
	// Re-enable admin, then admin disables third: two active → one, then refused.
	must(h.svc.EnableUser(thirdCtx, admin.ID, "third password long"))
	must(h.svc.DisableUser(ctx, third.ID, adminPW))
	n := 0
	for _, u := range must(h.svc.ListUsers(h.ctx)) {
		if u.Active() {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("active users = %d", n)
	}
	if _, err := h.svc.DisableUser(thirdCtx, admin.ID, "third password long"); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("a disabled user cannot disable the last active one: %v", err)
	}
}

func TestEnableUser(t *testing.T) {
	h := newHarness(t)
	ctx, _, other := h.users()
	if _, err := h.svc.EnableUser(ctx, other.ID, "guess"); fieldOf(err) != "currentPassword" {
		t.Fatalf("wrong password: %v", err)
	}
	if u := must(h.svc.EnableUser(ctx, other.ID, adminPW)); !u.Active() || h.auditCount("user.enabled") != 0 {
		t.Fatal("enabling an active user changes nothing")
	}
	must(h.svc.DisableUser(ctx, other.ID, adminPW))
	if _, _, err := h.svc.Login(h.ctx, other.Email, otherPW); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("a disabled user cannot sign in")
	}
	u := must(h.svc.EnableUser(ctx, other.ID, adminPW))
	if !u.Active() || !h.user(other.ID).Active() || h.auditCount("user.enabled") != 1 {
		t.Fatalf("enabled = %+v", u)
	}
	h.login(other.Email, otherPW)
	if _, err := h.svc.EnableUser(ctx, "nobody", adminPW); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}

func TestLoginRecordsWhyItFailed(t *testing.T) {
	h := newHarness(t)
	ctx, _, other := h.users()
	tests := []struct {
		name, login, pw, reason string
		setup                   func()
	}{
		{name: "unknown login", login: "nobody@example.com", pw: otherPW, reason: "unknown login"},
		{name: "wrong password", login: other.Email, pw: "wrong password!!", reason: "wrong password"},
		{name: "disabled", login: other.Email, pw: otherPW, reason: "the user is disabled", setup: func() { must(h.svc.DisableUser(ctx, other.ID, adminPW)) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			_, _, err := h.svc.Login(h.ctx, tt.login, tt.pw)
			if !errors.Is(err, domain.ErrUnauthenticated) || strings.Contains(err.Error(), tt.reason) {
				t.Fatalf("Login = %v: the failure is generic", err)
			}
			if e := h.audited("auth.login_failed"); !strings.HasSuffix(e.Summary, ": "+tt.reason) {
				t.Fatalf("audit summary = %q", e.Summary)
			}
		})
	}
}

func TestLoginRecordsLastLogin(t *testing.T) {
	h := newHarness(t)
	_, admin, _ := h.users()
	if !h.user(admin.ID).LastLoginAt.IsZero() {
		t.Fatal("never signed in")
	}
	h.clock.Advance(time.Hour)
	h.login("ADMIN@example.com", adminPW)
	if got := h.user(admin.ID).LastLoginAt; !got.Equal(start.Add(time.Hour)) {
		t.Fatalf("last login = %v", got)
	}
}

func TestAuthenticateRefusesADisabledUsersSession(t *testing.T) {
	h := newHarness(t)
	_, _, other := h.users()
	tok := h.login(other.Email, otherPW)
	// A session that survived the disable (e.g. written by a racing sign-in).
	if err := h.store.Repos().Users.SetDisabled(h.ctx, other.ID, start); err != nil {
		t.Fatal(err)
	}
	if h.signedIn(tok) {
		t.Fatal("a disabled user's session is refused")
	}
}

func TestSessionAllows(t *testing.T) {
	must := domain.User{ID: "u", MustChangePassword: true}
	free := domain.User{ID: "u", Access: domain.AccessFor([]domain.Group{{ID: domain.AdministratorsGroupID, Builtin: true}})}
	clerk := domain.User{ID: "c", Access: domain.AccessFor([]domain.Group{{ID: "g", Permissions: []domain.Permission{domain.PermInvoicesSend, domain.PermStatementsView}}})}
	nobody := domain.User{ID: "n"}
	tests := []struct {
		user domain.User
		op   string
		want error
	}{
		{must, "me", nil},
		{must, "changePassword", nil},
		{must, "logout", nil},
		{must, "ChangePassword", nil}, // the generated server's spelling
		{must, "Me", nil},
		{must, "ListInvoices", domain.ErrPasswordChangeRequired},
		{must, "login", domain.ErrPasswordChangeRequired},
		{must, "listInvoices", domain.ErrPasswordChangeRequired},
		{must, "listUsers", domain.ErrPasswordChangeRequired},
		{must, "exportBackup", domain.ErrPasswordChangeRequired},
		{must, "uploadBackup", domain.ErrPasswordChangeRequired},
		{must, "applyRestore", domain.ErrPasswordChangeRequired},
		{must, "", domain.ErrPasswordChangeRequired},
		{free, "listInvoices", nil},
		{free, "applyRestore", nil},
		{free, "createGroup", nil},
		{free, "noSuchOperation", domain.ErrNoAccess},
		{clerk, "sendInvoice", nil},
		{clerk, "listInvoices", nil},  // implied by invoices.send
		{clerk, "listCustomers", nil}, // implied by statements.view
		{clerk, "listEmails", nil},    // a statement page lists its e-mails
		{clerk, "markInvoicePaid", domain.ErrNoAccess},
		{clerk, "updateBusiness", domain.ErrNoAccess},
		{clerk, "listUsers", domain.ErrNoAccess},
		{clerk, "applyRestore", domain.ErrNoAccess},
		{clerk, "getBusiness", nil},
		{nobody, "me", nil},
		{nobody, "listCurrencies", nil},
		{nobody, "getDashboard", domain.ErrNoAccess},
		{nobody, "listInvoices", domain.ErrNoAccess},
	}
	for _, tt := range tests {
		if err := app.SessionAllows(tt.user, tt.op); !errors.Is(err, tt.want) || (tt.want == nil && err != nil) {
			t.Errorf("SessionAllows(must=%v, %q) = %v, want %v", tt.user.MustChangePassword, tt.op, err, tt.want)
		}
	}
}

func TestForcedPasswordChange(t *testing.T) {
	h := newHarness(t)
	if !must(h.svc.EnsureDefaultAdmin(h.ctx, "admin", "admin")) {
		t.Fatal("created")
	}
	admin := must(h.store.Repos().Users.GetByEmail(h.ctx, "admin"))
	if !admin.MustChangePassword {
		t.Fatal("the default administrator must change its password")
	}
	phone := h.login("admin", "admin")
	laptop := h.login("admin", "admin")
	ctx := app.WithActor(h.ctx, app.Actor{ID: admin.ID, Name: admin.Email})
	tests := []struct {
		name, current, next, field string
	}{
		{"wrong current password", "nimda", "a brand new password", "currentPassword"},
		{"short", "admin", "admin2", "newPassword"},
	}
	for _, tt := range tests {
		if err := h.svc.ChangePassword(ctx, admin.ID, phone, tt.current, tt.next); fieldOf(err) != tt.field {
			t.Fatalf("%s: %v", tt.name, err)
		}
	}
	if h.auditCount("user.reauth_failed") != 1 {
		t.Fatal("a wrong current password is audited")
	}
	if err := h.svc.ChangePassword(ctx, admin.ID, phone, "admin", "a brand new password"); err != nil {
		t.Fatal(err)
	}
	got := h.user(admin.ID)
	if got.MustChangePassword {
		t.Fatal("changing the password clears the flag")
	}
	if !h.signedIn(phone) || h.signedIn(laptop) {
		t.Fatal("the session used stays; the others end")
	}
	if err := app.SessionAllows(must(h.svc.Authenticate(h.ctx, phone)), "listInvoices"); err != nil {
		t.Fatalf("after the change: %v", err)
	}
	if err := h.svc.ChangePassword(ctx, admin.ID, phone, "a brand new password", "a brand new password"); fieldOf(err) != "newPassword" {
		t.Fatalf("the same password again: %v", err)
	}
	if e := h.audited("user.password_changed"); e.EntityID != admin.ID {
		t.Fatalf("audit = %+v", e)
	}
	// The old login keeps working although it is not an e-mail address.
	h.login("admin", "a brand new password")
}

func TestSetPasswordOnTheServer(t *testing.T) {
	h := newHarness(t)
	ctx, admin, other := h.users()
	must(h.svc.ResetUserPassword(ctx, other.ID, "a temporary password", adminPW))
	tok := h.login(other.Email, "a temporary password")
	if err := h.svc.SetPassword(h.ctx, " other@example.com ", "chosen at the host"); err != nil {
		t.Fatal(err)
	}
	if got := h.user(other.ID); got.MustChangePassword {
		t.Fatal("a password set on the server is not flagged")
	}
	if h.signedIn(tok) {
		t.Fatal("the user's sessions end")
	}
	h.login(other.Email, "chosen at the host")
	if h.login(admin.Email, adminPW) == "" {
		t.Fatal("other users are untouched")
	}
}

func TestUserUseCasesPropagateRepositoryErrors(t *testing.T) {
	boom := errors.New("db down")
	calls := []func(h *harness, ctx context.Context, other domain.User) error{
		func(h *harness, _ context.Context, _ domain.User) error { _, err := h.svc.ListUsers(h.ctx); return err },
		func(h *harness, ctx context.Context, _ domain.User) error {
			_, err := h.svc.AddUser(ctx, "new@example.com", "a new user password", adminPW, nil)
			return err
		},
		func(h *harness, ctx context.Context, o domain.User) error {
			_, err := h.svc.ResetUserPassword(ctx, o.ID, "a temporary password", adminPW)
			return err
		},
		func(h *harness, ctx context.Context, o domain.User) error {
			_, err := h.svc.DisableUser(ctx, o.ID, adminPW)
			return err
		},
		func(h *harness, ctx context.Context, o domain.User) error {
			_, err := h.svc.EnableUser(ctx, o.ID, adminPW)
			return err
		},
		func(h *harness, ctx context.Context, o domain.User) error {
			return h.svc.ChangePassword(ctx, o.ID, "", otherPW, "a brand new password")
		},
		func(h *harness, _ context.Context, o domain.User) error {
			return h.svc.SetPassword(h.ctx, o.Email, "a brand new password")
		},
		func(h *harness, _ context.Context, _ domain.User) error {
			_, err := h.svc.Authenticate(h.ctx, "token")
			return err
		},
	}
	for i, call := range calls {
		h := newHarness(t)
		ctx, _, other := h.users()
		h.store.FailNext = boom
		if err := call(h, ctx, other); !errors.Is(err, boom) {
			t.Errorf("call %d: err = %v, want db down", i, err)
		}
	}
}

// blockingHasher holds its first Verify until released: the password has been checked but
// the use case has not reached its transaction yet.
type blockingHasher struct {
	fakes.Hasher
	once             sync.Once
	entered, release chan struct{}
}

func newBlockingHasher() *blockingHasher {
	return &blockingHasher{entered: make(chan struct{}), release: make(chan struct{})}
}

func (b *blockingHasher) Verify(hash, pw string) (bool, error) {
	first := false
	b.once.Do(func() { first = true })
	if first {
		close(b.entered)
		<-b.release
	}
	return b.Hasher.Verify(hash, pw)
}

// TestSignInRacingAChange: a sign-in whose password was checked just before the user's
// password changed or the user was disabled starts no session.
func TestSignInRacingAChange(t *testing.T) {
	tests := []struct {
		name   string
		change func(h *harness, svc *app.Service, ctx context.Context, other domain.User) error
	}{
		{"own password change", func(h *harness, svc *app.Service, _ context.Context, o domain.User) error {
			return svc.ChangePassword(app.WithActor(h.ctx, app.Actor{ID: o.ID, Name: o.Email}), o.ID, "", otherPW, "a brand new password")
		}},
		{"reset by an administrator", func(_ *harness, svc *app.Service, ctx context.Context, o domain.User) error {
			_, err := svc.ResetUserPassword(ctx, o.ID, "a temporary password", adminPW)
			return err
		}},
		{"set on the server", func(h *harness, svc *app.Service, _ context.Context, o domain.User) error {
			return svc.SetPassword(h.ctx, o.Email, "chosen at the host")
		}},
		{"disabled", func(_ *harness, svc *app.Service, ctx context.Context, o domain.User) error {
			_, err := svc.DisableUser(ctx, o.ID, adminPW)
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			ctx, _, other := h.users()
			bh := newBlockingHasher()
			svc := app.New(app.Deps{Tx: h.store, Clock: h.clock, IDs: &fakes.IDs{}, Mailer: h.mailer, Renderer: fakes.Renderer{}, Hasher: bh})
			type result struct {
				tok string
				err error
			}
			done := make(chan result, 1)
			go func() {
				tok, _, err := svc.Login(h.ctx, other.Email, otherPW)
				done <- result{tok, err}
			}()
			<-bh.entered
			if err := tt.change(h, svc, ctx, other); err != nil {
				t.Fatal(err)
			}
			logins := h.auditCount("auth.login")
			close(bh.release)
			res := <-done
			if !errors.Is(res.err, domain.ErrUnauthenticated) || res.tok != "" {
				t.Fatalf("raced sign-in = %q, %v", res.tok, res.err)
			}
			if h.auditCount("auth.login") != logins || !h.user(other.ID).LastLoginAt.IsZero() {
				t.Fatal("a raced sign-in records no sign-in")
			}
			if e := h.audited("auth.login_failed"); !strings.Contains(e.Summary, "during the sign-in") {
				t.Fatalf("reason = %q", e.Summary)
			}
		})
	}
}

// TestActorDisabledDuringAChange: an actor disabled after re-authenticating changes nothing.
func TestActorDisabledDuringAChange(t *testing.T) {
	h := newHarness(t)
	ctx, admin, other := h.users()
	must(h.svc.DisableUser(ctx, other.ID, adminPW)) // for the enable case
	calls := map[string]func(svc *app.Service) error{
		"add": func(svc *app.Service) error {
			_, err := svc.AddUser(ctx, "new@example.com", "a new user password", adminPW, nil)
			return err
		},
		"reset": func(svc *app.Service) error {
			_, err := svc.ResetUserPassword(ctx, other.ID, "a temporary password", adminPW)
			return err
		},
		"enable": func(svc *app.Service) error {
			_, err := svc.EnableUser(ctx, other.ID, adminPW)
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			bh := newBlockingHasher()
			svc := app.New(app.Deps{Tx: h.store, Clock: h.clock, IDs: &fakes.IDs{}, Mailer: h.mailer, Renderer: fakes.Renderer{}, Hasher: bh})
			done := make(chan error, 1)
			go func() { done <- call(svc) }()
			<-bh.entered
			if err := h.store.Repos().Users.SetDisabled(h.ctx, admin.ID, start); err != nil {
				t.Fatal(err)
			}
			close(bh.release)
			if err := <-done; !errors.Is(err, domain.ErrUnauthenticated) {
				t.Fatalf("%s by a disabled actor: %v", name, err)
			}
			if err := h.store.Repos().Users.SetDisabled(h.ctx, admin.ID, time.Time{}); err != nil {
				t.Fatal(err)
			}
		})
	}
	if len(must(h.svc.ListUsers(h.ctx))) != 2 || h.user(other.ID).Active() || h.user(other.ID).MustChangePassword {
		t.Fatal("nothing changed")
	}
}

func TestEnablingEndsLeftoverSessions(t *testing.T) {
	h := newHarness(t)
	ctx, _, other := h.users()
	tok := h.login(other.Email, otherPW)
	// A session that survived the disable (written around it).
	if err := h.store.Repos().Users.SetDisabled(h.ctx, other.ID, start); err != nil {
		t.Fatal(err)
	}
	must(h.svc.EnableUser(ctx, other.ID, adminPW))
	if h.signedIn(tok) {
		t.Fatal("enabling a user does not revive a session from before")
	}
}

// strictHasher refuses a malformed hash before hashing, as argon2 does.
type strictHasher struct {
	fakes.Hasher
	malformed *int
}

func (h strictHasher) Verify(hash, pw string) (bool, error) {
	ok, err := h.Hasher.Verify(hash, pw)
	if err != nil {
		*h.malformed++
	}
	return ok, err
}

// An unknown login is checked against a real hash, so it takes as long as a wrong
// password (a malformed dummy hash answered at once and told which logins exist).
func TestUnknownLoginRunsARealPasswordCheck(t *testing.T) {
	h := newHarness(t)
	n := 0
	h.svc = app.New(app.Deps{Tx: h.store, Clock: h.clock, IDs: h.ids, Hasher: strictHasher{malformed: &n}})
	for range 2 {
		if _, _, err := h.svc.Login(h.ctx, "nobody@example.com", "whatever password"); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatal(err)
		}
	}
	if n != 0 {
		t.Fatalf("the dummy hash was refused %d times without hashing", n)
	}
}
