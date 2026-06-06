package dingtalk

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
	"dolphin/internal/i18n"
	transport "dolphin/internal/transport"
	dtmcp "dolphin/internal/transport/dingtalk/mcp"

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
	transport.Register("dingtalk", func(ctx context.Context, cfg map[string]any) (transport.IO, error) {
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

// valOr returns cfg[key] as string, or def if missing.
func valOr(cfg map[string]any, key, def string) string {
	if v, ok := cfg[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
		if arr, ok := v.([]any); ok {
			var parts []string
			for _, item := range arr {
				if s, ok := item.(string); ok {
					parts = append(parts, strings.TrimSpace(s))
				}
			}
			if len(parts) > 0 {
				return strings.Join(parts, ",")
			}
		}
	}
	return def
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
	*transport.SessionHolder
	id             string
	cfg            DingTalkConfig
	logger         *zap.Logger
	agentName      string
	httpClient     *http.Client
	streamCli      *dtclient.StreamClient
	msgChan        chan string
	closeCh        chan struct{}
	wg             sync.WaitGroup
	mu             sync.Mutex
	closed         bool
	allowUsers     []string // glob patterns for allowed sender nicks; nil = deny all
	lastSenderID   string   // set on each incoming message, for session user_id
	lastSenderNick string   // set on each incoming message, for session user_nick
	lastConvID     string   // set on each incoming message, for chat/send API
	ctx            context.Context
	cancel         context.CancelFunc
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
		SessionHolder: transport.NewSessionHolder(nil),
		id:            "dingtalk",
		cfg:           cfg,
		logger:        logger,
		agentName:     agentName,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
		msgChan:       make(chan string, 100),
		closeCh:       make(chan struct{}),
		allowUsers:    allowUsers,
	}

	// Start the DingTalk Stream client if credentials are configured.
	if cfg.ClientID != "" && cfg.ClientSecret != "" {
		dt.startStream()
	}

	return dt
}

func (d *DingTalk) ID() string { return d.id }

func (d *DingTalk) Context() string { return i18n.T("dingtalk.context") }
func (d *DingTalk) Tools() []common.ToolDesc {
	if d.cfg.ClientID == "" || d.cfg.ClientSecret == "" {
		return nil
	}
	return []common.ToolDesc{
		{
			Name:        "dingtalk_file",
			Description: "DingTalk file upload and message tools",
			Executor:    dtmcp.NewFileUploadSource(d.cfg.ClientID, d.cfg.ClientSecret, d.ConversationID, d.logger),
		},
	}
}
func (d *DingTalk) Start(ctx context.Context) error {
	if d.cfg.WebhookURL != "" {
		go d.sendStartupNotification(ctx)
	}
	return nil
}

// Read blocks until a message is received from DingTalk Stream Mode.
func (d *DingTalk) Read(ctx context.Context) (string, error) {
	select {
	case msg := <-d.msgChan:
		return msg, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// UserID returns the DingTalk user ID of the most recent message sender.
func (d *DingTalk) UserID() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastSenderID
}

// UserNick returns the display name of the most recent message sender.
func (d *DingTalk) UserNick() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastSenderNick
}

func (d *DingTalk) ConversationID() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastConvID
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

func (d *DingTalk) Capability() transport.Capability {
	return transport.Capability{
		Interactive:        false,
		Streamable:         false,
		NestRead:           false,
		RenderTextMarkdown: "markdown",
	}
}

func (d *DingTalk) RequestPermission(_ context.Context, _ string) (transport.PermissionResult, error) {
	return transport.PermissionDenied, fmt.Errorf("%s", i18n.T("dingtalk.no_interactive"))
}

// sendStartupNotification sends a one-shot startup message to the DingTalk webhook.
func (d *DingTalk) sendStartupNotification(ctx context.Context) {
	payload := map[string]any{
		"msgtype": "text",
		"text": map[string]string{
			"content": i18n.T("dingtalk.startup_notification"),
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		d.logger.Warn("startup notification marshal error", zap.Error(err))
		return
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, d.cfg.WebhookURL, bytes.NewReader(data))
	if err != nil {
		d.logger.Warn("startup notification request error", zap.Error(err))
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		d.logger.Warn("startup notification send error", zap.Error(err))
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
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
		// Disable SDK internal keepalive pings. The SDK's processLoop has
		// a race condition: when a ping/pong goroutine triggers processLoop
		// exit concurrently with the read goroutine, closeChan can be closed
		// while another goroutine tries to send on it, causing a panic.
		// Disabling keepalive eliminates the pong goroutine entirely, and
		// the only exit path becomes the read goroutine which is safe.
		dtclient.WithKeepAlive(8760 * time.Hour), // ~1 year, effectively off
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
		d.mu.Lock()
		d.lastSenderID = data.SenderId
		d.lastSenderNick = data.SenderNick
		d.lastConvID = data.ConversationId
		d.mu.Unlock()
		msg := strings.TrimSpace(data.Text.Content)
		msg = stripAtMention(msg, d.agentName)
		select {
		case d.msgChan <- msg:
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
	msg := fmt.Sprintf(i18n.T("dingtalk.denied"), nick)
	if len(d.allowUsers) == 0 {
		msg = i18n.T("dingtalk.no_whitelist")
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
	resp, err := d.httpClient.Do(req)
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
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

// stripAtMention strips a leading "@botName" prefix (e.g. "@小海豚 /models" → "/models")
// so slash commands are correctly detected by UserIO. The bot name is not needed —
// any leading @mention is removed.
func stripAtMention(msg, _ string) string {
	if strings.HasPrefix(msg, "@") {
		if idx := strings.IndexAny(msg, " \t\n\r"); idx > 0 {
			msg = strings.TrimSpace(msg[idx:])
		}
	}
	return msg
}

// Ensure DingTalk implements transport.IO.
var _ transport.IO = (*DingTalk)(nil)
