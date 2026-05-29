package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"dolphin/internal/common"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"
	dtclient "github.com/open-dingtalk/dingtalk-stream-sdk-go/client"
	dtlogger "github.com/open-dingtalk/dingtalk-stream-sdk-go/logger"
	"go.uber.org/zap"
)

// stdLogAdapter bridges DingTalk SDK logs to the application logger.
type stdLogAdapter struct {
	logger *zap.Logger
}

func (a *stdLogAdapter) Debugf(format string, args ...any) {}
func (a *stdLogAdapter) Infof(format string, args ...any) {
	a.logger.Info(fmt.Sprintf(format, args...))
}
func (a *stdLogAdapter) Warningf(format string, args ...any) {
	a.logger.Warn(fmt.Sprintf(format, args...))
}
func (a *stdLogAdapter) Errorf(format string, args ...any) {
	a.logger.Warn(fmt.Sprintf(format, args...))
}
func (a *stdLogAdapter) Fatalf(format string, args ...any) {
	a.logger.Error(fmt.Sprintf(format, args...))
}

func init() {
	Register("dingtalk", func(ctx context.Context, cfg map[string]any) (IO, error) {
		logger, _ := cfg["logger"].(*zap.Logger)
		agentName, _ := cfg["agent_name"].(string)
		return NewDingTalk(DingTalkConfig{
			ClientID:     valOr(cfg, "client_id", ""),
			ClientSecret: valOr(cfg, "client_secret", ""),
			WebhookURL:   valOr(cfg, "webhook_url", ""),
			AllowUsers:   valOr(cfg, "allow_users", ""),
		}, logger, agentName), nil
	})
}

// DingTalkConfig holds DingTalk transport configuration.
type DingTalkConfig struct {
	ClientID     string
	ClientSecret string
	WebhookURL   string
	AllowUsers   string // comma-separated list of allowed sender nicknames; empty = allow all
}

// DingTalk is a chunk-mode transport that sends messages via DingTalk webhook
// and receives messages via DingTalk Stream Mode (WebSocket, no public URL needed).
type DingTalk struct {
	id         string
	cfg        DingTalkConfig
	logger     *zap.Logger
	agentName  string
	httpClient *http.Client
	streamCli  *dtclient.StreamClient
	msgChan    chan string
	closeCh    chan struct{}
	wg         sync.WaitGroup
	mu         sync.Mutex
	closed     bool
	allowUsers []string // glob patterns for allowed sender nicks; nil = deny all
	ctx        context.Context
	cancel     context.CancelFunc
}

func NewDingTalk(cfg DingTalkConfig, logger *zap.Logger, agentName string) *DingTalk {
	if logger == nil {
		logger, _ = zap.NewProduction()
	}

	var allowUsers []string
	if cfg.AllowUsers != "" {
		for _, u := range strings.Split(cfg.AllowUsers, ",") {
			u = strings.TrimSpace(u)
			if u != "" {
				allowUsers = append(allowUsers, u)
			}
		}
	}

	dt := &DingTalk{
		id:         "dingtalk",
		cfg:        cfg,
		logger:     logger,
		agentName:  agentName,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		msgChan:    make(chan string, 100),
		closeCh:    make(chan struct{}),
		allowUsers: allowUsers,
	}

	// Start the DingTalk Stream client if credentials are configured.
	if cfg.ClientID != "" && cfg.ClientSecret != "" {
		dt.startStream()
	}

	return dt
}

func (d *DingTalk) ID() string { return d.id }

func (d *DingTalk) Context() string          { return "当前消息来自钉钉群" }
func (d *DingTalk) Tools() []common.ToolDesc { return nil }

// Read blocks until a message is received from DingTalk Stream Mode.
func (d *DingTalk) Read(ctx context.Context) (string, error) {
	select {
	case msg := <-d.msgChan:
		return msg, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Write sends a message to the DingTalk group via robot webhook.
func (d *DingTalk) Write(ctx context.Context, text string) error {
	payload := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": d.agentName,
			"text":  text,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("dingtalk: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.cfg.WebhookURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("dingtalk: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("dingtalk: send: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dingtalk: webhook error (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(body, &result); err == nil && result.ErrCode != 0 {
		return fmt.Errorf("dingtalk: webhook error: %s (code %d)", result.ErrMsg, result.ErrCode)
	}

	return nil
}

func (d *DingTalk) Flush() error { return nil }

func (d *DingTalk) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	d.closed = true
	close(d.closeCh)
	if d.streamCli != nil {
		d.streamCli.Close()
	}
	d.wg.Wait()
	return nil
}

func (d *DingTalk) Capability() Capability {
	return Capability{
		Interactive: false,
		Streamable:  false,
		NestRead:    false,
	}
}

// ---------------------------------------------------------------------------
// DingTalk Stream Mode via official SDK
// ---------------------------------------------------------------------------

func (d *DingTalk) startStream() {
	dtlogger.SetLogger(&stdLogAdapter{logger: d.logger})

	cred := dtclient.NewAppCredentialConfig(d.cfg.ClientID, d.cfg.ClientSecret)
	cli := dtclient.NewStreamClient(
		dtclient.WithAppCredential(cred),
		dtclient.WithAutoReconnect(true),
	)

	cli.RegisterChatBotCallbackRouter(d.handleBotMessage)
	d.streamCli = cli

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()

		// Retry loop: Start blocks until disconnect or error.
		for {
			err := cli.Start(context.Background())
			if err == nil {
				return
			}

			d.logger.Warn("dingtalk stream disconnected (will retry in 5s)", zap.Error(err))

			select {
			case <-d.closeCh:
				return
			case <-time.After(5 * time.Second):
			}
		}
	}()
}

// handleBotMessage processes an incoming bot message with optional sender whitelist check.
func (d *DingTalk) handleBotMessage(ctx context.Context, data *chatbot.BotCallbackDataModel) ([]byte, error) {
	if data.Msgtype == "text" {
		if !d.isSenderAllowed(data.SenderNick) {
			d.rejectMessage(ctx, data.SenderNick)
			return []byte("ok"), nil
		}
		select {
		case d.msgChan <- data.Text.Content:
		default:
			d.logger.Warn("dingtalk msgChan full, dropping message")
		}
	}
	return []byte("ok"), nil
}

// rejectMessage sends a rejection reply to the group via webhook.
func (d *DingTalk) rejectMessage(ctx context.Context, nick string) {
	if d.cfg.WebhookURL == "" {
		return
	}
	msg := "@" + nick + " 抱歉，您没有权限使用此机器人"
	if len(d.allowUsers) == 0 {
		msg = "机器人暂未配置白名单，请联系管理员配置后使用"
	}
	payload := map[string]any{
		"msgtype": "text",
		"text": map[string]string{
			"content": msg,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.cfg.WebhookURL, bytes.NewReader(data))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	d.httpClient.Do(req)
}

// isSenderAllowed checks whether a sender nickname matches any whitelist pattern.
// Patterns support glob wildcards (*, ?). Empty list means deny all.
func (d *DingTalk) isSenderAllowed(nick string) bool {
	if len(d.allowUsers) == 0 {
		return false
	}
	for _, pattern := range d.allowUsers {
		if ok, _ := path.Match(pattern, nick); ok {
			return true
		}
	}
	return false
}

// Ensure DingTalk implements IO.
var _ IO = (*DingTalk)(nil)
