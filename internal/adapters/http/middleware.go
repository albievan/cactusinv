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
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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

// Unwrap lets http.ResponseController reach the connection (per-route deadlines).
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

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
				if err, ok := p.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					log.WarnContext(ctx, "response aborted", "request_id", id, "method", r.Method, "path", r.URL.Path)
					panic(p) // the server drops the connection: a broken download must not look complete
				}
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

// withAPIGuards applies the body limit and deadlines (per route, see limitsFor), the CSRF
// check, session authentication and, on password routes, the user's rate-limit bucket.
func withAPIGuards(svc *app.Service, maxUpload int64, limiter *loginLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Larger bodies and longer deadlines only once the request is authenticated; until
		// then nothing reads the body of such a route.
		limits := limitsFor(r, maxUpload)
		if !limits.long() {
			applyLimits(w, r, limits)
		}
		if isMutation(r.Method) && r.Header.Get(csrfHeader) != csrfValue {
			writeProblem(w, Problem{Type: problemType("csrf"), Title: "Missing " + csrfHeader + " header", Status: http.StatusForbidden})
			return
		}
		if r.URL.Path == "/api/v1/auth/login" || r.URL.Path == "/api/v1/version" {
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
		if limits.long() {
			applyLimits(w, r, limits)
		}
		if reauthPath(r) { // the signed-in user's own bucket (ADR-011)
			key := "user:" + u.ID
			if !limiter.reserve(key) {
				tooManyAttempts(w)
				return
			}
			att := attemptFrom(r.Context())
			if att == nil {
				att = &passwordAttempt{}
				r = r.WithContext(context.WithValue(r.Context(), attemptKey{}, att))
			}
			defer func() { limiter.finish(key, att.failed.Load()) }()
		}
		ctx := context.WithValue(r.Context(), keyUser, u)
		access := u.Access
		ctx = app.WithActor(ctx, app.Actor{ID: u.ID, Name: u.Email, Access: &access})
		ctx = context.WithValue(ctx, keyToken, c.Value)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isMutation(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

const maxIdempotencyKey = 128

// withIdempotency replays the stored response when a mutating request repeats an
// Idempotency-Key (24 h retention, housekeeping deletes older keys).
func withIdempotency(store ports.IdempotencyRepository, clock ports.Clock, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		if key == "" || !isMutation(r.Method) || skipsIdempotency(r) {
			next.ServeHTTP(w, r)
			return
		}
		// Stored as "<user id>:<key>" in a 200-byte ASCII column: at most 128 printable
		// ASCII characters, so a valid key can never fail to store.
		if len(key) > maxIdempotencyKey || strings.IndexFunc(key, func(c rune) bool { return c < 0x21 || c > 0x7e }) >= 0 {
			writeProblem(w, Problem{Type: problemType("bad-request"), Title: "Idempotency-Key must be 1–128 printable ASCII characters", Status: http.StatusBadRequest})
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
		// A handler that panics must not leave the key "in progress" for a day.
		defer func() {
			if p := recover(); p != nil {
				_ = store.Release(context.WithoutCancel(ctx), storeKey)
				panic(p)
			}
		}()
		next.ServeHTTP(rec, r)
		// A server error, or a request refused before it ran (signed out, or a password
		// that must be changed first), is not the operation's answer: the key is freed so
		// the same request can run once it is allowed.
		if rec.status >= 500 || rec.status == 0 || rec.status == http.StatusUnauthorized || rec.status == http.StatusForbidden {
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

// loginLimiter bounds password guessing (ADR-011). Every request to a route that checks a
// password (reauthPath) takes a slot in its client address's bucket on arrival — and, once
// signed in, in its user's bucket — so that slots in flight plus failures in the last
// window never exceed max: at most max such requests per bucket reach the password check
// per window, however many arrive at once. A request whose password was right (or that
// never got to check one: no session, bad body) gives its slot back; a wrong password
// turns it into a failure that expires after the window.
//
// The address is the TCP peer. Behind NAT, a reverse proxy or a NodePort without
// externalTrafficPolicy: Local, many people share one address and share its bucket; the
// per-user bucket keeps one signed-in user's guesses from using up everybody's.
type loginLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	max     int
	clock   ports.Clock
	buckets map[string]*limitBucket
}

type limitBucket struct {
	failures []time.Time
	inflight int
}

func newLoginLimiter(clock ports.Clock) *loginLimiter {
	return &loginLimiter{window: time.Minute, max: 10, clock: clock, buckets: map[string]*limitBucket{}}
}

// reserve takes a slot in key's bucket, or reports that the bucket is full.
func (l *loginLimiter) reserve(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock.Now()
	b := l.buckets[key]
	if b == nil {
		if len(l.buckets) >= 10000 { // bound memory under a spray of addresses
			l.evict(now)
		}
		b = &limitBucket{}
		l.buckets[key] = b
	}
	kept := b.failures[:0]
	for _, t := range b.failures {
		if now.Sub(t) < l.window {
			kept = append(kept, t)
		}
	}
	b.failures = kept
	if len(b.failures)+b.inflight >= l.max {
		return false
	}
	b.inflight++
	return true
}

// evict frees buckets that hold nothing: no request in flight and no failure inside the
// window. A spray of new addresses therefore cannot wipe the recent failures of the one being
// guessed. If every bucket is still busy, the one whose last failure is oldest goes.
func (l *loginLimiter) evict(now time.Time) {
	var oldestKey string
	var oldest time.Time
	for k, b := range l.buckets {
		last := time.Time{}
		if n := len(b.failures); n > 0 {
			last = b.failures[n-1]
		}
		if b.inflight == 0 && now.Sub(last) >= l.window {
			delete(l.buckets, k)
			continue
		}
		if b.inflight == 0 && (oldestKey == "" || last.Before(oldest)) {
			oldestKey, oldest = k, last
		}
	}
	if len(l.buckets) >= 10000 && oldestKey != "" {
		delete(l.buckets, oldestKey)
	}
}

// addressKey is the rate-limit identity of a client address: the address itself for IPv4,
// its /64 network for IPv6 (one host usually has a whole /64 to rotate through).
func addressKey(ip string) string {
	a, err := netip.ParseAddr(ip)
	if err != nil || a.Is4() || a.Is4In6() {
		return ip
	}
	p, err := a.Prefix(64)
	if err != nil {
		return ip
	}
	return p.String()
}

// finish returns key's slot; failed records it as a failed password attempt.
func (l *loginLimiter) finish(key string, failed bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b == nil {
		return
	}
	b.inflight--
	if failed {
		b.failures = append(b.failures, l.clock.Now())
	}
	if b.inflight <= 0 && len(b.failures) == 0 {
		delete(l.buckets, key)
	}
}

// passwordAttempt carries, for one request, whether a password was checked and wrong. The
// response error handler sets it (wrongPassword); the limiter reads it when the request ends.
type passwordAttempt struct{ failed atomic.Bool }

type attemptKey struct{}

func attemptFrom(ctx context.Context) *passwordAttempt {
	a, _ := ctx.Value(attemptKey{}).(*passwordAttempt)
	return a
}

// wrongPassword reports whether err means a password was checked and did not match: a
// failed sign-in, or a wrong current password on a re-authenticated route.
func wrongPassword(r *http.Request, err error) bool {
	var ve *domain.ValidationError
	if errors.As(err, &ve) {
		return ve.Field == "currentPassword"
	}
	return r.URL.Path == "/api/v1/auth/login" && errors.Is(err, domain.ErrUnauthenticated)
}

func tooManyAttempts(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "60")
	writeProblem(w, Problem{Type: problemType("rate-limited"), Title: "Too many password attempts", Status: http.StatusTooManyRequests, Detail: "Wait a minute, then try again."})
}

// middleware applies the per-address bucket (the per-user one is taken in withAPIGuards,
// once the user is known).
func (l *loginLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !reauthPath(r) {
			next.ServeHTTP(w, r)
			return
		}
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		key := "ip:" + addressKey(ip)
		if !l.reserve(key) {
			tooManyAttempts(w)
			return
		}
		att := &passwordAttempt{}
		defer func() { l.finish(key, att.failed.Load()) }()
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), attemptKey{}, att)))
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
