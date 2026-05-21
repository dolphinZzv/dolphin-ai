package mqtt

import (
	"testing"
	"time"

	"dolphin/internal/config"
	servermqtt "dolphin/internal/server/mqtt"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

func newTestBroker(addr string) *servermqtt.Broker {
	return servermqtt.New(config.MQTTBrokerConfig{
		Enabled: true,
		Addr:    addr,
		Accounts: []config.MQTTAccount{
			{Username: "test", Password: "secret"},
		},
	})
}

func TestPubSubDirect(t *testing.T) {
	broker := newTestBroker(":19993")
	if err := broker.Start(); err != nil {
		t.Fatalf("broker start: %v", err)
	}
	defer broker.Close()

	time.Sleep(100 * time.Millisecond)
	brokerAddr := "tcp://" + broker.ClientAddr()

	subOpts := mqtt.NewClientOptions()
	subOpts.AddBroker(brokerAddr)
	subOpts.SetClientID("subscriber-direct")
	subOpts.SetUsername("test")
	subOpts.SetPassword("secret")
	subOpts.SetAutoReconnect(false)

	subClient := mqtt.NewClient(subOpts)
	if !subClient.Connect().WaitTimeout(5 * time.Second) {
		t.Fatal("subscriber connect failed")
	}
	defer subClient.Disconnect(250)

	msgCh := make(chan string, 1)
	token := subClient.Subscribe("/agent/test-id/message", 1, func(_ mqtt.Client, msg mqtt.Message) {
		msgCh <- string(msg.Payload())
	})
	if !token.WaitTimeout(5*time.Second) || token.Error() != nil {
		t.Fatalf("subscribe failed: %v", token.Error())
	}

	pubOpts := mqtt.NewClientOptions()
	pubOpts.AddBroker(brokerAddr)
	pubOpts.SetClientID("publisher-direct")
	pubOpts.SetUsername("test")
	pubOpts.SetPassword("secret")
	pubOpts.SetAutoReconnect(false)

	pubClient := mqtt.NewClient(pubOpts)
	if !pubClient.Connect().WaitTimeout(5 * time.Second) {
		t.Fatal("publisher connect failed")
	}
	defer pubClient.Disconnect(250)

	testMsg := "direct pub/sub test"
	if !pubClient.Publish("/agent/test-id/message", 1, false, testMsg).WaitTimeout(5 * time.Second) {
		t.Fatal("publish failed")
	}

	select {
	case got := <-msgCh:
		if got != testMsg {
			t.Errorf("got %q, want %q", got, testMsg)
		}
	case <-time.After(5 * time.Second):
		t.Error("timeout waiting for message")
	}
}

func TestPubSubWildcard(t *testing.T) {
	broker := newTestBroker(":19992")
	if err := broker.Start(); err != nil {
		t.Fatalf("broker start: %v", err)
	}
	defer broker.Close()

	time.Sleep(100 * time.Millisecond)
	brokerAddr := "tcp://" + broker.ClientAddr()

	subOpts := mqtt.NewClientOptions()
	subOpts.AddBroker(brokerAddr)
	subOpts.SetClientID("subscriber-wildcard")
	subOpts.SetUsername("test")
	subOpts.SetPassword("secret")
	subOpts.SetAutoReconnect(false)

	subClient := mqtt.NewClient(subOpts)
	if !subClient.Connect().WaitTimeout(5 * time.Second) {
		t.Fatal("subscriber connect failed")
	}
	defer subClient.Disconnect(250)

	msgCh := make(chan string, 1)
	token := subClient.Subscribe("/agent/+/message", 1, func(_ mqtt.Client, msg mqtt.Message) {
		msgCh <- string(msg.Payload())
	})
	if !token.WaitTimeout(5*time.Second) || token.Error() != nil {
		t.Fatalf("subscribe wildcard failed: %v", token.Error())
	}

	pubOpts := mqtt.NewClientOptions()
	pubOpts.AddBroker(brokerAddr)
	pubOpts.SetClientID("publisher-wildcard")
	pubOpts.SetUsername("test")
	pubOpts.SetPassword("secret")
	pubOpts.SetAutoReconnect(false)

	pubClient := mqtt.NewClient(pubOpts)
	if !pubClient.Connect().WaitTimeout(5 * time.Second) {
		t.Fatal("publisher connect failed")
	}
	defer pubClient.Disconnect(250)

	testMsg := "wildcard pub/sub test"
	if !pubClient.Publish("/agent/agent-123/message", 1, false, testMsg).WaitTimeout(5 * time.Second) {
		t.Fatal("publish failed")
	}

	select {
	case got := <-msgCh:
		if got != testMsg {
			t.Errorf("got %q, want %q", got, testMsg)
		}
	case <-time.After(5 * time.Second):
		t.Error("timeout waiting for message via wildcard subscription")
	}
}
