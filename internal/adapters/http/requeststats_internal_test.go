package httpapi

import (
	"testing"
	"time"
)

func TestRequestStats(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	s := &RequestStats{now: func() time.Time { return now }}
	if got := s.RequestStats(); got.Total != 0 || got.Average != 0 {
		t.Fatalf("empty = %+v", got)
	}
	for i := 1; i <= 100; i++ {
		status := 200
		if i%25 == 0 {
			status = 500
		} else if i%10 == 0 {
			status = 404
		}
		s.record(status, time.Duration(i)*time.Millisecond)
	}
	got := s.RequestStats()
	if got.Total != 100 || got.ServerErrors != 4 || got.ClientErrors != 8 || got.Average != 50500*time.Microsecond || got.P95 != 95*time.Millisecond || got.LastMinute != 100 {
		t.Fatalf("stats = %+v", got)
	}
	now = now.Add(61 * time.Second)
	if got := s.RequestStats(); got.LastMinute != 0 || got.Total != 100 {
		t.Fatalf("a minute later = %+v", got)
	}
	for range recentRequests + 5 {
		s.record(200, time.Millisecond)
	}
	if got := s.RequestStats(); got.Average != time.Millisecond || got.Total != 100+recentRequests+5 {
		t.Fatalf("only the newest count: %+v", got)
	}
}
