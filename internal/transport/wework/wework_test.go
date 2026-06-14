package wework

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	transport "dolphin/internal/transport"
	"go.uber.org/zap"
)

type mockWSConn struct {
	mu         sync.Mutex
	readMsg    func() (int, []byte, error)
	writeMsg   func(int, []byte) error
	closeFn    func() error
	deadlineFn func(time.Time) error
	writeLog   []struct {
		msgType int
		data    []byte
	}
	closed bool
}

func (m *mockWSConn) ReadMessage() (int, []byte, error) {
	m.mu.Lock()
	fn := m.readMsg
	m.mu.Unlock()
	if fn != nil {
		return fn()
	}
	return 0, nil, errors.New("unexpected ReadMessage call")
}

func (m *mockWSConn) WriteMessage(msgType int, data []byte) error {
	m.mu.Lock()
	m.writeLog = append(m.writeLog, struct {
		msgType int
		data    []byte
	}{msgType, append([]byte{}, data...)})
	fn := m.writeMsg
	m.mu.Unlock()
	if fn != nil {
		return fn(msgType, data)
	}
	return nil
}

func (m *mockWSConn) Close() error {
	m.mu.Lock()
	m.closed = true
	fn := m.closeFn
	m.mu.Unlock()
	if fn != nil {
		return fn()
	}
	return nil
}

func (m *mockWSConn) SetWriteDeadline(t time.Time) error {
	m.mu.Lock()
	fn := m.deadlineFn
	m.mu.Unlock()
	if fn != nil {
		return fn(t)
	}
	return nil
}

func (m *mockWSConn) lastWrite() (int, []byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.writeLog) == 0 {
		return 0, nil, false
	}
	w := m.writeLog[len(m.writeLog)-1]
	return w.msgType, w.data, true
}

func TestWeWorkValOr(t *testing.T) {
	t.Run("returns value when present", func(t *testing.T) {
		cfg := map[string]any{"key": "value"}
		if got := valOr(cfg, "key", "default"); got != "value" {
			t.Errorf("valOr = %q", got)
		}
	})

	t.Run("returns default when missing", func(t *testing.T) {
		cfg := map[string]any{}
		if got := valOr(cfg, "missing", "default"); got != "default" {
			t.Errorf("valOr = %q", got)
		}
	})

	t.Run("returns default when empty string", func(t *testing.T) {
		cfg := map[string]any{"key": ""}
		if got := valOr(cfg, "key", "default"); got != "default" {
			t.Errorf("valOr = %q", got)
		}
	})

	t.Run("joins []any values", func(t *testing.T) {
		cfg := map[string]any{"key": []any{"a", "b"}}
		if got := valOr(cfg, "key", ""); got != "a,b" {
			t.Errorf("valOr = %q", got)
		}
	})

	t.Run("returns default for non-string non-array", func(t *testing.T) {
		cfg := map[string]any{"key": 42}
		if got := valOr(cfg, "key", "default"); got != "default" {
			t.Errorf("valOr = %q", got)
		}
	})
}

func TestWeWorkID(t *testing.T) {
	w := &WeWork{id: "wework"}
	if w.ID() != "wework" {
		t.Errorf("ID = %q", w.ID())
	}
}

func TestWeWorkStart(t *testing.T) {
	w := &WeWork{}
	if err := w.Start(context.Background()); err != nil {
		t.Errorf("Start returned error: %v", err)
	}
}

func TestWeWorkContext(t *testing.T) {
	w := &WeWork{}
	if ctx := w.Context(); ctx == "" {
		t.Error("expected non-empty context string")
	}
}

func TestWeWorkToolsWithDirectStruct(t *testing.T) {
	t.Run("returns nil when BotID empty", func(t *testing.T) {
		w := &WeWork{cfg: WeWorkConfig{Secret: "secret"}}
		if tools := w.Tools(); tools != nil {
			t.Errorf("expected nil, got %d tools", len(tools))
		}
	})

	t.Run("returns nil when Secret empty", func(t *testing.T) {
		w := &WeWork{cfg: WeWorkConfig{BotID: "bot"}}
		if tools := w.Tools(); tools != nil {
			t.Errorf("expected nil, got %d tools", len(tools))
		}
	})
}

func TestWeWorkCapability(t *testing.T) {
	w := &WeWork{}
	c := w.Capability()
	if c.Interactive {
		t.Error("expected Interactive=false")
	}
	if c.Streamable {
		t.Error("expected Streamable=false")
	}
	if c.NestRead {
		t.Error("expected NestRead=false")
	}
}

func TestWeWorkRequestPermission(t *testing.T) {
	w := &WeWork{}
	result, err := w.RequestPermission(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error")
	}
	if result != transport.PermissionDenied {
		t.Errorf("expected PermissionDenied, got %d", result)
	}
}

func TestWeWorkFlush(t *testing.T) {
	w := &WeWork{}
	if err := w.Flush(); err != nil {
		t.Errorf("Flush returned error: %v", err)
	}
}

func TestWeWorkUserID(t *testing.T) {
	w := &WeWork{}
	// Initially empty
	if id := w.UserID(); id != "" {
		t.Errorf("expected empty, got %q", id)
	}
	// Set via internal field
	w.stateMu.Lock()
	w.lastUserID = "user001"
	w.stateMu.Unlock()
	if id := w.UserID(); id != "user001" {
		t.Errorf("expected 'user001', got %q", id)
	}
}

func TestWeWorkUserNick(t *testing.T) {
	w := &WeWork{}
	if nick := w.UserNick(); nick != "" {
		t.Errorf("expected empty, got %q", nick)
	}
}

func TestWeWorkIsSenderAllowed(t *testing.T) {
	t.Run("empty whitelist denies all", func(t *testing.T) {
		w := &WeWork{allowUsers: nil}
		if w.isSenderAllowed("anyone") {
			t.Error("expected false")
		}
	})

	t.Run("exact match", func(t *testing.T) {
		w := &WeWork{allowUsers: []string{"alice", "bob"}}
		if !w.isSenderAllowed("alice") {
			t.Error("expected true")
		}
	})

	t.Run("non-matching", func(t *testing.T) {
		w := &WeWork{allowUsers: []string{"alice"}}
		if w.isSenderAllowed("mallory") {
			t.Error("expected false")
		}
	})

	t.Run("glob match", func(t *testing.T) {
		w := &WeWork{allowUsers: []string{"*@example.com"}}
		if !w.isSenderAllowed("user@example.com") {
			t.Error("expected true")
		}
		if w.isSenderAllowed("user@other.com") {
			t.Error("expected false")
		}
	})
}

func TestWeWorkStripAtMention(t *testing.T) {
	t.Run("strips leading @mention", func(t *testing.T) {
		if got := stripAtMention("@bot /cmd", "bot"); got != "/cmd" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("handles no leading @", func(t *testing.T) {
		if got := stripAtMention("/cmd", "bot"); got != "/cmd" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("returns original when only @mention and no trailing space", func(t *testing.T) {
		if got := stripAtMention("@bot", "bot"); got != "@bot" {
			t.Errorf("got %q", got)
		}
	})
}

func TestWeWorkNewWeWork(t *testing.T) {
	t.Run("parses single allow user", func(t *testing.T) {
		w := NewWeWork(WeWorkConfig{AllowUsers: "alice"}, zap.NewNop(), "bot")
		if len(w.allowUsers) != 1 || w.allowUsers[0] != "alice" {
			t.Errorf("allowUsers = %v", w.allowUsers)
		}
	})

	t.Run("parses multiple allow users", func(t *testing.T) {
		w := NewWeWork(WeWorkConfig{AllowUsers: "alice,bob"}, zap.NewNop(), "bot")
		if len(w.allowUsers) != 2 {
			t.Errorf("expected 2 users, got %v", w.allowUsers)
		}
	})

	t.Run("trims spaces in allow users", func(t *testing.T) {
		w := NewWeWork(WeWorkConfig{AllowUsers: " alice , bob "}, zap.NewNop(), "bot")
		if len(w.allowUsers) != 2 || w.allowUsers[0] != "alice" || w.allowUsers[1] != "bob" {
			t.Errorf("allowUsers = %v", w.allowUsers)
		}
	})

	t.Run("skips empty entries in allow users", func(t *testing.T) {
		w := NewWeWork(WeWorkConfig{AllowUsers: "alice,,bob"}, zap.NewNop(), "bot")
		if len(w.allowUsers) != 2 {
			t.Errorf("expected 2 users, got %v", w.allowUsers)
		}
	})

	t.Run("nil logger creates default", func(t *testing.T) {
		w := NewWeWork(WeWorkConfig{}, nil, "bot")
		if w.logger == nil {
			t.Error("logger should not be nil")
		}
	})
}

func TestWeWorkHandleMessageCallback(t *testing.T) {
	makeMsg := func(msgtype string, content string, chatType string, userID string) []byte {
		body := map[string]any{
			"headers": map[string]any{
				"req_id": "req-123",
			},
			"body": map[string]any{
				"msgid":    "msg-1",
				"chatid":   "chat-1",
				"chattype": chatType,
				"from": map[string]string{
					"userid": userID,
				},
				"msgtype": msgtype,
			},
		}
		if content != "" {
			body["body"].(map[string]any)["text"] = map[string]string{
				"content": content,
			}
		}
		data, _ := json.Marshal(body)
		return data
	}

	t.Run("non-text msgtype is ignored", func(t *testing.T) {
		w := &WeWork{
			allowUsers: []string{"user1"},
			msgChan:    make(chan string, 10),
		}
		w.handleMessageCallback(makeMsg("image", "content", "single", "user1"))
		select {
		case <-w.msgChan:
			t.Error("expected no message for image type")
		default:
		}
	})

	t.Run("nil text is ignored", func(t *testing.T) {
		w := &WeWork{
			allowUsers: []string{"user1"},
			msgChan:    make(chan string, 10),
		}
		data := makeMsg("text", "", "single", "user1")
		var raw map[string]any
		_ = json.Unmarshal(data, &raw)
		delete(raw["body"].(map[string]any), "text")
		data, _ = json.Marshal(raw)

		w.handleMessageCallback(data)
		select {
		case <-w.msgChan:
			t.Error("expected no message when Text is nil")
		default:
		}
	})

	t.Run("allowed user pushes to msgChan", func(t *testing.T) {
		w := &WeWork{
			allowUsers: []string{"user1"},
			msgChan:    make(chan string, 10),
		}
		w.handleMessageCallback(makeMsg("text", "hello world", "single", "user1"))
		select {
		case msg := <-w.msgChan:
			if msg != "hello world" {
				t.Errorf("expected 'hello world', got %q", msg)
			}
		default:
			t.Error("expected message in msgChan")
		}
	})

	t.Run("single chat sets chatID to userID", func(t *testing.T) {
		w := &WeWork{
			allowUsers: []string{"user1"},
			msgChan:    make(chan string, 10),
		}
		w.handleMessageCallback(makeMsg("text", "hi", "single", "user1"))
		w.stateMu.Lock()
		if w.lastChatID != "user1" {
			t.Errorf("expected chatID=user1, got %q", w.lastChatID)
		}
		if w.lastChatType != 1 {
			t.Errorf("expected chatType=1, got %d", w.lastChatType)
		}
		w.stateMu.Unlock()
	})

	t.Run("group chat preserves chatID", func(t *testing.T) {
		w := &WeWork{
			allowUsers: []string{"user1"},
			msgChan:    make(chan string, 10),
		}
		w.handleMessageCallback(makeMsg("text", "hi", "group", "user1"))
		w.stateMu.Lock()
		if w.lastChatID != "chat-1" {
			t.Errorf("expected chatID=chat-1, got %q", w.lastChatID)
		}
		if w.lastChatType != 2 {
			t.Errorf("expected chatType=2, got %d", w.lastChatType)
		}
		w.stateMu.Unlock()
	})

	t.Run("strips @mention from content", func(t *testing.T) {
		w := &WeWork{
			allowUsers: []string{"user1"},
			msgChan:    make(chan string, 10),
			agentName:  "bot",
		}
		w.handleMessageCallback(makeMsg("text", "@bot /cmd", "single", "user1"))
		select {
		case msg := <-w.msgChan:
			if msg != "/cmd" {
				t.Errorf("expected '/cmd', got %q", msg)
			}
		default:
			t.Error("expected message in msgChan")
		}
	})

	t.Run("trims whitespace from content", func(t *testing.T) {
		w := &WeWork{
			allowUsers: []string{"user1"},
			msgChan:    make(chan string, 10),
		}
		w.handleMessageCallback(makeMsg("text", "  hello  ", "single", "user1"))
		select {
		case msg := <-w.msgChan:
			if msg != "hello" {
				t.Errorf("expected 'hello', got %q", msg)
			}
		default:
			t.Error("expected message in msgChan")
		}
	})

	t.Run("full msgChan drops message without blocking", func(t *testing.T) {
		w := &WeWork{
			logger:     zap.NewNop(),
			allowUsers: []string{"user1"},
			msgChan:    make(chan string, 1),
		}
		w.handleMessageCallback(makeMsg("text", "first message", "single", "user1"))
		w.handleMessageCallback(makeMsg("text", "second message", "single", "user1"))
		if len(w.msgChan) != 1 {
			t.Errorf("expected 1 message in buffer, got %d", len(w.msgChan))
		}
		select {
		case msg := <-w.msgChan:
			if msg != "first message" {
				t.Errorf("expected 'first message', got %q", msg)
			}
		default:
			t.Error("expected one message in buffer")
		}
	})

	t.Run("disallowed user does not push to msgChan", func(t *testing.T) {
		w := &WeWork{
			logger:     zap.NewNop(),
			allowUsers: []string{"user1"},
			msgChan:    make(chan string, 10),
		}
		w.handleMessageCallback(makeMsg("text", "hello", "single", "mallory"))
		select {
		case <-w.msgChan:
			t.Error("expected no message for disallowed user")
		default:
		}
	})
}

func TestWeWorkHandleEventCallback(t *testing.T) {
	makeEvent := func(eventType string) []byte {
		data, _ := json.Marshal(map[string]any{
			"body": map[string]any{
				"event": map[string]string{
					"eventtype": eventType,
				},
			},
		})
		return data
	}

	t.Run("disconnected event does not panic", func(t *testing.T) {
		w := &WeWork{logger: zap.NewNop()}
		w.handleEventCallback(makeEvent("disconnected_event"))
	})

	t.Run("unknown event does not panic", func(t *testing.T) {
		w := &WeWork{logger: zap.NewNop()}
		w.handleEventCallback(makeEvent("some_event"))
	})

	t.Run("invalid json does not panic", func(t *testing.T) {
		w := &WeWork{logger: zap.NewNop()}
		w.handleEventCallback([]byte("invalid"))
	})
}

func TestWeWorkTryDeliverPending(t *testing.T) {
	makeResp := func(reqID string) []byte {
		data, _ := json.Marshal(map[string]any{
			"headers": map[string]string{
				"req_id": reqID,
			},
		})
		return data
	}

	t.Run("delivers to matching pending channel", func(t *testing.T) {
		w := &WeWork{
			pendingResp: make(map[string]chan []byte),
		}
		ch := make(chan []byte, 1)
		w.pendingResp["req-1"] = ch

		result := w.tryDeliverPending(makeResp("req-1"))
		if !result {
			t.Error("expected true")
		}
		select {
		case <-ch:
		default:
			t.Error("expected message on channel")
		}
	})

	t.Run("non-matching req_id returns false", func(t *testing.T) {
		w := &WeWork{
			pendingResp: make(map[string]chan []byte),
		}
		w.pendingResp["req-1"] = make(chan []byte, 1)

		if w.tryDeliverPending(makeResp("req-2")) {
			t.Error("expected false")
		}
	})

	t.Run("invalid JSON returns false", func(t *testing.T) {
		w := &WeWork{}
		if w.tryDeliverPending([]byte("not-json")) {
			t.Error("expected false")
		}
	})

	t.Run("empty req_id returns false", func(t *testing.T) {
		data, _ := json.Marshal(map[string]any{
			"headers": map[string]string{"req_id": ""},
		})
		w := &WeWork{}
		if w.tryDeliverPending(data) {
			t.Error("expected false")
		}
	})
}

func TestWeWorkWrite(t *testing.T) {
	t.Run("no chat session returns error", func(t *testing.T) {
		w := &WeWork{}
		err := w.Write(context.Background(), "hello")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "no chat session") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("not connected returns error", func(t *testing.T) {
		w := &WeWork{lastChatID: "chat-1", lastChatType: 1}
		err := w.Write(context.Background(), "hello")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "not connected") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestWeWorkClose(t *testing.T) {
	t.Run("close with nil conn succeeds", func(t *testing.T) {
		w := &WeWork{closeCh: make(chan struct{})}
		err := w.Close()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("close twice returns nil", func(t *testing.T) {
		w := &WeWork{closeCh: make(chan struct{})}
		_ = w.Close()
		err := w.Close()
		if err != nil {
			t.Errorf("second close should return nil, got: %v", err)
		}
	})

	t.Run("close with non-nil conn closes connection", func(t *testing.T) {
		mock := &mockWSConn{}
		w := &WeWork{
			closeCh: make(chan struct{}),
			conn:    mock,
		}
		err := w.Close()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !mock.closed {
			t.Error("expected WebSocket conn to be closed")
		}
	})
}

func TestWeWorkRejectMessage(t *testing.T) {
	t.Run("empty req_id is noop", func(t *testing.T) {
		w := &WeWork{}
		w.rejectMessage("", "user1")
	})

	t.Run("connected sends rejection message", func(t *testing.T) {
		mock := &mockWSConn{}
		w := &WeWork{
			logger:     zap.NewNop(),
			conn:       mock,
			allowUsers: []string{"allowed_user"},
		}
		w.rejectMessage("req-reject-1", "unauthorized_user")
		_, data, ok := mock.lastWrite()
		if !ok {
			t.Fatal("expected write")
		}
		var msg struct {
			Cmd     string `json:"cmd"`
			Headers struct {
				ReqID string `json:"req_id"`
			} `json:"headers"`
			Body struct {
				MsgType  string `json:"msgtype"`
				Markdown struct {
					Content string `json:"content"`
				} `json:"markdown"`
			} `json:"body"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if msg.Cmd != "aibot_respond_msg" {
			t.Errorf("cmd = %q", msg.Cmd)
		}
		if msg.Headers.ReqID != "req-reject-1" {
			t.Errorf("req_id = %q", msg.Headers.ReqID)
		}
		if msg.Body.MsgType != "markdown" {
			t.Errorf("msgtype = %q", msg.Body.MsgType)
		}
		if !strings.Contains(msg.Body.Markdown.Content, "unauthorized_user") {
			t.Errorf("content = %q", msg.Body.Markdown.Content)
		}
	})
}

func TestWeWorkStripImageMarkdown(t *testing.T) {
	t.Run("removes image markdown", func(t *testing.T) {
		input := "Hello ![alt](http://example.com/img.png) world"
		expected := "Hello world"
		if got := stripImageMarkdown(input); got != expected {
			t.Errorf("got %q, want %q", got, expected)
		}
	})

	t.Run("removes image markdown with https", func(t *testing.T) {
		input := "![alt](https://example.com/img.png)"
		if got := stripImageMarkdown(input); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("removes relative image markdown", func(t *testing.T) {
		input := "![alt](image.png)"
		if got := stripImageMarkdown(input); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("preserves regular text", func(t *testing.T) {
		input := "Hello [link](http://example.com) world"
		expected := "Hello [link](http://example.com) world"
		if got := stripImageMarkdown(input); got != expected {
			t.Errorf("got %q, want %q", got, expected)
		}
	})

	t.Run("handles string without images", func(t *testing.T) {
		input := "just plain text"
		if got := stripImageMarkdown(input); got != input {
			t.Errorf("got %q, want %q", got, input)
		}
	})

	t.Run("handles empty string", func(t *testing.T) {
		if got := stripImageMarkdown(""); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

func TestWeWorkSendPing(t *testing.T) {
	t.Run("not connected is noop", func(t *testing.T) {
		w := &WeWork{logger: zap.NewNop()}
		w.sendPing()
	})

	t.Run("connected sends ping message", func(t *testing.T) {
		mock := &mockWSConn{}
		w := &WeWork{
			logger: zap.NewNop(),
			conn:   mock,
		}
		w.sendPing()
		_, data, ok := mock.lastWrite()
		if !ok {
			t.Fatal("expected write")
		}
		var p struct {
			Cmd     string            `json:"cmd"`
			Headers map[string]string `json:"headers"`
		}
		if err := json.Unmarshal(data, &p); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if p.Cmd != "ping" {
			t.Errorf("cmd = %q", p.Cmd)
		}
		if !strings.HasPrefix(p.Headers["req_id"], "ping_") {
			t.Errorf("req_id = %q", p.Headers["req_id"])
		}
	})
}

func TestWeWorkSendAndWait(t *testing.T) {
	t.Run("missing req_id returns error", func(t *testing.T) {
		w := &WeWork{logger: zap.NewNop()}
		data, _ := json.Marshal(map[string]any{
			"headers": map[string]string{"req_id": ""},
		})
		_, err := w.sendAndWait(data, time.Second)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "missing req_id") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		w := &WeWork{logger: zap.NewNop()}
		_, err := w.sendAndWait([]byte("not-json"), time.Second)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "missing req_id") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("not connected returns error", func(t *testing.T) {
		w := &WeWork{logger: zap.NewNop(), pendingResp: make(map[string]chan []byte)}
		data, _ := json.Marshal(map[string]any{
			"headers": map[string]string{"req_id": "test-req"},
		})
		_, err := w.sendAndWait(data, time.Second)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "not connected") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("write error returns error", func(t *testing.T) {
		mock := &mockWSConn{
			writeMsg: func(int, []byte) error {
				return fmt.Errorf("write failed")
			},
		}
		w := &WeWork{
			logger:      zap.NewNop(),
			conn:        mock,
			pendingResp: make(map[string]chan []byte),
		}
		data, _ := json.Marshal(map[string]any{
			"headers": map[string]string{"req_id": "test-req"},
		})
		_, err := w.sendAndWait(data, time.Second)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "write failed") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("success returns matching response", func(t *testing.T) {
		mock := &mockWSConn{}
		w := &WeWork{
			logger:      zap.NewNop(),
			conn:        mock,
			pendingResp: make(map[string]chan []byte),
		}
		reqID := "req-sendwait-ok"
		data, _ := json.Marshal(map[string]any{
			"headers": map[string]string{"req_id": reqID},
		})
		respData, _ := json.Marshal(map[string]any{
			"headers": map[string]string{"req_id": reqID},
			"body":    map[string]string{"result": "ok"},
		})

		go func() {
			time.Sleep(5 * time.Millisecond)
			w.pendingMu.Lock()
			ch := w.pendingResp[reqID]
			w.pendingMu.Unlock()
			if ch != nil {
				ch <- respData
			}
		}()

		result, err := w.sendAndWait(data, time.Second)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var resp struct {
			Body struct {
				Result string `json:"result"`
			} `json:"body"`
		}
		if err := json.Unmarshal(result, &resp); err != nil {
			t.Fatalf("decode result: %v", err)
		}
		if resp.Body.Result != "ok" {
			t.Errorf("result = %q", resp.Body.Result)
		}
	})

	t.Run("timeout returns error", func(t *testing.T) {
		mock := &mockWSConn{}
		w := &WeWork{
			logger:      zap.NewNop(),
			conn:        mock,
			pendingResp: make(map[string]chan []byte),
		}
		data, _ := json.Marshal(map[string]any{
			"headers": map[string]string{"req_id": "req-timeout"},
		})
		_, err := w.sendAndWait(data, time.Millisecond)
		if err == nil {
			t.Fatal("expected timeout error")
		}
		if !strings.Contains(err.Error(), "timeout") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestWeWorkSendImageMessage(t *testing.T) {
	t.Run("no callback context returns error", func(t *testing.T) {
		w := &WeWork{logger: zap.NewNop()}
		err := w.sendImageMessage(context.Background(), "media-123")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "no callback context") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("not connected returns error", func(t *testing.T) {
		w := &WeWork{
			logger:    zap.NewNop(),
			lastReqID: "req-img-1",
		}
		err := w.sendImageMessage(context.Background(), "media-123")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "not connected") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("success sends image respond_msg", func(t *testing.T) {
		mock := &mockWSConn{}
		w := &WeWork{
			logger:    zap.NewNop(),
			conn:      mock,
			lastReqID: "req-img-ok",
		}
		err := w.sendImageMessage(context.Background(), "media-123")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		_, data, ok := mock.lastWrite()
		if !ok {
			t.Fatal("expected write")
		}
		var msg struct {
			Cmd     string            `json:"cmd"`
			Headers map[string]string `json:"headers"`
			Body    map[string]any    `json:"body"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if msg.Cmd != "aibot_respond_msg" {
			t.Errorf("cmd = %q", msg.Cmd)
		}
		if msg.Headers["req_id"] != "req-img-ok" {
			t.Errorf("req_id = %q", msg.Headers["req_id"])
		}
		if msg.Body["msgtype"] != "image" {
			t.Errorf("msgtype = %v", msg.Body["msgtype"])
		}
		if w.lastReqID != "" {
			t.Error("lastReqID should be cleared")
		}
	})
}

func TestWeWorkSendFileMessage(t *testing.T) {
	t.Run("no chat session returns error", func(t *testing.T) {
		w := &WeWork{logger: zap.NewNop()}
		err := w.sendFileMessage(context.Background(), "media-123")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "no chat session") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("not connected returns error", func(t *testing.T) {
		w := &WeWork{
			logger:       zap.NewNop(),
			lastChatID:   "chat-1",
			lastChatType: 2,
		}
		err := w.sendFileMessage(context.Background(), "media-123")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "not connected") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("success sends file send_msg", func(t *testing.T) {
		mock := &mockWSConn{}
		w := &WeWork{
			logger:       zap.NewNop(),
			conn:         mock,
			lastChatID:   "chat-1",
			lastChatType: 2,
		}
		err := w.sendFileMessage(context.Background(), "media-123")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		_, data, ok := mock.lastWrite()
		if !ok {
			t.Fatal("expected write")
		}
		var msg struct {
			Cmd     string            `json:"cmd"`
			Headers map[string]string `json:"headers"`
			Body    map[string]any    `json:"body"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if msg.Cmd != "aibot_send_msg" {
			t.Errorf("cmd = %q", msg.Cmd)
		}
		if msg.Body["chatid"] != "chat-1" {
			t.Errorf("chatid = %v", msg.Body["chatid"])
		}
		if msg.Body["chat_type"] != float64(2) {
			t.Errorf("chat_type = %v", msg.Body["chat_type"])
		}
	})
}

func TestWeWorkSendMediaMessage(t *testing.T) {
	t.Run("image type calls sendImageMessage", func(t *testing.T) {
		mock := &mockWSConn{}
		w := &WeWork{
			logger:    zap.NewNop(),
			conn:      mock,
			lastReqID: "req-media-img",
		}
		err := w.SendMediaMessage(context.Background(), "media-123", "image")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		_, data, ok := mock.lastWrite()
		if !ok {
			t.Fatal("expected write")
		}
		var msg struct {
			Cmd string `json:"cmd"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if msg.Cmd != "aibot_respond_msg" {
			t.Errorf("cmd = %q, want aibot_respond_msg for image", msg.Cmd)
		}
	})

	t.Run("file type calls sendFileMessage", func(t *testing.T) {
		mock := &mockWSConn{}
		w := &WeWork{
			logger:       zap.NewNop(),
			conn:         mock,
			lastChatID:   "chat-1",
			lastChatType: 2,
		}
		err := w.SendMediaMessage(context.Background(), "media-456", "file")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		_, data, ok := mock.lastWrite()
		if !ok {
			t.Fatal("expected write")
		}
		var msg struct {
			Cmd string `json:"cmd"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if msg.Cmd != "aibot_send_msg" {
			t.Errorf("cmd = %q, want aibot_send_msg for file", msg.Cmd)
		}
	})
}

func TestWeWorkProactiveMessage(t *testing.T) {
	t.Run("no chat session returns error", func(t *testing.T) {
		w := &WeWork{logger: zap.NewNop()}
		err := w.ProactiveMessage(context.Background(), "hello", "text")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "no chat session") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("not connected returns error", func(t *testing.T) {
		w := &WeWork{
			logger:       zap.NewNop(),
			lastChatID:   "chat-1",
			lastChatType: 1,
		}
		err := w.ProactiveMessage(context.Background(), "hello", "text")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "not connected") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("sends text message", func(t *testing.T) {
		mock := &mockWSConn{}
		w := &WeWork{
			logger:       zap.NewNop(),
			conn:         mock,
			lastChatID:   "chat-1",
			lastChatType: 1,
		}
		err := w.ProactiveMessage(context.Background(), "hello", "text")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		_, data, ok := mock.lastWrite()
		if !ok {
			t.Fatal("expected write")
		}
		var msg struct {
			Cmd  string         `json:"cmd"`
			Body map[string]any `json:"body"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if msg.Cmd != "aibot_send_msg" {
			t.Errorf("cmd = %q", msg.Cmd)
		}
		if msg.Body["msgtype"] != "text" {
			t.Errorf("msgtype = %v", msg.Body["msgtype"])
		}
	})

	t.Run("sends markdown message with image stripping", func(t *testing.T) {
		mock := &mockWSConn{}
		w := &WeWork{
			logger:       zap.NewNop(),
			conn:         mock,
			lastChatID:   "chat-1",
			lastChatType: 1,
		}
		err := w.ProactiveMessage(context.Background(), "hello ![img](http://x.com/a.png)", "markdown")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		_, data, ok := mock.lastWrite()
		if !ok {
			t.Fatal("expected write")
		}
		var msg struct {
			Cmd  string         `json:"cmd"`
			Body map[string]any `json:"body"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if msg.Cmd != "aibot_send_msg" {
			t.Errorf("cmd = %q", msg.Cmd)
		}
		if msg.Body["msgtype"] != "markdown" {
			t.Errorf("msgtype = %v", msg.Body["msgtype"])
		}
		content := msg.Body["markdown"].(map[string]any)["content"].(string)
		if strings.Contains(content, "![img]") {
			t.Error("image markdown should be stripped")
		}
	})
}

func TestWeWorkWriteConnected(t *testing.T) {
	t.Run("connected with reqID uses aibot_respond_msg", func(t *testing.T) {
		mock := &mockWSConn{}
		w := &WeWork{
			logger:       zap.NewNop(),
			conn:         mock,
			lastChatID:   "chat-1",
			lastChatType: 1,
			lastReqID:    "req-write-1",
		}
		err := w.Write(context.Background(), "reply text")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		_, data, ok := mock.lastWrite()
		if !ok {
			t.Fatal("expected write")
		}
		var msg struct {
			Cmd     string            `json:"cmd"`
			Headers map[string]string `json:"headers"`
			Body    map[string]any    `json:"body"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if msg.Cmd != "aibot_respond_msg" {
			t.Errorf("cmd = %q, want aibot_respond_msg", msg.Cmd)
		}
		if msg.Headers["req_id"] != "req-write-1" {
			t.Errorf("req_id = %q", msg.Headers["req_id"])
		}
		if _, ok := msg.Body["chatid"]; ok {
			t.Error("should not have chatid for respond_msg")
		}
	})

	t.Run("connected without reqID uses aibot_send_msg", func(t *testing.T) {
		mock := &mockWSConn{}
		w := &WeWork{
			logger:       zap.NewNop(),
			conn:         mock,
			lastChatID:   "chat-1",
			lastChatType: 2,
		}
		err := w.Write(context.Background(), "proactive text")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		_, data, ok := mock.lastWrite()
		if !ok {
			t.Fatal("expected write")
		}
		var msg struct {
			Cmd  string         `json:"cmd"`
			Body map[string]any `json:"body"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if msg.Cmd != "aibot_send_msg" {
			t.Errorf("cmd = %q, want aibot_send_msg", msg.Cmd)
		}
		if msg.Body["chatid"] != "chat-1" {
			t.Errorf("chatid = %v", msg.Body["chatid"])
		}
		if msg.Body["chat_type"] != float64(2) {
			t.Errorf("chat_type = %v", msg.Body["chat_type"])
		}
	})

	t.Run("connected strips image markdown before sending", func(t *testing.T) {
		mock := &mockWSConn{}
		w := &WeWork{
			logger:       zap.NewNop(),
			conn:         mock,
			lastChatID:   "chat-1",
			lastChatType: 1,
			lastReqID:    "req-imgstrip",
		}
		err := w.Write(context.Background(), "before ![img](http://x.com/a.png) after")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		_, data, ok := mock.lastWrite()
		if !ok {
			t.Fatal("expected write")
		}
		var msg struct {
			Body struct {
				Markdown struct {
					Content string `json:"content"`
				} `json:"markdown"`
			} `json:"body"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if msg.Body.Markdown.Content != "before after" {
			t.Errorf("content = %q", msg.Body.Markdown.Content)
		}
	})
}

func TestWeWorkRead(t *testing.T) {
	t.Run("returns message from msgChan", func(t *testing.T) {
		w := &WeWork{
			msgChan: make(chan string, 1),
		}
		w.msgChan <- "test message"
		msg, err := w.Read(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if msg != "test message" {
			t.Errorf("msg = %q", msg)
		}
	})

	t.Run("closed returns closed error", func(t *testing.T) {
		w := &WeWork{
			msgChan: make(chan string),
			closeCh: make(chan struct{}),
		}
		close(w.closeCh)
		_, err := w.Read(context.Background())
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "closed") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("context cancelled returns context error", func(t *testing.T) {
		w := &WeWork{
			msgChan: make(chan string),
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := w.Read(ctx)
		if err == nil {
			t.Fatal("expected error")
		}
		if err != context.Canceled {
			t.Errorf("expected Canceled, got %v", err)
		}
	})
}
