// Package dingtalk provides DingTalk bot stream transport.
package dingtalk

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"dolphin/internal/config"
	transport "dolphin/internal/transport"

	"github.com/open-dingtalk/dingtalk-stream-sdk-go/chatbot"
	"github.com/open-dingtalk/dingtalk-stream-sdk-go/client"
	"go.uber.org/zap"
)

func init() { transport.Register("dingtalk", New) }

// DingTalkTransport provides DingTalk bot I/O via Stream mode using the official SDK.
type DingTalkTransport struct {
	cfg       *config.DingTalkConfig
	msgCh     chan string
	closeCh   chan struct{}
	closeOnce sync.Once

	sdkCli    *client.StreamClient
	webhook   string
	webhookMu sync.RWMutex
}

func New(cfg *config.Config) (transport.Transport, error) {
	return &DingTalkTransport{
		cfg:     &cfg.Transport.DingTalk,
		msgCh:   make(chan string, 1024),
		closeCh: make(chan struct{}),
	}, nil
}

func (t *DingTalkTransport) Name() string { return "dingtalk" }

func (t *DingTalkTransport) Banner() string {
	return "  DingTalk bot active (Stream mode)\n"
}

func (t *DingTalkTransport) Context() string {
	return "Connected via DingTalk bot (Stream mode). " +
		"The user is on a mobile device. Keep responses concise."
}

func (t *DingTalkTransport) Capabilities() transport.Capabilities {
	return transport.Capabilities{Streaming: false}
}

func (t *DingTalkTransport) Start(ctx context.Context) error {
	transport.ActiveConnections.Add(1)
	defer transport.ActiveConnections.Add(-1)

	cred := client.NewAppCredentialConfig(t.cfg.ClientID, t.cfg.ClientSecret)
	t.sdkCli = client.NewStreamClient(
		client.WithAppCredential(cred),
	)
	t.sdkCli.RegisterChatBotCallbackRouter(t.onMessage)

	if err := t.sdkCli.Start(ctx); err != nil {
		return fmt.Errorf("dingtalk stream start: %w", err)
	}
	zap.S().Infow("dingtalk stream connected")

	<-ctx.Done()
	return t.Close()
}

func (t *DingTalkTransport) onMessage(ctx context.Context, data *chatbot.BotCallbackDataModel) ([]byte, error) {
	if data.SessionWebhook != "" {
		t.webhookMu.Lock()
		t.webhook = data.SessionWebhook
		t.webhookMu.Unlock()
	}

	msgText := data.Text.Content
	if msgText == "" {
		return nil, nil
	}

	zap.S().Infow("dingtalk message received", "sender", data.SenderNick, "len", len(msgText))

	select {
	case t.msgCh <- msgText:
		transport.MsgsReceived.Inc()
	default:
		zap.S().Warnw("dingtalk message dropped, channel full")
	}

	return nil, nil
}

func (t *DingTalkTransport) ReadLine() (string, error) {
	select {
	case msg, ok := <-t.msgCh:
		if !ok {
			return "", fmt.Errorf("dingtalk transport closed")
		}
		return msg, nil
	case <-t.closeCh:
		return "", fmt.Errorf("dingtalk transport closed")
	case <-time.After(5 * time.Minute):
		return "", fmt.Errorf("dingtalk: read timeout (5m)")
	}
}

func (t *DingTalkTransport) WriteLine(s string) error {
	return t.sendMessage(s)
}

func (t *DingTalkTransport) WriteString(s string) error {
	return t.sendMessage(s)
}

func (t *DingTalkTransport) Flush() error { return nil }

func (t *DingTalkTransport) sendMessage(body string) error {
	transport.MsgsSent.Inc()

	t.webhookMu.RLock()
	webhook := t.webhook
	t.webhookMu.RUnlock()

	if webhook == "" {
		return fmt.Errorf("dingtalk: no session webhook — wait for a user to @ the bot first")
	}

	replier := chatbot.NewChatbotReplier()

	if isMarkdownContent(body) {
		if err := replier.SimpleReplyMarkdown(context.Background(), webhook, []byte("Dolphin"), []byte(body)); err != nil {
			return fmt.Errorf("dingtalk markdown reply: %w", err)
		}
	} else {
		if err := replier.SimpleReplyText(context.Background(), webhook, []byte(body)); err != nil {
			return fmt.Errorf("dingtalk reply: %w", err)
		}
	}

	zap.S().Debugw("dingtalk message sent", "len", len(body))
	return nil
}

func isMarkdownContent(s string) bool {
	markdownIndicators := []string{
		"# ", "**", "```", "`", "- ", "* ", "1. ", "> ", "---", "[](",
	}
	for _, indicator := range markdownIndicators {
		if strings.Contains(s, indicator) {
			return true
		}
	}
	return false
}

func (t *DingTalkTransport) Close() error {
	t.closeOnce.Do(func() {
		close(t.closeCh)
		if t.sdkCli != nil {
			t.sdkCli.Close()
		}
	})
	return nil
}
