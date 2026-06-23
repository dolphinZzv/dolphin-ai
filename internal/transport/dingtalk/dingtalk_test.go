package dingtalk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	dtclient "github.com/open-dingtalk/dingtalk-stream-sdk-go/client"
	"go.uber.org/zap"

	transport "dolphin/internal/transport"
)

func TestDingTalkCapability(t *testing.T) {
	dt := NewDingTalk(DingTalkConfig{}, nil, "")
	defer dt.Close()

	cap := dt.Capability()
	if cap.Interactive {
		t.Error("expected Interactive=false for chunk mode")
	}
	if cap.Streamable {
		t.Error("expected Streamable=false for chunk mode")
	}
	if cap.NestRead {
		t.Error("expected NestRead=false for chunk mode")
	}
}

func TestDingTalkWrite(t *testing.T) {
	var received string
	webhookSvr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			MsgType  string `json:"msgtype"`
			Markdown struct {
				Title string `json:"title"`
				Text  string `json:"text"`
			} `json:"markdown"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		received = body.Markdown.Text
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer webhookSvr.Close()

	dt := NewDingTalk(DingTalkConfig{}, nil, "")
	dt.cfg.WebhookURL = webhookSvr.URL
	defer dt.Close()

	ctx := context.Background()
	if err := dt.Write(ctx, "hello dingtalk"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if received != "hello dingtalk" {
		t.Errorf("expected 'hello dingtalk', got %q", received)
	}
}

func TestDingTalkRead(t *testing.T) {
	// No credentials means stream won't start; we inject directly into msgChan.
	dt := NewDingTalk(DingTalkConfig{}, nil, "")
	defer dt.Close()

	expected := "hi from dingtalk"
	dt.msgChan <- expected

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	msg, err := dt.Read(ctx)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if msg != expected {
		t.Errorf("expected %q, got %q", expected, msg)
	}
}

func TestDingTalkReadWithContextCancel(t *testing.T) {
	dt := NewDingTalk(DingTalkConfig{}, nil, "")
	defer dt.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := dt.Read(ctx)
	if err == nil {
		t.Error("expected timeout error from Read")
	}
}

func TestDingTalkWriteWebhookError(t *testing.T) {
	webhookSvr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer webhookSvr.Close()

	dt := NewDingTalk(DingTalkConfig{}, nil, "")
	dt.cfg.WebhookURL = webhookSvr.URL
	defer dt.Close()

	ctx := context.Background()
	err := dt.Write(ctx, "test")
	if err == nil {
		t.Fatal("expected error on webhook 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 in error, got: %v", err)
	}
}

func TestDingTalkCloseTwice(t *testing.T) {
	dt := NewDingTalk(DingTalkConfig{}, nil, "")
	dt.Close()
	// Second close should not panic.
	dt.Close()
}

func TestDingTalkStreamNotStartWithoutCreds(t *testing.T) {
	// Without credentials, the stream should not start.
	dt := NewDingTalk(DingTalkConfig{}, nil, "")
	defer dt.Close()

	if dt.streamCli != nil {
		t.Error("expected streamCli to be nil when no credentials")
	}
}

func TestDingTalkID(t *testing.T) {
	dt := NewDingTalk(DingTalkConfig{}, nil, "")
	defer dt.Close()
	if dt.ID() != "dingtalk" {
		t.Errorf("expected 'dingtalk', got '%s'", dt.ID())
	}
}

func TestDingTalkFlush(t *testing.T) {
	dt := NewDingTalk(DingTalkConfig{}, nil, "")
	defer dt.Close()
	if err := dt.Flush(); err != nil {
		t.Errorf("Flush should be a no-op, got: %v", err)
	}
}

func TestDingTalkWriteErrCode(t *testing.T) {
	webhookSvr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"errcode":400,"errmsg":"invalid webhook"}`))
	}))
	defer webhookSvr.Close()

	dt := NewDingTalk(DingTalkConfig{}, nil, "")
	dt.cfg.WebhookURL = webhookSvr.URL
	defer dt.Close()

	err := dt.Write(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error on errcode!=0")
	}
	if !strings.Contains(err.Error(), "invalid webhook") {
		t.Errorf("expected 'invalid webhook' in error, got: %v", err)
	}
}

func TestDingTalkCloseWithStreamCli(t *testing.T) {
	// Close with streamCli set to a closed client should be safe.
	dt := NewDingTalk(DingTalkConfig{}, nil, "")
	defer dt.Close()

	// Use a closed stream client to verify Close handles it gracefully.
	cli := dtclient.NewStreamClient()
	cli.Close()
	dt.streamCli = cli

	dt.Close()
}

func TestDingTalkTools(t *testing.T) {
	t.Run("returns executor when ClientID and ClientSecret are set", func(t *testing.T) {
		dt := NewDingTalk(DingTalkConfig{ClientID: "id", ClientSecret: "secret"}, nil, "")
		defer dt.Close()
		tools := dt.Tools()
		if len(tools) != 1 {
			t.Fatalf("expected 1 tool desc, got %d", len(tools))
		}
		if tools[0].Name != "dingtalk_file" {
			t.Errorf("expected name 'dingtalk_file', got %q", tools[0].Name)
		}
		if tools[0].Executor == nil {
			t.Error("expected non-nil executor")
		}
	})

	t.Run("returns nil when ClientID is empty", func(t *testing.T) {
		dt := NewDingTalk(DingTalkConfig{ClientID: "", ClientSecret: "secret"}, nil, "")
		defer dt.Close()
		tools := dt.Tools()
		if tools != nil {
			t.Errorf("expected nil, got %d tools", len(tools))
		}
	})

	t.Run("returns nil when ClientSecret is empty", func(t *testing.T) {
		dt := NewDingTalk(DingTalkConfig{ClientID: "id", ClientSecret: ""}, nil, "")
		defer dt.Close()
		tools := dt.Tools()
		if tools != nil {
			t.Errorf("expected nil, got %d tools", len(tools))
		}
	})
}

// ---------------------------------------------------------------------------
// Pure functions
// ---------------------------------------------------------------------------

func TestDingTalkValOr(t *testing.T) {
	t.Run("returns value when present", func(t *testing.T) {
		cfg := map[string]any{"key": "value"}
		if got := valOr(cfg, "key", "default"); got != "value" {
			t.Errorf("valOr = %q", got)
		}
	})

	t.Run("returns default when value is empty", func(t *testing.T) {
		cfg := map[string]any{"key": ""}
		if got := valOr(cfg, "key", "default"); got != "default" {
			t.Errorf("valOr = %q", got)
		}
	})

	t.Run("returns default when missing", func(t *testing.T) {
		cfg := map[string]any{}
		if got := valOr(cfg, "missing", "default"); got != "default" {
			t.Errorf("valOr = %q", got)
		}
	})

	t.Run("joins []any values", func(t *testing.T) {
		cfg := map[string]any{"key": []any{"a", "b", "c"}}
		if got := valOr(cfg, "key", ""); got != "a,b,c" {
			t.Errorf("valOr = %q", got)
		}
	})

	t.Run("filters non-string items in []any", func(t *testing.T) {
		cfg := map[string]any{"key": []any{"a", 42, "c"}}
		if got := valOr(cfg, "key", ""); got != "a,c" {
			t.Errorf("valOr = %q", got)
		}
	})

	t.Run("returns default for empty []any", func(t *testing.T) {
		cfg := map[string]any{"key": []any{}}
		if got := valOr(cfg, "key", "default"); got != "default" {
			t.Errorf("valOr = %q", got)
		}
	})

	t.Run("returns default for non-string non-array value", func(t *testing.T) {
		cfg := map[string]any{"key": 42}
		if got := valOr(cfg, "key", "default"); got != "default" {
			t.Errorf("valOr = %q", got)
		}
	})
}

func TestStripAtMention(t *testing.T) {
	t.Run("strips leading @mention", func(t *testing.T) {
		if got := stripAtMention("@bot /models", "bot"); got != "/models" {
			t.Errorf("stripAtMention = %q", got)
		}
	})

	t.Run("handles no leading @", func(t *testing.T) {
		if got := stripAtMention("/models", "bot"); got != "/models" {
			t.Errorf("stripAtMention = %q", got)
		}
	})

	t.Run("handles @mention with trailing space", func(t *testing.T) {
		if got := stripAtMention("@bot  /models", "bot"); got != "/models" {
			t.Errorf("stripAtMention = %q", got)
		}
	})

	t.Run("handles @mention with tab", func(t *testing.T) {
		if got := stripAtMention("@bot\t/models", "bot"); got != "/models" {
			t.Errorf("stripAtMention = %q", got)
		}
	})

	t.Run("returns original when only @mention and no trailing space", func(t *testing.T) {
		if got := stripAtMention("@bot", "bot"); got != "@bot" {
			t.Errorf("stripAtMention = %q", got)
		}
	})
}

func TestIsSenderAllowed(t *testing.T) {
	t.Run("empty whitelist denies all", func(t *testing.T) {
		d := &DingTalk{allowUsers: nil}
		if d.isSenderAllowed("anyone") {
			t.Error("expected false")
		}
	})

	t.Run("exact match", func(t *testing.T) {
		d := &DingTalk{allowUsers: []string{"alice", "bob"}}
		if !d.isSenderAllowed("alice") {
			t.Error("expected true")
		}
	})

	t.Run("non-matching", func(t *testing.T) {
		d := &DingTalk{allowUsers: []string{"alice"}}
		if d.isSenderAllowed("mallory") {
			t.Error("expected false")
		}
	})

	t.Run("glob match", func(t *testing.T) {
		d := &DingTalk{allowUsers: []string{"*@example.com"}}
		if !d.isSenderAllowed("user@example.com") {
			t.Error("expected true")
		}
		if d.isSenderAllowed("user@other.com") {
			t.Error("expected false")
		}
	})
}

// ---------------------------------------------------------------------------
// Simple getters / return-value methods on DingTalk
// ---------------------------------------------------------------------------

func TestDingTalkRequestPermission(t *testing.T) {
	dt := NewDingTalk(DingTalkConfig{}, nil, "")
	defer dt.Close()
	result, err := dt.RequestPermission(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error")
	}
	if result != transport.PermissionDenied {
		t.Errorf("expected PermissionDenied, got %d", result)
	}
}

func TestDingTalkContext(t *testing.T) {
	dt := NewDingTalk(DingTalkConfig{}, nil, "")
	defer dt.Close()
	ctx := dt.Context()
	if ctx == "" {
		t.Error("expected non-empty context string")
	}
}

func TestDingTalkStart(t *testing.T) {
	t.Run("no-op without webhook URL", func(t *testing.T) {
		dt := NewDingTalk(DingTalkConfig{}, nil, "")
		defer dt.Close()
		err := dt.Start(context.Background())
		if err != nil {
			t.Errorf("Start returned error: %v", err)
		}
	})
}

func TestDingTalkUserID(t *testing.T) {
	dt := NewDingTalk(DingTalkConfig{}, nil, "")
	defer dt.Close()
	// Initially empty
	if id := dt.UserID(); id != "" {
		t.Errorf("expected empty, got %q", id)
	}
	// Set via internal field
	dt.mu.Lock()
	dt.lastSenderID = "user123"
	dt.mu.Unlock()
	if id := dt.UserID(); id != "user123" {
		t.Errorf("expected 'user123', got %q", id)
	}
}

func TestDingTalkUserNick(t *testing.T) {
	dt := NewDingTalk(DingTalkConfig{}, nil, "")
	defer dt.Close()
	dt.mu.Lock()
	dt.lastSenderNick = "Alice"
	dt.mu.Unlock()
	if nick := dt.UserNick(); nick != "Alice" {
		t.Errorf("expected 'Alice', got %q", nick)
	}
}

func TestDingTalkConversationID(t *testing.T) {
	dt := NewDingTalk(DingTalkConfig{}, nil, "")
	defer dt.Close()
	dt.mu.Lock()
	dt.lastConvID = "conv_abc"
	dt.mu.Unlock()
	if cid := dt.ConversationID(); cid != "conv_abc" {
		t.Errorf("expected 'conv_abc', got %q", cid)
	}
}

func TestNewDingTalkNilLogger(t *testing.T) {
	dt := NewDingTalk(DingTalkConfig{}, nil, "")
	defer dt.Close()
	if dt.logger == nil {
		t.Error("expected non-nil logger fallback")
	}
}

func TestDingTalkStdLogAdapter(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	adapter := &stdLogAdapter{logger: logger}
	adapter.Debugf("test %d", 1) // no-op
	adapter.Infof("test %d", 2)
	adapter.Warningf("test %d", 3)
	adapter.Errorf("test %d", 4) // uses Warn in Errorf
	adapter.Fatalf("test %d", 5) // uses Error in Fatalf
}

func TestDingTalkConfirm(t *testing.T) {
	dt := NewDingTalk(DingTalkConfig{}, nil, "")
	defer dt.Close()
	ok, err := dt.Confirm(context.Background(), "test?")
	if ok {
		t.Error("expected false from Confirm")
	}
	if err == nil {
		t.Fatal("expected error from Confirm")
	}
}

func TestDingTalkInitRegistration(t *testing.T) {
	// The init function registers "dingtalk" with the transport registry.
	// Verify the registration works by building a transport via the global registry.
	built, err := transport.Build(context.Background(), "dingtalk", map[string]any{
		"logger":     nil,
		"agent_name": "test-agent",
	})
	if err != nil {
		t.Fatalf("Build dingtalk failed: %v", err)
	}
	defer built.Close()
	if built.ID() != "dingtalk" {
		t.Errorf("expected ID 'dingtalk', got %q", built.ID())
	}
}

func TestDingTalkStartWithWebhook(t *testing.T) {
	var notified atomic.Bool
	webhookSvr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		notified.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookSvr.Close()

	dt := NewDingTalk(DingTalkConfig{}, nil, "")
	dt.cfg.WebhookURL = webhookSvr.URL
	defer dt.Close()

	ctx := context.Background()
	if err := dt.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Give the goroutine time to send the startup notification.
	time.Sleep(100 * time.Millisecond)
	if !notified.Load() {
		t.Error("expected startup notification to be sent")
	}
}

func TestDingTalkNewDingTalkWithLogger(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	dt := NewDingTalk(DingTalkConfig{}, logger, "test-agent")
	defer dt.Close()
	if dt.logger != logger {
		t.Error("expected provided logger to be used")
	}
	if dt.agentName != "test-agent" {
		t.Errorf("expected agentName 'test-agent', got %q", dt.agentName)
	}
}

func TestDingTalkNewDingTalkWithAllowUsers(t *testing.T) {
	dt := NewDingTalk(DingTalkConfig{AllowUsers: "alice,bob"}, nil, "")
	defer dt.Close()
	if len(dt.allowUsers) != 2 {
		t.Errorf("expected 2 allowUsers, got %d", len(dt.allowUsers))
	}
	if dt.allowUsers[0] != "alice" || dt.allowUsers[1] != "bob" {
		t.Errorf("unexpected allowUsers: %v", dt.allowUsers)
	}
}

func TestDingTalkNewDingTalkWithCredentials(t *testing.T) {
	// With client_id and client_secret set, startStream should be called.
	dt := NewDingTalk(DingTalkConfig{ClientID: "test-id", ClientSecret: "test-secret"}, nil, "")
	defer dt.Close()
	// streamCli should be non-nil after startStream.
	if dt.streamCli == nil {
		t.Error("expected streamCli to be initialized when credentials are provided")
	}
}
