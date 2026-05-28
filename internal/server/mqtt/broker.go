// Package mqtt provides an embedded MQTT broker server for dolphin.
// It is independent of the MQTT transport client and runs as a standalone in-process service.
package mqtt

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"reflect"

	"dolphin/internal/config"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	"go.uber.org/zap"
)

// Broker runs an in-process MQTT broker so dolphin does not require
// an external broker when MQTT transport is enabled.
type Broker struct {
	server *mqtt.Server
	cfg    config.MQTTBrokerConfig
}

// New creates a new MQTT broker from the given server config.
func New(cfg config.MQTTBrokerConfig) *Broker {
	return &Broker{cfg: cfg}
}

// Start creates the server, adds an auth hook with the configured accounts, binds
// a TCP listener, and begins serving in a background goroutine.
func (b *Broker) Start() error {
	b.server = mqtt.New(&mqtt.Options{
		Logger: slog.New(newZapHandler()),
	})

	ledger := buildLedger(b.cfg.Accounts)
	if err := b.server.AddHook(new(auth.Hook), &auth.Options{Ledger: ledger}); err != nil {
		return fmt.Errorf("add auth hook: %w", err)
	}

	tcp := listeners.NewTCP(listeners.Config{
		ID:      "dolphin-mqtt",
		Address: b.cfg.Addr,
	})
	if err := b.server.AddListener(tcp); err != nil {
		return fmt.Errorf("add tcp listener: %w", err)
	}

	go func() {
		if err := b.server.Serve(); err != nil {
			zap.S().Errorw("mqtt broker stopped", "error", err)
		}
	}()

	fmt.Fprintf(os.Stderr, "\n=== MQTT broker started ===\nAddress: %s  Accounts: %d\n\n",
		b.cfg.Addr, len(b.cfg.Accounts))
	zap.S().Infow("mqtt broker started", "address", b.cfg.Addr, "accounts", len(b.cfg.Accounts))
	return nil
}

// Close gracefully shuts down the broker.
func (b *Broker) Close() error {
	if b.server != nil {
		b.server.Close()
	}
	return nil
}

// ClientAddr returns the address an MQTT client should use to connect
// to this broker. When the broker listens on all interfaces, returns localhost.
func (b *Broker) ClientAddr() string {
	host, port, err := net.SplitHostPort(b.cfg.Addr)
	if err != nil {
		return "localhost:1883"
	}
	if host == "" || host == "0.0.0.0" {
		host = "localhost"
	}
	return net.JoinHostPort(host, port)
}

// OnConfigChange handles broker config hot-reload. If the address or accounts
// changed, the broker restarts itself with the new configuration.
func (b *Broker) OnConfigChange(oldCfg, newCfg *config.Config) {
	oldS := oldCfg.Servers.MQTTBroker
	newS := newCfg.Servers.MQTTBroker

	if oldS.Addr == newS.Addr && reflect.DeepEqual(oldS.Accounts, newS.Accounts) {
		return
	}

	b.cfg = newS

	// Close existing server and restart with new config.
	if b.server != nil {
		b.server.Close()
		b.server = nil
	}
	if err := b.Start(); err != nil {
		zap.S().Errorw("mqtt broker restart failed", "error", err)
	} else {
		zap.S().Infow("mqtt broker restarted with new config", "addr", newS.Addr)
	}
}

func buildLedger(accounts []config.MQTTAccount) *auth.Ledger {
	users := make(auth.Users, len(accounts))
	for _, a := range accounts {
		users[a.Username] = auth.UserRule{
			Username: auth.RString(a.Username),
			Password: auth.RString(a.Password),
			ACL: auth.Filters{
				"#": auth.ReadWrite,
			},
		}
	}
	return &auth.Ledger{Users: users}
}
