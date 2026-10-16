package domain

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Permission names one thing a group can be allowed to do (ADR-012).
type Permission string

// The permissions a group can be given. Administration (users, groups, backups and
// restore) is not among them: only the Administrators group has it (PermAdminister).
const (
	PermDashboardView  Permission = "dashboard.view"
	PermInvoicesView   Permission = "invoices.view"
	PermInvoicesEdit   Permission = "invoices.edit"
	PermInvoicesSend   Permission = "invoices.send"
	PermInvoicesStatus Permission = "invoices.status"
	PermCustomersView  Permission = "customers.view"
	PermCustomersEdit  Permission = "customers.edit"
	PermStatementsView Permission = "statements.view"
	PermStatementsSend Permission = "statements.send"
	PermSchedulesView  Permission = "schedules.view"
	PermSchedulesEdit  Permission = "schedules.edit"
	PermProductsView   Permission = "products.view"
	PermProductsEdit   Permission = "products.edit"
	PermEmailsView     Permission = "emails.view"
	PermEmailsManage   Permission = "emails.manage"
	PermAuditView      Permission = "audit.view"
	PermSettingsView   Permission = "settings.view"
	PermSettingsEdit   Permission = "settings.edit"

	// PermAdminister is users, groups, backups and restore. It cannot be granted: a user
	// has it only as a member of the Administrators group.
	PermAdminister Permission = "administer"
)

// AdministratorsGroupID is the built-in group that has every permission, administration
// included. It cannot be renamed, changed or deleted and always keeps an active member.
const AdministratorsGroupID = "administrators"

// PermissionInfo describes a grantable permission for the group editor.
type PermissionInfo struct {
	Key   Permission
	Area  string
	Label string
	// Implies lists the permissions this one needs to be usable (e.g. editing invoices
	// needs to see them, and to pick customers and products); they are granted with it.
	Implies []Permission
}

// Permissions is the catalogue of grantable permissions, in the order the editor shows
// them (grouped by area).
var Permissions = []PermissionInfo{
	{PermDashboardView, "Dashboard", "See the dashboard", nil},
	{PermInvoicesView, "Invoices", "See invoices and their PDFs", nil},
	{PermInvoicesEdit, "Invoices", "Create, edit, issue and delete draft invoices", []Permission{PermInvoicesView, PermCustomersView, PermProductsView}},
	{PermInvoicesSend, "Invoices", "E-mail invoices", []Permission{PermInvoicesView}},
	{PermInvoicesStatus, "Invoices", "Mark invoices paid or overdue, and void them", []Permission{PermInvoicesView}},
	{PermCustomersView, "Customers", "See customers", nil},
	{PermCustomersEdit, "Customers", "Add, edit and delete customers", []Permission{PermCustomersView}},
	{PermStatementsView, "Statements", "See statements and their PDFs", []Permission{PermCustomersView}},
	{PermStatementsSend, "Statements", "E-mail statements", []Permission{PermStatementsView}},
	{PermSchedulesView, "Schedules", "See schedules", nil},
	{PermSchedulesEdit, "Schedules", "Create, edit, pause, resume, run and delete schedules", []Permission{PermSchedulesView, PermCustomersView, PermProductsView}},
	{PermProductsView, "Products", "See products", nil},
	{PermProductsEdit, "Products", "Add, edit and delete products", []Permission{PermProductsView}},
	{PermEmailsView, "Outbox", "See the e-mail outbox", nil},
	{PermEmailsManage, "Outbox", "Cancel and retry e-mails", []Permission{PermEmailsView}},
	{PermAuditView, "Audit log", "See and verify the audit log", nil},
	{PermSettingsView, "Settings", "See the business, e-mail server and currency settings", nil},
	{PermSettingsEdit, "Settings", "Change the business, e-mail server, currencies and bank accounts", []Permission{PermSettingsView}},
}

// Known reports whether p is a grantable permission.
func (p Permission) Known() bool {
	return slices.ContainsFunc(Permissions, func(i PermissionInfo) bool { return i.Key == p })
}

func implies(p Permission) []Permission {
	for _, i := range Permissions {
		if i.Key == p {
			return i.Implies
		}
	}
	return nil
}

// WithImplied returns ps with every permission they imply, transitively, sorted and
// without duplicates.
func WithImplied(ps []Permission) []Permission {
	seen := map[Permission]bool{}
	var add func(p Permission)
	add = func(p Permission) {
		if seen[p] {
			return
		}
		seen[p] = true
		for _, q := range implies(p) {
			add(q)
		}
	}
	for _, p := range ps {
		add(p)
	}
	out := make([]Permission, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// Group gives its members permissions. A user's access is the union of their groups'.
type Group struct {
	ID          string
	Name        string
	Description string
	// Builtin is set for the Administrators group only.
	Builtin bool
	// Permissions are those granted (implied ones are added when access is computed). The
	// Administrators group stores none: it has everything.
	Permissions []Permission
	// MemberIDs are the ids of the users in the group, sorted.
	MemberIDs []string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// IsAdministrators reports whether g is the built-in Administrators group.
func (g Group) IsAdministrators() bool { return g.ID == AdministratorsGroupID }

// HasMember reports whether the user is in the group.
func (g Group) HasMember(userID string) bool { return slices.Contains(g.MemberIDs, userID) }

// Normalize trims and checks a group's name, description and permissions, and sorts its
// permissions and members without duplicates. Names are 1–80 characters, descriptions
// at most 500; every permission must be in the catalogue.
func (g *Group) Normalize() error {
	g.Name = strings.TrimSpace(g.Name)
	g.Description = strings.TrimSpace(g.Description)
	switch n := utf8.RuneCountInString(g.Name); {
	case n == 0:
		return invalid("name", "is required")
	case n > 80:
		return invalid("name", "must be at most 80 characters")
	}
	if utf8.RuneCountInString(g.Description) > 500 {
		return invalid("description", "must be at most 500 characters")
	}
	for _, p := range g.Permissions {
		if !p.Known() {
			return invalid("permissions", "unknown permission "+string(p))
		}
	}
	g.Permissions = sortedUnique(g.Permissions)
	g.MemberIDs = sortedUnique(g.MemberIDs)
	return nil
}

func sortedUnique[T ~string](xs []T) []T {
	out := slices.Clone(xs)
	slices.Sort(out)
	return slices.Compact(out)
}

// Access is what a signed-in user may do: the union of their groups' permissions, with
// the permissions those imply. A member of Administrators may do everything.
type Access struct {
	Administrator bool
	granted       []Permission
}

// AccessFor computes the access a member of groups has.
func AccessFor(groups []Group) Access {
	var a Access
	var ps []Permission
	for _, g := range groups {
		if g.IsAdministrators() {
			a.Administrator = true
		}
		ps = append(ps, g.Permissions...)
	}
	a.granted = WithImplied(ps)
	return a
}

// Has reports whether the access includes p.
func (a Access) Has(p Permission) bool {
	return a.Administrator || slices.Contains(a.granted, p)
}

// Permissions lists the grantable permissions the access includes, sorted (every one for
// an administrator).
func (a Access) Permissions() []Permission {
	if !a.Administrator {
		return slices.Clone(a.granted)
	}
	out := make([]Permission, len(Permissions))
	for i, p := range Permissions {
		out[i] = p.Key
	}
	slices.Sort(out)
	return out
}

// CheckGroupChange decides whether actorID may make a membership change, given the active
// members of Administrators read under a lock. Only an active administrator changes
// groups (ErrNoAccess otherwise — e.g. removed meanwhile by another administrator), and
// nobody removes themselves from Administrators (ErrForbidden), so it always keeps an
// active member: the actor.
func CheckGroupChange(actorID string, activeAdmins []string, removedFromAdmins []string) error {
	if !slices.Contains(activeAdmins, actorID) {
		return ErrNoAccess
	}
	if slices.Contains(removedFromAdmins, actorID) {
		return ErrForbidden
	}
	return nil
}
