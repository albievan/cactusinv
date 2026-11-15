package httpapi

import (
	"slices"
	"sync"
	"time"

	"github.com/albievan/cactusinv/internal/ports"
)

// recentRequests is how many of the newest API requests the average and 95th percentile
// are taken over.
const recentRequests = 1000

// RequestStats counts the API requests served since the start, for the dashboard's
// performance (M57). It implements ports.RequestStatsSource; safe for concurrent use.
type RequestStats struct {
	now func() time.Time

	mu                    sync.Mutex
	total, client, server int64
	recent                [recentRequests]time.Duration
	filled, next          int
	perSecond             [60]struct{ sec, n int64 }
}

var _ ports.RequestStatsSource = (*RequestStats)(nil)

// NewRequestStats starts counting.
func NewRequestStats() *RequestStats { return &RequestStats{now: time.Now} }

// record adds a finished request.
func (s *RequestStats) record(status int, took time.Duration) {
	sec := s.now().Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total++
	switch {
	case status >= 500:
		s.server++
	case status >= 400:
		s.client++
	}
	s.recent[s.next] = took
	s.next = (s.next + 1) % recentRequests
	s.filled = min(s.filled+1, recentRequests)
	b := &s.perSecond[sec%60]
	if b.sec != sec {
		b.sec, b.n = sec, 0
	}
	b.n++
}

// RequestStats implements ports.RequestStatsSource.
func (s *RequestStats) RequestStats() ports.RequestStats {
	sec := s.now().Unix()
	s.mu.Lock()
	out := ports.RequestStats{Total: s.total, ClientErrors: s.client, ServerErrors: s.server}
	recent := slices.Clone(s.recent[:s.filled])
	for _, b := range s.perSecond {
		if sec-b.sec < 60 {
			out.LastMinute += b.n
		}
	}
	s.mu.Unlock()
	if len(recent) == 0 {
		return out
	}
	var sum time.Duration
	for _, d := range recent {
		sum += d
	}
	out.Average = sum / time.Duration(len(recent))
	slices.Sort(recent)
	out.P95 = recent[(len(recent)*95+99)/100-1]
	return out
}
