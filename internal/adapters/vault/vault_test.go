package vault

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func fakeVault(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "s.test" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/v1/secret/data/cactusinv":
			_, _ = w.Write([]byte(`{"data":{"data":{"db_password":"pw","smtp_password":"smtp","n":1}}}`))
		case "/v1/auth/token/lookup-self":
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func tokenFile(t *testing.T, token string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(p, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestVault(t *testing.T) {
	srv := fakeVault(t)
	ctx := context.Background()
	c, err := New(ctx, Config{Addr: srv.URL, TokenFile: tokenFile(t, "s.test"), KVPath: "secret/data/cactusinv"})
	if err != nil {
		t.Fatal(err)
	}
	if v, err := c.Get(ctx, "db_password"); err != nil || v != "pw" {
		t.Fatalf("Get = %q %v", v, err)
	}
	if _, err := c.Get(ctx, "n"); err == nil {
		t.Fatal("non-string values are ignored")
	}
	if err := c.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := New(ctx, Config{Addr: srv.URL, TokenFile: tokenFile(t, "wrong"), KVPath: "secret/data/cactusinv"}); err == nil {
		t.Fatal("forbidden")
	}
	if _, err := New(ctx, Config{Addr: srv.URL, TokenFile: tokenFile(t, " "), KVPath: "x"}); err == nil {
		t.Fatal("empty token")
	}
	if _, err := New(ctx, Config{Addr: srv.URL, TokenFile: "/nonexistent", KVPath: "x"}); err == nil {
		t.Fatal("missing token file")
	}
	if _, err := New(ctx, Config{}); err == nil {
		t.Fatal("missing config")
	}
	if _, err := New(ctx, Config{Addr: srv.URL, TokenFile: tokenFile(t, "s.test"), KVPath: "secret/data/missing"}); err == nil {
		t.Fatal("missing path")
	}
}
