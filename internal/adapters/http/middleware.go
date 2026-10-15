package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

type ctxKey int

const (
	keyRequestID ctxKey = iota
	keyUser
	keyToken
)

// SessionCookie is the name of the session cookie.
const SessionCookie = "cinv_session"

// csrfHeader must accompany every mutating request; browsers cannot add it cross-site
// without a CORS preflight, which this server never grants.
const csrfHeader, csrfValue = "X-Requested-With", "cactusinv"

// maxBody bounds request bodies.
const maxBody = 1 << 20

func requestID(ctx context.Context) string {
	id, _ := ctx.Value(keyRequestID).(string)
	return id
}

func currentUser(ctx context.Context) domain.User {
	u, _ := ctx.Value(keyUser).(domain.User)
	return u
}

func sessionToken(ctx context.Context) string {
	t, _ := ctx.Value(keyToken).(string)
	return t
}

// statusRecorder captures the status code and, optionally, the body.
type statusRecorder struct {
	http.ResponseWriter
	status  int
	capture *bytes.Buffer
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if r.capture != nil && r.capture.Len() < maxBody {
		r.capture.Write(b)
	}
	return r.ResponseWriter.Write(b)
}

// withRequestLog assigns a request ID, recovers panics and logs one line per request.
// Only the path is logged: query strings can contain names or e-mail addresses.
func withRequestLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := newRequestID()
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), keyRequestID, id)
		rec := &statusRecorder{ResponseWriter: w}
		defer func() {
			if p := recover(); p != nil {
				log.ErrorContext(ctx, "panic", "request_id", id, "panic", p)
				if rec.status == 0 {
					writeProblem(rec, problemFor(errors.New("panic")))
				}
			}
			if strings.HasPrefix(r.URL.Path, "/assets/") {
				return
			}
			log.InfoContext(ctx, "http", "request_id", id, "method", r.Method, "path", r.URL.Path,
				"status", rec.status, "duration_ms", time.Since(start).Milliseconds())
		}()
		next.ServeHTTP(rec, r.WithContext(ctx))
	})
}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// withSecurityHeaders sets conservative browser security headers.
func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-src 'self'; object-src 'self'; frame-ancestors 'none'; base-uri 'none'")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

// withAPIGuards applies the body limit, CSRF check and session authentication to /api.
func withAPIGuards(svc *app.Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		if isMutation(r.Method) && r.Header.Get(csrfHeader) != csrfValue {
			writeProblem(w, Problem{Type: problemType("csrf"), Title: "Missing " + csrfHeader + " header", Status: http.StatusForbidden})
			return
		}
		if r.URL.Path == "/api/v1/auth/login" {
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(SessionCookie)
		if err != nil {
			writeProblem(w, problemFor(domain.ErrUnauthenticated))
			return
		}
		u, err := svc.Authenticate(r.Context(), c.Value)
		if err != nil {
			writeProblem(w, problemFor(err))
			return
		}
		ctx := context.WithValue(r.Context(), keyUser, u)
		ctx = context.WithValue(ctx, keyToken, c.Value)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isMutation(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

// withIdempotency replays the stored response when a mutating request repeats an
// Idempotency-Key (24 h retention, housekeeping deletes older keys).
func withIdempotency(store ports.IdempotencyRepository, clock ports.Clock, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		if key == "" || !isMutation(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		if len(key) > 200 {
			writeProblem(w, Problem{Type: problemType("bad-request"), Title: "Idempotency-Key too long", Status: http.StatusBadRequest})
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeProblem(w, Problem{Type: problemType("bad-request"), Title: "Body too large", Status: http.StatusRequestEntityTooLarge})
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		sum := sha256.Sum256([]byte(currentUser(r.Context()).ID + "\n" + r.Method + " " + r.URL.Path + "\n" + string(body)))
		fingerprint := hex.EncodeToString(sum[:])
		storeKey := currentUser(r.Context()).ID + ":" + key
		ctx := r.Context()
		err = store.Reserve(ctx, ports.IdempotencyRecord{Key: storeKey, Fingerprint: fingerprint, CreatedAt: clock.Now()})
		if errors.Is(err, domain.ErrConflict) {
			replay(w, r, store, storeKey, fingerprint)
			return
		}
		if err != nil {
			writeProblem(w, problemFor(err))
			return
		}
		rec := &statusRecorder{ResponseWriter: w, capture: &bytes.Buffer{}}
		next.ServeHTTP(rec, r)
		if rec.status >= 500 || rec.status == 0 {
			_ = store.Release(context.WithoutCancel(ctx), storeKey)
			return
		}
		_ = store.Complete(context.WithoutCancel(ctx), storeKey, rec.status, rec.capture.Bytes())
	})
}

func replay(w http.ResponseWriter, r *http.Request, store ports.IdempotencyRepository, key, fingerprint string) {
	prev, err := store.Get(r.Context(), key)
	switch {
	case err != nil:
		writeProblem(w, problemFor(err))
	case prev.Fingerprint != fingerprint:
		writeProblem(w, Problem{Type: problemType("idempotency-mismatch"), Title: "Idempotency-Key reused with a different request", Status: http.StatusUnprocessableEntity})
	case prev.Status == 0:
		writeProblem(w, Problem{Type: problemType("in-progress"), Title: "The original request is still in progress", Status: http.StatusConflict})
	default:
		if len(prev.Body) > 0 {
			ct := "application/json"
			if prev.Status >= 400 {
				ct = "application/problem+json"
			}
			w.Header().Set("Content-Type", ct)
		}
		w.Header().Set("Idempotent-Replayed", "true")
		w.WriteHeader(prev.Status)
		_, _ = w.Write(prev.Body)
	}
}

// loginLimiter allows a burst of attempts per client IP per window.
type loginLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	max     int
	clock   ports.Clock
	entries map[string][]time.Time
}

func newLoginLimiter(clock ports.Clock) *loginLimiter {
	return &loginLimiter{window: time.Minute, max: 10, clock: clock, entries: map[string][]time.Time{}}
}

func (l *loginLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock.Now()
	kept := l.entries[ip][:0]
	for _, t := range l.entries[ip] {
		if now.Sub(t) < l.window {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.entries[ip] = kept
		return false
	}
	l.entries[ip] = append(kept, now)
	if len(l.entries) > 10000 { // bound memory under a spray of addresses
		l.entries = map[string][]time.Time{ip: l.entries[ip]}
	}
	return true
}

func (l *loginLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/login" && r.Method == http.MethodPost {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}
			if !l.allow(ip) {
				w.Header().Set("Retry-After", "60")
				writeProblem(w, Problem{Type: problemType("rate-limited"), Title: "Too many sign-in attempts", Status: http.StatusTooManyRequests})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// metrics are the HTTP RED metrics.
type metrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

func newMetrics(reg prometheus.Registerer) *metrics {
	m := &metrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cactusinv_http_requests_total", Help: "HTTP requests by route and status.",
		}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "cactusinv_http_request_duration_seconds", Help: "HTTP request latency by route.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
	}
	reg.MustRegister(m.requests, m.duration)
	return m
}

// route is a generated-router middleware: r.Pattern holds the matched route template,
// so metrics never carry IDs.
func (m *metrics) route(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		route := r.Pattern
		if i := strings.IndexByte(route, ' '); i >= 0 {
			route = route[i+1:]
		}
		m.requests.WithLabelValues(r.Method, route, strconv.Itoa(rec.status)).Inc()
		m.duration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
	})
}

// cookieConfig builds the session cookie.
type cookieConfig struct {
	secure bool
}

// session builds the session cookie. Max-Age (relative) rather than Expires keeps it
// independent of clock skew between server and browser.
func (c cookieConfig) session(token string, expires, now time.Time) *http.Cookie {
	return &http.Cookie{
		Name: SessionCookie, Value: token, Path: "/", MaxAge: max(int(expires.Sub(now).Seconds()), 1),
		HttpOnly: true, Secure: c.secure, SameSite: http.SameSiteStrictMode,
	}
}

func (c cookieConfig) cleared() *http.Cookie {
	return &http.Cookie{
		Name: SessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: c.secure,
		SameSite: http.SameSiteStrictMode,
	}
}
