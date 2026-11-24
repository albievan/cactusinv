//go:build integration

package mariadb_test

import (
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
)

// The account link of a statement e-mail on MariaDB 11 (M59): the token's hash finds the
// customer until it expires.
func TestPortalTokenPersistence(t *testing.T) {
	e := setup(t)
	inv := must(e.svc.IssueInvoice(e.ctx, must(e.svc.CreateInvoice(e.ctx, e.reference())).ID, domain.Date{}))
	m := must(e.svc.SendStatement(e.ctx, inv.CustomerID, app.SendInput{LinkBase: "https://inv.example.com"}))
	link := regexp.MustCompile(`/a/([A-Za-z0-9_-]{43})\n`).FindStringSubmatch(m.Body)
	if link == nil {
		t.Fatalf("no link in %q", m.Body)
	}
	v := must(e.svc.GetPortal(e.ctx, link[1]))
	if v.Statement.Customer.ID != inv.CustomerID || len(v.Statement.Lines) != 1 {
		t.Fatalf("portal = %+v", v)
	}
	e.clock.Set(e.clock.Now().Add(domain.PortalLinkValidity + time.Second))
	if _, err := e.svc.GetPortal(e.ctx, link[1]); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expired: %v", err)
	}
}
