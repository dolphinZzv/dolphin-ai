package agentmesh

import (
	"sync"
	"time"
)

// ServerRateLimiter throttles incoming delegations on the receiver side, at
// three layers: per parent session, per upstream agent, and global. Defaults
// follow the design doc's corrected values (per-session 30/m, per-peer 60/m,
// global 120/m) so a parent spawning max_children_per_session in parallel is
// not throttled by its own traffic.
type ServerRateLimiter struct {
	mu         sync.Mutex
	perSession map[string]*tokenBucket
	perPeer    map[string]*tokenBucket
	global     *tokenBucket

	sessionRate float64 // req/s
	peerRate    float64
	globalRate  float64
	burst       int
	now         func() time.Time
}

// NewServerRateLimiter builds a receiver-side limiter. Rates are per-second.
func NewServerRateLimiter(sessionPerMin, peerPerMin, globalPerMin int) *ServerRateLimiter {
	if sessionPerMin <= 0 {
		sessionPerMin = 30
	}
	if peerPerMin <= 0 {
		peerPerMin = 60
	}
	if globalPerMin <= 0 {
		globalPerMin = 120
	}
	burst := 10
	return &ServerRateLimiter{
		perSession:  map[string]*tokenBucket{},
		perPeer:     map[string]*tokenBucket{},
		global:      newTokenBucket(float64(globalPerMin)/60.0, burst),
		sessionRate: float64(sessionPerMin) / 60.0,
		peerRate:    float64(peerPerMin) / 60.0,
		burst:       burst,
		now:         func() time.Time { return time.Now() },
	}
}

// Allow reports whether an incoming delegation from `from` (peer agent addr)
// under `parentSessionID` may be accepted now.
func (s *ServerRateLimiter) Allow(from, parentSessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if parentSessionID != "" {
		sb, ok := s.perSession[parentSessionID]
		if !ok {
			sb = newTokenBucket(s.sessionRate, s.burst)
			s.perSession[parentSessionID] = sb
		}
		if !sb.allow(now) {
			return false
		}
	}
	if from != "" {
		pb, ok := s.perPeer[from]
		if !ok {
			pb = newTokenBucket(s.peerRate, s.burst)
			s.perPeer[from] = pb
		}
		if !pb.allow(now) {
			return false
		}
	}
	return s.global.allow(now)
}

// AllowTask is an alias for Allow, satisfying the a2a.TaskRateLimiter interface.
func (s *ServerRateLimiter) AllowTask(from, sessionID string) bool {
	return s.Allow(from, sessionID)
}

// withClock injects a clock (tests).
func (s *ServerRateLimiter) withClock(now func() time.Time) *ServerRateLimiter {
	s.now = now
	s.global.last = time.Time{}
	return s
}
