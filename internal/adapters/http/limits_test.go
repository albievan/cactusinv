package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestDeadlinesReachTheConnection: per-route deadlines are set through the response
// writer wrappers (statusRecorder unwraps), and the routes get their limits.
func TestDeadlinesReachTheConnection(t *testing.T) {
	var got error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rec := &statusRecorder{ResponseWriter: &statusRecorder{ResponseWriter: w}}
		got = http.NewResponseController(rec).SetWriteDeadline(time.Now().Add(time.Minute))
		w.WriteHeader(204)
	}))
	defer srv.Close()
	req, _ := http.NewRequestWithContext(context.Background(), "GET", srv.URL, nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got != nil {
		t.Fatalf("SetWriteDeadline through the wrappers: %v", got)
	}

	cases := []struct {
		method, path string
		body         int64
		long, skip   bool
	}{
		{"POST", pathUploads, 99, true, true},
		{"POST", pathExport, maxBody, true, true},
		{"POST", "/api/v1/restores", maxBody, true, true},
		{"POST", "/api/v1/restores/x/apply", maxBody, true, true},
		{"DELETE", "/api/v1/restores/x", maxBody, false, false},
		{"POST", "/api/v1/backups", maxBody, true, false},
		{"GET", "/api/v1/backups", maxBody, false, false},
		{"POST", "/api/v1/invoices", maxBody, false, false},
		{"POST", pathImportSettings, maxSettingsFile, false, true},
		{"POST", "/api/v1/settings/export", maxBody, false, true},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, c.path, strings.NewReader(""))
		l := limitsFor(r, 99)
		if l.body != c.body || (l.written > 0) != c.long || skipsIdempotency(r) != c.skip {
			t.Errorf("%s %s: %+v skip=%v", c.method, c.path, l, skipsIdempotency(r))
		}
	}
}
