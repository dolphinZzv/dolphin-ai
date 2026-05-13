package transport

import (
	"fmt"
	"net"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	"go.uber.org/zap"
)

// EmbeddedBroker runs an in-process MQTT broker so dolphin does not require
// an external broker when MQTT transport is enabled.
type EmbeddedBroker struct {
	server *mqtt.Server
	addr   string
}

// NewEmbeddedBroker creates a new embedded MQTT broker listening on addr (e.g. ":1883" or "127.0.0.1:1883").
func NewEmbeddedBroker(addr string) *EmbeddedBroker {
	return &EmbeddedBroker{addr: addr}
}

// Start creates the server, adds an allow-all auth hook, binds a TCP listener,
// and begins serving in a background goroutine.
func (b *EmbeddedBroker) Start() error {
	b.server = mqtt.New(nil)

	if err := b.server.AddHook(new(auth.AllowHook), nil); err != nil {
		return fmt.Errorf("add auth hook: %w", err)
	}

	tcp := listeners.NewTCP(listeners.Config{
		ID:      "dolphin-embedded",
		Address: b.addr,
	})
	if err := b.server.AddListener(tcp); err != nil {
		return fmt.Errorf("add tcp listener: %w", err)
	}

	go func() {
		if err := b.server.Serve(); err != nil {
			zap.S().Errorw("embedded mqtt broker stopped", "error", err)
		}
	}()

	zap.S().Infow("embedded mqtt broker started", "address", b.addr)
	return nil
}

// Close gracefully shuts down the embedded broker.
func (b *EmbeddedBroker) Close() error {
	if b.server != nil {
		b.server.Close()
	}
	return nil
}

// ClientAddr returns the address the MQTT transport client should use to connect
// to this embedded broker. When the broker listens on all interfaces (":port"),
// we use localhost; otherwise the configured address is used as-is.
func (b *EmbeddedBroker) ClientAddr() string {
	host, port, err := net.SplitHostPort(b.addr)
	if err != nil {
		return "localhost:1883"
	}
	if host == "" {
		host = "localhost"
	}
	return net.JoinHostPort(host, port)
}
