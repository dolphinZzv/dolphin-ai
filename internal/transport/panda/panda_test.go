package panda

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dolphin/internal/transport"

	"github.com/gorilla/websocket"
)

func TestPanda_ID(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	if p.ID() != "panda" {
		t.Fatalf("expected 'panda', got '%s'", p.ID())
	}
}

func TestPanda_Context(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	if p.Context() == "" {
		t.Fatal("expected non-empty context")
	}
}

func TestPanda_Tools(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	tools := p.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "panda_mcp" {
		t.Fatalf("expected 'panda_mcp', got '%s'", tools[0].Name)
	}
	if tools[0].Executor == nil {
		t.Fatal("expected non-nil Executor")
	}
}

func TestPanda_Token(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	if p.Token() != "" {
		t.Fatal("expected empty token initially")
	}
	p.token = "test-token"
	if p.Token() != "test-token" {
		t.Fatalf("expected 'test-token', got '%s'", p.Token())
	}
}

func TestPanda_Capability(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	c := p.Capability()
	if c.Interactive {
		t.Fatal("expected Interactive=false")
	}
	if c.Streamable {
		t.Fatal("expected Streamable=false")
	}
	if c.NestRead {
		t.Fatal("expected NestRead=false")
	}
	if c.RenderTextMarkdown != "markdown" {
		t.Fatalf("expected 'markdown', got '%s'", c.RenderTextMarkdown)
	}
}

func TestPanda_Flush(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	if err := p.Flush(); err != nil {
		t.Fatal(err)
	}
}

func TestPanda_RequestPermission(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	result, err := p.RequestPermission(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error")
	}
	if result != transport.PermissionDenied {
		t.Fatalf("expected PermissionDenied, got %d", result)
	}
}

func TestPanda_Read(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	p.msgChan <- "hello"

	msg, err := p.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if msg != "hello" {
		t.Fatalf("expected 'hello', got '%s'", msg)
	}
}

func TestPanda_ReadContextCancelled(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.Read(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestPanda_Write_NoConv(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	err := p.Write(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error when no conv_id and no lastConvID")
	}
	if !strings.Contains(err.Error(), "no conv_id") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPanda_Write_UseCfgConv(t *testing.T) {
	p := NewPanda(PandaConfig{ConvID: "conv_abc"}, nil, "test")
	err := p.Write(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error (not connected)")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPanda_Write_UseLastConv(t *testing.T) {
	srv := newTestWSServer(t, func(msg []byte) []byte { return nil })
	defer srv.Close()

	p := NewPanda(PandaConfig{Server: srv.URL, Account: "test", Password: "test"}, nil, "test")

	p.mu.Lock()
	p.lastConvID = "conv_from_push"
	p.mu.Unlock()

	err := p.Write(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error (not connected)")
	}
}

func TestPanda_writeFrame_NotConnected(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	err := p.writeFrame(frame{Type: msgTypePing})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPanda_Close(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	// Double close should be safe
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPanda_CloseStopsReconnect(t *testing.T) {
	p := NewPanda(PandaConfig{Server: "http://127.0.0.1:1", Account: "test", Password: "test"}, nil, "test")
	p.token = "fake"
	p.userID = "test"

	p.wg.Add(1)
	go p.run()

	time.Sleep(50 * time.Millisecond)
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPanda_NewPanda_NilLogger(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	if p.logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestPanda_NewPanda_AllowUsers(t *testing.T) {
	p := NewPanda(PandaConfig{AllowUsers: "u1,u2,*"}, nil, "test")
	if len(p.allowUsers) != 3 {
		t.Fatalf("expected 3 allowUsers, got %d", len(p.allowUsers))
	}
	if p.allowUsers[0] != "u1" {
		t.Fatalf("expected 'u1', got '%s'", p.allowUsers[0])
	}
	if p.allowUsers[2] != "*" {
		t.Fatalf("expected '*', got '%s'", p.allowUsers[2])
	}
}

func TestPanda_NewPanda_AllowUsersEmpty(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	if p.allowUsers != nil {
		t.Fatalf("expected nil allowUsers")
	}
}

// --- isSenderAllowed ---

func TestPanda_IsSenderAllowed_EmptyList(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	if p.isSenderAllowed("anyone") {
		t.Fatal("expected false for empty allow list")
	}
}

func TestPanda_IsSenderAllowed_Match(t *testing.T) {
	p := NewPanda(PandaConfig{AllowUsers: "user*"}, nil, "test")
	if !p.isSenderAllowed("user123") {
		t.Fatal("expected match")
	}
}

func TestPanda_IsSenderAllowed_NoMatch(t *testing.T) {
	p := NewPanda(PandaConfig{AllowUsers: "alice"}, nil, "test")
	if p.isSenderAllowed("bob") {
		t.Fatal("expected no match")
	}
}

func TestPanda_IsSenderAllowed_Wildcard(t *testing.T) {
	p := NewPanda(PandaConfig{AllowUsers: "*"}, nil, "test")
	if !p.isSenderAllowed("anyone") {
		t.Fatal("expected match for wildcard")
	}
}

// --- handleMsgPush ---

func TestPanda_HandleMsgPush_OwnMessage(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	p.userID = "self"

	push := msgPushPayload{SenderID: "self", ConvID: "conv1", ContentType: 0, Body: "hello"}
	data, _ := json.Marshal(push)

	err := p.handleMsgPush(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.msgChan) != 0 {
		t.Fatal("expected own message to be skipped")
	}
}

func TestPanda_HandleMsgPush_ConvFilter(t *testing.T) {
	p := NewPanda(PandaConfig{ConvID: "conv_target"}, nil, "test")
	p.userID = "bot"
	p.allowUsers = []string{"*"}

	push := msgPushPayload{SenderID: "other", ConvID: "conv_wrong", ContentType: 0, Body: "hello"}
	data, _ := json.Marshal(push)

	err := p.handleMsgPush(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.msgChan) != 0 {
		t.Fatal("expected message from wrong conv to be filtered")
	}
}

func TestPanda_HandleMsgPush_ConvFilterMatch(t *testing.T) {
	p := NewPanda(PandaConfig{ConvID: "conv_target"}, nil, "test")
	p.userID = "bot"
	p.allowUsers = []string{"*"}

	push := msgPushPayload{SenderID: "other", ConvID: "conv_target", ContentType: 0, Body: "hello"}
	data, _ := json.Marshal(push)

	err := p.handleMsgPush(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.msgChan) != 1 {
		t.Fatal("expected message to pass filter")
	}
}

func TestPanda_HandleMsgPush_SenderNotAllowed(t *testing.T) {
	p := NewPanda(PandaConfig{AllowUsers: "alice"}, nil, "test")
	p.userID = "bot"

	push := msgPushPayload{SenderID: "bob", ConvID: "conv1", ContentType: 0, Body: "hello"}
	data, _ := json.Marshal(push)

	err := p.handleMsgPush(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.msgChan) != 0 {
		t.Fatal("expected unallowed sender to be filtered")
	}
}

func TestPanda_HandleMsgPush_NonText(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	p.allowUsers = []string{"*"}

	// ContentType 1 = image should be filtered
	push := msgPushPayload{SenderID: "other", ConvID: "conv1", ContentType: 1, Body: "image data"}
	data, _ := json.Marshal(push)
	err := p.handleMsgPush(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.msgChan) != 0 {
		t.Fatal("expected image message to be filtered")
	}

	// ContentType 0 = text should pass
	push = msgPushPayload{SenderID: "other", ConvID: "conv1", ContentType: 0, Body: "text"}
	data, _ = json.Marshal(push)
	if err := p.handleMsgPush(data); err != nil {
		t.Fatal(err)
	}
	if len(p.msgChan) != 1 {
		t.Fatal("expected text message to pass")
	}
}

func TestPanda_HandleMsgPush_Success(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	p.userID = "bot"
	p.allowUsers = []string{"*"}

	push := msgPushPayload{SenderID: "other", ConvID: "conv1", ContentType: 0, Body: "hello world"}
	data, _ := json.Marshal(push)

	err := p.handleMsgPush(data)
	if err != nil {
		t.Fatal(err)
	}

	msg := <-p.msgChan
	if msg != "hello world" {
		t.Fatalf("expected 'hello world', got '%s'", msg)
	}

	p.mu.Lock()
	if p.lastConvID != "conv1" {
		t.Fatalf("expected lastConvID='conv1', got '%s'", p.lastConvID)
	}
	if p.lastSenderID != "other" {
		t.Fatalf("expected lastSenderID='other', got '%s'", p.lastSenderID)
	}
	p.mu.Unlock()
}

func TestPanda_HandleMsgPush_InvalidJSON(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	err := p.handleMsgPush(json.RawMessage(`{invalid}`))
	if err == nil {
		t.Fatal("expected error from invalid JSON")
	}
}

func TestPanda_HandleMsgPush_MsgChanFull(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	p.allowUsers = []string{"*"}
	// Fill the channel
	for i := 0; i < cap(p.msgChan); i++ {
		p.msgChan <- fmt.Sprintf("msg-%d", i)
	}

	push := msgPushPayload{SenderID: "other", ConvID: "conv1", ContentType: 0, Body: "dropped"}
	data, _ := json.Marshal(push)

	err := p.handleMsgPush(data)
	if err != nil {
		t.Fatal(err)
	}
	// Channel should still be full, message dropped without error
	if len(p.msgChan) != cap(p.msgChan) {
		t.Fatalf("expected channel full (%d), got %d", cap(p.msgChan), len(p.msgChan))
	}
}

// --- handleFrame ---

func TestPanda_HandleFrame_Ping(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	err := p.handleFrame(frame{Type: msgTypePing})
	if err == nil {
		t.Fatal("expected error (not connected, can't write pong)")
	}
}

func TestPanda_HandleFrame_Pong(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	err := p.handleFrame(frame{Type: msgTypePong})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPanda_HandleFrame_SessionRecAck(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	err := p.handleFrame(frame{Type: msgTypeSessionRecAck})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPanda_HandleFrame_Error(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	errPayload, _ := json.Marshal(errorPayload{Code: 4001, Message: "kicked"})
	err := p.handleFrame(frame{Type: msgTypeError, Payload: errPayload})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPanda_HandleFrame_UnknownType(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	err := p.handleFrame(frame{Type: msgTypeSendAck})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPanda_HandleFrame_MsgPush(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	p.userID = "bot"
	p.allowUsers = []string{"*"}

	push := msgPushPayload{SenderID: "other", ConvID: "conv1", ContentType: 0, Body: "from frame"}
	data, _ := json.Marshal(push)

	err := p.handleFrame(frame{Type: msgTypePush, Payload: data})
	if err != nil {
		t.Fatal(err)
	}

	msg := <-p.msgChan
	if msg != "from frame" {
		t.Fatalf("expected 'from frame', got '%s'", msg)
	}
}

// --- makeWSURL ---

func TestPanda_MakeWSURL(t *testing.T) {
	p := NewPanda(PandaConfig{Server: "http://localhost:8080"}, nil, "test")
	p.token = "tok123"
	url := p.makeWSURL()
	expected := "ws://localhost:8080/ws?token=tok123"
	if url != expected {
		t.Fatalf("expected '%s', got '%s'", expected, url)
	}
}

func TestPanda_MakeWSURL_HTTPS(t *testing.T) {
	p := NewPanda(PandaConfig{Server: "https://example.com"}, nil, "test")
	p.token = "tok456"
	url := p.makeWSURL()
	expected := "wss://example.com/ws?token=tok456"
	if url != expected {
		t.Fatalf("expected '%s', got '%s'", expected, url)
	}
}

func TestPanda_MakeWSURL_TrailingSlash(t *testing.T) {
	p := NewPanda(PandaConfig{Server: "http://localhost:8080/"}, nil, "test")
	p.token = "tok"
	url := p.makeWSURL()
	expected := "ws://localhost:8080/ws?token=tok"
	if url != expected {
		t.Fatalf("expected '%s', got '%s'", expected, url)
	}
}

// --- Builtin Registration ---

func TestPanda_BuiltinRegistration(t *testing.T) {
	io, err := transport.Build(context.Background(), "panda", map[string]any{
		"server":   "http://127.0.0.1:8080",
		"account":  "test",
		"password": "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if io.ID() != "panda" {
		t.Fatalf("expected 'panda', got '%s'", io.ID())
	}
}

// --- isClosed ---

func TestPanda_IsClosed(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	if p.isClosed() {
		t.Fatal("expected not closed initially")
	}
	p.Close()
	if !p.isClosed() {
		t.Fatal("expected closed after Close()")
	}
}

// --- login (with mock HTTP server) ---

func TestPanda_Login_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users/login" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":0,"msg":"ok","data":{"user_id":"uid1","account":"test","name":"Test","token":"jwt123","refresh_token":"rtok","expires_at":9999999999}}`))
	}))
	defer srv.Close()

	p := NewPanda(PandaConfig{Server: srv.URL, Account: "test", Password: "test"}, nil, "test")
	err := p.login(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if p.token != "jwt123" {
		t.Fatalf("expected 'jwt123', got '%s'", p.token)
	}
	if p.userID != "uid1" {
		t.Fatalf("expected 'uid1', got '%s'", p.userID)
	}
}

func TestPanda_Login_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"code":401,"msg":"unauthorized","data":null}`))
	}))
	defer srv.Close()

	p := NewPanda(PandaConfig{Server: srv.URL, Account: "bad", Password: "bad"}, nil, "test")
	err := p.login(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "login failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPanda_Login_EmptyToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":0,"msg":"ok","data":{"user_id":"uid1","token":""}}`))
	}))
	defer srv.Close()

	p := NewPanda(PandaConfig{Server: srv.URL, Account: "test", Password: "test"}, nil, "test")
	err := p.login(context.Background())
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestPanda_Login_NonZeroCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":400,"msg":"bad request","data":null}`))
	}))
	defer srv.Close()

	p := NewPanda(PandaConfig{Server: srv.URL, Account: "test", Password: "test"}, nil, "test")
	err := p.login(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "login rejected") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPanda_Login_NetworkError(t *testing.T) {
	p := NewPanda(PandaConfig{Server: "http://127.0.0.1:1", Account: "test", Password: "test"}, nil, "test")
	err := p.login(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- Write with real WS server ---

func TestPanda_Write_Connected(t *testing.T) {
	got := make(chan frame, 1)
	srv := newTestWSServer(t, func(msg []byte) []byte {
		var f frame
		if err := json.Unmarshal(msg, &f); err == nil && f.Type == msgTypeSend {
			got <- f
		}
		return nil
	})
	defer srv.Close()

	p := NewPanda(PandaConfig{
		Server:   "http://" + srv.Listener.Addr().String(),
		ConvID:   "conv_42",
		Account:  "test",
		Password: "test",
	}, nil, "test")

	u := "ws://" + srv.Listener.Addr().String() + "/ws?token=fake"
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	p.connMu.Lock()
	p.conn = conn
	p.connMu.Unlock()

	if err := p.Write(context.Background(), "test message"); err != nil {
		t.Fatal(err)
	}

	select {
	case f := <-got:
		if f.Type != msgTypeSend {
			t.Fatalf("expected msgTypeSend(1), got %d", f.Type)
		}
		if f.ID == "" {
			t.Fatal("expected non-empty frame ID")
		}
		var payload msgSendPayload
		if err := json.Unmarshal(f.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.ConvID != "conv_42" {
			t.Fatalf("expected 'conv_42', got '%s'", payload.ConvID)
		}
		if payload.Body != "test message" {
			t.Fatalf("expected 'test message', got '%s'", payload.Body)
		}
		if payload.ContentType != 0 {
			t.Fatalf("expected ContentType=0, got %d", payload.ContentType)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for server to receive frame")
	}
}

// --- connect/reconnect graceful stop ---

func TestPanda_Connect_ClosedWhileDialing(t *testing.T) {
	p := NewPanda(PandaConfig{Server: "http://127.0.0.1:1"}, nil, "test")
	p.token = "fake"
	p.userID = "test"

	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()

	if p.connect() {
		t.Fatal("expected connect to return false when closed")
	}
}

// Test that run() exits when closeCh is signaled
func TestPanda_Run_ExitsOnClose(t *testing.T) {
	p := NewPanda(PandaConfig{Server: "http://127.0.0.1:1"}, nil, "test")
	p.token = "fake"
	p.userID = "test"

	done := make(chan struct{})
	p.wg.Add(1)
	go func() {
		p.run()
		close(done)
	}()

	// Let it try once
	time.Sleep(50 * time.Millisecond)
	p.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("run() did not exit after Close()")
	}
}

// --- Start ---

func TestPanda_Start_LoginFailed(t *testing.T) {
	p := NewPanda(PandaConfig{Server: "http://127.0.0.1:1", Account: "test", Password: "test"}, nil, "test")
	err := p.Start(context.Background())
	if err == nil {
		t.Fatal("expected error from login failure")
	}
}

// --- connect ---

func TestPanda_connect_Success(t *testing.T) {
	srv := newTestWSServer(t, func(msg []byte) []byte { return nil })
	defer srv.Close()

	p := NewPanda(PandaConfig{Server: "http://" + srv.Listener.Addr().String(), Account: "test", Password: "test"}, nil, "test")
	p.token = "fake"
	p.userID = "test"

	if !p.connect() {
		t.Fatal("expected connect to succeed")
	}

	p.connMu.Lock()
	conn := p.conn
	p.connMu.Unlock()

	if conn == nil {
		t.Fatal("expected non-nil connection after connect")
	}
	conn.Close()
	p.connMu.Lock()
	p.conn = nil
	p.connMu.Unlock()
}

// --- readLoop ---

func TestPanda_readLoop_ProcessesFrame(t *testing.T) {
	pushData, _ := json.Marshal(msgPushPayload{
		SenderID:    "user1",
		ConvID:      "conv1",
		ContentType: 0,
		Body:        "hello from readLoop",
	})
	frameData, _ := json.Marshal(frame{
		Type:    msgTypePush,
		ID:      "test-id",
		Payload: pushData,
	})

	upgrader := &websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		conn.WriteMessage(websocket.TextMessage, frameData)
		conn.ReadMessage() // block until client closes
	}))
	defer srv.Close()

	p := NewPanda(PandaConfig{}, nil, "test")
	p.token = "fake"
	p.userID = "bot"
	p.allowUsers = []string{"*"}

	u := "ws://" + srv.Listener.Addr().String() + "/ws?token=fake"
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatal(err)
	}

	p.connMu.Lock()
	p.conn = conn
	p.connMu.Unlock()

	done := make(chan struct{})
	go func() {
		p.readLoop()
		close(done)
	}()

	select {
	case msg := <-p.msgChan:
		if msg != "hello from readLoop" {
			t.Fatalf("expected 'hello from readLoop', got '%s'", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message from readLoop")
	}

	p.connMu.Lock()
	if p.conn != nil {
		p.conn.Close()
	}
	p.connMu.Unlock()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("readLoop did not exit after close")
	}
}

func TestPanda_WriteContent_Image(t *testing.T) {
	got := make(chan frame, 1)
	srv := newTestWSServer(t, func(msg []byte) []byte {
		var f frame
		if err := json.Unmarshal(msg, &f); err == nil && f.Type == msgTypeSend {
			got <- f
		}
		return nil
	})
	defer srv.Close()

	p := NewPanda(PandaConfig{
		Server:   "http://" + srv.Listener.Addr().String(),
		ConvID:   "conv_42",
		Account:  "test",
		Password: "test",
	}, nil, "test")

	u := "ws://" + srv.Listener.Addr().String() + "/ws?token=fake"
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	p.connMu.Lock()
	p.conn = conn
	p.connMu.Unlock()

	if err := p.WriteContent(context.Background(), "http://example.com/img.png", 1); err != nil {
		t.Fatal(err)
	}

	select {
	case f := <-got:
		if f.Type != msgTypeSend {
			t.Fatalf("expected msgTypeSend(1), got %d", f.Type)
		}
		var payload msgSendPayload
		if err := json.Unmarshal(f.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.ContentType != 1 {
			t.Fatalf("expected ContentType=1, got %d", payload.ContentType)
		}
		if payload.Body != "http://example.com/img.png" {
			t.Fatalf("expected body with url, got '%s'", payload.Body)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for frame")
	}
}

func TestPanda_WriteContent_NoConv(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	err := p.WriteContent(context.Background(), "hello", 1)
	if err == nil {
		t.Fatal("expected error when no conv_id")
	}
	if !strings.Contains(err.Error(), "no conv_id") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- handleMsgPush historical message filtering ---

func TestPanda_HandleMsgPush_HistoricalMessage(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	p.userID = "bot"
	p.allowUsers = []string{"*"}
	p.connectedAt = 2000 // messages before this timestamp should be filtered

	push := msgPushPayload{SenderID: "other", ConvID: "conv1", ContentType: 0, Body: "old message", Timestamp: 1000}
	data, _ := json.Marshal(push)
	err := p.handleMsgPush(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.msgChan) != 0 {
		t.Fatal("expected historical message to be filtered")
	}

	// Current message should pass
	push = msgPushPayload{SenderID: "other", ConvID: "conv1", ContentType: 0, Body: "new message", Timestamp: 3000}
	data, _ = json.Marshal(push)
	if err := p.handleMsgPush(data); err != nil {
		t.Fatal(err)
	}
	if len(p.msgChan) != 1 {
		t.Fatal("expected current message to pass")
	}
	msg := <-p.msgChan
	if msg != "new message" {
		t.Fatalf("expected 'new message', got '%s'", msg)
	}
}

func TestPanda_HandleMsgPush_HistoricalZeroTimestamp(t *testing.T) {
	// When Timestamp is 0 (default), should not be filtered even with connectedAt set
	p := NewPanda(PandaConfig{}, nil, "test")
	p.userID = "bot"
	p.allowUsers = []string{"*"}
	p.connectedAt = 2000

	push := msgPushPayload{SenderID: "other", ConvID: "conv1", ContentType: 0, Body: "no timestamp", Timestamp: 0}
	data, _ := json.Marshal(push)
	err := p.handleMsgPush(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.msgChan) != 1 {
		t.Fatal("expected message with zero timestamp to pass")
	}
}

// --- helpers ---

// newTestWSServer creates a WebSocket test server that echoes or handles messages.
func newTestWSServer(t *testing.T, handler func([]byte) []byte) *httptest.Server {
	t.Helper()

	var upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			resp := handler(msg)
			if resp != nil {
				conn.WriteMessage(websocket.TextMessage, resp)
			}
		}
	}))

	return srv
}
