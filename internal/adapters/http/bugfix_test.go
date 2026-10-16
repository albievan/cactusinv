package httpapi_test

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	httpapi "github.com/albievan/cactusinv/internal/adapters/http"
	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/ports/fakes"
)

func TestIdempotencyKeyLimitsAndPasswordRoutes(t *testing.T) {
	c, store, _, _ := newServer(t)
	c.login()
	var p httpapi.Problem
	long := strings.Repeat("k", 129)
	c.ok(c.do("POST", "/api/v1/currencies", obj{"code": "USD", "name": "US Dollar", "symbol": "$", "minorUnits": 2, "active": true}, &p, "Idempotency-Key", long), 400, "key too long")
	c.ok(c.do("POST", "/api/v1/currencies", obj{"code": "USD", "name": "US Dollar", "symbol": "$", "minorUnits": 2, "active": true}, &p, "Idempotency-Key", "naïve"), 400, "non-ASCII key")
	// PUT routes with a password are never fingerprinted (the hash of a password is guessable).
	var me httpapi.Me
	c.ok(c.do("GET", "/api/v1/auth/me", nil, &me), 200, "me")
	body := obj{"groupIds": []string{"administrators"}, "currentPassword": albiePW}
	c.ok(c.do("PUT", "/api/v1/users/"+me.Id+"/groups", body, nil, "Idempotency-Key", "pw-1"), 200, "groups")
	if _, err := store.Repos().Idempotency.Get(context.Background(), me.Id+":pw-1"); err == nil {
		t.Fatal("a request carrying a password was stored for replay")
	}
}

func TestOversizedJSONIs413(t *testing.T) {
	c, _, _, _ := newServer(t)
	c.login()
	big := `{"code":"USD","name":"` + strings.Repeat("x", 2<<20) + `"}`
	req, _ := http.NewRequestWithContext(context.Background(), "POST", c.srv.URL+"/api/v1/currencies", strings.NewReader(big))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "cactusinv")
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestUnknownCustomerIsAFieldError(t *testing.T) {
	c, _, _, _ := newServer(t)
	c.login()
	var p httpapi.Problem
	c.ok(c.do("POST", "/api/v1/invoices", obj{"customerId": "nope", "lines": []obj{{"productId": "x", "quantity": "1"}}}, &p), 422, "unknown customer")
	if p.Field != "customerId" {
		t.Fatalf("problem = %+v", p)
	}
}

func TestSPANeverListsOrFakesAssets(t *testing.T) {
	store := fakes.NewStore()
	clock := &fakes.Clock{T: time.Date(2026, 8, 21, 6, 0, 0, 0, time.UTC)}
	svc := app.New(app.Deps{Tx: store, Clock: clock, IDs: &fakes.IDs{}, Mailer: &fakes.Mailer{}, Renderer: fakes.Renderer{}, Hasher: fakes.Hasher{}})
	web := fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>cactusInv</title>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	h := httpapi.NewHandler(httpapi.Options{Service: svc, Clock: clock, Web: web})
	get := func(path string) (int, string) {
		req, _ := http.NewRequestWithContext(context.Background(), "GET", path, nil)
		rec := newRecorder()
		h.ServeHTTP(rec, req)
		return rec.code, rec.body.String()
	}
	for _, p := range []string{"/assets/", "/assets", "/assets/missing.js"} {
		if code, body := get(p); code != 404 || strings.Contains(body, "app.js") || strings.Contains(body, "<title>") {
			t.Errorf("%s: %d %q", p, code, body)
		}
	}
	if code, body := get("/invoices/123"); code != 200 || !strings.Contains(body, "<title>cactusInv") {
		t.Errorf("client route: %d", code)
	}
	if code, body := get("/assets/app.js"); code != 200 || body != "console.log(1)" {
		t.Errorf("asset: %d %q", code, body)
	}
}

type recorder struct {
	code int
	hdr  http.Header
	body bytes.Buffer
}

func newRecorder() *recorder                    { return &recorder{code: 200, hdr: http.Header{}} }
func (r *recorder) Header() http.Header         { return r.hdr }
func (r *recorder) Write(b []byte) (int, error) { return r.body.Write(b) }
func (r *recorder) WriteHeader(c int)           { r.code = c }
