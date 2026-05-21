// Package mqtt provides MQTT pub/sub transport.
package mqtt

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dolphin/internal/config"
	transport "dolphin/internal/transport"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"go.uber.org/zap"
)

func init() { transport.Register("mqtt", New) }

type mqttMsg struct {
	payload   string
	respTopic string
}

// MQTTTransport provides MQTT pub/sub transport implementing UserIO.
type MQTTTransport struct {
	cfg       *config.MQTTConfig
	client    mqtt.Client
	msgCh     chan mqttMsg
	closeCh   chan struct{}
	closeOnce sync.Once
	respTopic string
	respMu    sync.Mutex
	connected atomic.Bool
	closeMu   sync.Mutex
}

func New(cfg *config.Config) (transport.Transport, error) {
	t := &MQTTTransport{
		cfg:       &cfg.Transport.MQTT,
		msgCh:     make(chan mqttMsg, 4096),
		closeCh:   make(chan struct{}),
		respTopic: cfg.Transport.MQTT.PublishTopic,
	}
	return t, nil
}

func (t *MQTTTransport) Name() string { return "mqtt" }

func (t *MQTTTransport) Context() string {
	return fmt.Sprintf("Connected via MQTT (broker: %s, command topic: %s). Responses are published as MQTT messages. Keep responses concise since each publish is a separate message.",
		t.cfg.Broker, t.cfg.SubscribeTopic)
}

func (t *MQTTTransport) Capabilities() transport.Capabilities {
	return transport.Capabilities{Streaming: false}
}

func (t *MQTTTransport) Start(ctx context.Context) error {
	transport.ActiveConnections.Add(1)
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
	opts.SetCleanSession(false)
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
		"command_topic", t.cfg.SubscribeTopic,
		"response_topic", t.cfg.PublishTopic,
	)

	token = t.client.Subscribe(t.cfg.SubscribeTopic, 1, func(c mqtt.Client, msg mqtt.Message) {
		respTopic := deriveResponseTopic(t.cfg.SubscribeTopic, t.cfg.PublishTopic, msg.Topic())
		payload := string(msg.Payload())
		zap.S().Debugw("mqtt command received",
			"topic", msg.Topic(),
			"response_topic", respTopic,
			"payload", transport.Truncate(payload, 200),
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

func (t *MQTTTransport) ReadLine() (string, error) {
	select {
	case msg, ok := <-t.msgCh:
		if !ok {
			return "", fmt.Errorf("mqtt transport closed")
		}
		transport.MsgsReceived.Inc()
		t.respMu.Lock()
		t.respTopic = msg.respTopic
		t.respMu.Unlock()
		return msg.payload, nil
	case <-t.closeCh:
		return "", fmt.Errorf("mqtt transport closed")
	}
}

func (t *MQTTTransport) WriteLine(s string) error {
	return t.publish(s + "\n")
}

func (t *MQTTTransport) WriteString(s string) error {
	return t.publish(s)
}

func (t *MQTTTransport) Flush() error { return nil }

func (t *MQTTTransport) publish(payload string) error {
	if !t.connected.Load() {
		return fmt.Errorf("mqtt not connected")
	}
	transport.MsgsSent.Inc()
	t.respMu.Lock()
	topic := t.respTopic
	t.respMu.Unlock()
	if topic == "" {
		topic = t.cfg.PublishTopic
	}
	zap.S().Debugw("mqtt publish", "topic", topic, "payload", transport.Truncate(payload, 200))
	token := t.client.Publish(topic, 0, false, payload)
	token.Wait()
	return token.Error()
}

func (t *MQTTTransport) Close() error {
	t.closeOnce.Do(func() {
		transport.ActiveConnections.Add(-1)
		t.closeMu.Lock()
		defer t.closeMu.Unlock()
		if t.client != nil && t.connected.Load() {
			t.connected.Store(false)
			t.client.Unsubscribe(t.cfg.SubscribeTopic)
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
	base := strings.TrimSuffix(incomingTopic, "/message")
	if base == incomingTopic {
		return strings.TrimRight(respTopic, "/") + suffix
	}
	return base + "/response"
}
