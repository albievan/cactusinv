package httpapi_test

import (
	"os"
	"regexp"
	"slices"
	"testing"

	httpapi "github.com/albievan/cactusinv/internal/adapters/http"
	"github.com/albievan/cactusinv/internal/app"
)

// Every operation in the contract must have an access rule: one without is refused for
// everybody, so a new endpoint would silently be unusable (ADR-012).
func TestEveryOperationHasAnAccessRule(t *testing.T) {
	spec, err := os.ReadFile("../../../api/openapi/cactusinv.yaml")
	if err != nil {
		t.Fatal(err)
	}
	ops := regexp.MustCompile(`operationId:\s*(\w+)`).FindAllStringSubmatch(string(spec), -1)
	if len(ops) < 70 {
		t.Fatalf("found only %d operations", len(ops))
	}
	for _, m := range ops {
		if !app.OperationKnown(m[1]) {
			t.Errorf("operation %s has no access rule in internal/app/access.go", m[1])
		}
	}
}

func TestGroupRoutesAndAccess(t *testing.T) {
	albie, _, _, _ := newServer(t)
	albie.login()
	var me httpapi.Me
	albie.ok(albie.do("GET", "/api/v1/auth/me", nil, &me), 200, "me")
	if !me.Administrator || len(me.Permissions) < 18 {
		t.Fatalf("me = %+v", me)
	}

	// A user added with no group can sign in and read their account, nothing else.
	var carol httpapi.User
	albie.ok(albie.do("POST", "/api/v1/users", obj{"email": "carol@example.com", "password": "chosen by albie", "currentPassword": albiePW}, &carol), 201, "add carol")
	if len(carol.GroupIds) != 0 {
		t.Fatalf("carol = %+v", carol)
	}
	cc := albie.second()
	cc.ok(cc.loginAs("carol@example.com", "chosen by albie"), 204, "carol signs in")
	cc.ok(cc.do("POST", "/api/v1/auth/password", obj{"currentPassword": "chosen by albie", "newPassword": "carol's own password"}, nil), 204, "carol's password")
	cc.ok(cc.do("GET", "/api/v1/auth/me", nil, &me), 200, "carol me")
	if me.Administrator || len(me.Permissions) != 0 {
		t.Fatalf("carol me = %+v", me)
	}
	var p httpapi.Problem
	cc.ok(cc.do("GET", "/api/v1/invoices", nil, &p), 403, "no access")
	if p.Type != problemType("no-access") {
		t.Fatalf("problem = %+v", p)
	}
	cc.ok(cc.do("GET", "/api/v1/currencies", nil, nil), 200, "reference data")

	// A group that allows e-mailing invoices.
	var list httpapi.GroupList
	albie.ok(albie.do("GET", "/api/v1/groups", nil, &list), 200, "groups")
	if len(list.Items) != 1 || !list.Items[0].Builtin || len(list.Permissions) < 18 {
		t.Fatalf("groups = %+v", list)
	}
	body := obj{"name": "Accounts", "permissions": []string{"invoices.send"}, "memberIds": []string{carol.Id}, "currentPassword": albiePW}
	var g httpapi.Group
	albie.ok(albie.do("POST", "/api/v1/groups", body, &g, "Idempotency-Key", "g1"), 201, "create group")
	cc.ok(cc.do("GET", "/api/v1/auth/me", nil, &me), 200, "carol me")
	if !slices.Contains(me.Permissions, "invoices.view") || slices.Contains(me.Permissions, "invoices.void") {
		t.Fatalf("carol's permissions = %v", me.Permissions)
	}
	cc.ok(cc.do("GET", "/api/v1/invoices", nil, nil), 200, "invoices now")
	cc.ok(cc.do("GET", "/api/v1/groups", nil, &p), 403, "groups are for administrators")
	cc.ok(cc.do("GET", "/api/v1/backups", nil, &p), 403, "backups are for administrators")

	// Remove the access again; a wrong password changes nothing.
	body["permissions"] = []string{"audit.view"}
	body["currentPassword"] = "wrong"
	albie.ok(albie.do("PUT", "/api/v1/groups/"+g.Id, body, &p), 422, "wrong password")
	body["currentPassword"] = albiePW
	albie.ok(albie.do("PUT", "/api/v1/groups/"+g.Id, body, &g, "Idempotency-Key", "g2"), 200, "update group")
	cc.ok(cc.do("GET", "/api/v1/invoices", nil, nil), 403, "invoices gone")
	cc.ok(cc.do("GET", "/api/v1/audit", nil, nil), 200, "audit now")

	// The user's own groups.
	albie.ok(albie.do("PUT", "/api/v1/users/"+carol.Id+"/groups", obj{"groupIds": []string{}, "currentPassword": albiePW}, &carol), 200, "no groups")
	cc.ok(cc.do("GET", "/api/v1/audit", nil, nil), 403, "audit gone")
	var self httpapi.User
	albie.ok(albie.do("PUT", "/api/v1/users/no-such-user/groups", obj{"groupIds": []string{}, "currentPassword": albiePW}, &p), 404, "unknown user")
	var users httpapi.UserList
	albie.ok(albie.do("GET", "/api/v1/users", nil, &users), 200, "users")
	for _, u := range users.Items {
		if u.Email == "albie@example.com" {
			self = u
		}
	}
	albie.ok(albie.do("PUT", "/api/v1/users/"+self.Id+"/groups", obj{"groupIds": []string{}, "currentPassword": albiePW}, &p), 403, "leave Administrators")
	if p.Type != problemType("forbidden") {
		t.Fatalf("problem = %+v", p)
	}

	albie.ok(albie.do("POST", "/api/v1/groups/administrators/delete", obj{"currentPassword": albiePW}, &p), 403, "delete Administrators")
	albie.ok(albie.do("POST", "/api/v1/groups/"+g.Id+"/delete", obj{"currentPassword": albiePW}, nil), 204, "delete group")
	albie.ok(albie.do("GET", "/api/v1/groups", nil, &list), 200, "groups")
	if len(list.Items) != 1 {
		t.Fatalf("groups = %+v", list.Items)
	}
}
