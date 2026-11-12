package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

// Regression tests for the v1.19.0 code review (M36).

// Behind a trusted proxy the client is the right-most address it did not add itself; a
// client cannot forge its way past that, and nobody else's X-Forwarded-For is believed.
func TestClientAddress(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.42.0.0/16")}
	req := func(remote string, xff ...string) *http.Request {
		r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/login", nil)
		r.RemoteAddr = remote
		for _, v := range xff {
			r.Header.Add("X-Forwarded-For", v)
		}
		return r
	}
	for name, c := range map[string]struct {
		r    *http.Request
		want string
	}{
		"direct":               {req("192.168.1.9:5000", "1.2.3.4"), "192.168.1.9"},
		"via the ingress":      {req("10.42.0.7:5000", "203.0.113.5"), "203.0.113.5"},
		"forged on the left":   {req("10.42.0.7:5000", "6.6.6.6, 203.0.113.5"), "203.0.113.5"},
		"two headers":          {req("10.42.0.7:5000", "6.6.6.6", "203.0.113.5"), "203.0.113.5"},
		"proxies in the chain": {req("10.42.0.7:5000", "203.0.113.5, 10.42.1.1"), "203.0.113.5"},
		"garbage":              {req("10.42.0.7:5000", "not-an-ip"), "10.42.0.7"},
		"no header":            {req("10.42.0.7:5000"), "10.42.0.7"},
	} {
		if got := clientAddress(c.r, trusted); got != c.want {
			t.Errorf("%s: %s, want %s", name, got, c.want)
		}
	}
	if got := clientAddress(req("10.42.0.7:5000", "203.0.113.5"), nil); got != "10.42.0.7" {
		t.Errorf("no trusted proxies: %s", got)
	}
}

// A malformed body is described without the decoder's Go type names.
func TestMalformedRequestDetail(t *testing.T) {
	for body, want := range map[string]string{
		`{"name": 5}`: "name has the wrong type (expected a string)",
		`{"name": `:   "the body is not valid JSON",
	} {
		var v struct {
			Name string `json:"name"`
		}
		err := json.NewDecoder(strings.NewReader(body)).Decode(&v)
		w := httptest.NewRecorder()
		requestErrorHandler(w, nil, err)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), want) || strings.Contains(w.Body.String(), "Go struct") {
			t.Errorf("%s: %d %s", body, w.Code, w.Body.String())
		}
	}
}
