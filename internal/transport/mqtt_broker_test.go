package transport

import (
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

func TestNewEmbeddedBroker(t *testing.T) {
	b := NewEmbeddedBroker(":9999")
	if b == nil {
		t.Fatal("NewEmbeddedBroker returned nil")
	}
	if b.addr != ":9999" {
		t.Errorf("addr = %q, want :9999", b.addr)
	}
	if b.server != nil {
		t.Error("server should be nil before Start")
	}
}

func TestEmbeddedBrokerStartClose(t *testing.T) {
	b := NewEmbeddedBroker(":19999")
	if err := b.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if b.server == nil {
		t.Fatal("server should be non-nil after Start")
	}

	// Closing should succeed without error.
	if err := b.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestEmbeddedBrokerClientAddr(t *testing.T) {
	tests := []struct {
		addr string
		want string
	}{
		{":1883", "localhost:1883"},
		{"0.0.0.0:1883", "0.0.0.0:1883"},
		{"127.0.0.1:1883", "127.0.0.1:1883"},
		{"192.168.1.1:8888", "192.168.1.1:8888"},
		{"", "localhost:1883"},
		{"invalid", "localhost:1883"},
	}
	for _, tt := range tests {
		b := NewEmbeddedBroker(tt.addr)
		got := b.ClientAddr()
		if got != tt.want {
			t.Errorf("ClientAddr(%q) = %q, want %q", tt.addr, got, tt.want)
		}
	}
}

func TestEmbeddedBrokerClientConnect(t *testing.T) {
	b := NewEmbeddedBroker(":19998")
	if err := b.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer b.Close()

	// Connect a real MQTT client to the embedded broker.
	opts := mqtt.NewClientOptions()
	opts.AddBroker("tcp://" + b.ClientAddr())
	opts.SetClientID("test-client")
	opts.SetConnectTimeout(5 * time.Second)
	opts.SetAutoReconnect(false)

	client := mqtt.NewClient(opts)
	token := client.Connect()
	if token.WaitTimeout(10*time.Second) && token.Error() != nil {
		t.Fatalf("client connect: %v", token.Error())
	}
	defer client.Disconnect(250)

	if !client.IsConnected() {
		t.Fatal("client should be connected")
	}
}

func TestEmbeddedBrokerPubSub(t *testing.T) {
	b := NewEmbeddedBroker(":19997")
	if err := b.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer b.Close()

	addr := "tcp://" + b.ClientAddr()

	// Subscriber
	subOpts := mqtt.NewClientOptions()
	subOpts.AddBroker(addr)
	subOpts.SetClientID("sub-client")
	subOpts.SetAutoReconnect(false)
	sub := mqtt.NewClient(subOpts)
	if token := sub.Connect(); token.WaitTimeout(10*time.Second) && token.Error() != nil {
		t.Fatalf("sub connect: %v", token.Error())
	}
	defer sub.Disconnect(250)

	received := make(chan string, 1)
	sub.Subscribe("test/topic", 0, func(_ mqtt.Client, msg mqtt.Message) {
		received <- string(msg.Payload())
	})

	// Publisher
	pubOpts := mqtt.NewClientOptions()
	pubOpts.AddBroker(addr)
	pubOpts.SetClientID("pub-client")
	pubOpts.SetAutoReconnect(false)
	pub := mqtt.NewClient(pubOpts)
	if token := pub.Connect(); token.WaitTimeout(10*time.Second) && token.Error() != nil {
		t.Fatalf("pub connect: %v", token.Error())
	}
	defer pub.Disconnect(250)

	pub.Publish("test/topic", 0, false, "hello from test")
	select {
	case msg := <-received:
		if msg != "hello from test" {
			t.Errorf("got %q, want %q", msg, "hello from test")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for message")
	}
}
