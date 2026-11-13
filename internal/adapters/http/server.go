// Package httpapi is cactusInv's HTTP adapter: the generated OpenAPI server
// (api.gen.go, from api/openapi/cactusinv.yaml), its handlers, middleware, health and
// metrics endpoints, and the embedded mobile-first web app.
package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"net/netip"
	"path"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/ports"
)

// ReadyCheck reports whether a dependency is usable (database, Vault).
type ReadyCheck func(ctx context.Context) error

// Options configure the HTTP adapter.
type Options struct {
	Service      *app.Service
	Clock        ports.Clock
	Logger       *slog.Logger
	CookieSecure bool
	Ready        map[string]ReadyCheck
	Registry     *prometheus.Registry
	// Web is the built web app (nil serves only the API).
	Web fs.FS
	// Version and Build identify the release (shown in the app and on the login screen).
	Version, Build string
	// MaxUploadBytes limits a backup upload (default DefaultMaxUploadBytes).
	MaxUploadBytes int64
	// TrustedProxies are the reverse proxies (an ingress) whose X-Forwarded-For names the
	// client. Without them every client behind a proxy shares its address, so one client's
	// wrong passwords would lock everyone out.
	TrustedProxies []netip.Prefix
	// MetricsToken guards GET /metrics: it answers only a request with
	// "Authorization: Bearer <token>". Empty = no /metrics at all (M40).
	MetricsToken string
}

// NewHandler builds the complete HTTP handler.
func NewHandler(o Options) http.Handler {
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	if o.Registry == nil {
		o.Registry = prometheus.NewRegistry()
	}
	if o.MaxUploadBytes <= 0 {
		o.MaxUploadBytes = DefaultMaxUploadBytes
	}
	m := newMetrics(o.Registry)
	h := &handlers{svc: o.Service, clock: o.Clock, cookies: cookieConfig{secure: o.CookieSecure}, version: o.Version, build: o.Build, maxUpload: o.MaxUploadBytes}
	strict := NewStrictHandlerWithOptions(h, []StrictMiddlewareFunc{sessionGate}, StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  requestErrorHandler,
		ResponseErrorHandlerFunc: responseErrorHandler(o.Logger),
	})
	apiMux := http.NewServeMux()
	api := HandlerWithOptions(strict, StdHTTPServerOptions{
		BaseURL:          "/api/v1",
		BaseRouter:       apiMux,
		Middlewares:      []MiddlewareFunc{m.route},
		ErrorHandlerFunc: requestErrorHandler,
	})
	apiMux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, Problem{Type: problemType("not-found"), Title: "No such endpoint", Status: http.StatusNotFound})
	})

	limiter := newLoginLimiter(o.Clock)
	limiter.trusted = o.TrustedProxies
	root := http.NewServeMux()
	root.Handle("/api/", withSecureSignIn(o.CookieSecure, limiter.middleware(withAPIGuards(o.Service, o.MaxUploadBytes, limiter, withIdempotency(o.Service.Idempotency(), o.Clock, api)))))
	root.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	root.HandleFunc("GET /readyz", readyz(o.Ready))
	root.Handle("GET /metrics", metricsGuard(o.MetricsToken, promhttp.HandlerFor(o.Registry, promhttp.HandlerOpts{})))
	if o.Web != nil {
		root.Handle("/", spa(o.Web))
	}
	return withRequestLog(o.Logger, withSecurityHeaders(root))
}

// metricsGuard serves next only with the bearer token; without a configured token the
// endpoint does not exist. The figures (request counts, outbox depth) are not for everyone.
func metricsGuard(token string, next http.Handler) http.Handler {
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token == "" {
			writeProblem(w, Problem{Type: problemType("not-found"), Title: "No such endpoint", Status: http.StatusNotFound})
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
			writeProblem(w, Problem{Type: problemType("unauthorized"), Title: "Metrics need the bearer token", Status: http.StatusUnauthorized})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func readyz(checks map[string]ReadyCheck) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		status := map[string]string{}
		code := http.StatusOK
		for name, check := range checks {
			if err := check(ctx); err != nil {
				status[name] = "unavailable"
				code = http.StatusServiceUnavailable
				continue
			}
			status[name] = "ok"
		}
		writeJSON(w, code, status)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// spa serves the built web app, falling back to index.html for client-side routes.
// Hashed assets are cached for a year; index.html never.
var fontTypes = map[string]string{".woff2": "font/woff2", ".woff": "font/woff"}

func spa(web fs.FS) http.Handler {
	files := http.FileServerFS(web)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		st, err := fs.Stat(web, p)
		switch {
		case err == nil && !st.IsDir():
			// a file: served as is
		case strings.HasPrefix(p, "assets/") || p == "assets":
			// A missing or directory asset is a 404, never index.html (a stale page's lazy
			// chunk must fail clearly) and never a directory listing.
			http.NotFound(w, r)
			return
		default: // a client-side route
			r = r.Clone(r.Context())
			r.URL.Path = "/"
			p = "index.html"
		}
		// Go's MIME table has no web-font types (the Bootstrap Icons font is .woff2).
		if t, ok := fontTypes[path.Ext(p)]; ok {
			w.Header().Set("Content-Type", t)
		}
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}
