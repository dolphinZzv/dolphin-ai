package transport

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"dolphin/internal/config"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

func TestPandaSendMessageDolphinReceivesAndResponds(t *testing.T) {
	testPassword := os.Getenv("DOLPHIN_TEST_PASSWORD")
	if testPassword == "" {
		testPassword = "test"
	}

	accounts := []config.MQTTAccount{{Username: "dolphin", Password: testPassword}}
	broker := NewEmbeddedBroker(":19991", accounts)
	if err := broker.Start(accounts); err != nil {
		t.Fatalf("broker start: %v", err)
	}
	defer broker.Close()

	time.Sleep(100 * time.Millisecond)
	brokerAddr := "tcp://" + broker.ClientAddr()

	cfg := &config.Config{}
	cfg.Transport.MQTT.Broker = brokerAddr
	cfg.Transport.MQTT.Topic = "/agent/+/message"
	cfg.Transport.MQTT.ResponseTopic = "/agent/response"
	cfg.Transport.MQTT.Username = "dolphin"
	cfg.Transport.MQTT.Password = testPassword
	cfg.Transport.MQTT.ClientID = "dolphin-transport"

	transport := NewMQTTTransport(cfg)
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := transport.Start(ctx); err != nil {
			t.Logf("transport error: %v", err)
		}
	}()

	time.Sleep(300 * time.Millisecond)

	agentID := "panda-test"
	responseTopic := fmt.Sprintf("/agent/%s/response", agentID)
	messageTopic := fmt.Sprintf("/agent/%s/message", agentID)

	pandaOpts := mqtt.NewClientOptions()
	pandaOpts.AddBroker(brokerAddr)
	pandaOpts.SetClientID("panda-client")
	pandaOpts.SetUsername("dolphin")
	pandaOpts.SetPassword(testPassword)

	panda := mqtt.NewClient(pandaOpts)
	if !panda.Connect().WaitTimeout(5 * time.Second) {
		t.Fatal("panda connect failed")
	}
	defer panda.Disconnect(250)

	responseCh := make(chan string, 1)
	token := panda.Subscribe(responseTopic, 1, func(_ mqtt.Client, msg mqtt.Message) {
		responseCh <- string(msg.Payload())
	})
	if !token.WaitTimeout(5*time.Second) || token.Error() != nil {
		t.Fatalf("panda subscribe failed: %v", token.Error())
	}
	t.Logf("Panda subscribed to %s", responseTopic)

	testMsg := "hello from panda"
	if !panda.Publish(messageTopic, 1, false, testMsg).WaitTimeout(5 * time.Second) {
		t.Fatal("panda publish failed")
	}
	t.Logf("Panda published to %s: %s", messageTopic, testMsg)

	line, err := transport.ReadLine()
	if err != nil {
		t.Fatalf("Dolphin ReadLine error: %v", err)
	}
	t.Logf("Dolphin received: %s", line)

	transport.WriteLine("dolphin ack: " + line)
	t.Logf("Dolphin wrote response")

	select {
	case resp := <-responseCh:
		t.Logf("Panda received response: %s", resp)
		expected := "dolphin ack: " + line
		if resp != expected && resp != expected+"\n" {
			t.Errorf("response mismatch: got %q, want %q", resp, expected)
		}
	case <-time.After(10 * time.Second):
		t.Error("timeout waiting for dolphin response")
	}

	cancel()
	wg.Wait()
}
