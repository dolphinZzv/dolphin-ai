package dream

import (
	"dolphin/internal/session"
	"dolphin/internal/types"
)

// shouldRun implements the Phase 0 gate: is there enough signal to justify
// a dream run?
func (d *Dream) shouldRun(sessions []*session.Session) (bool, string) {
	if d.state == nil {
		return false, "state not initialised"
	}
	// Find sessions newer than last dream.
	var newSessions []*session.Session
	for _, s := range sessions {
		if s.CreatedAt.After(d.state.LastDreamAt) {
			newSessions = append(newSessions, s)
		}
	}

	if len(newSessions) < d.minSessions {
		return false, "insufficient sessions"
	}

	totalUserMsgs := 0
	for _, s := range newSessions {
		msgs, _ := d.memory.Read(d.ctx, s.ID, 0, 0)
		for _, m := range msgs {
			if m.Role == types.RoleUser {
				totalUserMsgs++
			}
		}
	}

	if totalUserMsgs < d.minUserMessages {
		return false, "insufficient user messages"
	}

	// If recent dreams produced nothing, only allow if enough new sessions.
	if d.state.ConsecutiveEmpty >= 2 && len(newSessions) < 5 {
		return false, "recent dreams produced no edits"
	}

	// Avoid running again if sessions overlap with the previous dream.
	if d.sessionsOverlapWithLastDream(newSessions) {
		return false, "too soon, overlapping sessions"
	}

	return true, ""
}

// sessionsOverlapWithLastDream returns true if any of the given sessions were
// seen by the last dream.
func (d *Dream) sessionsOverlapWithLastDream(sessions []*session.Session) bool {
	if d.state.LastDreamAt.IsZero() {
		return false
	}
	// If the last dream was very recent and some sessions pre-date it,
	// we are looking at overlap.
	overlapCutoff := d.state.LastDreamAt.Add(-d.interval * 3)
	for _, s := range sessions {
		if s.CreatedAt.Before(overlapCutoff) {
			continue // too old to matter
		}
		if s.CreatedAt.Before(d.state.LastDreamAt) {
			return true
		}
	}
	return false
}
