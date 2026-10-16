package domain

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestWithImpliedIsTransitive(t *testing.T) {
	got := WithImplied([]Permission{PermStatementsSend, PermStatementsSend})
	want := []Permission{PermCustomersView, PermStatementsSend, PermStatementsView}
	if !slices.Equal(got, want) {
		t.Fatalf("WithImplied = %v, want %v", got, want)
	}
	if got := WithImplied(nil); len(got) != 0 {
		t.Fatalf("WithImplied(nil) = %v", got)
	}
}

func TestEveryImpliedPermissionIsKnown(t *testing.T) {
	seen := map[Permission]bool{}
	for _, p := range Permissions {
		if seen[p.Key] {
			t.Fatalf("%s listed twice", p.Key)
		}
		seen[p.Key] = true
		if p.Area == "" || p.Label == "" {
			t.Fatalf("%s has no area or label", p.Key)
		}
		for _, q := range p.Implies {
			if !q.Known() {
				t.Fatalf("%s implies unknown %s", p.Key, q)
			}
		}
	}
	if PermAdminister.Known() {
		t.Fatal("administer must not be grantable")
	}
}

func TestGroupNormalize(t *testing.T) {
	tests := []struct {
		name  string
		g     Group
		field string
	}{
		{"ok", Group{Name: " Accounts ", Permissions: []Permission{PermInvoicesView, PermAuditView, PermInvoicesView}, MemberIDs: []string{"b", "a", "b"}}, ""},
		{"no name", Group{Name: "  "}, "name"},
		{"long name", Group{Name: strings.Repeat("x", 81)}, "name"},
		{"long description", Group{Name: "x", Description: strings.Repeat("é", 501)}, "description"},
		{"unknown permission (retired by 00019)", Group{Name: "x", Permissions: []Permission{"invoices.status"}}, "permissions"},
		{"administer is not grantable", Group{Name: "x", Permissions: []Permission{PermAdminister}}, "permissions"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.g.Normalize()
			var ve *ValidationError
			switch {
			case tc.field == "" && err != nil:
				t.Fatalf("unexpected %v", err)
			case tc.field != "" && (!errors.As(err, &ve) || ve.Field != tc.field):
				t.Fatalf("want a validation error on %s, got %v", tc.field, err)
			}
			if tc.field == "" {
				if tc.g.Name != "Accounts" || !slices.Equal(tc.g.Permissions, []Permission{PermAuditView, PermInvoicesView}) || !slices.Equal(tc.g.MemberIDs, []string{"a", "b"}) {
					t.Fatalf("not normalised: %+v", tc.g)
				}
			}
		})
	}
}

func TestAccessFor(t *testing.T) {
	none := AccessFor(nil)
	if none.Administrator || none.Has(PermDashboardView) || len(none.Permissions()) != 0 {
		t.Fatalf("no groups must give no access: %+v", none)
	}
	a := AccessFor([]Group{
		{ID: "g1", Permissions: []Permission{PermInvoicesEdit}},
		{ID: "g2", Permissions: []Permission{PermAuditView}},
	})
	for _, p := range []Permission{PermInvoicesEdit, PermInvoicesView, PermCustomersView, PermProductsView, PermAuditView} {
		if !a.Has(p) {
			t.Fatalf("missing %s", p)
		}
	}
	for _, p := range []Permission{PermInvoicesSend, PermCustomersEdit, PermAdminister} {
		if a.Has(p) {
			t.Fatalf("must not have %s", p)
		}
	}
	admin := AccessFor([]Group{{ID: AdministratorsGroupID, Builtin: true}})
	if !admin.Administrator || !admin.Has(PermAdminister) || !admin.Has(PermSettingsBanks) || len(admin.Permissions()) != len(Permissions) {
		t.Fatalf("administrators must have everything: %+v", admin)
	}
	if !slices.IsSorted(admin.Permissions()) {
		t.Fatal("permissions must be sorted")
	}
}

func TestCheckGroupChange(t *testing.T) {
	admins := []string{"a", "b"}
	if err := CheckGroupChange("a", admins, []string{"b"}); err != nil {
		t.Fatalf("removing another administrator: %v", err)
	}
	if err := CheckGroupChange("a", admins, []string{"a"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("removing yourself: %v", err)
	}
	if err := CheckGroupChange("c", admins, nil); !errors.Is(err, ErrNoAccess) {
		t.Fatalf("not an administrator: %v", err)
	}
}

func TestGroupHelpers(t *testing.T) {
	g := Group{ID: AdministratorsGroupID, MemberIDs: []string{"a"}}
	if !g.IsAdministrators() || !g.HasMember("a") || g.HasMember("b") {
		t.Fatal("helpers")
	}
}

func TestActionPermissionsImplyOnlyWhatTheyNeed(t *testing.T) {
	create := AccessFor([]Group{{ID: "g", Permissions: []Permission{PermInvoicesCreate, PermCustomersCreate}}})
	for _, p := range []Permission{PermInvoicesCreate, PermInvoicesView, PermCustomersView, PermProductsView, PermCustomersCreate} {
		if !create.Has(p) {
			t.Errorf("creating invoices and customers needs %s", p)
		}
	}
	for _, p := range []Permission{PermInvoicesEdit, PermInvoicesIssue, PermInvoicesDelete, PermInvoicesSend, PermInvoicesVoid, PermCustomersEdit, PermCustomersDelete} {
		if create.Has(p) {
			t.Errorf("creating must not give %s", p)
		}
	}
	for _, i := range Permissions {
		for _, q := range WithImplied([]Permission{i.Key}) {
			if q != i.Key && !strings.HasSuffix(string(q), ".view") {
				t.Errorf("%s implies the action %s: only seeing may be implied", i.Key, q)
			}
		}
	}
}
