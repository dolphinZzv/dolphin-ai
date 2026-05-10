package transport

import (
	"testing"

	"dolphinzZ/internal/config"
)

func TestDeriveResponseTopicExactMatch(t *testing.T) {
	got := deriveResponseTopic("dolphinzZ/agent/command", "dolphinzZ/agent/response", "dolphinzZ/agent/command")
	if got != "dolphinzZ/agent/response" {
		t.Errorf("exact match: got %q", got)
	}
}

func TestDeriveResponseTopicWildcardSuffix(t *testing.T) {
	got := deriveResponseTopic("dolphinzZ/agent/command/+", "dolphinzZ/agent/response", "dolphinzZ/agent/command/agent-1")
	if got != "dolphinzZ/agent/response/agent-1" {
		t.Errorf("wildcard suffix: got %q", got)
	}
}

func TestDeriveResponseTopicHashWildcard(t *testing.T) {
	got := deriveResponseTopic("dolphinzZ/#", "response", "dolphinzZ/a/b/c")
	if got != "response/a/b/c" {
		t.Errorf("hash wildcard: got %q", got)
	}
}

func TestDeriveResponseTopicStripsTrailingSlash(t *testing.T) {
	got := deriveResponseTopic("topic/", "response", "topic/extra")
	if got != "response/extra" {
		t.Errorf("trailing slash: got %q", got)
	}
}

func TestTruncateShort(t *testing.T) {
	if s := truncate("hello", 10); s != "hello" {
		t.Errorf("short: got %q", s)
	}
}

func TestTruncateLong(t *testing.T) {
	if s := truncate("hello world", 5); s != "hello..." {
		t.Errorf("truncated: got %q", s)
	}
}

func TestTruncateExact(t *testing.T) {
	if s := truncate("hello", 5); s != "hello" {
		t.Errorf("exact: got %q", s)
	}
}

func TestTruncateEmpty(t *testing.T) {
	if s := truncate("", 5); s != "" {
		t.Errorf("empty: got %q", s)
	}
}

func TestMQTTTransportName(t *testing.T) {
	tp := &MQTTTransport{}
	if n := tp.Name(); n != "mqtt" {
		t.Errorf("Name() = %q", n)
	}
}

func TestNewMQTTTransport(t *testing.T) {
	cfg := &config.Config{}
	cfg.Transport.MQTT.Topic = "test/topic"
	cfg.Transport.MQTT.ResponseTopic = "test/response"
	tp := NewMQTTTransport(cfg)
	if tp == nil {
		t.Fatal("NewMQTTTransport returned nil")
	}
	if tp.cfg.Topic != "test/topic" {
		t.Errorf("topic = %q", tp.cfg.Topic)
	}
	respTopic, _ := tp.respTopic.Load().(string)
	if respTopic != "test/response" {
		t.Errorf("response topic = %q", respTopic)
	}
}

func TestMQTTTransportCloseUninitialized(t *testing.T) {
	tp := &MQTTTransport{closeCh: make(chan struct{})}
	if err := tp.Close(); err != nil {
		t.Errorf("Close error: %v", err)
	}
	// Second close should be safe
	if err := tp.Close(); err != nil {
		t.Errorf("second Close error: %v", err)
	}
}

func TestMQTTTransportCloseInitialized(t *testing.T) {
	tp := &MQTTTransport{
		cfg:     &config.MQTTConfig{},
		closeCh: make(chan struct{}),
	}
	tp.connected.Store(false)
	if err := tp.Close(); err != nil {
		t.Errorf("Close error: %v", err)
	}
}

func TestMQTTTransportReadLineClosed(t *testing.T) {
	tp := &MQTTTransport{closeCh: make(chan struct{})}
	close(tp.closeCh) // simulate closed state
	_, err := tp.ReadLine()
	if err == nil {
		t.Error("expected error after close")
	}
}
