package limit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"

	"dolphin/internal/event"
)

// WebhookType defines the message format for the webhook notifier.
type WebhookType string

const (
	WebhookHTTP     WebhookType = "http"     // generic JSON POST
	WebhookDingTalk WebhookType = "dingtalk" // DingTalk robot markdown
	WebhookWeWork   WebhookType = "wework"   // WeCom robot markdown
)

// WebhookNotifier sends alert messages when limit events are triggered.
type WebhookNotifier struct {
	typ    WebhookType
	url    string
	client *http.Client
	logger *zap.Logger
}

// NewWebhookNotifier creates a WebhookNotifier.
func NewWebhookNotifier(webhookType WebhookType, url string, logger *zap.Logger) *WebhookNotifier {
	return &WebhookNotifier{
		typ: webhookType,
		url: url,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

// Handle processes limit events and sends webhooks asynchronously.
func (w *WebhookNotifier) Handle(ctx context.Context, e event.Event) {
	switch e.Type {
	case event.EventLimitSoftWarn, event.EventLimitHardBlock:
		go w.send(ctx, e) //nolint:contextcheck
	}
}

func (w *WebhookNotifier) send(ctx context.Context, e event.Event) {
	body := w.formatMessage(e)
	data, err := json.Marshal(body)
	if err != nil {
		w.logger.Error("webhook: marshal failed", zap.Error(err))
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(data))
	if err != nil {
		w.logger.Warn("webhook: build request failed", zap.Error(err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client.Do(req)
	if err != nil {
		w.logger.Warn("webhook: send failed", zap.Error(err))
		return
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()

	if resp.StatusCode >= 300 {
		w.logger.Warn("webhook: non-2xx response",
			zap.Int("status", resp.StatusCode),
			zap.Any("body", result),
		)
		return
	}

	// DingTalk / WeWork return 200 with errcode in body.
	if errcode, _ := result["errcode"].(float64); errcode != 0 {
		errmsg, _ := result["errmsg"].(string)
		w.logger.Warn("webhook: api error",
			zap.Float64("errcode", errcode),
			zap.String("errmsg", errmsg),
		)
		return
	}

	w.logger.Debug("webhook: sent successfully",
		zap.String("type", string(e.Type)),
	)
}

func (w *WebhookNotifier) formatMessage(e event.Event) any {
	switch w.typ {
	case WebhookDingTalk:
		return w.formatDingTalk(e)
	case WebhookWeWork:
		return w.formatWeWork(e)
	default:
		return w.formatGeneric(e)
	}
}

func (w *WebhookNotifier) formatGeneric(e event.Event) map[string]any {
	return map[string]any{
		"type":       string(e.Type),
		"session_id": e.SessionID,
		"timestamp":  e.Timestamp.Format(time.RFC3339),
		"payload":    e.Payload,
	}
}

func (w *WebhookNotifier) formatDingTalk(e event.Event) map[string]any {
	text := w.formatMarkdownText(e)
	return map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": "限流告警",
			"text":  text,
		},
	}
}

func (w *WebhookNotifier) formatWeWork(e event.Event) map[string]any {
	text := w.formatMarkdownText(e)
	return map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"content": text,
		},
	}
}

func (w *WebhookNotifier) formatMarkdownText(e event.Event) string {
	payload := e.Payload
	metric, _ := payload["metric"].(string)
	current := toInt64(payload["current"])
	soft := toInt64(payload["soft"])
	hard := toInt64(payload["hard"])
	model, _ := payload["model"].(string)
	eventType := friendlyName(e.Type)

	s := fmt.Sprintf("## 限流告警\n\n**类型**: %s\n**指标**: %s\n**当前值**: %d", eventType, metric, current)
	if model != "" {
		s += fmt.Sprintf("\n**模型**: %s", model)
	}
	if soft > 0 {
		s += fmt.Sprintf("\n**软限**: %d", soft)
	}
	if hard > 0 {
		s += fmt.Sprintf("\n**硬限**: %d", hard)
	}
	s += fmt.Sprintf("\n**会话**: %s\n**时间**: %s", e.SessionID, e.Timestamp.Format(time.RFC3339))

	return s
}

// toInt64 coerces any numeric type (int, int64, float64) to int64.
func toInt64(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case float64:
		return int64(x)
	}
	return 0
}

func friendlyName(t event.Type) string {
	switch t {
	case event.EventLimitSoftWarn:
		return "软限告警"
	case event.EventLimitHardBlock:
		return "硬限阻断"
	default:
		return string(t)
	}
}
