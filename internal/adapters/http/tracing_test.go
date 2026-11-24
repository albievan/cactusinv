package httpapi

import "testing"

func TestRedactPath(t *testing.T) {
	tok := "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_abcde"
	for in, want := range map[string]string{
		"/a/" + tok: "/a/:token",
		"/api/v1/public/accounts/" + tok + "/statement": "/api/v1/public/accounts/:token/statement",
		"/api/v1/public/invoices/" + tok + "/pdf":       "/api/v1/public/invoices/:token/pdf",
		"/reset-password/" + tok:                        "/reset-password/:token",
		"/api/v1/invoices/123":                          "/api/v1/invoices/123",
		"/a/":                                           "/a/",
		"/assets/app.js":                                "/assets/app.js",
	} {
		if got := redactPath(in); got != want {
			t.Errorf("redactPath(%q) = %q, want %q", in, got, want)
		}
	}
}
