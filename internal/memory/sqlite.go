package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"

	"dolphin/internal/types"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// writeReq is a write operation submitted to the writer goroutine.
type writeReq struct {
	kind      writeKind
	sessionID string
	data      []byte // JSON-serialized message or message array
	errCh     chan error
}

type writeKind uint8

const (
	writeSingle  writeKind = iota // Write: append one message
	writeReplace                  // Replace: overwrite entire session
)

const schema = `
PRAGMA journal_mode=WAL;
PRAGMA busy_timeout=5000;
PRAGMA synchronous=NORMAL;
CREATE TABLE IF NOT EXISTS messages (
    session_id TEXT NOT NULL,
    seq        INTEGER NOT NULL DEFAULT 0,
    data       TEXT NOT NULL,
    PRIMARY KEY (session_id, seq)
);
`

// SQLiteMemory implements Memory backed by a SQLite database with a
// single-writer queue to avoid concurrent-write contention.
type SQLiteMemory struct {
	db      *sql.DB
	writeCh chan writeReq
	cancel  context.CancelFunc
	done    chan struct{}
	mu      sync.Mutex // protects Close
	closed  bool
}

// NewSQLiteMemory opens dsn, initializes the schema, starts the write
// goroutine, and returns a ready SQLiteMemory. dsn is passed directly
// to sql.Open so it may be a file path or an in-memory URI.
func NewSQLiteMemory(dsn string) (*SQLiteMemory, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open: %w", err)
	}
	db.SetMaxOpenConns(4)

	// Run schema.
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite: init schema: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	m := &SQLiteMemory{
		db:      db,
		writeCh: make(chan writeReq, 8), // pool_size * 2 buffer
		cancel:  cancel,
		done:    make(chan struct{}),
	}

	go m.writer(ctx)

	return m, nil
}

// writer drains writeCh and executes all writes sequentially.
// It is the sole goroutine that writes to SQLite, avoiding contention.
func (m *SQLiteMemory) writer(ctx context.Context) {
	defer close(m.done)
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-m.writeCh:
			switch req.kind {
			case writeSingle:
				req.errCh <- m.writeMsg(req.sessionID, req.data)
			case writeReplace:
				req.errCh <- m.replaceMsgs(req.sessionID, req.data)
			}
		}
	}
}

func (m *SQLiteMemory) writeMsg(sessionID string, data []byte) error {
	// Insert with auto-increment seq: use max+1.
	var maxSeq int
	err := m.db.QueryRow(
		"SELECT COALESCE(MAX(seq), 0) FROM messages WHERE session_id=?",
		sessionID,
	).Scan(&maxSeq)
	if err != nil {
		return fmt.Errorf("sqlite: write seq: %w", err)
	}
	_, err = m.db.Exec(
		"INSERT INTO messages (session_id, seq, data) VALUES (?, ?, ?)",
		sessionID, maxSeq+1, string(data),
	)
	if err != nil {
		return fmt.Errorf("sqlite: write insert: %w", err)
	}
	return nil
}

func (m *SQLiteMemory) replaceMsgs(sessionID string, data []byte) error {
	var msgs []json.RawMessage
	if err := json.Unmarshal(data, &msgs); err != nil {
		return fmt.Errorf("sqlite: replace unmarshal: %w", err)
	}

	tx, err := m.db.Begin()
	if err != nil {
		return fmt.Errorf("sqlite: replace begin: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM messages WHERE session_id=?", sessionID); err != nil {
		return fmt.Errorf("sqlite: replace delete: %w", err)
	}
	for i, raw := range msgs {
		if _, err := tx.Exec(
			"INSERT INTO messages (session_id, seq, data) VALUES (?, ?, ?)",
			sessionID, i, string(raw),
		); err != nil {
			return fmt.Errorf("sqlite: replace insert %d: %w", i, err)
		}
	}
	return tx.Commit()
}

// Read returns messages for a session. Both 0 means all messages.
// Negative start counts from the end (e.g. -5, 0 = last 5).
func (m *SQLiteMemory) Read(ctx context.Context, sessionID string, start, end int) ([]types.Message, error) {
	// Count total rows.
	var count int
	if err := m.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM messages WHERE session_id=?",
		sessionID,
	).Scan(&count); err != nil || count == 0 {
		return nil, nil
	}

	applyRange := func(count int) (limit, offset int) {
		if start == 0 && end == 0 {
			return count, 0
		}
		rEnd := end
		if rEnd <= 0 || rEnd > count {
			rEnd = count
		}
		rStart := start
		if rStart < 0 {
			rStart = count + rStart
			if rStart < 0 {
				rStart = 0
			}
		}
		if rStart >= rEnd || rStart >= count {
			return 0, 0
		}
		return rEnd - rStart, rStart
	}

	limit, offset := applyRange(count)
	if limit <= 0 {
		return nil, nil
	}

	rows, err := m.db.QueryContext(ctx,
		"SELECT data FROM messages WHERE session_id=? ORDER BY seq LIMIT ? OFFSET ?",
		sessionID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: read query: %w", err)
	}
	defer rows.Close()

	var msgs []types.Message
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("sqlite: read scan: %w", err)
		}
		var msg types.Message
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			return nil, fmt.Errorf("sqlite: read unmarshal: %w", err)
		}
		msgs = append(msgs, msg)
	}
	return msgs, rows.Err()
}

// Write appends a single message for the given session via the write queue.
func (m *SQLiteMemory) Write(ctx context.Context, sessionID string, msg types.Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("sqlite: write marshal: %w", err)
	}
	ch := make(chan error, 1)
	select {
	case m.writeCh <- writeReq{kind: writeSingle, sessionID: sessionID, data: data, errCh: ch}:
	case <-ctx.Done():
		return ctx.Err()
	}
	return <-ch
}

// Replace overwrites all messages for sessionID with msgs via the write queue.
func (m *SQLiteMemory) Replace(ctx context.Context, sessionID string, msgs []types.Message) error {
	arr := make([]json.RawMessage, len(msgs))
	for i, msg := range msgs {
		data, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("sqlite: replace marshal %d: %w", i, err)
		}
		arr[i] = json.RawMessage(data)
	}
	data, err := json.Marshal(arr)
	if err != nil {
		return fmt.Errorf("sqlite: replace marshal array: %w", err)
	}
	ch := make(chan error, 1)
	select {
	case m.writeCh <- writeReq{kind: writeReplace, sessionID: sessionID, data: data, errCh: ch}:
	case <-ctx.Done():
		return ctx.Err()
	}
	return <-ch
}

// Close stops the writer goroutine and closes the database.
func (m *SQLiteMemory) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	m.cancel()
	<-m.done // wait for writer to drain
	return m.db.Close()
}
