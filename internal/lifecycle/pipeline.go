package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dolphin/internal/agentio"
	"dolphin/internal/agentloop"
	"dolphin/internal/config"
	"dolphin/internal/event"
	"dolphin/internal/mcp"
	"dolphin/internal/session"
	"dolphin/internal/signal"
	"dolphin/internal/tool"
	"dolphin/internal/transport"
	"dolphin/internal/userio"
	"go.uber.org/zap"
)

type Pipeline struct {
	transports         []transport.IO
	userIO             *userio.UserIO
	agentIO            *agentio.AgentIO
	agentLoop          *agentloop.AgentLoop
	sessionMgr         *session.Manager
	signalBus          *signal.Bus
	eventBus           *event.Bus
	logger             *zap.Logger
	cancel             context.CancelFunc
	otelShutdown       func()
	dingtalkWebhookURL string
}

func New(cfg *config.Config) *Pipeline {
	return NewBuilder(cfg).
		StepLogger().
		StepBuses().
		StepSession().
		StepMemory().
		StepLLM().
		StepTools().
		StepBrain().
		StepAgentIO().
		StepUserIO().
		StepObservability().
		StepTransports().
		Assemble().
		Build()
}

func (p *Pipeline) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	p.cancel = cancel

	p.logger.Info("pipeline starting", zap.Int("transports", len(p.transports)))
	p.eventBus.Publish(ctx, event.Event{Type: event.EventPipelineStart})

	go p.agentLoop.Run(ctx)

	p.agentLoop.SetOnResult(func(tr agentio.TurnResult) {
		p.agentIO.OnResult(&tr)
	})

	for _, tio := range p.transports {
		t := tio
		go func() {
			for {
				input, err := t.Read(ctx)
				if err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						p.logger.Info("transport read stopped",
							zap.String("transport_id", t.ID()),
							zap.Error(err),
						)
						return
					}
					p.logger.Warn("transport read error, retrying in 5s",
						zap.String("transport_id", t.ID()),
						zap.Error(err),
					)
					select {
					case <-ctx.Done():
						return
					case <-time.After(5 * time.Second):
					}
					continue
				}
				if !p.userIO.Handle(ctx, t, input) {
					continue
				}
			}
		}()
	}

	// Send startup notification via DingTalk webhook if configured.
	if p.dingtalkWebhookURL != "" {
		go sendStartupNotification(p.logger, p.dingtalkWebhookURL)
	}
}

func sendStartupNotification(logger *zap.Logger, webhookURL string) {

	payload := map[string]any{
		"msgtype": "text",
		"text": map[string]string{
			"content": "Dolphin AI assistant online \u2713",
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		logger.Warn("startup notification marshal error", zap.Error(err))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(data))
	if err != nil {
		logger.Warn("startup notification request error", zap.Error(err))
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		logger.Warn("startup notification send error", zap.Error(err))
		return
	}
	defer resp.Body.Close()
}

func (p *Pipeline) Shutdown() {
	p.logger.Info("pipeline shutting down")
	p.eventBus.Publish(context.Background(), event.Event{Type: event.EventPipelineShutdown})

	for _, tio := range p.transports {
		if err := tio.Close(); err != nil {
			p.logger.Warn("transport close error",
				zap.String("transport_id", tio.ID()),
				zap.Error(err),
			)
		}
	}

	if p.cancel != nil {
		p.cancel()
	}

	if p.otelShutdown != nil {
		p.otelShutdown()
	}
}

type transportConfig struct {
	Type   string
	Config map[string]any
}

func loadTransportConfigs(cfg *config.Config, agentName string) ([]transportConfig, error) {
	var tcs []transportConfig
	hasExplicit := false
	for i := 0; ; i++ {
		typ := cfg.GetString("transport." + strconv.Itoa(i) + ".type")
		if typ == "" {
			break
		}
		// Skip disabled transports. Default to enabled if not set.
		if !cfg.GetBool("transport." + strconv.Itoa(i) + ".enabled") {
			continue
		}
		hasExplicit = true
		tcs = append(tcs, transportConfig{
			Type: typ,
			Config: map[string]any{
				"type":       typ,
				"agent_name": agentName,
			},
		})
	}
	// Auto-detect DingTalk if enabled and configured.
	if cfg.GetBool("dingtalk.enabled") {
		dingtalkClientID := cfg.GetString("dingtalk.client_id")
		dingtalkClientSecret := cfg.GetString("dingtalk.client_secret")
		dingtalkWebhook := cfg.GetString("dingtalk.webhook_url")
		dingtalkConfigured := dingtalkClientID != "" && dingtalkClientSecret != "" && dingtalkWebhook != ""
		if !hasTransportType(tcs, "dingtalk") && dingtalkConfigured {
			tcs = append(tcs, transportConfig{
				Type: "dingtalk",
				Config: map[string]any{
					"type":          "dingtalk",
					"agent_name":    agentName,
					"client_id":     dingtalkClientID,
					"client_secret": dingtalkClientSecret,
					"webhook_url":   dingtalkWebhook,
					"allow_users":   configListOrString(cfg, "dingtalk.allow_users"),
				},
			})
		}
	}
	// Auto-detect email if enabled and configured.
	if cfg.GetBool("email.enabled") {
		emailAddr := cfg.GetString("email.address")
		emailPass := cfg.GetString("email.password")
		emailKey := cfg.GetString("email.key")
		if !hasTransportType(tcs, "email") && emailAddr != "" && emailPass != "" {
			emailCfg := map[string]any{
				"type":          "email",
				"agent_name":    agentName,
				"email_address": emailAddr,
				"password":      emailPass,
				"imap_server":   cfg.GetString("email.imap_server"),
				"imap_port":     cfg.GetString("email.imap_port"),
				"smtp_server":   cfg.GetString("email.smtp_server"),
				"smtp_port":     cfg.GetString("email.smtp_port"),
				"key":           emailKey,
				"allow_senders": configListOrString(cfg, "email.allow_senders"),
			}
			tcs = append(tcs, transportConfig{Type: "email", Config: emailCfg})
		}
	}
	// Always add stdio unless the user explicitly specified their own transports.
	if !hasExplicit && !hasTransportType(tcs, "stdio") {
		tcs = append([]transportConfig{{Type: "stdio", Config: map[string]any{"type": "stdio"}}}, tcs...)
	}
	return tcs, nil
}

func hasTransportType(tcs []transportConfig, typ string) bool {
	for _, tc := range tcs {
		if tc.Type == typ {
			return true
		}
	}
	return false
}

type mcpServerConfig struct {
	Name    string   `yaml:"name"`
	Type    string   `yaml:"type"`
	Enabled bool     `yaml:"enabled"`
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
	URL     string   `yaml:"url"`
}

func loadMCPServers(cfg *config.Config, reg *tool.Registry, logger *zap.Logger) {
	builtinHandlers := tool.BuiltinMCPHandlers()
	builtinDescs := tool.BuiltinMCPDescriptions()
	builtinSchemas := tool.BuiltinMCPSchemas()

	for i := 0; ; i++ {
		prefix := "mcp_servers." + strconv.Itoa(i)
		name := cfg.GetString(prefix + ".name")
		if name == "" {
			break
		}

		if !cfg.GetBool(prefix + ".enabled") {
			continue
		}

		mtype := cfg.GetString(prefix + ".type")

		// Builtin MCP tools run in-process without a subprocess.
		if mtype == "builtin" {
			handler, ok := builtinHandlers[name]
			if !ok {
				logger.Warn("unknown builtin MCP server", zap.String("name", name))
				continue
			}
			reg.RegisterBuiltin(name, builtinDescs[name], builtinSchemas[name], handler)
			logger.Info("registered builtin MCP server", zap.String("name", name))
			continue
		}

		switch mtype {
		case "url":
			url := cfg.GetString(prefix + ".url")
			if url == "" {
				logger.Warn("mcp server missing url", zap.String("name", name))
				continue
			}
			client := mcp.NewClient(url)
			defs, err := client.List(context.Background())
			if err != nil {
				logger.Warn("mcp server connect failed", zap.String("name", name), zap.String("url", url), zap.Error(err))
				continue
			}
			reg.AddSource(client)
			logger.Info("loaded MCP server", zap.String("name", name), zap.String("url", url), zap.Int("tools", len(defs)))

		case "stdio":
			cmd := cfg.GetString(prefix + ".command")
			if cmd == "" {
				logger.Warn("mcp server missing command", zap.String("name", name))
				continue
			}
			var args []string
			for j := 0; ; j++ {
				a := cfg.GetString(prefix + ".args." + strconv.Itoa(j))
				if a == "" {
					break
				}
				args = append(args, a)
			}
			client, err := mcp.NewStdioClient(context.Background(), cmd, args)
			if err != nil {
				logger.Warn("mcp stdio server start failed", zap.String("name", name), zap.String("command", cmd), zap.Error(err))
				continue
			}
			defs, err := client.List(context.Background())
			if err != nil {
				logger.Warn("mcp stdio server list failed", zap.String("name", name), zap.Error(err))
				client.Close()
				continue
			}
			reg.AddSource(client)
			logger.Info("loaded stdio MCP server", zap.String("name", name), zap.String("command", cmd), zap.Int("tools", len(defs)))
		}
	}
}

func loadCatalogFromConfig(cfg *config.Config) ([]tool.CatalogEntry, error) {
	var entries []tool.CatalogEntry
	for i := 0; ; i++ {
		name := cfg.GetString("mcp_catalog." + strconv.Itoa(i) + ".name")
		if name == "" {
			break
		}
		entries = append(entries, tool.CatalogEntry{
			Name:        name,
			Description: cfg.GetString("mcp_catalog." + strconv.Itoa(i) + ".description"),
			URL:         cfg.GetString("mcp_catalog." + strconv.Itoa(i) + ".url"),
		})
	}
	return entries, nil
}

// configListOrString reads a config value that may be a comma-separated string
// or a YAML list (indexed subkeys like key.0, key.1, ...).
func configListOrString(cfg *config.Config, key string) string {
	if v := cfg.GetString(key); v != "" {
		return v
	}
	var parts []string
	for i := 0; ; i++ {
		v := cfg.GetString(key + "." + strconv.Itoa(i))
		if v == "" {
			break
		}
		parts = append(parts, v)
	}
	return strings.Join(parts, ",")
}
