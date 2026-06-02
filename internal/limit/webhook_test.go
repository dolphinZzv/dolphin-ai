package limit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dolphin/internal/event"
)

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", timeout)
}

func TestWebhookHandleIgnoresOtherEvents(t *testing.T) {
	w := NewWebhookNotifier(WebhookHTTP, "http://127.0.0.1:1/x", newTestLogger(t))
	w.Handle(context.Background(), event.Event{Type: event.EventLLMStart})
}

func TestWebhookSendSuccess(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	w := NewWebhookNotifier(WebhookHTTP, srv.URL, newTestLogger(t))
	w.Handle(context.Background(), event.Event{
		Type:      event.EventLimitHardBlock,
		SessionID: "s1",
		Timestamp: time.Now(),
		Payload:   map[string]any{"metric": "requests", "current": 10, "hard": 10, "model": "m1"},
	})
	waitFor(t, 2*time.Second, func() bool { return received.Load() == 1 })
}

func TestWebhookSendNon2xx(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`server error`))
	}))
	defer srv.Close()

	w := NewWebhookNotifier(WebhookHTTP, srv.URL, newTestLogger(t))
	w.Handle(context.Background(), event.Event{
		Type:      event.EventLimitHardBlock,
		Timestamp: time.Now(),
		Payload:   map[string]any{"metric": "x", "current": 1, "hard": 1, "model": "m"},
	})
	waitFor(t, 2*time.Second, func() bool { return received.Load() == 1 })
}

func TestWebhookSendAPIErrorCode(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"errcode":310000,"errmsg":"bad sign"}`))
	}))
	defer srv.Close()

	w := NewWebhookNotifier(WebhookDingTalk, srv.URL, newTestLogger(t))
	w.Handle(context.Background(), event.Event{
		Type:      event.EventLimitSoftWarn,
		Timestamp: time.Now(),
		Payload:   map[string]any{"metric": "x", "current": 1, "soft": 1, "hard": 1, "model": "m"},
	})
	waitFor(t, 2*time.Second, func() bool { return received.Load() == 1 })
}

func TestWebhookSendConnectionFailure(t *testing.T) {
	w := NewWebhookNotifier(WebhookHTTP, "http://127.0.0.1:1/never", newTestLogger(t))
	w.Handle(context.Background(), event.Event{
		Type:      event.EventLimitHardBlock,
		Timestamp: time.Now(),
		Payload:   map[string]any{"metric": "x", "current": 1, "hard": 1, "model": "m"},
	})
	time.Sleep(200 * time.Millisecond)
}

func TestFormatGeneric(t *testing.T) {
	w := NewWebhookNotifier(WebhookHTTP, "http://x", newTestLogger(t))
	e := event.Event{
		Type:      event.EventLimitHardBlock,
		SessionID: "s",
		Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Payload:   map[string]any{"foo": "bar"},
	}
	got := w.formatGeneric(e)
	if got["type"] != string(event.EventLimitHardBlock) {
		t.Fatalf("type wrong: %v", got)
	}
	if got["session_id"] != "s" {
		t.Fatalf("session_id wrong: %v", got)
	}
	if got["timestamp"] != "2025-01-01T00:00:00Z" {
		t.Fatalf("timestamp wrong: %v", got)
	}
	if _, ok := got["payload"].(map[string]any); !ok {
		t.Fatalf("payload missing: %v", got)
	}
}

func TestFormatDingTalk(t *testing.T) {
	w := NewWebhookNotifier(WebhookDingTalk, "http://x", newTestLogger(t))
	got := w.formatDingTalk(event.Event{
		Type:      event.EventLimitSoftWarn,
		SessionID: "s",
		Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Payload:   map[string]any{"metric": "requests", "current": int64(10), "soft": int64(10), "hard": int64(20), "model": "m"},
	})
	md, ok := got["markdown"].(map[string]string)
	if !ok {
		t.Fatalf("missing markdown map: %v", got)
	}
	if md["title"] != "限流告警" {
		t.Fatalf("title wrong: %v", md)
	}
	for _, want := range []string{"软限告警", "m", "**软限**: 10", "**硬限**: 20"} {
		if !strings.Contains(md["text"], want) {
			t.Fatalf("body missing %q, got: %s", want, md["text"])
		}
	}
}

func TestFormatWeWork(t *testing.T) {
	w := NewWebhookNotifier(WebhookWeWork, "http://x", newTestLogger(t))
	got := w.formatWeWork(event.Event{
		Type:      event.EventLimitHardBlock,
		SessionID: "s",
		Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Payload:   map[string]any{"metric": "requests", "current": 20, "hard": 20, "model": "m"},
	})
	if _, ok := got["markdown"]; !ok {
		t.Fatalf("missing markdown: %v", got)
	}
	if got["msgtype"] != "markdown" {
		t.Fatalf("msgtype wrong: %v", got)
	}
}

func TestFormatMessageDispatch(t *testing.T) {
	w := NewWebhookNotifier(WebhookDingTalk, "http://x", newTestLogger(t))
	e := event.Event{Type: event.EventLimitHardBlock, Payload: map[string]any{}}
	if _, ok := w.formatMessage(e).(map[string]any)["markdown"]; !ok {
		t.Fatal("dingtalk dispatch should yield markdown key")
	}
	w = NewWebhookNotifier(WebhookWeWork, "http://x", newTestLogger(t))
	if _, ok := w.formatMessage(e).(map[string]any)["markdown"]; !ok {
		t.Fatal("wework dispatch should yield markdown key")
	}
	w = NewWebhookNotifier("unknown", "http://x", newTestLogger(t))
	if got, ok := w.formatMessage(e).(map[string]any)["type"]; !ok || got != string(event.EventLimitHardBlock) {
		t.Fatal("unknown type should fall back to generic format")
	}
}

func TestFormatMarkdownSkipsEmptyFields(t *testing.T) {
	w := NewWebhookNotifier(WebhookHTTP, "http://x", newTestLogger(t))
	text := w.formatMarkdownText(event.Event{
		Type:      event.EventLimitSoftWarn,
		SessionID: "s",
		Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Payload:   map[string]any{"metric": "x", "current": 1, "soft": 1, "hard": 0, "model": ""},
	})
	if strings.Contains(text, "**模型**") {
		t.Fatalf("should not contain model line when empty, got: %s", text)
	}
	if strings.Contains(text, "**硬限**") {
		t.Fatalf("should not contain hard line when 0, got: %s", text)
	}
}

func TestFriendlyName(t *testing.T) {
	cases := map[event.Type]string{
		event.EventLimitSoftWarn:  "软限告警",
		event.EventLimitHardBlock: "硬限阻断",
		event.EventLLMStart:       "llm.start",
	}
	for typ, want := range cases {
		if got := friendlyName(typ); got != want {
			t.Fatalf("friendlyName(%q) = %q, want %q", typ, got, want)
		}
	}
}
