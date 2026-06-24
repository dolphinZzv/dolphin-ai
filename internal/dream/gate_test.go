package dream

import (
	"testing"
	"time"

	"dolphin/internal/session"
	"dolphin/internal/types"
)

// ─────────────────────────────────────────────────────────────────
// Phase 0 Gate tests
// ─────────────────────────────────────────────────────────────────

func TestGate_TooFewSessions(t *testing.T) {
	d := &Dream{minSessions: 2}
	ss := &mockSessionMgr{sessions: []*session.Session{
		makeSession("s1", time.Now(), true),
	}}
	ok, _ := d.shouldRun(ss.sessions)
	if ok {
		t.Fatal("expected skip: too few sessions")
	}
}

func TestGate_ZeroSessions(t *testing.T) {
	d := &Dream{minSessions: 2}
	ss := &mockSessionMgr{sessions: []*session.Session{}}
	ok, _ := d.shouldRun(ss.sessions)
	if ok {
		t.Fatal("expected skip: zero sessions")
	}
}

func TestGate_InsufficientMessages(t *testing.T) {
	d := &Dream{minSessions: 1, minUserMessages: 8, memory: newMockMemory()}
	now := time.Now()
	ss := &mockSessionMgr{sessions: []*session.Session{
		makeSession("s1", now.Add(-1*time.Hour), true),
		makeSession("s2", now, true),
	}}
	d.state = newState()
	if ok, _ := d.shouldRun(ss.sessions); ok {
		t.Fatal("expected skip: 0 user messages")
	}
}

func TestGate_EnoughMessages(t *testing.T) {
	mem := newMockMemory()
	now := time.Now()
	mem.messages["s1"] = []types.Message{
		userMsg("hello", now), userMsg("do X", now),
		userMsg("more", now), userMsg("again", now),
	}
	mem.messages["s2"] = []types.Message{
		userMsg("hi", now), userMsg("deploy", now),
		userMsg("test", now), userMsg("push", now),
	}
	d := &Dream{minSessions: 1, minUserMessages: 8, memory: mem}
	ss := &mockSessionMgr{sessions: []*session.Session{
		makeSession("s1", now.Add(-1*time.Hour), true),
		makeSession("s2", now, true),
	}}
	d.state = newState()
	d.state.LastDreamAt = now.Add(-2 * time.Hour) // sessions after this
	if ok, _ := d.shouldRun(ss.sessions); !ok {
		t.Fatal("expected pass: 8 user messages")
	}
}

func TestGate_ConsecutiveEmptyBlocks(t *testing.T) {
	mem := newMockMemory()
	now := time.Now()
	for i := 0; i < 10; i++ {
		mem.messages["s_t"] = append(mem.messages["s_t"], userMsg("x", now))
	}
	d := &Dream{minSessions: 1, minUserMessages: 8, maxConsecutiveEmpty: 2, memory: mem}
	d.state = newState()
	d.state.ConsecutiveEmpty = 3
	ss := &mockSessionMgr{sessions: []*session.Session{
		makeSession("s1", now.Add(-1*time.Hour), true),
		makeSession("s2", now, true),
	}}
	// consecutiveEmpty >= 2, sessions < 5 → should be blocked
	if ok, _ := d.shouldRun(ss.sessions); ok {
		t.Fatal("expected skip: consecutive empty blockade")
	}
}

func TestGate_ConsecutiveEmptyLiftedByManySessions(t *testing.T) {
	mem := newMockMemory()
	now := time.Now()
	for i := 0; i < 10; i++ {
		mem.messages["s"] = append(mem.messages["s"], userMsg("x", now))
	}
	d := &Dream{minSessions: 1, minUserMessages: 1, maxConsecutiveEmpty: 2, memory: mem}
	d.state = newState()
	d.state.ConsecutiveEmpty = 3
	d.state.LastDreamAt = now.Add(-2 * time.Hour) // sessions after this
	// 6 sessions, >= 6 user messages → should pass despite consecutive empty
	var sessions []*session.Session
	for i := 0; i < 6; i++ {
		mid := "s" + string(rune('a'+i))
		mem.messages[mid] = []types.Message{userMsg("x", now)}
		sessions = append(sessions, makeSession(mid, now.Add(-1*time.Hour), true))
	}
	ss := &mockSessionMgr{sessions: sessions}
	if ok, _ := d.shouldRun(ss.sessions); !ok {
		t.Fatal("expected pass: enough sessions lifts blockade")
	}
}

func TestGate_SessionOverlap(t *testing.T) {
	mem := newMockMemory()
	now := time.Now()
	mem.messages["s1"] = []types.Message{userMsg("x", now)}
	d := &Dream{minSessions: 1, minUserMessages: 1, interval: 20 * time.Minute, memory: mem}
	d.state = newState()
	d.state.LastDreamAt = now.Add(-10 * time.Minute) // last dream 10 min ago
	ss := &mockSessionMgr{sessions: []*session.Session{
		makeSession("s1", now.Add(-15*time.Minute), true), // pre-dates last dream → overlap
		makeSession("s2", now, true),
	}}
	if ok, _ := d.shouldRun(ss.sessions); ok {
		t.Fatal("expected skip: overlapping session")
	}
}

func TestGate_SessionsOverlapWithLastDream_NoPriorDream(t *testing.T) {
	d := &Dream{interval: 20 * time.Minute}
	d.state = newState()
	// No prior dream → no overlap.
	if d.sessionsOverlapWithLastDream([]*session.Session{
		makeSession("s1", time.Now(), true),
	}) {
		t.Error("should not overlap with no prior dream")
	}
}

func TestGate_SessionsOverlap_AllNew(t *testing.T) {
	d := &Dream{interval: 20 * time.Minute}
	d.state = newState()
	d.state.LastDreamAt = time.Now().Add(-1 * time.Hour)
	// Session created after last dream → no overlap.
	if d.sessionsOverlapWithLastDream([]*session.Session{
		makeSession("s1", time.Now().Add(-30*time.Minute), true),
	}) {
		t.Error("session after last dream should not overlap")
	}
}

func TestGate_SessionsOverlap_HasOverlap(t *testing.T) {
	d := &Dream{interval: 20 * time.Minute}
	d.state = newState()
	d.state.LastDreamAt = time.Now().Add(-10 * time.Minute)
	// Session created before last dream and within the (interval*3) window.
	if !d.sessionsOverlapWithLastDream([]*session.Session{
		makeSession("s1", time.Now().Add(-15*time.Minute), true),
	}) {
		t.Error("should detect overlap")
	}
}

func TestGate_SessionsOverlap_TooOld(t *testing.T) {
	d := &Dream{interval: 20 * time.Minute}
	d.state = newState()
	d.state.LastDreamAt = time.Now().Add(-10 * time.Minute)
	// Session created 3 hours ago → well before last_dream_at - interval*3.
	if d.sessionsOverlapWithLastDream([]*session.Session{
		makeSession("s1", time.Now().Add(-3*time.Hour), true),
	}) {
		t.Error("old session should not be considered overlap")
	}
}

func TestGate_ExactlyMinSessions(t *testing.T) {
	mem := newMockMemory()
	now := time.Now()
	mem.messages["s1"] = []types.Message{userMsg("x", now)}
	mem.messages["s2"] = []types.Message{userMsg("x", now)}
	d := &Dream{minSessions: 2, minUserMessages: 1, memory: mem}
	d.state = newState()
	d.state.LastDreamAt = now.Add(-2 * time.Hour)
	ss := &mockSessionMgr{sessions: []*session.Session{
		makeSession("s1", now.Add(-1*time.Hour), true),
		makeSession("s2", now.Add(-30*time.Minute), true),
	}}
	ok, _ := d.shouldRun(ss.sessions)
	if !ok {
		t.Fatal("expected pass with exactly 2 sessions")
	}
}
