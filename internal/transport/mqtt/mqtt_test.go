package mqtt

import (
	"testing"
	"time"

	"dolphin/internal/config"
	transport "dolphin/internal/transport"
)

func TestDeriveResponseTopicExactMatch(t *testing.T) {
	got := deriveResponseTopic("dolphin/agent/command", "dolphin/agent/response", "dolphin/agent/command")
	if got != "dolphin/agent/response" {
		t.Errorf("exact match: got %q", got)
	}
}

func TestDeriveResponseTopicWildcardSuffix(t *testing.T) {
	got := deriveResponseTopic("dolphin/agent/command/+", "dolphin/agent/response", "dolphin/agent/command/agent-1")
	if got != "dolphin/agent/response/agent-1" {
		t.Errorf("wildcard suffix: got %q", got)
	}
}

func TestDeriveResponseTopicHashWildcard(t *testing.T) {
	got := deriveResponseTopic("dolphin/#", "response", "dolphin/a/b/c")
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
	if s := transport.Truncate("hello", 10); s != "hello" {
		t.Errorf("short: got %q", s)
	}
}

func TestTruncateLong(t *testing.T) {
	if s := transport.Truncate("hello world", 5); s != "hello..." {
		t.Errorf("truncated: got %q", s)
	}
}

func TestTruncateExact(t *testing.T) {
	if s := transport.Truncate("hello", 5); s != "hello" {
		t.Errorf("exact: got %q", s)
	}
}

func TestTruncateEmpty(t *testing.T) {
	if s := transport.Truncate("", 5); s != "" {
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
	cfg.Transport.MQTT.SubscribeTopic = "test/topic"
	cfg.Transport.MQTT.PublishTopic = "test/response"
	tpI, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tp := tpI.(*MQTTTransport)
	if tp == nil {
		t.Fatal("New returned nil")
	}
	if tp.cfg.SubscribeTopic != "test/topic" {
		t.Errorf("topic = %q", tp.cfg.SubscribeTopic)
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
	close(tp.closeCh)
	_, err := tp.ReadLine()
	if err == nil {
		t.Error("expected error after close")
	}
}

func TestMQTTTransportReadLineReceivesPayload(t *testing.T) {
	tp := &MQTTTransport{
		msgCh:   make(chan mqttMsg, 4),
		closeCh: make(chan struct{}),
	}

	go func() {
		tp.msgCh <- mqttMsg{payload: "hello from mqtt", respTopic: "resp/test"}
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
	cfg := &config.Config{}
	tpI, _ := New(cfg)
	tp := tpI.(*MQTTTransport)
	err := tp.WriteString("test")
	if err == nil {
		t.Error("expected error for WriteString on disconnected transport")
	}
}

func TestMQTTTransportWriteLinePublish(t *testing.T) {
	cfg := &config.Config{}
	tpI, _ := New(cfg)
	tp := tpI.(*MQTTTransport)
	err := tp.WriteLine("test")
	if err == nil {
		t.Error("expected error for WriteLine on disconnected transport")
	}
}

func TestMQTTTransportConcurrentReadClose(t *testing.T) {
	tp := &MQTTTransport{
		msgCh:   make(chan mqttMsg, 4),
		closeCh: make(chan struct{}),
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := tp.ReadLine()
		errCh <- err
	}()

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
		cfg:     &config.MQTTConfig{SubscribeTopic: "cmd", PublishTopic: "resp"},
		msgCh:   make(chan mqttMsg, 1),
		closeCh: make(chan struct{}),
	}
	tp.connected.Store(true)

	tp.msgCh <- mqttMsg{payload: "first", respTopic: "resp"}

	select {
	case tp.msgCh <- mqttMsg{payload: "second", respTopic: "resp"}:
		t.Log("second message sent (unexpected — channel should be full)")
	default:
		t.Log("second message correctly dropped (channel full)")
	}

	select {
	case msg := <-tp.msgCh:
		if msg.payload != "first" {
			t.Errorf("expected 'first', got %q", msg.payload)
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
}

func TestMQTTTransportRespTopicConcurrent(t *testing.T) {
	cfg := &config.Config{}
	cfg.Transport.MQTT.SubscribeTopic = "cmd/+/test"
	cfg.Transport.MQTT.PublishTopic = "resp"
	tpI, _ := New(cfg)
	tp := tpI.(*MQTTTransport)

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
