package httpapi

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecureSignInRefusesPlainHTTP(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	cases := []struct {
		name, host, path, proto string
		tls, secure             bool
		want                    int
	}{
		{name: "plain http on an address", host: "10.101.18.101:30080", path: "/api/v1/auth/login", secure: true, want: http.StatusForbidden},
		{name: "plain http on a name", host: "invoices.example.com", path: "/api/v1/auth/login", secure: true, want: http.StatusForbidden},
		{name: "proxy says http", host: "invoices.example.com", path: "/api/v1/auth/login", proto: "http", secure: true, want: http.StatusForbidden},
		{name: "direct TLS", host: "invoices.example.com", path: "/api/v1/auth/login", tls: true, secure: true, want: http.StatusNoContent},
		{name: "TLS proxy", host: "10.101.18.101", path: "/api/v1/auth/login", proto: "HTTPS, http", secure: true, want: http.StatusNoContent},
		{name: "localhost", host: "localhost:8080", path: "/api/v1/auth/login", secure: true, want: http.StatusNoContent},
		{name: "sub.localhost", host: "app.localhost:8080", path: "/api/v1/auth/login", secure: true, want: http.StatusNoContent},
		{name: "loopback v4", host: "127.0.0.1:8080", path: "/api/v1/auth/login", secure: true, want: http.StatusNoContent},
		{name: "loopback v6", host: "[::1]:8080", path: "/api/v1/auth/login", secure: true, want: http.StatusNoContent},
		{name: "other routes untouched", host: "10.101.18.101:30080", path: "/api/v1/version", secure: true, want: http.StatusNoContent},
		{name: "cookie not HTTPS-only", host: "10.101.18.101:30080", path: "/api/v1/auth/login", want: http.StatusNoContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "http://"+tc.host+tc.path, strings.NewReader("{}"))
			if tc.proto != "" {
				r.Header.Set("X-Forwarded-Proto", tc.proto)
			}
			if tc.tls {
				r.TLS = &tls.ConnectionState{}
			}
			w := httptest.NewRecorder()
			withSecureSignIn(tc.secure, next).ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status %d, want %d (%s)", w.Code, tc.want, w.Body.String())
			}
			if tc.want == http.StatusForbidden && !strings.Contains(w.Body.String(), "https-required") {
				t.Fatalf("problem type missing: %s", w.Body.String())
			}
		})
	}
}
