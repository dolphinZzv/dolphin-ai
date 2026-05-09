package transport

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dolphinzZ/internal/config"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// MQTTTransport provides MQTT pub/sub transport implementing UserIO.
//
// Subscribes to the configured topic (supports MQTT wildcards + / #).
// The response topic is derived from each incoming message's topic so that
// multiple publishers on sub-topics each get their own response channel:
//
//	subscribe:   dolphinzZ/agent/command/+
//	receive on:  dolphinzZ/agent/command/agent-1
//	respond to:  dolphinzZ/agent/response/agent-1
//
// When the incoming topic is an exact match (no wildcard suffix), the
// configured response_topic is used as-is (backward compatible).
type MQTTTransport struct {
	cfg       *config.MQTTConfig
	client    mqtt.Client
	msgCh     chan string
	respTopic atomic.Value // stores last derived response topic (string)
	connected atomic.Bool
	closeMu   sync.Mutex
}

func NewMQTTTransport(cfg *config.Config) *MQTTTransport {
	t := &MQTTTransport{
		cfg:   &cfg.Transport.MQTT,
		msgCh: make(chan string, 1024),
	}
	t.respTopic.Store(cfg.Transport.MQTT.ResponseTopic)
	return t
}

func (t *MQTTTransport) Name() string { return "mqtt" }

func (t *MQTTTransport) Start(ctx context.Context) error {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(t.cfg.Broker)
	opts.SetClientID(t.cfg.ClientID)
	opts.SetKeepAlive(60 * time.Second)
	opts.SetPingTimeout(10 * time.Second)
	opts.SetCleanSession(true)
	opts.SetConnectionLostHandler(func(c mqtt.Client, err error) {
		t.connected.Store(false)
		slog.Error("mqtt connection lost", "error", err)
	})

	t.client = mqtt.NewClient(opts)
	token := t.client.Connect()
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("mqtt connect: %w", token.Error())
	}
	t.connected.Store(true)
	slog.Info("mqtt connected",
		"broker", t.cfg.Broker,
		"command_topic", t.cfg.Topic,
		"response_topic", t.cfg.ResponseTopic,
	)

	// Subscribe to command topic — push payloads to msgCh
	token = t.client.Subscribe(t.cfg.Topic, 0, func(c mqtt.Client, msg mqtt.Message) {
		// Derive response topic from the actual incoming topic
		respTopic := deriveResponseTopic(t.cfg.Topic, t.cfg.ResponseTopic, msg.Topic())
		t.respTopic.Store(respTopic)

		payload := string(msg.Payload())
		slog.Debug("mqtt command received",
			"topic", msg.Topic(),
			"response_topic", respTopic,
			"payload", truncate(payload, 200),
		)
		select {
		case t.msgCh <- payload:
		default:
			slog.Warn("mqtt message dropped, channel full")
		}
	})
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("mqtt subscribe: %w", token.Error())
	}

	<-ctx.Done()
	return t.Close()
}

// ReadLine blocks until an MQTT command message arrives.
func (t *MQTTTransport) ReadLine() (string, error) {
	msg, ok := <-t.msgCh
	if !ok {
		return "", fmt.Errorf("mqtt transport closed")
	}
	return msg, nil
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
	topic, _ := t.respTopic.Load().(string)
	if topic == "" {
		topic = t.cfg.ResponseTopic
	}
	slog.Debug("mqtt publish", "topic", topic, "payload", truncate(payload, 200))
	token := t.client.Publish(topic, 0, false, payload)
	token.Wait()
	return token.Error()
}

func (t *MQTTTransport) Close() error {
	t.closeMu.Lock()
	defer t.closeMu.Unlock()
	if t.client != nil && t.connected.Load() {
		t.connected.Store(false)
		t.client.Unsubscribe(t.cfg.Topic)
		t.client.Disconnect(250)
	}
	return nil
}

// deriveResponseTopic maps an incoming command topic to the response topic.
//
//	cmdTopic:   "dolphinzZ/agent/command/+"
//	incoming:   "dolphinzZ/agent/command/agent-1"
//	result:     "dolphinzZ/agent/response/agent-1"
//
// The MQTT wildcard suffix (/+ /#) is stripped from cmdTopic to find the
// prefix. The remainder of the incoming topic after that prefix is appended
// to the response topic base.
func deriveResponseTopic(cmdTopic, respTopic, incomingTopic string) string {
	prefix := cmdTopic
	prefix = strings.TrimSuffix(prefix, "/+")
	prefix = strings.TrimSuffix(prefix, "/#")
	prefix = strings.TrimSuffix(prefix, "/")

	suffix := strings.TrimPrefix(incomingTopic, prefix)
	if suffix == "" {
		// Exact match — use configured response topic as-is
		return respTopic
	}
	return strings.TrimRight(respTopic, "/") + suffix
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
