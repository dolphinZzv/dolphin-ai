package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	dtclient "github.com/open-dingtalk/dingtalk-stream-sdk-go/client"
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
