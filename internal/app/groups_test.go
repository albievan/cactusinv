package app_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
)

func TestNewUserWithoutGroupsHasNoAccess(t *testing.T) {
	h := newHarness(t)
	ctx, _, _ := h.users()
	u := must(h.svc.AddUser(ctx, "clerk@example.com", "a first password!", adminPW, nil))
	if len(u.GroupIDs) != 0 || !strings.Contains(h.audited("user.created").Summary, "groups: none") {
		t.Fatalf("user = %+v", u)
	}
	must0(t, h.svc.ChangePassword(h.ctx, u.ID, "", "a first password!", "the clerk's own one"))
	got := must(h.svc.Authenticate(h.ctx, h.login("clerk@example.com", "the clerk's own one")))
	if got.Access.Administrator || len(got.Access.Permissions()) != 0 {
		t.Fatalf("access = %+v", got.Access)
	}
	if err := app.SessionAllows(got, "getDashboard"); !errors.Is(err, domain.ErrNoAccess) {
		t.Fatalf("dashboard: %v", err)
	}
}

func TestGroupLifecycle(t *testing.T) {
	h := newHarness(t)
	ctx, admin, other := h.users()
	clerk := must(h.svc.AddUser(ctx, "clerk@example.com", "a first password!", adminPW, nil))

	in := app.GroupInput{Name: " Accounts ", Description: "Bills customers", Permissions: []domain.Permission{domain.PermInvoicesSend}, MemberIDs: []string{clerk.ID}}
	if _, err := h.svc.CreateGroup(ctx, in, "wrong password"); fieldOf(err) != "currentPassword" || h.auditCount("user.reauth_failed") != 1 {
		t.Fatalf("wrong password: %v", err)
	}
	g := must(h.svc.CreateGroup(ctx, in, adminPW))
	if g.Name != "Accounts" || h.audited("group.created").Summary != "Accounts; access: invoices.send; members: clerk@example.com" {
		t.Fatalf("group = %+v, audit %q", g, h.audited("group.created").Summary)
	}
	if _, err := h.svc.CreateGroup(ctx, app.GroupInput{Name: "accounts"}, adminPW); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate name: %v", err)
	}
	if _, err := h.svc.CreateGroup(ctx, app.GroupInput{Name: "x", MemberIDs: []string{"nobody"}}, adminPW); fieldOf(err) != "memberIds" {
		t.Fatalf("unknown member: %v", err)
	}
	if _, err := h.svc.CreateGroup(ctx, app.GroupInput{Name: "x", Permissions: []domain.Permission{domain.PermAdminister}}, adminPW); fieldOf(err) != "permissions" {
		t.Fatalf("administer: %v", err)
	}

	// The clerk's access follows the group at once.
	must0(t, h.svc.ChangePassword(h.ctx, clerk.ID, "", "a first password!", "the clerk's own one"))
	tok := h.login("clerk@example.com", "the clerk's own one")
	if u := must(h.svc.Authenticate(h.ctx, tok)); !u.Access.Has(domain.PermInvoicesView) || u.Access.Has(domain.PermInvoicesStatus) || !slices.Equal(u.GroupIDs, []string{g.ID}) {
		t.Fatalf("access = %+v", u.Access)
	}

	// Add access, remove access, a member.
	in = app.GroupInput{Name: "Accounts", Permissions: []domain.Permission{domain.PermInvoicesStatus, domain.PermAuditView}, MemberIDs: []string{clerk.ID, other.ID}}
	g = must(h.svc.UpdateGroup(ctx, g.ID, in, adminPW))
	want := "Accounts; description changed; access added: audit.view, invoices.status; access removed: invoices.send; members added: other@example.com"
	if s := h.audited("group.updated").Summary; s != want {
		t.Fatalf("audit %q", s)
	}
	if u := must(h.svc.Authenticate(h.ctx, tok)); !u.Access.Has(domain.PermInvoicesStatus) || u.Access.Has(domain.PermInvoicesSend) {
		t.Fatalf("access after the change = %+v", u.Access)
	}
	if us := must(h.svc.ListUsers(ctx)); !slices.Equal(us[slices.IndexFunc(us, func(u domain.User) bool { return u.ID == other.ID })].GroupIDs, []string{domain.AdministratorsGroupID, g.ID}) {
		t.Fatalf("users = %+v", us)
	}

	// Delete: the clerk loses the access.
	if err := h.svc.DeleteGroup(ctx, domain.AdministratorsGroupID, adminPW); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("delete Administrators: %v", err)
	}
	must0(t, h.svc.DeleteGroup(ctx, g.ID, adminPW))
	if !strings.Contains(h.audited("group.deleted").Summary, "clerk@example.com") {
		t.Fatal("audit names the members")
	}
	if u := must(h.svc.Authenticate(h.ctx, tok)); len(u.Access.Permissions()) != 0 {
		t.Fatalf("access after delete = %+v", u.Access)
	}
	if gs := must(h.svc.ListGroups(ctx)); len(gs) != 1 || !gs[0].IsAdministrators() || !slices.Equal(gs[0].MemberIDs, sortedIDs(admin.ID, other.ID)) {
		t.Fatalf("groups = %+v", gs)
	}
}

func sortedIDs(ids ...string) []string {
	slices.Sort(ids)
	return ids
}

func TestAdministratorsGroup(t *testing.T) {
	h := newHarness(t)
	ctx, admin, other := h.users()
	clerk := must(h.svc.AddUser(ctx, "clerk@example.com", "a first password!", adminPW, nil))

	// Only the members change: a name or permissions sent are ignored.
	g := must(h.svc.UpdateGroup(ctx, domain.AdministratorsGroupID, app.GroupInput{
		Name: "Renamed", Permissions: []domain.Permission{domain.PermAuditView}, MemberIDs: []string{admin.ID, other.ID, clerk.ID},
	}, adminPW))
	if g.Name != "Administrators" || !g.Builtin || len(g.Permissions) != 0 || len(g.MemberIDs) != 3 {
		t.Fatalf("group = %+v", g)
	}
	// Nobody removes themselves — from the group or through their own groups.
	if _, err := h.svc.UpdateGroup(ctx, domain.AdministratorsGroupID, app.GroupInput{MemberIDs: []string{other.ID}}, adminPW); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("remove yourself: %v", err)
	}
	if _, err := h.svc.SetUserGroups(ctx, admin.ID, nil, adminPW); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("leave yourself: %v", err)
	}
	// Removing another administrator is fine, and audited.
	u := must(h.svc.SetUserGroups(ctx, other.ID, nil, adminPW))
	if len(u.GroupIDs) != 0 || h.audited("user.groups_changed").Summary != "other@example.com: groups none (was Administrators)" {
		t.Fatalf("user = %+v, audit %q", u, h.audited("user.groups_changed").Summary)
	}
	// Someone no longer an administrator cannot change groups, even with a valid password.
	otherCtx := app.WithActor(h.ctx, app.Actor{ID: other.ID, Name: other.Email})
	if _, err := h.svc.SetUserGroups(otherCtx, other.ID, []string{domain.AdministratorsGroupID}, otherPW); !errors.Is(err, domain.ErrNoAccess) {
		t.Fatalf("former administrator: %v", err)
	}
	if _, err := h.svc.AddUser(otherCtx, "x@example.com", "a first password!", otherPW, nil); !errors.Is(err, domain.ErrNoAccess) {
		t.Fatalf("former administrator adds a user: %v", err)
	}
	if _, err := h.svc.SetUserGroups(ctx, clerk.ID, []string{"nope"}, adminPW); fieldOf(err) != "groupIds" {
		t.Fatalf("unknown group: %v", err)
	}
	if _, err := h.svc.SetUserGroups(ctx, clerk.ID, nil, "wrong password"); fieldOf(err) != "currentPassword" {
		t.Fatalf("wrong password: %v", err)
	}
}

func TestCommandLineAndDefaultUsersAreAdministrators(t *testing.T) {
	h := newHarness(t)
	if !must(h.svc.EnsureDefaultAdmin(h.ctx, "admin", "admin")) {
		t.Fatal("created")
	}
	cli := must(h.svc.CreateUser(h.ctx, "ops@example.com", "a long cli password"))
	g := must(h.store.Repos().Groups.Get(h.ctx, domain.AdministratorsGroupID))
	if len(g.MemberIDs) != 2 || !g.HasMember(cli.ID) {
		t.Fatalf("administrators = %+v", g)
	}
}
