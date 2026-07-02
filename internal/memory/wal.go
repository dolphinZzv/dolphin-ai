package memory

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"sync"
	"time"

	"dolphin/internal/types"

	"github.com/tidwall/wal"
)

// WAL entry type constants.
const (
	walTypeMsg     byte = 0 // types.Message
	walTypeCompact byte = 1 // CompactPayload
	walTypeTurn    byte = 2 // TurnPayload
)

// CompactPayload is the data stored in a walTypeCompact entry.
type CompactPayload struct {
	Messages []types.Message
	SrcStart int    // first msg index included in this compact
	SrcEnd   int    // first msg index after this compact
	Summary  string // human-readable description of what was compacted
}

// TurnPayload is the data stored in a walTypeTurn entry.
type TurnPayload struct {
	TurnID       string
	Input        string
	ModelName    string
	InTokens     int
	OutTokens    int
	Rounds       int
	MsgStart     int // index in the entries slice
	MsgEnd       int // index in the entries slice
	SystemPrompt string // the system prompt at turn time (appended for gob compat)
}

// walCompactEntry is an in-memory cache of the latest compact entry's decoded
// payload. It lives on the index and allows Read() to serve the baseline
// message list without touching the WAL.
type walCompactCache struct {
	messages []types.Message // decoded compact payload messages
	msgCount int             // total effective messages at compact time
	summary  string          // compact summary description
}

// walEntry is a lightweight entry in the in-memory index.
type walEntry struct {
	seq uint64 // WAL sequence number
	ts  int64  // Unix timestamp from the entry header
	typ byte   // walType*
}

// TurnMark is a lightweight turn boundary marker in the index.
type TurnMark struct {
	Seq          uint64
	TurnID       string
	Input        string
	SystemPrompt string
	ModelName    string
	InTokens     int
	OutTokens    int
	Rounds       int
}

// walIndex holds the in-memory index for one session.
type walIndex struct {
	entries   []walEntry  // ALL entries, including those before the compact point
	compactAt int         // position in the entries slice of the last walTypeCompact, or -1
	compact   walCompactCache
	turnMarks []TurnMark
	msgCount  int // total effective message count (compact + post-compact msgs)
}

// WALMemory implements Memory backed by a tidwall/wal append-only log.
// It keeps a lightweight in-memory index (seq/type/ts only, no message data)
// and caches only the latest compact snapshot. All message data lives in the
// WAL — Read() replays compact-snapshot + subsequent entries.
type WALMemory struct {
	mu          sync.Mutex
	dir         string
	sessions    map[string]*walIndex
	logs        map[string]*wal.Log // sessionID → open WAL log
	gcNext      time.Time
	retention   time.Duration // how long to keep entries before GC
	keepTurns   int           // minimum turn marks to preserve regardless of age
}

// NewWALMemory opens or creates a WAL-backed memory store in the given
// directory. The WAL files are named {dir}/session_{id}.wal.
func NewWALMemory(dir string, retention time.Duration, keepTurns int) (*WALMemory, error) {
	if retention <= 0 {
		retention = 30 * 24 * time.Hour
	}
	if keepTurns <= 0 {
		keepTurns = 10
	}
	m := &WALMemory{
		dir:       dir,
		sessions:  make(map[string]*walIndex),
		logs:      make(map[string]*wal.Log),
		gcNext:    time.Now().Add(24 * time.Hour),
		retention: retention,
		keepTurns: keepTurns,
	}
	return m, nil
}

// walPath returns the WAL file path for a session.
func (m *WALMemory) walPath(sessionID string) string {
	return m.dir + "/session_" + sessionID + ".wal"
}

// getWAL returns the open WAL log for the session, creating it if needed.
func (m *WALMemory) getWAL(sessionID string) (*wal.Log, error) {
	if log, ok := m.logs[sessionID]; ok {
		return log, nil
	}
	log, err := wal.Open(m.walPath(sessionID), wal.DefaultOptions)
	if err != nil {
		return nil, err
	}
	m.logs[sessionID] = log
	return log, nil
}

// getIndex returns the walIndex for a session, creating/rebuilding if needed.
func (m *WALMemory) getIndex(sessionID string) (*walIndex, error) {
	if idx, ok := m.sessions[sessionID]; ok && idx != nil {
		return idx, nil
	}

	log, err := m.getWAL(sessionID)
	if err != nil {
		return nil, fmt.Errorf("wal: open session %q: %w", sessionID, err)
	}

	idx, err := m.rebuildIndex(log)
	if err != nil {
		return nil, fmt.Errorf("wal: rebuild index for %q: %w", sessionID, err)
	}

	m.sessions[sessionID] = idx
	return idx, nil
}

// rebuildIndex reads all WAL entries and reconstructs the in-memory index.
// It caches the latest compact entry's messages and tracks turn boundaries.
func (m *WALMemory) rebuildIndex(log *wal.Log) (*walIndex, error) {
	idx := &walIndex{
		entries:   nil,
		compactAt: -1,
	}

	lastIdx, err := log.LastIndex()
	if err != nil {
		return nil, err
	}
	if lastIdx == 0 {
		return idx, nil // empty WAL
	}

	firstIdx, err := log.FirstIndex()
	if err != nil {
		return nil, err
	}

	idx.entries = make([]walEntry, 0, int(lastIdx-firstIdx+1))

	for seq := firstIdx; seq <= lastIdx; seq++ {
		data, err := log.Read(seq)
		if err != nil {
			return nil, fmt.Errorf("wal: read seq %d: %w", seq, err)
		}
		if len(data) < 9 {
			return nil, fmt.Errorf("wal: corrupt entry at seq %d (len %d < 9)", seq, len(data))
		}

		ts := int64(binary.BigEndian.Uint64(data[0:8]))
		typ := data[8]
		payload := data[9:]

		idx.entries = append(idx.entries, walEntry{seq: seq, ts: ts, typ: typ})

		switch typ {
		case walTypeCompact:
			var cp CompactPayload
			if err := gob.NewDecoder(bytes.NewReader(payload)).Decode(&cp); err != nil {
				return nil, fmt.Errorf("wal: decode compact at seq %d: %w", seq, err)
			}
			idx.compactAt = len(idx.entries) - 1
			idx.compact = walCompactCache{
				messages: cp.Messages,
				msgCount: len(cp.Messages),
				summary:  cp.Summary,
			}
			idx.msgCount = len(cp.Messages)

		case walTypeMsg:
			idx.msgCount++

		case walTypeTurn:
			var tp TurnPayload
			if err := gob.NewDecoder(bytes.NewReader(payload)).Decode(&tp); err != nil {
				return nil, fmt.Errorf("wal: decode turn at seq %d: %w", seq, err)
			}
			idx.turnMarks = append(idx.turnMarks, TurnMark{
				Seq:          seq,
				TurnID:       tp.TurnID,
				Input:        tp.Input,
				SystemPrompt: tp.SystemPrompt,
				ModelName:    tp.ModelName,
				InTokens:     tp.InTokens,
				OutTokens:    tp.OutTokens,
				Rounds:       tp.Rounds,
			})
		}
	}

	return idx, nil
}

// Read returns messages for a session.
func (m *WALMemory) Read(ctx context.Context, sessionID string, start, end int) ([]types.Message, error) {
	m.mu.Lock()
	idx, err := m.getIndex(sessionID)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}

	// If no compact entry exists yet, we have no data to serve.
	if idx.compactAt < 0 || len(idx.compact.messages) == 0 {
		return nil, nil
	}

	// Start from the compact baseline.
	msgs := make([]types.Message, len(idx.compact.messages))
	copy(msgs, idx.compact.messages)

	// Replay subsequent msg entries from the WAL.
	log, err := m.getWAL(sessionID)
	if err != nil {
		return nil, fmt.Errorf("wal: reopen for read %q: %w", sessionID, err)
	}

	for _, e := range idx.entries[idx.compactAt+1:] {
		if e.typ != walTypeMsg {
			continue
		}
		data, err := log.Read(e.seq)
		if err != nil {
			return nil, fmt.Errorf("wal: read seq %d: %w", e.seq, err)
		}
		var msg types.Message
		if err := gob.NewDecoder(bytes.NewReader(data[9:])).Decode(&msg); err != nil {
			return nil, fmt.Errorf("wal: decode msg at seq %d: %w", e.seq, err)
		}
		msgs = append(msgs, msg)
	}

	return sliceMessages(msgs, start, end), nil
}

// Write appends a single message for the session.
func (m *WALMemory) Write(ctx context.Context, sessionID string, msg types.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	log, err := m.getWAL(sessionID)
	if err != nil {
		return fmt.Errorf("wal: open for write %q: %w", sessionID, err)
	}

	// Build entry bytes.
	var buf bytes.Buffer
	ts := msg.Timestamp.UnixNano()
	if ts == 0 {
		ts = time.Now().UnixNano()
	}
	if err := binary.Write(&buf, binary.BigEndian, ts); err != nil {
		return fmt.Errorf("wal: write ts: %w", err)
	}
	buf.WriteByte(walTypeMsg)
	if err := gob.NewEncoder(&buf).Encode(msg); err != nil {
		return fmt.Errorf("wal: encode msg: %w", err)
	}

	seq, err := log.LastIndex()
	if err != nil {
		return fmt.Errorf("wal: last index: %w", err)
	}
	seq++

	if err := log.Write(seq, buf.Bytes()); err != nil {
		return fmt.Errorf("wal: write seq %d: %w", seq, err)
	}

	// Update index.
	idx, err := m.getIndex(sessionID)
	if err != nil {
		idx = &walIndex{compactAt: -1}
		m.sessions[sessionID] = idx
	}
	idx.entries = append(idx.entries, walEntry{seq: seq, ts: ts, typ: walTypeMsg})
	idx.msgCount++

	return nil
}

// Replace overwrites all messages for the session by writing a compact entry.
func (m *WALMemory) Replace(ctx context.Context, sessionID string, msgs []types.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	log, err := m.getWAL(sessionID)
	if err != nil {
		return fmt.Errorf("wal: open for replace %q: %w", sessionID, err)
	}

	idx, err := m.getIndex(sessionID)
	if err != nil {
		idx = &walIndex{compactAt: -1}
		m.sessions[sessionID] = idx
	}

	cp := CompactPayload{
		Messages: msgs,
		SrcStart: idx.compact.msgCount,
		SrcEnd:   idx.msgCount,
	}

	// Build entry.
	var buf bytes.Buffer
	ts := time.Now().UnixNano()
	if err := binary.Write(&buf, binary.BigEndian, ts); err != nil {
		return fmt.Errorf("wal: write ts: %w", err)
	}
	buf.WriteByte(walTypeCompact)
	if err := gob.NewEncoder(&buf).Encode(cp); err != nil {
		return fmt.Errorf("wal: encode compact: %w", err)
	}

	seq, err := log.LastIndex()
	if err != nil {
		return fmt.Errorf("wal: last index: %w", err)
	}
	seq++

	if err := log.Write(seq, buf.Bytes()); err != nil {
		return fmt.Errorf("wal: write compact seq %d: %w", seq, err)
	}

	// Update index.
	idx.entries = append(idx.entries, walEntry{seq: seq, ts: ts, typ: walTypeCompact})
	idx.compactAt = len(idx.entries) - 1
	idx.compact = walCompactCache{
		messages: msgs,
		msgCount: len(msgs),
	}
	idx.msgCount = len(msgs)

	return nil
}

// WriteTurn appends a turn checkpoint entry. This is NOT part of the Memory
// interface — it is a WALMemory-specific method used by the agent loop to
// record turn metadata for /replay /rewind /diff.
func (m *WALMemory) WriteTurn(ctx context.Context, sessionID string, tp TurnPayload) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	log, err := m.getWAL(sessionID)
	if err != nil {
		return fmt.Errorf("wal: open for turn %q: %w", sessionID, err)
	}

	var buf bytes.Buffer
	ts := time.Now().UnixNano()
	if err := binary.Write(&buf, binary.BigEndian, ts); err != nil {
		return fmt.Errorf("wal: write ts: %w", err)
	}
	buf.WriteByte(walTypeTurn)
	if err := gob.NewEncoder(&buf).Encode(tp); err != nil {
		return fmt.Errorf("wal: encode turn: %w", err)
	}

	seq, err := log.LastIndex()
	if err != nil {
		return fmt.Errorf("wal: last index: %w", err)
	}
	seq++

	if err := log.Write(seq, buf.Bytes()); err != nil {
		return fmt.Errorf("wal: write turn seq %d: %w", seq, err)
	}

	idx, _ := m.getIndex(sessionID)
	idx.entries = append(idx.entries, walEntry{seq: seq, ts: ts, typ: walTypeTurn})
	idx.turnMarks = append(idx.turnMarks, TurnMark{
		Seq:          seq,
		TurnID:       tp.TurnID,
		Input:        tp.Input,
		SystemPrompt: tp.SystemPrompt,
		ModelName:    tp.ModelName,
		InTokens:     tp.InTokens,
		OutTokens:    tp.OutTokens,
		Rounds:       tp.Rounds,
	})

	return nil
}

// TurnMarks returns all turn marks for a session, most recent first.
func (m *WALMemory) TurnMarks(sessionID string) ([]TurnMark, error) {
	m.mu.Lock()
	idx, err := m.getIndex(sessionID)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	out := make([]TurnMark, len(idx.turnMarks))
	copy(out, idx.turnMarks)
	return out, nil
}

// RewindTo creates a new compact entry at the given WAL seq, effectively
// rolling back the session state to that point. Subsequent Read() calls
// see only the messages up to and including seq.
func (m *WALMemory) RewindTo(sessionID string, seq uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx, err := m.getIndex(sessionID)
	if err != nil {
		return err
	}
	if idx.compactAt < 0 {
		return fmt.Errorf("wal: no compact entry in session %q", sessionID)
	}

	// Replay from the most recent compact that is <= seq.
	targetAt := idx.compactAt
	for i := len(idx.entries) - 1; i >= 0; i-- {
		if idx.entries[i].typ == walTypeCompact && idx.entries[i].seq <= seq {
			targetAt = i
			break
		}
	}

	log, err := m.getWAL(sessionID)
	if err != nil {
		return err
	}
	data, err := log.Read(idx.entries[targetAt].seq)
	if err != nil {
		return fmt.Errorf("wal: read compact at seq %d: %w", idx.entries[targetAt].seq, err)
	}
	var cp CompactPayload
	if err := gob.NewDecoder(bytes.NewReader(data[9:])).Decode(&cp); err != nil {
		return fmt.Errorf("wal: decode compact: %w", err)
	}

	msgs := make([]types.Message, len(cp.Messages))
	copy(msgs, cp.Messages)
	for _, e := range idx.entries[targetAt+1:] {
		if e.seq > seq {
			break
		}
		if e.typ != walTypeMsg {
			continue
		}
		data, err := log.Read(e.seq)
		if err != nil {
			return fmt.Errorf("wal: read msg at seq %d: %w", e.seq, err)
		}
		var msg types.Message
		if err := gob.NewDecoder(bytes.NewReader(data[9:])).Decode(&msg); err != nil {
			return fmt.Errorf("wal: decode msg at seq %d: %w", e.seq, err)
		}
		msgs = append(msgs, msg)
	}

	// Write a new compact entry to mark the rewind point.
	cp = CompactPayload{Messages: msgs}
	var buf bytes.Buffer
	ts := time.Now().UnixNano()
	binary.Write(&buf, binary.BigEndian, ts)
	buf.WriteByte(walTypeCompact)
	gob.NewEncoder(&buf).Encode(cp)

	newSeq, _ := log.LastIndex()
	newSeq++
	if err := log.Write(newSeq, buf.Bytes()); err != nil {
		return fmt.Errorf("wal: write rewind compact: %w", err)
	}

	idx.entries = append(idx.entries, walEntry{seq: newSeq, ts: ts, typ: walTypeCompact})
	idx.compactAt = len(idx.entries) - 1
	idx.compact.messages = msgs
	idx.compact.msgCount = len(msgs)
	idx.msgCount = len(msgs)

	return nil
}

// Close closes all open WAL files.
func (m *WALMemory) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, log := range m.logs {
		log.Close()
	}
	m.logs = nil
	m.sessions = nil
	return nil
}

// GC runs daily garbage collection: deletes entries older than 30 days,
// keeping the latest compact entry and the last 10 turn marks' messages.
func (m *WALMemory) GC(now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if now.Before(m.gcNext) {
		return nil
	}
	m.gcNext = now.Add(24 * time.Hour)

	cutoff := now.Add(-m.retention).Unix()
	keepTurns := m.keepTurns

	for sessionID, idx := range m.sessions {
		log, err := m.getWAL(sessionID)
		if err != nil {
			continue
		}

		firstSeq, err := log.FirstIndex()
		if err != nil {
			continue
		}

		// Find the first entry to keep.
		// Must keep: the latest compact entry + entries within 10 most recent turns.
		var keepFromSeq uint64
		if idx.compactAt >= 0 {
			keepFromSeq = idx.entries[idx.compactAt].seq
		}

		// Also keep the last 10 turn marks' message ranges.
		if len(idx.turnMarks) > 0 {
			start := 0
			if len(idx.turnMarks) > keepTurns {
				start = len(idx.turnMarks) - keepTurns
			}
			// The first turn mark we want to keep may be before the compact.
			// Find the compact before that turn's first msg.
			for _, tm := range idx.turnMarks[start:] {
				// Find entries before this turn's seq
				for i := len(idx.entries) - 1; i >= 0; i-- {
					if idx.entries[i].seq <= tm.Seq && idx.entries[i].typ == walTypeCompact {
						if idx.entries[i].seq < keepFromSeq || keepFromSeq == 0 {
							keepFromSeq = idx.entries[i].seq
						}
						break
					}
				}
			}
		}

		if keepFromSeq == 0 {
			continue
		}

		// Truncate entries before keepFromSeq that are older than cutoff.
		for seq := firstSeq; seq < keepFromSeq; seq++ {
			data, err := log.Read(seq)
			if err != nil {
				break
			}
			ts := int64(binary.BigEndian.Uint64(data[0:8]))
			if ts < cutoff {
				continue // will be truncated
			}
			// This entry is too recent to delete — stop.
			keepFromSeq = seq
			break
		}

		if keepFromSeq > firstSeq {
			if err := log.TruncateFront(keepFromSeq); err != nil {
				continue
			}
			// Rebuild index entries to remove truncated front.
			newEntries := make([]walEntry, 0, len(idx.entries))
			for _, e := range idx.entries {
				if e.seq >= keepFromSeq {
					newEntries = append(newEntries, e)
				}
			}
			idx.entries = newEntries
			// Fix compactAt.
			idx.compactAt = -1
			for i, e := range idx.entries {
				if e.typ == walTypeCompact {
					idx.compactAt = i
				}
			}
		}
	}

	return nil
}

func init() {
	gob.Register(types.Message{})
	gob.Register(types.ContentPart{})
	gob.Register(types.ToolCall{})
	gob.Register(types.ToolDef{})
	gob.Register(CompactPayload{})
	gob.Register(TurnPayload{})
}
