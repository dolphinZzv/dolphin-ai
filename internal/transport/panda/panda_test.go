package panda

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"dolphin/internal/transport"
	"dolphin/internal/types"
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
	if !p.isSenderAllowed("anyone") {
		t.Fatal("expected true for empty allow list (whitelist disabled, allow all)")
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

// --- handleMsgPush @mention ---

func TestPanda_HandleMsgPush_AtMention_Enabled_SkipsWithoutMention(t *testing.T) {
	p := NewPanda(PandaConfig{AtMention: true}, nil, "mybot")
	p.userID = "bot"
	// Mention field does not contain "mybot"
	push := msgPushPayload{SenderID: "other", ConvID: "conv1", ContentType: 0, Body: "hello world", Mention: []string{"otheruser"}}
	data, _ := json.Marshal(push)

	err := p.handleMsgPush(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.msgChan) != 0 {
		t.Fatal("expected message without agent in Mention to be filtered when at_mention enabled")
	}
}

func TestPanda_HandleMsgPush_AtMention_Enabled_AllowsWithMention(t *testing.T) {
	p := NewPanda(PandaConfig{AtMention: true}, nil, "mybot")
	p.userID = "bot"
	// Mention field contains "mybot"
	push := msgPushPayload{SenderID: "other", ConvID: "conv1", ContentType: 0, Body: "hello @mybot how are you", Mention: []string{"mybot"}}
	data, _ := json.Marshal(push)

	err := p.handleMsgPush(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.msgChan) != 1 {
		t.Fatal("expected message with agent in Mention to pass when at_mention enabled")
	}
}

func TestPanda_HandleMsgPush_AtMention_Disabled_AllowsAll(t *testing.T) {
	p := NewPanda(PandaConfig{AtMention: false}, nil, "mybot")
	p.userID = "bot"
	// Mention is empty, but at_mention is disabled so should pass
	push := msgPushPayload{SenderID: "other", ConvID: "conv1", ContentType: 0, Body: "hello world"}
	data, _ := json.Marshal(push)

	err := p.handleMsgPush(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.msgChan) != 1 {
		t.Fatal("expected message to pass when at_mention disabled")
	}
}

func TestPanda_HandleMsgPush_AtMention_ExactMatch(t *testing.T) {
	// agentName "mybot" should not match "mybot2" in the Mention list
	p := NewPanda(PandaConfig{AtMention: true}, nil, "mybot")
	p.userID = "bot"
	// Mention contains "mybot2" but not "mybot"
	push := msgPushPayload{SenderID: "other", ConvID: "conv1", ContentType: 0, Body: "hello", Mention: []string{"mybot2", "supermybot"}}
	data, _ := json.Marshal(push)

	err := p.handleMsgPush(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.msgChan) != 0 {
		t.Fatal("expected message without exact agentName in Mention to be filtered")
	}
}

func TestPanda_HandleMsgPush_AtMention_MultipleMentions(t *testing.T) {
	// Multiple mentions including agentName
	p := NewPanda(PandaConfig{AtMention: true}, nil, "mybot")
	p.userID = "bot"
	push := msgPushPayload{SenderID: "other", ConvID: "conv1", ContentType: 0, Body: "hello @alice @mybot", Mention: []string{"alice", "mybot"}}
	data, _ := json.Marshal(push)

	err := p.handleMsgPush(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.msgChan) != 1 {
		t.Fatal("expected message with agentName among Mention list to pass")
	}
}

func TestPanda_HandleMsgPush_AtMention_AllowsByUserID(t *testing.T) {
	// Mention contains p.userID but not p.agentName — should still pass.
	p := NewPanda(PandaConfig{AtMention: true}, nil, "mybot")
	p.userID = "u_12345"
	// agentName "mybot" is not in Mention, but userID "u_12345" is
	push := msgPushPayload{SenderID: "other", ConvID: "conv1", ContentType: 0, Body: "hello", Mention: []string{"u_12345"}}
	data, _ := json.Marshal(push)

	err := p.handleMsgPush(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.msgChan) != 1 {
		t.Fatal("expected message mentioning p.userID to pass when at_mention enabled")
	}
}

func TestPanda_HandleMsgPush_AtMention_CombinedWithWhitelist(t *testing.T) {
	// Both @mention and whitelist enabled: both must pass
	p := NewPanda(PandaConfig{AtMention: true, AllowUsers: "alice,bob"}, nil, "mybot")
	p.userID = "bot"

	// Allowed sender with @mention
	push := msgPushPayload{SenderID: "alice", ConvID: "conv1", ContentType: 0, Body: "hey @mybot", Mention: []string{"mybot"}}
	data, _ := json.Marshal(push)
	err := p.handleMsgPush(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.msgChan) != 1 {
		t.Fatal("expected message from allowed sender with @mention to pass")
	}

	// Allowed sender without @mention — should be filtered
	push2 := msgPushPayload{SenderID: "alice", ConvID: "conv1", ContentType: 0, Body: "hey there"}
	data2, _ := json.Marshal(push2)
	err = p.handleMsgPush(data2)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.msgChan) != 1 {
		t.Fatal("expected message from allowed sender without @mention to be filtered")
	}

	// Not allowed sender with @mention — should be filtered
	push3 := msgPushPayload{SenderID: "eve", ConvID: "conv1", ContentType: 0, Body: "hey @mybot", Mention: []string{"mybot"}}
	data3, _ := json.Marshal(push3)
	err = p.handleMsgPush(data3)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.msgChan) != 1 {
		t.Fatal("expected message from unallowed sender (even with @mention) to be filtered")
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
	conn, resp, err := websocket.DefaultDialer.Dial(u, nil)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

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
		if payload.ContentType != 0 {
			t.Fatalf("expected ContentType=0 (plain text for command), got %d", payload.ContentType)
		}
		if payload.Body != "test message" {
			t.Fatalf("expected 'test message', got '%s'", payload.Body)
		}
	case <-time.After(10 * time.Second):
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
	case <-time.After(10 * time.Second):
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
	_ = conn.Close()
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
		defer func() { _ = conn.Close() }()

		conn.WriteMessage(websocket.TextMessage, frameData)
		conn.ReadMessage() // block until client closes
	}))
	defer srv.Close()

	p := NewPanda(PandaConfig{}, nil, "test")
	p.token = "fake"
	p.userID = "bot"
	p.allowUsers = []string{"*"}

	u := "ws://" + srv.Listener.Addr().String() + "/ws?token=fake"
	conn, resp, err := websocket.DefaultDialer.Dial(u, nil)
	if resp != nil {
		defer resp.Body.Close()
	}
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
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for message from readLoop")
	}

	p.connMu.Lock()
	if p.conn != nil {
		p.conn.Close()
	}
	p.connMu.Unlock()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
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
	conn, resp, err := websocket.DefaultDialer.Dial(u, nil)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

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
	case <-time.After(10 * time.Second):
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

// --- autoUploadImages ---

func TestAutoUploadImages_LocalPathReplaced(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "screenshot.png")
	if err := os.WriteFile(tmpFile, []byte("fake-png-data"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":0,"msg":"ok","data":{"url":"http://example.com/uploaded.png"}}`))
	}))
	defer srv.Close()

	p := NewPanda(PandaConfig{Server: srv.URL}, nil, "test")
	p.token = "test-token"

	result := p.autoUploadImages(context.Background(), tmpFile)
	if !strings.Contains(result, "![screenshot.png]") {
		t.Fatalf("expected markdown image link, got: %s", result)
	}
	if !strings.Contains(result, "http://example.com/uploaded.png") {
		t.Fatalf("expected uploaded URL in result, got: %s", result)
	}
}

func TestAutoUploadImages_NoPath(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	p.token = "test-token"

	result := p.autoUploadImages(context.Background(), "hello world")
	if result != "hello world" {
		t.Fatalf("expected unchanged text, got: %s", result)
	}
}

func TestAutoUploadImages_NoToken(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	// token is empty — autoUploadImages should NOT be called since Write() checks p.token != ""
	result := p.autoUploadImages(context.Background(), "/tmp/screenshot.png")
	if result != "/tmp/screenshot.png" {
		t.Fatalf("expected unchanged text when no token, got: %s", result)
	}
}

func TestAutoUploadImages_NonImagePath(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "document.pdf")
	if err := os.WriteFile(tmpFile, []byte("pdf-data"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := NewPanda(PandaConfig{}, nil, "test")
	p.token = "test-token"

	result := p.autoUploadImages(context.Background(), tmpFile)
	if !strings.Contains(result, "document.pdf") {
		t.Fatalf("expected non-image path to remain unchanged, got: %s", result)
	}
}

func TestAutoUploadImages_UploadFails(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "fail.png")
	if err := os.WriteFile(tmpFile, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := NewPanda(PandaConfig{Server: srv.URL}, nil, "test")
	p.token = "test-token"

	result := p.autoUploadImages(context.Background(), tmpFile)
	if !strings.Contains(result, "fail.png") {
		t.Fatalf("expected fallback to original path when upload fails, got: %s", result)
	}
}

// --- Write with auto-upload ---

func TestPanda_Write_AutoUploadsImage(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "write_test.png")
	if err := os.WriteFile(tmpFile, []byte("fake-png-data"), 0o644); err != nil {
		t.Fatal(err)
	}

	var uploadCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/files/upload" {
			uploadCalled = true
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"code":0,"msg":"ok","data":{"url":"http://example.com/written.png"}}`))
			return
		}
	}))
	defer srv.Close()

	got := make(chan frame, 1)
	wsSrv := newTestWSServer(t, func(msg []byte) []byte {
		var f frame
		if err := json.Unmarshal(msg, &f); err == nil && f.Type == msgTypeSend {
			got <- f
		}
		return nil
	})
	defer wsSrv.Close()

	p := NewPanda(PandaConfig{
		Server:   srv.URL,
		ConvID:   "conv_auto",
		Account:  "test",
		Password: "test",
	}, nil, "test")
	p.token = "test-token"
	p.userID = "bot"

	u := "ws://" + wsSrv.Listener.Addr().String() + "/ws?token=fake"
	conn, resp, err := websocket.DefaultDialer.Dial(u, nil)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	p.connMu.Lock()
	p.conn = conn
	p.connMu.Unlock()

	if err := p.Write(context.Background(), tmpFile); err != nil {
		t.Fatal(err)
	}

	if !uploadCalled {
		t.Fatal("expected upload to be called")
	}

	select {
	case f := <-got:
		var payload msgSendPayload
		if err := json.Unmarshal(f.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(payload.Body, "http://example.com/written.png") {
			t.Fatalf("expected uploaded URL in body, got: %s", payload.Body)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for frame")
	}
}

// --- replaceMarkdownLinks (via autoUploadImages) ---

func TestReplaceMarkdownLinks_AltTextLink(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "photo.png")
	if err := os.WriteFile(tmpFile, []byte("png-data"), 0o644); err != nil {
		t.Fatal(err)
	}

	uploaded := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/files/upload" {
			uploaded <- "ok"
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"url":"http://example.com/remote.png"}}`))
		}
	}))
	defer srv.Close()

	p := NewPanda(PandaConfig{Server: srv.URL}, nil, "test")
	p.token = "test-token"

	input := "Look at [my photo](" + tmpFile + ")"
	result := p.autoUploadImages(context.Background(), input)

	if !strings.Contains(result, "![my photo](http://example.com/remote.png)") {
		t.Fatalf("expected markdown image with alt text, got: %s", result)
	}
	select {
	case <-uploaded:
	default:
		t.Fatal("upload was not triggered")
	}
}

func TestReplaceMarkdownLinks_ImageSyntax(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "img.jpg")
	if err := os.WriteFile(tmpFile, []byte("jpg-data"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/files/upload" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"url":"http://example.com/up.jpg"}}`))
		}
	}))
	defer srv.Close()

	p := NewPanda(PandaConfig{Server: srv.URL}, nil, "test")
	p.token = "test-token"

	input := "See ![]( " + tmpFile + " ) and ![desc](" + tmpFile + ")"
	result := p.autoUploadImages(context.Background(), input)

	if !strings.Contains(result, "![](") {
		t.Fatalf("expected image syntax preserved, got: %s", result)
	}
	if !strings.Contains(result, "![desc](") {
		t.Fatalf("expected image syntax with desc preserved, got: %s", result)
	}
	if !strings.Contains(result, "http://example.com/up.jpg") {
		t.Fatalf("expected uploaded URL, got: %s", result)
	}
}

func TestReplaceMarkdownLinks_NonExistentPath(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	p.token = "test-token"

	input := "Check [screenshot](/nonexistent/path.png) here"
	result := p.autoUploadImages(context.Background(), input)

	if !strings.Contains(result, "[screenshot](/nonexistent/path.png)") {
		t.Fatalf("expected original text preserved for non-existent path, got: %s", result)
	}
}

func TestReplaceMarkdownLinks_HTTPPathSkipped(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	p.token = "test-token"

	input := "Image: [remote](https://example.com/remote.png)"
	result := p.autoUploadImages(context.Background(), input)

	if !strings.Contains(result, "[remote](https://example.com/remote.png)") {
		t.Fatalf("expected HTTP URL to be preserved, got: %s", result)
	}
}

func TestReplaceMarkdownLinks_UploadFails(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "fail.png")
	if err := os.WriteFile(tmpFile, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := NewPanda(PandaConfig{Server: srv.URL}, nil, "test")
	p.token = "test-token"

	input := "[bad](" + tmpFile + ")"
	result := p.autoUploadImages(context.Background(), input)

	if !strings.Contains(result, "[bad]") {
		t.Fatalf("expected original text preserved on upload failure, got: %s", result)
	}
}

func TestReplaceMarkdownLinks_MixedContent(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "chart.png")
	if err := os.WriteFile(tmpFile, []byte("chart-data"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/files/upload" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"url":"http://example.com/chart_remote.png"}}`))
		}
	}))
	defer srv.Close()

	p := NewPanda(PandaConfig{Server: srv.URL}, nil, "test")
	p.token = "test-token"

	input := "Here is [the chart](" + tmpFile + ") and some text after."
	result := p.autoUploadImages(context.Background(), input)

	if !strings.Contains(result, "![the chart](http://example.com/chart_remote.png)") {
		t.Fatalf("expected replaced markdown link, got: %s", result)
	}
	if !strings.Contains(result, "and some text after.") {
		t.Fatalf("expected trailing text preserved, got: %s", result)
	}
	if !strings.HasPrefix(result, "Here is ") {
		t.Fatalf("expected leading text preserved, got: %s", result)
	}
}

// --- replaceBarePaths edge cases ---

func TestReplaceBarePaths_NonExistentBarePath(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	p.token = "test-token"

	result := p.autoUploadImages(context.Background(), "See /fake/dir/img.png")
	if !strings.Contains(result, "/fake/dir/img.png") {
		t.Fatalf("expected non-existent bare path to remain, got: %s", result)
	}
}

func TestReplaceBarePaths_RelativePath(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "rel_img.png")
	if err := os.WriteFile(tmpFile, []byte("rel-data"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/files/upload" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"url":"http://example.com/rel_remote.png"}}`))
		}
	}))
	defer srv.Close()

	p := NewPanda(PandaConfig{Server: srv.URL}, nil, "test")
	p.token = "test-token"

	result := p.autoUploadImages(context.Background(), "Image: "+tmpFile+" end")
	if !strings.Contains(result, "![rel_img.png](http://example.com/rel_remote.png)") {
		t.Fatalf("expected replaced bare path, got: %s", result)
	}
}

// --- isConvAllowed ---

func TestPanda_IsConvAllowed_EmptyList(t *testing.T) {
	p := NewPanda(PandaConfig{}, nil, "test")
	if !p.isConvAllowed("any_conv") {
		t.Fatal("expected true for empty conv allowlist (allow all)")
	}
}

func TestPanda_IsConvAllowed_Match(t *testing.T) {
	p := NewPanda(PandaConfig{AllowConvs: "conv_abc,conv_*"}, nil, "test")
	if !p.isConvAllowed("conv_xyz") {
		t.Fatal("expected match for conv_* pattern")
	}
}

func TestPanda_IsConvAllowed_NoMatch(t *testing.T) {
	p := NewPanda(PandaConfig{AllowConvs: "conv_specific"}, nil, "test")
	if p.isConvAllowed("other_conv") {
		t.Fatal("expected no match")
	}
}

// --- handleMsgPush conv filter ---

func TestPanda_HandleMsgPush_ConvAllowedFilter(t *testing.T) {
	p := NewPanda(PandaConfig{AllowConvs: "conv_target"}, nil, "test")
	p.userID = "bot"
	p.allowUsers = []string{"*"}

	push := msgPushPayload{SenderID: "other", ConvID: "conv_wrong", ContentType: 0, Body: "hello"}
	data, _ := json.Marshal(push)
	err := p.handleMsgPush(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.msgChan) != 0 {
		t.Fatal("expected message from unallowed conv to be filtered")
	}
}

// --- handleFrame sendAck buffer flush ---

func TestPanda_HandleFrame_SendAck_FlushesPending(t *testing.T) {
	got := make(chan frame, 3)
	srv := newTestWSServer(t, func(msg []byte) []byte {
		var f frame
		if err := json.Unmarshal(msg, &f); err == nil && f.Type == msgTypeSend {
			got <- f
			var payload msgSendPayload
			_ = json.Unmarshal(f.Payload, &payload)
			if payload.ClientSeq == 0 {
				ack, _ := json.Marshal(msgSendAckPayload{
					MsgID:     42,
					ClientSeq: payload.ClientSeq,
					Status:    1,
				})
				resp, _ := json.Marshal(frame{Type: msgTypeSendAck, Payload: ack})
				return resp
			}
		}
		return nil
	})
	defer srv.Close()

	p := NewPanda(PandaConfig{
		Server:  "http://" + srv.Listener.Addr().String(),
		ConvID:  "conv_flush",
		Account: "test",
	}, nil, "Dolphin")
	p.token = "fake"
	p.userID = "test"

	u := "ws://" + srv.Listener.Addr().String() + "/ws?token=fake"
	conn, resp, _ := websocket.DefaultDialer.Dial(u, nil)
	if resp != nil {
		defer resp.Body.Close()
	}
	defer func() { _ = conn.Close() }()
	p.connMu.Lock()
	p.conn = conn
	p.connMu.Unlock()

	go func() {
		for {
			var f frame
			if err := conn.ReadJSON(&f); err != nil {
				return
			}
			_ = p.handleFrame(f)
		}
	}()

	_ = p.WriteThinking(context.Background(), "think")
	_ = p.WriteToolCall(context.Background(), types.ToolCall{ID: "a", Name: "t1", Arguments: "{}"})

	// Wait for the first-send ack to arrive and flush pending entries,
	// rather than a fixed sleep that races the async readLoop on slow
	// CI runners under -race.
	deadline := time.Now().Add(10 * time.Second)
	for {
		p.mu.Lock()
		pending := len(p.pendingEntries)
		flushed := p.timelineRootMsgID != 0
		p.mu.Unlock()
		if flushed && pending == 0 {
			break
		}
		if time.Now().After(deadline) {
			p.mu.Lock()
			pending := len(p.pendingEntries)
			p.mu.Unlock()
			t.Fatalf("expected pending entries flushed after ack, got %d", pending)
		}
		time.Sleep(5 * time.Millisecond)
	}

	_ = p.WriteToolResult(context.Background(), types.ToolResult{ToolCallID: "a", Content: "ok"})
	drainFrames(t, got)

	p.mu.Lock()
	pending := len(p.pendingEntries)
	p.mu.Unlock()
	if pending != 0 {
		t.Fatalf("expected pending entries flushed after ack, got %d", pending)
	}
}

// --- helpers ---

// newTestWSServer creates a WebSocket test server that echoes or handles messages.
func newTestWSServer(t *testing.T, handler func([]byte) []byte) *httptest.Server {
	t.Helper()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

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

// --- AgentTimeline ---

func TestPanda_AgentTimeline_WriteThinking_Appends(t *testing.T) {
	p := NewPanda(PandaConfig{ConvID: "c1"}, nil, "Dolphin")

	// WriteThinking only accumulates locally — no frames are sent.
	_ = p.WriteThinking(context.Background(), "嗯，")
	_ = p.WriteThinking(context.Background(), "用户想知道...")

	p.timelineMu.Lock()
	defer p.timelineMu.Unlock()

	if len(p.timelineEntries) != 1 {
		t.Fatalf("expected 1 thinking entry, got %d", len(p.timelineEntries))
	}
	e := p.timelineEntries[0]
	if e.Type != TimelineEntryThinking {
		t.Fatalf("expected thinking, got %s", e.Type)
	}
	if e.Content != "嗯，用户想知道..." {
		t.Fatalf("expected appended content, got '%s'", e.Content)
	}
	// Thinking not yet sent (no toolCall or Write has been called).
	if p.thinkingSent {
		t.Fatal("expected thinkingSent=false (only accumulated, not sent)")
	}
}

func TestPanda_AgentTimeline_ProgressiveSend_WithParentMsgID(t *testing.T) {
	got := make(chan frame, 3)
	srv := newTestWSServer(t, func(msg []byte) []byte {
		var f frame
		if err := json.Unmarshal(msg, &f); err == nil && f.Type == msgTypeSend {
			got <- f
			// Respond with an ack for the first send so parentMsgID can be captured.
			var payload msgSendPayload
			_ = json.Unmarshal(f.Payload, &payload)
			ack, _ := json.Marshal(msgSendAckPayload{
				MsgID:     999,
				ClientSeq: payload.ClientSeq,
				Status:    1,
			})
			resp, _ := json.Marshal(frame{Type: msgTypeSendAck, Payload: ack})
			return resp
		}
		return nil
	})
	defer srv.Close()

	p := NewPanda(PandaConfig{
		Server:  "http://" + srv.Listener.Addr().String(),
		ConvID:  "conv_timeline",
		Account: "test",
	}, nil, "Dolphin")
	p.token = "fake"
	p.userID = "test"

	u := "ws://" + srv.Listener.Addr().String() + "/ws?token=fake"
	conn, resp, err := websocket.DefaultDialer.Dial(u, nil)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	p.connMu.Lock()
	p.conn = conn
	p.connMu.Unlock()

	// Start a read goroutine to process incoming acks.
	go func() {
		for {
			var f frame
			if err := conn.ReadJSON(&f); err != nil {
				return
			}
			_ = p.handleFrame(f)
		}
	}()

	// WriteThinking only accumulates locally — no frame is sent.
	_ = p.WriteThinking(context.Background(), "Let me think...")

	// WriteToolCall bundles thinking + toolCall in one frame (parentMsgID=0, creates bubble).
	_ = p.WriteToolCall(context.Background(), types.ToolCall{ID: "tc1", Name: "search", Arguments: `{"q":"test"}`})
	time.Sleep(50 * time.Millisecond) // let ack arrive

	// WriteToolResult sends toolResult as an appended entry.
	_ = p.WriteToolResult(context.Background(), types.ToolResult{ToolCallID: "tc1", Content: "found 3 results"})

	// Write sends response + completed.
	_ = p.Write(context.Background(), "Here is the answer.")

	var frames []frame
	for i := 0; i < 3; i++ {
		select {
		case f := <-got:
			frames = append(frames, f)
		case <-time.After(10 * time.Second):
			t.Fatalf("timed out waiting for frame %d", i+1)
		}
	}

	if len(frames) != 3 {
		t.Fatalf("expected 3 frames, got %d", len(frames))
	}

	// Frame 1: thinking + toolCall bundled, parentMsgID=0
	var p1 msgSendPayload
	_ = json.Unmarshal(frames[0].Payload, &p1)
	if p1.ContentType != 9 {
		t.Fatalf("frame 1: expected ContentType=9, got %d", p1.ContentType)
	}
	var t1 AgentTimelineBody
	_ = json.Unmarshal([]byte(p1.Body), &t1)
	if t1.ParentMsgID != 0 {
		t.Fatalf("frame 1: expected ParentMsgID=0, got %d", t1.ParentMsgID)
	}
	if t1.Status != "running" {
		t.Fatalf("frame 1: expected status 'running', got '%s'", t1.Status)
	}
	if len(t1.Entries) != 2 {
		t.Fatalf("frame 1: expected 2 entries (thinking + toolCall), got %d", len(t1.Entries))
	}
	if t1.Entries[0].Type != TimelineEntryThinking {
		t.Fatalf("frame 1: expected thinking entry, got '%s'", t1.Entries[0].Type)
	}
	if t1.Entries[0].Content != "Let me think..." {
		t.Fatalf("frame 1: expected thinking content 'Let me think...', got '%s'", t1.Entries[0].Content)
	}
	if t1.Entries[1].ToolName != "search" {
		t.Fatalf("frame 1: expected toolName 'search', got '%s'", t1.Entries[1].ToolName)
	}

	// Frame 2: tool result, parentMsgID=999 (from ack)
	var p2 msgSendPayload
	_ = json.Unmarshal(frames[1].Payload, &p2)
	var t2 AgentTimelineBody
	_ = json.Unmarshal([]byte(p2.Body), &t2)
	if t2.ParentMsgID != 999 {
		t.Fatalf("frame 2: expected ParentMsgID=999, got %d", t2.ParentMsgID)
	}
	if len(t2.Entries) != 1 {
		t.Fatalf("frame 2: expected 1 entry (toolResult), got %d", len(t2.Entries))
	}
	if t2.Entries[0].Content != "found 3 results" {
		t.Fatalf("frame 2: expected 'found 3 results', got '%s'", t2.Entries[0].Content)
	}

	// Frame 3: final response, status completed
	var p3 msgSendPayload
	_ = json.Unmarshal(frames[2].Payload, &p3)
	var t3 AgentTimelineBody
	_ = json.Unmarshal([]byte(p3.Body), &t3)
	if t3.Status != "completed" {
		t.Fatalf("frame 3: expected status 'completed', got '%s'", t3.Status)
	}
	if len(t3.Entries) != 1 {
		t.Fatalf("frame 3: expected 1 entry (response), got %d", len(t3.Entries))
	}
	if t3.Entries[0].Content != "Here is the answer." {
		t.Fatalf("frame 3: expected response content, got '%s'", t3.Entries[0].Content)
	}
}

func TestPanda_AgentTimeline_Write_ResetsState(t *testing.T) {
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
		Server:  "http://" + srv.Listener.Addr().String(),
		ConvID:  "conv_reset",
		Account: "test",
	}, nil, "Dolphin")
	p.token = "fake"
	p.userID = "test"

	u := "ws://" + srv.Listener.Addr().String() + "/ws?token=fake"
	conn, resp, _ := websocket.DefaultDialer.Dial(u, nil)
	if resp != nil {
		defer resp.Body.Close()
	}
	defer func() { _ = conn.Close() }()
	p.connMu.Lock()
	p.conn = conn
	p.connMu.Unlock()

	// Write should clear timelineEntries and reset rootMsgID/ackCh
	p.timelineRootMsgID = 999
	_ = p.Write(context.Background(), "done")

	p.timelineMu.Lock()
	if len(p.timelineEntries) != 0 {
		t.Fatalf("expected empty entries after Write, got %d", len(p.timelineEntries))
	}
	p.timelineMu.Unlock()

	p.mu.Lock()
	if p.timelineRootMsgID != 0 {
		t.Fatalf("expected timelineRootMsgID=0 after Write, got %d", p.timelineRootMsgID)
	}

	p.mu.Unlock()
}

func TestPanda_AgentTimeline_WriteThinking_CreatesNewEntryAfterToolCall(t *testing.T) {
	p := NewPanda(PandaConfig{ConvID: "c1"}, nil, "Dolphin")

	// WriteThinking only accumulates locally.
	_ = p.WriteThinking(context.Background(), "thinking...")
	// WriteToolCall flushes thinking and adds toolCall.
	_ = p.WriteToolCall(context.Background(), types.ToolCall{ID: "tc1", Name: "search", Arguments: "{}"})
	// New thinking after tool call starts a fresh thinking entry.
	_ = p.WriteThinking(context.Background(), "more thinking...")

	p.timelineMu.Lock()
	defer p.timelineMu.Unlock()

	// Entries: thinking (sent), toolCall, new thinking (unsent, accumulated)
	if len(p.timelineEntries) != 3 {
		t.Fatalf("expected 3 entries (thinking, toolCall, new-thinking), got %d", len(p.timelineEntries))
	}
	if p.timelineEntries[2].Type != TimelineEntryThinking {
		t.Fatalf("expected 3rd entry thinking, got %s", p.timelineEntries[2].Type)
	}
	if p.timelineEntries[2].Content != "more thinking..." {
		t.Fatalf("expected new thinking content, got '%s'", p.timelineEntries[2].Content)
	}
	// New thinking phase started, thinkingSent resets to false so the
	// next tool call will bundle this new thinking.
	if p.thinkingSent {
		t.Fatal("expected thinkingSent=false after new thinking phase")
	}
}

func TestPanda_AgentTimeline_BufferBeforeAck(t *testing.T) {
	// Verify that entries sent before the first ack arrives are buffered,
	// not sent with parentMsgID=0 (which would create a second bubble).
	got := make(chan frame, 3)
	srv := newTestWSServer(t, func(msg []byte) []byte {
		var f frame
		if err := json.Unmarshal(msg, &f); err == nil && f.Type == msgTypeSend {
			got <- f
			// Deliberately do NOT return an ack; verify buffering prevents split bubbles.
		}
		return nil
	})
	defer srv.Close()

	p := NewPanda(PandaConfig{
		Server:  "http://" + srv.Listener.Addr().String(),
		ConvID:  "conv_buf",
		Account: "test",
	}, nil, "Dolphin")
	p.token = "fake"
	p.userID = "test"

	u := "ws://" + srv.Listener.Addr().String() + "/ws?token=fake"
	conn, resp, _ := websocket.DefaultDialer.Dial(u, nil)
	if resp != nil {
		defer resp.Body.Close()
	}
	defer func() { _ = conn.Close() }()
	p.connMu.Lock()
	p.conn = conn
	p.connMu.Unlock()

	_ = p.WriteThinking(context.Background(), "thinking...")

	// First send creates the bubble (parentMsgID=0).
	_ = p.WriteToolCall(context.Background(), types.ToolCall{ID: "tc1", Name: "search", Arguments: "{}"})

	// No ack yet — this should be buffered, not sent.
	_ = p.WriteToolResult(context.Background(), types.ToolResult{ToolCallID: "tc1", Content: "result"})

	// Only 1 frame should have been sent.
	select {
	case f := <-got:
		var p1 msgSendPayload
		_ = json.Unmarshal(f.Payload, &p1)
		var t1 AgentTimelineBody
		_ = json.Unmarshal([]byte(p1.Body), &t1)
		if len(t1.Entries) != 2 {
			t.Fatalf("expected 2 entries (thinking + toolCall), got %d", len(t1.Entries))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for first frame")
	}

	// No second frame should arrive.
	select {
	case <-got:
		t.Fatal("unexpected second frame — WriteToolResult should have been buffered")
	case <-time.After(100 * time.Millisecond):
	}

	// Buffered entry should be pending.
	p.mu.Lock()
	buffered := len(p.pendingEntries)
	p.mu.Unlock()
	if buffered != 1 {
		t.Fatalf("expected 1 buffered entry, got %d", buffered)
	}
}

func TestPanda_AgentTimeline_StaleAckIgnored(t *testing.T) {
	// After Flush resets state, a late ack from the previous turn must not
	// leak its MsgID into the next turn.
	got := make(chan frame, 3)
	srv := newTestWSServer(t, func(msg []byte) []byte {
		var f frame
		if err := json.Unmarshal(msg, &f); err == nil && f.Type == msgTypeSend {
			got <- f
			// Return ack so timelineRootMsgID gets set during turn 1.
			var payload msgSendPayload
			_ = json.Unmarshal(f.Payload, &payload)
			ack, _ := json.Marshal(msgSendAckPayload{
				MsgID:     100,
				ClientSeq: payload.ClientSeq,
				Status:    1,
			})
			resp, _ := json.Marshal(frame{Type: msgTypeSendAck, Payload: ack})
			return resp
		}
		return nil
	})
	defer srv.Close()

	p := NewPanda(PandaConfig{
		Server:  "http://" + srv.Listener.Addr().String(),
		ConvID:  "conv_stale",
		Account: "test",
	}, nil, "Dolphin")
	p.token = "fake"
	p.userID = "test"

	u := "ws://" + srv.Listener.Addr().String() + "/ws?token=fake"
	conn, resp, _ := websocket.DefaultDialer.Dial(u, nil)
	if resp != nil {
		defer resp.Body.Close()
	}
	defer func() { _ = conn.Close() }()
	p.connMu.Lock()
	p.conn = conn
	p.connMu.Unlock()

	// Start reader so acks are processed via handleFrame.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			var f frame
			if err := conn.ReadJSON(&f); err != nil {
				return
			}
			_ = p.handleFrame(f)
		}
	}()

	// ── Turn 1: normal agent turn ──
	_ = p.WriteThinking(context.Background(), "t1")
	_ = p.WriteToolCall(context.Background(), types.ToolCall{ID: "t1", Name: "s", Arguments: "{}"})
	time.Sleep(100 * time.Millisecond) // let ack arrive
	_ = p.WriteToolResult(context.Background(), types.ToolResult{ToolCallID: "t1", Content: "r1"})
	_ = p.Write(context.Background(), "done1")

	// Flush completes the turn.
	_ = p.Flush()

	// Drain turn 1 frames.
	drainFrames(t, got)

	// ── Inject stale ack AFTER Flush ──
	stalePayload, _ := json.Marshal(msgSendAckPayload{
		MsgID:     99999,
		ClientSeq: 999,
		Status:    1,
	})
	_ = p.handleFrame(frame{Type: msgTypeSendAck, Payload: stalePayload})

	// Stale ack must be ignored (firstSendDone was reset by Flush).
	p.mu.Lock()
	root := p.timelineRootMsgID
	p.mu.Unlock()
	if root != 0 {
		t.Fatalf("stale ack should not set timelineRootMsgID after Flush, got %d", root)
	}

	// ── Turn 2: must use parentMsgID=0 (new bubble) ──
	_ = p.WriteThinking(context.Background(), "t2")
	_ = p.WriteToolCall(context.Background(), types.ToolCall{ID: "t2", Name: "c", Arguments: "{}"})

	select {
	case f := <-got:
		var payload msgSendPayload
		_ = json.Unmarshal(f.Payload, &payload)
		var body AgentTimelineBody
		_ = json.Unmarshal([]byte(payload.Body), &body)
		if body.ParentMsgID != 0 {
			t.Fatalf("turn 2 must use parentMsgID=0, got %d", body.ParentMsgID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for turn 2 frame")
	}

	// Cleanup: close connection so reader goroutine exits.
	_ = conn.Close()
	<-done
}

func drainFrames(t *testing.T, ch chan frame) {
	t.Helper()
	for {
		select {
		case <-ch:
		case <-time.After(50 * time.Millisecond):
			return
		}
	}
}
