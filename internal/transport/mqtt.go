package transport

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dolphin/internal/config"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"go.uber.org/zap"
)

// mqttMsg pairs an incoming payload with its derived response topic.
type mqttMsg struct {
	payload   string
	respTopic string
}

// MQTTTransport provides MQTT pub/sub transport implementing UserIO.
//
// Subscribes to the configured topic (supports MQTT wildcards + / #).
// The response topic is derived from each incoming message's topic so that
// multiple publishers on sub-topics each get their own response channel:
//
//	subscribe:   /agent/+/message
//	receive on:  /agent/agent-1/message
//	respond to:  /agent/agent-1/response
//
// When the incoming topic is an exact match (no wildcard suffix), the
// configured response_topic is used as-is (backward compatible).
type MQTTTransport struct {
	cfg       *config.MQTTConfig
	client    mqtt.Client
	msgCh     chan mqttMsg
	closeCh   chan struct{}
	closeOnce sync.Once
	respTopic string // guarded by respMu, set on ReadLine, read on publish
	respMu    sync.Mutex
	connected atomic.Bool
	closeMu   sync.Mutex
}

func NewMQTTTransport(cfg *config.Config) *MQTTTransport {
	t := &MQTTTransport{
		cfg:       &cfg.Transport.MQTT,
		msgCh:     make(chan mqttMsg, 4096),
		closeCh:   make(chan struct{}),
		respTopic: cfg.Transport.MQTT.ResponseTopic,
	}
	return t
}

func (t *MQTTTransport) Name() string { return "mqtt" }

func (t *MQTTTransport) Context() string {
	return fmt.Sprintf("Connected via MQTT (broker: %s, command topic: %s). Responses are published as MQTT messages. Keep responses concise since each publish is a separate message.",
		t.cfg.Broker, t.cfg.Topic)
}

func (t *MQTTTransport) Capabilities() Capabilities {
	return Capabilities{Streaming: false, Flushable: true}
}

func (t *MQTTTransport) Start(ctx context.Context) error {
	activeConnections.Add(1)
	opts := mqtt.NewClientOptions()
	opts.AddBroker(t.cfg.Broker)
	opts.SetClientID(t.cfg.ClientID)
	if t.cfg.Username != "" {
		opts.SetUsername(t.cfg.Username)
	}
	if t.cfg.Password != "" {
		opts.SetPassword(t.cfg.Password)
	}
	opts.SetKeepAlive(60 * time.Second)
	opts.SetPingTimeout(10 * time.Second)
	opts.SetCleanSession(false) // preserve subscriptions across reconnect
	opts.SetAutoReconnect(true)
	opts.SetMaxReconnectInterval(30 * time.Second)
	opts.SetConnectionLostHandler(func(c mqtt.Client, err error) {
		t.connected.Store(false)
		zap.S().Errorw("mqtt connection lost, will auto-reconnect", "error", err)
	})

	t.client = mqtt.NewClient(opts)
	token := t.client.Connect()
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("mqtt connect: %w", token.Error())
	}
	t.connected.Store(true)
	zap.S().Infow("mqtt connected",
		"broker", t.cfg.Broker,
		"command_topic", t.cfg.Topic,
		"response_topic", t.cfg.ResponseTopic,
	)

	// Subscribe to command topic — push payloads with response topic to msgCh
	token = t.client.Subscribe(t.cfg.Topic, 1, func(c mqtt.Client, msg mqtt.Message) {
		respTopic := deriveResponseTopic(t.cfg.Topic, t.cfg.ResponseTopic, msg.Topic())
		payload := string(msg.Payload())
		zap.S().Debugw("mqtt command received",
			"topic", msg.Topic(),
			"response_topic", respTopic,
			"payload", truncate(payload, 200),
		)
		select {
		case t.msgCh <- mqttMsg{payload: payload, respTopic: respTopic}:
		case <-time.After(10 * time.Second):
			zap.S().Errorw("mqtt message dropped after 10s timeout, channel full")
		}
	})
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("mqtt subscribe: %w", token.Error())
	}

	<-ctx.Done()
	return t.Close()
}

// ReadLine blocks until an MQTT command message arrives or the transport is closed.
func (t *MQTTTransport) ReadLine() (string, error) {
	select {
	case msg, ok := <-t.msgCh:
		if !ok {
			return "", fmt.Errorf("mqtt transport closed")
		}
		msgsReceived.Inc()
		// Store the response topic atomically so publish() uses the correct topic
		t.respMu.Lock()
		t.respTopic = msg.respTopic
		t.respMu.Unlock()
		return msg.payload, nil
	case <-t.closeCh:
		return "", fmt.Errorf("mqtt transport closed")
	}
}

// WriteLine publishes a line to the derived response topic.
func (t *MQTTTransport) WriteLine(s string) error {
	return t.publish(s + "\n")
}

// WriteString publishes text to the derived response topic.
func (t *MQTTTransport) WriteString(s string) error {
	return t.publish(s)
}

func (t *MQTTTransport) publish(payload string) error {
	if !t.connected.Load() {
		return fmt.Errorf("mqtt not connected")
	}
	msgsSent.Inc()
	t.respMu.Lock()
	topic := t.respTopic
	t.respMu.Unlock()
	if topic == "" {
		topic = t.cfg.ResponseTopic
	}
	zap.S().Debugw("mqtt publish", "topic", topic, "payload", truncate(payload, 200))
	token := t.client.Publish(topic, 0, false, payload)
	token.Wait()
	return token.Error()
}

func (t *MQTTTransport) Close() error {
	t.closeOnce.Do(func() {
		activeConnections.Add(-1)
		t.closeMu.Lock()
		defer t.closeMu.Unlock()
		if t.client != nil && t.connected.Load() {
			t.connected.Store(false)
			t.client.Unsubscribe(t.cfg.Topic)
			t.client.Disconnect(250)
		}
		close(t.closeCh)
	})
	return nil
}

func deriveResponseTopic(cmdTopic, respTopic, incomingTopic string) string {
	prefix := cmdTopic
	prefix = strings.TrimSuffix(prefix, "/+")
	prefix = strings.TrimSuffix(prefix, "/#")
	prefix = strings.TrimSuffix(prefix, "/")

	suffix := strings.TrimPrefix(incomingTopic, prefix)
	if suffix == "" {
		return respTopic
	}
	// Build response topic by replacing "message" suffix with "response"
	// e.g., /agent/panda-test/message -> /agent/panda-test/response
	base := strings.TrimSuffix(incomingTopic, "/message")
	if base == incomingTopic {
		// No "/message" suffix, just append response topic suffix
		return strings.TrimRight(respTopic, "/") + suffix
	}
	return base + "/response"
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
