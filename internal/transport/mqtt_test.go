package transport

import (
	"testing"
	"time"

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
	respTopic := tp.respTopic
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

// --- E2E: MQTT transport channel roundtrip ---

func TestMQTTTransportReadLineReceivesPayload(t *testing.T) {
	tp := &MQTTTransport{
		msgCh:   make(chan string, 4),
		closeCh: make(chan struct{}),
	}

	// Send a payload through the channel
	go func() {
		tp.msgCh <- "hello from mqtt"
	}()

	line, err := tp.ReadLine()
	if err != nil {
		t.Fatalf("ReadLine error: %v", err)
	}
	if line != "hello from mqtt" {
		t.Errorf("ReadLine = %q, want %q", line, "hello from mqtt")
	}
}

func TestMQTTTransportWriteStringPublish(t *testing.T) {
	tp := NewMQTTTransport(&config.Config{})
	// WriteString to a disconnected transport should return an error
	err := tp.WriteString("test")
	if err == nil {
		t.Error("expected error for WriteString on disconnected transport")
	}
}

func TestMQTTTransportWriteLinePublish(t *testing.T) {
	tp := NewMQTTTransport(&config.Config{})
	// WriteLine to a disconnected transport should return an error
	err := tp.WriteLine("test")
	if err == nil {
		t.Error("expected error for WriteLine on disconnected transport")
	}
}

func TestMQTTTransportConcurrentReadClose(t *testing.T) {
	tp := &MQTTTransport{
		msgCh:   make(chan string, 4),
		closeCh: make(chan struct{}),
	}

	// Start a ReadLine that will block, then close
	errCh := make(chan error, 1)
	go func() {
		_, err := tp.ReadLine()
		errCh <- err
	}()

	// Small delay to ensure ReadLine is blocked
	time.Sleep(10 * time.Millisecond)

	tp.Close()

	select {
	case err := <-errCh:
		if err == nil {
			t.Error("expected error from ReadLine after Close")
		}
	case <-time.After(time.Second):
		t.Error("ReadLine did not return after Close")
	}
}

func TestMQTTTransportChannelFullDropped(t *testing.T) {
	tp := &MQTTTransport{
		cfg:     &config.MQTTConfig{Topic: "cmd", ResponseTopic: "resp"},
		msgCh:   make(chan string, 1),
		closeCh: make(chan struct{}),
	}
	tp.connected.Store(true)

	// Fill the channel
	tp.msgCh <- "first"

	// This should not panic — the MQTT callback path uses select/default
	// Simulate what the subscribe callback does:
	payload := "second"
	select {
	case tp.msgCh <- payload:
		t.Log("second message sent (unexpected — channel should be full)")
	default:
		t.Log("second message correctly dropped (channel full)")
	}

	// Drain and verify
	select {
	case msg := <-tp.msgCh:
		if msg != "first" {
			t.Errorf("expected 'first', got %q", msg)
		}
	default:
		t.Error("expected first message to be in channel")
	}
}

func TestMQTTTransportCloseIdempotent(t *testing.T) {
	tp := &MQTTTransport{
		closeCh: make(chan struct{}),
	}

	for i := 0; i < 5; i++ {
		if err := tp.Close(); err != nil {
			t.Errorf("Close #%d returned error: %v", i+1, err)
		}
	}
}

func TestMQTTTransportCapabilities(t *testing.T) {
	tp := &MQTTTransport{}
	caps := tp.Capabilities()
	if caps.Streaming {
		t.Error("MQTT should not support streaming")
	}
	if !caps.Flushable {
		t.Error("MQTT should support flushable (block transport)")
	}
}

func TestMQTTTransportRespTopicConcurrent(t *testing.T) {
	cfg := &config.Config{}
	cfg.Transport.MQTT.Topic = "cmd/+/test"
	cfg.Transport.MQTT.ResponseTopic = "resp"
	tp := NewMQTTTransport(cfg)

	// Simulate concurrent update from subscribe callback and read from publish
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			tp.respMu.Lock()
			tp.respTopic = "resp/agent-" + string(rune('0'+i%10))
			tp.respMu.Unlock()
		}
		done <- struct{}{}
	}()

	go func() {
		for i := 0; i < 100; i++ {
			tp.respMu.Lock()
			_ = tp.respTopic
			tp.respMu.Unlock()
		}
		done <- struct{}{}
	}()

	<-done
	<-done
}
