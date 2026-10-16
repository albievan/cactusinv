package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/ports/fakes"
)

func loginRequest(remote string) *http.Request {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{}`))
	r.RemoteAddr = remote
	return r
}

// TestLimiterBoundsConcurrentAttempts: 50 attempts arriving at once from one address — at
// most 10 reach the password check; the rest are refused before it.
func TestLimiterBoundsConcurrentAttempts(t *testing.T) {
	clock := &fakes.Clock{T: time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)}
	l := newLoginLimiter(clock)
	var entered atomic.Int32
	release := make(chan struct{})
	h := l.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered.Add(1)
		<-release
		attemptFrom(r.Context()).failed.Store(true) // a wrong password
		w.WriteHeader(http.StatusUnauthorized)
	}))
	var wg sync.WaitGroup
	var limited atomic.Int32
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, loginRequest("10.0.0.1:5000"))
			if rec.Code == http.StatusTooManyRequests {
				limited.Add(1)
			}
		}()
	}
	for entered.Load()+limited.Load() < 50 {
		time.Sleep(time.Millisecond)
	}
	if entered.Load() != 10 || limited.Load() != 40 {
		t.Fatalf("entered %d, limited %d", entered.Load(), limited.Load())
	}
	close(release)
	wg.Wait()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, loginRequest("10.0.0.1:5001"))
	if rec.Code != http.StatusTooManyRequests || entered.Load() != 10 {
		t.Fatal("ten failures keep the address limited for the window")
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, loginRequest("10.0.0.2:5000"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatal("another address has its own bucket")
	}
	clock.Advance(time.Minute)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, loginRequest("10.0.0.1:5002"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("after the window: %d", rec.Code)
	}
}

// TestLimiterSuccessesGiveSlotsBack: right passwords (and requests that never checked one)
// do not use up the bucket.
func TestLimiterSuccessesGiveSlotsBack(t *testing.T) {
	l := newLoginLimiter(&fakes.Clock{T: time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)})
	h := l.middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for i := range 30 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, loginRequest("10.0.0.1:5000"))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("attempt %d: %d", i, rec.Code)
		}
	}
	if len(l.buckets) != 0 {
		t.Fatal("an idle, failure-free bucket is dropped")
	}
}

// TestUserBucketOnReauthRoutes: a signed-in user's wrong current passwords are limited per
// user, whatever address they come from.
func TestUserBucketOnReauthRoutes(t *testing.T) {
	clock := &fakes.Clock{T: time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)}
	store := fakes.NewStore()
	svc := app.New(app.Deps{Tx: store, Clock: clock, IDs: &fakes.IDs{}, Hasher: fakes.Hasher{}})
	if _, err := svc.CreateUser(context.Background(), "albie@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	token, _, err := svc.Login(context.Background(), "albie@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	l := newLoginLimiter(clock)
	var entered int
	h := withAPIGuards(svc, maxBody, l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered++
		attemptFrom(r.Context()).failed.Store(true)
		w.WriteHeader(http.StatusUnprocessableEntity)
	}))
	codes := map[int]int{}
	for i := range 12 {
		r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/users/x/disable", strings.NewReader(`{}`))
		r.RemoteAddr = "10.0.0." + string(rune('1'+i%9)) + ":5000" // many addresses
		r.Header.Set(csrfHeader, csrfValue)
		r.AddCookie(&http.Cookie{Name: SessionCookie, Value: token})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		codes[rec.Code]++
	}
	if entered != 10 || codes[http.StatusTooManyRequests] != 2 {
		t.Fatalf("entered %d, codes %v", entered, codes)
	}
}
