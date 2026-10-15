// Package httpapi is cactusInv's HTTP adapter: the generated OpenAPI server
// (api.gen.go, from api/openapi/cactusinv.yaml), its handlers, middleware, health and
// metrics endpoints, and the embedded mobile-first web app.
package httpapi

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/albievan/cactusinve/internal/app"
	"github.com/albievan/cactusinve/internal/ports"
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
}

// NewHandler builds the complete HTTP handler.
func NewHandler(o Options) http.Handler {
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	if o.Registry == nil {
		o.Registry = prometheus.NewRegistry()
	}
	m := newMetrics(o.Registry)
	h := &handlers{svc: o.Service, clock: o.Clock, cookies: cookieConfig{secure: o.CookieSecure}}
	strict := NewStrictHandlerWithOptions(h, nil, StrictHTTPServerOptions{
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
	root := http.NewServeMux()
	root.Handle("/api/", limiter.middleware(withAPIGuards(o.Service, withIdempotency(o.Service.Idempotency(), o.Clock, api))))
	root.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	root.HandleFunc("GET /readyz", readyz(o.Ready))
	root.Handle("GET /metrics", promhttp.HandlerFor(o.Registry, promhttp.HandlerOpts{}))
	if o.Web != nil {
		root.Handle("/", spa(o.Web))
	}
	return withRequestLog(o.Logger, withSecurityHeaders(root))
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
		if _, err := fs.Stat(web, p); err != nil {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
			p = "index.html"
		}
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}
