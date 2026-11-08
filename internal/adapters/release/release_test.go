package release_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/albievan/cactusinv/internal/adapters/release"
)

func TestLatestVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/VERSION":
			_, _ = w.Write([]byte("1.13.0\n"))
		case "/long":
			_, _ = w.Write([]byte(strings.Repeat("9", 1<<20)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	if v, err := release.New(srv.URL + "/VERSION").LatestVersion(ctx); err != nil || v != "1.13.0" {
		t.Fatalf("%q %v", v, err)
	}
	if v, err := release.New(srv.URL + "/long").LatestVersion(ctx); err != nil || len(v) != 64 {
		t.Fatalf("long file read %d bytes (%v)", len(v), err)
	}
	if _, err := release.New(srv.URL + "/missing").LatestVersion(ctx); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("missing: %v", err)
	}
	if _, err := release.New("http://127.0.0.1:1/VERSION").LatestVersion(ctx); err == nil {
		t.Fatal("unreachable: no error")
	}
}
