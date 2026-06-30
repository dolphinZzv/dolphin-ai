package wework

import (
	"context"
	"crypto/md5" //nolint:gosec // G501: WeWork upload API requires md5 file digest
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"dolphin/internal/common"
	"dolphin/internal/i18n"
	transport "dolphin/internal/transport"
	wemcp "dolphin/internal/transport/wework/mcp"
)

type wsConn interface {
	ReadMessage() (int, []byte, error)
	WriteMessage(int, []byte) error
	Close() error
	SetWriteDeadline(time.Time) error
}

const (
	wssURL             = "wss://openws.work.weixin.qq.com"
	heartbeatInterval  = 30 * time.Second
	reconnectBaseDelay = 1 * time.Second
	reconnectMaxDelay  = 30 * time.Second
)

func init() {
	transport.Register("wework", func(ctx context.Context, cfg map[string]any) (transport.IO, error) {
		logger, _ := cfg["logger"].(*zap.Logger)
		agentName, _ := cfg["agent_name"].(string)
		return NewWeWork(WeWorkConfig{
			BotID:      valOr(cfg, "bot_id", ""),
			Secret:     valOr(cfg, "bot_secret", ""),
			AllowUsers: valOr(cfg, "allow_users", ""),
		}, logger, agentName), nil
	})
}

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

// WeWorkConfig holds enterprise wechat smart bot configuration.
type WeWorkConfig struct {
	BotID      string
	Secret     string
	AllowUsers string // comma-separated list of allowed user IDs; empty = deny all
}

// WeWork is a transport that connects to WeWork Smart Bot via WebSocket (WSS).
// It receives messages and sends responses over a persistent long connection.
type WeWork struct {
	*transport.SessionHolder
	id        string
	cfg       WeWorkConfig
	logger    *zap.Logger
	agentName string

	conn   wsConn
	connMu sync.Mutex

	writeMu sync.Mutex

	msgChan chan string
	closeCh chan struct{}
	wg      sync.WaitGroup

	stateMu      sync.Mutex
	lastReqID    string
	lastChatID   string
	lastChatType int // 0=auto, 1=single, 2=group
	lastUserID   string

	allowUsers []string // glob patterns for allowed user IDs; empty = deny all

	pendingMu   sync.Mutex
	pendingResp map[string]chan []byte
}

func NewWeWork(cfg WeWorkConfig, logger *zap.Logger, agentName string) *WeWork {
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

	w := &WeWork{
		SessionHolder: transport.NewSessionHolder(nil),
		id:            "wework",
		cfg:           cfg,
		logger:        logger,
		agentName:     agentName,
		allowUsers:    allowUsers,
		msgChan:       make(chan string, 100),
		closeCh:       make(chan struct{}),
		pendingResp:   make(map[string]chan []byte),
	}

	w.wg.Add(1)
	go w.run()

	return w
}

func (w *WeWork) ID() string { return w.id }

func (w *WeWork) Start(ctx context.Context) error { return nil }
func (w *WeWork) Context() string {
	return i18n.T("wework.context")
}

func (w *WeWork) Tools() []common.ToolDesc {
	if w.cfg.BotID == "" || w.cfg.Secret == "" {
		return nil
	}
	return []common.ToolDesc{
		{
			Name:        "wework_mcp",
			Description: "WeWork built-in MCP tools",
			Executor:    wemcp.NewSource(w, w.cfg.BotID, w.cfg.Secret, w.logger),
		},
	}
}

// run is the main reconnection loop.
func (w *WeWork) run() {
	defer w.wg.Done()

	delay := reconnectBaseDelay
	for {
		select {
		case <-w.closeCh:
			return
		default:
		}

		err := w.connectAndServe()
		if err == nil {
			return
		}

		w.logger.Warn("wework disconnected", zap.Error(err))

		select {
		case <-w.closeCh:
			return
		case <-time.After(delay):
		}

		delay *= 2
		if delay > reconnectMaxDelay {
			delay = reconnectMaxDelay
		}
	}
}

// connectAndServe connects, subscribes, and enters the read loop.
func (w *WeWork) connectAndServe() error {
	conn, _, err := websocket.DefaultDialer.DialContext(context.Background(), wssURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	w.connMu.Lock()
	if w.conn != nil {
		_ = w.conn.Close()
	}
	w.conn = conn
	w.connMu.Unlock()

	if err := w.subscribe(conn); err != nil {
		_ = conn.Close()
		return fmt.Errorf("subscribe: %w", err)
	}

	w.logger.Info("wework smart bot connected")

	// Heartbeat goroutine.
	heartbeatStop := make(chan struct{})
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				w.sendPing()
			case <-heartbeatStop:
				return
			case <-w.closeCh:
				return
			}
		}
	}()
	defer close(heartbeatStop)

	// Read loop.
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		// Route to pending response handler if this is a response to a request.
		if w.tryDeliverPending(message) {
			continue
		}

		var frame struct {
			Cmd string `json:"cmd"`
		}
		if err := json.Unmarshal(message, &frame); err != nil {
			w.logger.Warn("wework decode frame error", zap.Error(err))
			continue
		}

		switch frame.Cmd {
		case "aibot_msg_callback":
			w.handleMessageCallback(message)
		case "aibot_event_callback":
			w.handleEventCallback(message)
		case "pong":
			// heartbeat response
		default:
			w.logger.Debug("wework unknown cmd", zap.String("cmd", frame.Cmd))
		}
	}
}

// subscribe sends aibot_subscribe to authenticate.
func (w *WeWork) subscribe(conn wsConn) error {
	payload := map[string]any{
		"cmd": "aibot_subscribe",
		"headers": map[string]string{
			"req_id": fmt.Sprintf("sub_%d", time.Now().UnixNano()),
		},
		"body": map[string]string{
			"bot_id": w.cfg.BotID,
			"secret": w.cfg.Secret,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("write subscribe: %w", err)
	}

	_, resp, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read subscribe response: %w", err)
	}

	var sr struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(resp, &sr); err != nil {
		return fmt.Errorf("decode subscribe response: %w", err)
	}
	if sr.ErrCode != 0 {
		return fmt.Errorf("subscribe error (code %d): %s", sr.ErrCode, sr.ErrMsg)
	}
	return nil
}

// msgCallback represents an incoming aibot_msg_callback frame.
type msgCallback struct {
	Headers struct {
		ReqID string `json:"req_id"`
	} `json:"headers"`
	Body struct {
		MsgID    string `json:"msgid"`
		ChatID   string `json:"chatid"`
		ChatType string `json:"chattype"`
		From     struct {
			UserID string `json:"userid"`
		} `json:"from"`
		MsgType string `json:"msgtype"`
		Text    *struct {
			Content string `json:"content"`
		} `json:"text,omitempty"`
	} `json:"body"`
}

// handleMessageCallback processes an incoming message and pushes it to msgChan.
func (w *WeWork) handleMessageCallback(data []byte) {
	var cb msgCallback
	if err := json.Unmarshal(data, &cb); err != nil {
		w.logger.Warn("wework decode msg callback error", zap.Error(err))
		return
	}

	if cb.Body.MsgType != "text" || cb.Body.Text == nil {
		return
	}

	// Reject unauthorized users.
	if !w.isSenderAllowed(cb.Body.From.UserID) {
		w.logger.Warn("wework unauthorized user", zap.String("userid", cb.Body.From.UserID))
		w.rejectMessage(cb.Headers.ReqID, cb.Body.From.UserID)
		return
	}

	chatType := 0
	switch cb.Body.ChatType {
	case "single":
		chatType = 1
	case "group":
		chatType = 2
	}

	chatID := cb.Body.ChatID
	if cb.Body.ChatType == "single" {
		chatID = cb.Body.From.UserID
	}

	w.stateMu.Lock()
	w.lastReqID = cb.Headers.ReqID
	w.lastChatID = chatID
	w.lastChatType = chatType
	w.lastUserID = cb.Body.From.UserID
	w.stateMu.Unlock()

	msg := strings.TrimSpace(cb.Body.Text.Content)
	msg = stripAtMention(msg, w.agentName)

	select {
	case w.msgChan <- msg:
	default:
		w.logger.Warn("wework msgChan full, dropping message")
	}
}

// handleEventCallback processes events from the server.
func (w *WeWork) handleEventCallback(data []byte) {
	var ev struct {
		Body struct {
			Event struct {
				EventType string `json:"eventtype"`
			} `json:"event"`
		} `json:"body"`
	}
	if err := json.Unmarshal(data, &ev); err != nil {
		return
	}
	if ev.Body.Event.EventType == "disconnected_event" {
		w.logger.Warn("wework disconnected by another connection")
	}
}

// rejectMessage sends a rejection text reply via aibot_respond_msg.
func (w *WeWork) rejectMessage(reqID, userID string) {
	if reqID == "" {
		return
	}
	msg := fmt.Sprintf(i18n.T("wework.denied"), userID)
	payload := map[string]any{
		"cmd": "aibot_respond_msg",
		"headers": map[string]string{
			"req_id": reqID,
		},
		"body": map[string]any{
			"msgtype": "markdown",
			"markdown": map[string]string{
				"content": msg,
			},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	w.writeMu.Lock()
	w.connMu.Lock()
	conn := w.conn
	w.connMu.Unlock()
	if conn != nil {
		_ = conn.WriteMessage(websocket.TextMessage, data)
	}
	w.writeMu.Unlock()
}

// isSenderAllowed checks whether a user ID matches any whitelist pattern.

// Patterns support glob wildcards (*, ?). Empty list means deny all.
func (w *WeWork) isSenderAllowed(userID string) bool {
	if len(w.allowUsers) == 0 {
		return false
	}
	for _, pattern := range w.allowUsers {
		if ok, _ := path.Match(pattern, userID); ok {
			return true
		}
	}
	return false
}

// sendPing sends a heartbeat ping over the WebSocket.
func (w *WeWork) sendPing() {
	payload := map[string]any{
		"cmd": "ping",
		"headers": map[string]string{
			"req_id": fmt.Sprintf("ping_%d", time.Now().UnixNano()),
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	w.connMu.Lock()
	conn := w.conn
	w.connMu.Unlock()
	if conn == nil {
		return
	}
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_ = conn.WriteMessage(websocket.TextMessage, data)
	_ = conn.SetWriteDeadline(time.Time{})
}

// ---------------------------------------------------------------------------
// Request/Response correlation for upload operations.
// ---------------------------------------------------------------------------

// tryDeliverPending routes an incoming message to a pending response channel if
// its req_id matches an outstanding request.
func (w *WeWork) tryDeliverPending(data []byte) bool {
	var m struct {
		Headers struct {
			ReqID string `json:"req_id"`
		} `json:"headers"`
	}
	if err := json.Unmarshal(data, &m); err != nil || m.Headers.ReqID == "" {
		return false
	}

	w.pendingMu.Lock()
	ch, ok := w.pendingResp[m.Headers.ReqID]
	if ok {
		delete(w.pendingResp, m.Headers.ReqID)
	}
	w.pendingMu.Unlock()

	if !ok {
		return false
	}

	select {
	case ch <- data:
	default:
	}
	close(ch)
	return true
}

// sendAndWait sends a message and blocks until a matching response arrives.
func (w *WeWork) sendAndWait(data []byte, timeout time.Duration) ([]byte, error) {
	var tmp struct {
		Headers struct {
			ReqID string `json:"req_id"`
		} `json:"headers"`
	}
	if err := json.Unmarshal(data, &tmp); err != nil || tmp.Headers.ReqID == "" {
		return nil, fmt.Errorf("sendAndWait: missing req_id")
	}
	reqID := tmp.Headers.ReqID

	ch := make(chan []byte, 1)
	w.pendingMu.Lock()
	w.pendingResp[reqID] = ch
	w.pendingMu.Unlock()

	cleanup := func() {
		w.pendingMu.Lock()
		delete(w.pendingResp, reqID)
		w.pendingMu.Unlock()
	}

	w.writeMu.Lock()
	w.connMu.Lock()
	conn := w.conn
	w.connMu.Unlock()
	if conn == nil {
		w.writeMu.Unlock()
		cleanup()
		return nil, fmt.Errorf("sendAndWait: not connected")
	}
	err := conn.WriteMessage(websocket.TextMessage, data)
	w.writeMu.Unlock()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("sendAndWait: send: %w", err)
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("sendAndWait: channel closed")
		}
		return resp, nil
	case <-time.After(timeout):
		cleanup()
		return nil, fmt.Errorf("sendAndWait: timeout after %v", timeout)
	}
}

// ---------------------------------------------------------------------------
// WebSocket-based file upload (aibot_upload_media_init / chunk / finish).
// ---------------------------------------------------------------------------

// UploadMedia uploads a file using the WebSocket chunked upload protocol and
// returns the media_id, file name, and media type.
func (w *WeWork) UploadMedia(ctx context.Context, filePath string) (mediaID, fileName, mediaType string, err error) {
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return "", "", "", fmt.Errorf("read file: %w", err)
	}

	fileName = filepath.Base(filePath)
	ext := strings.ToLower(filepath.Ext(fileName))
	mediaType = "file"
	if ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif" || ext == ".webp" || ext == ".bmp" {
		mediaType = "image"
	}

	totalSize := len(fileData)
	chunkSize := 512 * 1024
	totalChunks := (totalSize + chunkSize - 1) / chunkSize
	md5Hash := md5.Sum(fileData) //nolint:gosec // G401: WeWork upload API requires md5 digest
	md5Str := fmt.Sprintf("%x", md5Hash)

	w.logger.Info("wework upload start",
		zap.String("file", fileName),
		zap.Int("size", totalSize),
		zap.Int("chunks", totalChunks),
		zap.String("type", mediaType),
	)

	uploadTimeout := 30 * time.Second

	initData, _ := json.Marshal(map[string]any{
		"cmd": "aibot_upload_media_init",
		"headers": map[string]string{
			"req_id": fmt.Sprintf("ul_init_%d", time.Now().UnixNano()),
		},
		"body": map[string]any{
			"type":         mediaType,
			"filename":     fileName,
			"total_size":   totalSize,
			"total_chunks": totalChunks,
			"md5":          md5Str,
		},
	})
	resp, err := w.sendAndWait(initData, uploadTimeout)
	if err != nil {
		return "", "", "", fmt.Errorf("upload init: %w", err)
	}
	var initResult struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		Body    struct {
			UploadID string `json:"upload_id"`
		} `json:"body"`
	}
	if err := json.Unmarshal(resp, &initResult); err != nil {
		return "", "", "", fmt.Errorf("decode init response: %w", err)
	}
	if initResult.ErrCode != 0 {
		return "", "", "", fmt.Errorf("upload init error (code %d): %s", initResult.ErrCode, initResult.ErrMsg)
	}
	uploadID := initResult.Body.UploadID
	w.logger.Info("wework upload init ok", zap.String("upload_id", uploadID))

	for i := 0; i < totalChunks; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > totalSize {
			end = totalSize
		}
		b64Chunk := base64.StdEncoding.EncodeToString(fileData[start:end])

		chunkData, _ := json.Marshal(map[string]any{
			"cmd": "aibot_upload_media_chunk",
			"headers": map[string]string{
				"req_id": fmt.Sprintf("ul_chunk_%d_%d", i, time.Now().UnixNano()),
			},
			"body": map[string]any{
				"upload_id":   uploadID,
				"chunk_index": i,
				"base64_data": b64Chunk,
			},
		})
		resp, err := w.sendAndWait(chunkData, uploadTimeout)
		if err != nil {
			return "", "", "", fmt.Errorf("upload chunk %d: %w", i, err)
		}
		var chunkResult struct {
			ErrCode int    `json:"errcode"`
			ErrMsg  string `json:"errmsg"`
		}
		if err := json.Unmarshal(resp, &chunkResult); err != nil {
			return "", "", "", fmt.Errorf("decode chunk %d response: %w", i, err)
		}
		if chunkResult.ErrCode != 0 {
			return "", "", "", fmt.Errorf("upload chunk %d error (code %d): %s", i, chunkResult.ErrCode, chunkResult.ErrMsg)
		}
	}
	w.logger.Info("wework upload chunks done", zap.Int("total", totalChunks))

	finishData, _ := json.Marshal(map[string]any{
		"cmd": "aibot_upload_media_finish",
		"headers": map[string]string{
			"req_id": fmt.Sprintf("ul_finish_%d", time.Now().UnixNano()),
		},
		"body": map[string]any{
			"upload_id": uploadID,
		},
	})
	resp, err = w.sendAndWait(finishData, uploadTimeout)
	if err != nil {
		return "", "", "", fmt.Errorf("upload finish: %w", err)
	}
	var finishResult struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		Body    struct {
			Type    string `json:"type"`
			MediaID string `json:"media_id"`
		} `json:"body"`
	}
	if err := json.Unmarshal(resp, &finishResult); err != nil {
		return "", "", "", fmt.Errorf("decode finish response: %w", err)
	}
	if finishResult.ErrCode != 0 {
		return "", "", "", fmt.Errorf("upload finish error (code %d): %s", finishResult.ErrCode, finishResult.ErrMsg)
	}

	return finishResult.Body.MediaID, fileName, finishResult.Body.Type, nil
}

// SendMediaMessage sends an uploaded image or file to the conversation.
//   - Images: use aibot_respond_msg (callback reply, natively supported)
//   - Files:  use aibot_send_msg (proactive push, supported since 2026/03 for
//     long-connection Smart Bots; aibot_respond_msg does NOT support file type)
func (w *WeWork) SendMediaMessage(ctx context.Context, mediaID, mediaType string) error {
	if mediaType == "image" {
		return w.sendImageMessage(ctx, mediaID)
	}
	return w.sendFileMessage(ctx, mediaID)
}

// sendImageMessage replies with an image via aibot_respond_msg, then clears
// lastReqID so Write() uses aibot_send_msg for the text reply.
func (w *WeWork) sendImageMessage(ctx context.Context, mediaID string) error {
	w.stateMu.Lock()
	reqID := w.lastReqID
	w.stateMu.Unlock()
	if reqID == "" {
		return fmt.Errorf("wework: no callback context to respond with image")
	}

	payload := map[string]any{
		"cmd":     "aibot_respond_msg",
		"headers": map[string]string{"req_id": reqID},
		"body": map[string]any{
			"msgtype": "image",
			"image":   map[string]string{"media_id": mediaID},
		},
	}
	data, _ := json.Marshal(payload)
	w.logger.Info("wework send image via respond_msg",
		zap.String("media_id", mediaID),
		zap.String("req_id", reqID),
	)

	w.writeMu.Lock()
	w.connMu.Lock()
	conn := w.conn
	w.connMu.Unlock()
	if conn == nil {
		w.writeMu.Unlock()
		return fmt.Errorf("wework: not connected")
	}
	err := conn.WriteMessage(websocket.TextMessage, data)
	w.writeMu.Unlock()

	// Clear lastReqID so Write() uses aibot_send_msg and does not interfere.
	w.stateMu.Lock()
	w.lastReqID = ""
	w.stateMu.Unlock()

	return err
}

// sendFileMessage proactively pushes a file via aibot_send_msg.
// aibot_respond_msg does NOT support msgtype:file for Smart Bots.
func (w *WeWork) sendFileMessage(ctx context.Context, mediaID string) error {
	w.stateMu.Lock()
	chatID := w.lastChatID
	chatType := w.lastChatType
	w.stateMu.Unlock()
	if chatID == "" {
		return fmt.Errorf("wework: no chat session available for sending file")
	}

	payload := map[string]any{
		"cmd": "aibot_send_msg",
		"headers": map[string]string{
			"req_id": fmt.Sprintf("send_file_%d", time.Now().UnixNano()),
		},
		"body": map[string]any{
			"chatid":    chatID,
			"chat_type": chatType,
			"msgtype":   "file",
			"file":      map[string]string{"media_id": mediaID},
		},
	}
	data, _ := json.Marshal(payload)
	w.logger.Info("wework send file via send_msg",
		zap.String("media_id", mediaID),
		zap.String("chatid", chatID),
		zap.Int("chat_type", chatType),
	)

	w.writeMu.Lock()
	w.connMu.Lock()
	conn := w.conn
	w.connMu.Unlock()
	if conn == nil {
		w.writeMu.Unlock()
		return fmt.Errorf("wework: not connected")
	}
	err := conn.WriteMessage(websocket.TextMessage, data)
	w.writeMu.Unlock()
	return err
}

// ProactiveMessage sends a text or markdown message proactively via aibot_send_msg.
func (w *WeWork) ProactiveMessage(ctx context.Context, content, msgType string) error {
	w.stateMu.Lock()
	chatID := w.lastChatID
	chatType := w.lastChatType
	w.stateMu.Unlock()
	if chatID == "" {
		return fmt.Errorf("wework: no chat session available for sending message")
	}

	reqID := fmt.Sprintf("send_msg_%d", time.Now().UnixNano())

	var payload map[string]any
	if msgType == "text" {
		payload = map[string]any{
			"cmd":     "aibot_send_msg",
			"headers": map[string]string{"req_id": reqID},
			"body": map[string]any{
				"chatid":    chatID,
				"chat_type": chatType,
				"msgtype":   "text",
				"text":      map[string]string{"content": content},
			},
		}
	} else {
		content = stripImageMarkdown(content)
		payload = map[string]any{
			"cmd":     "aibot_send_msg",
			"headers": map[string]string{"req_id": reqID},
			"body": map[string]any{
				"chatid":    chatID,
				"chat_type": chatType,
				"msgtype":   "markdown",
				"markdown":  map[string]string{"content": content},
			},
		}
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	w.writeMu.Lock()
	w.connMu.Lock()
	conn := w.conn
	w.connMu.Unlock()
	if conn == nil {
		w.writeMu.Unlock()
		return fmt.Errorf("wework: not connected")
	}
	err = conn.WriteMessage(websocket.TextMessage, data)
	w.writeMu.Unlock()
	return err
}

// Read blocks until a message is received from WeWork Smart Bot.
func (w *WeWork) Read(ctx context.Context) (transport.Input, error) {
	select {
	case msg := <-w.msgChan:
		return transport.Input{Text: msg}, nil
	case <-w.closeCh:
		return transport.Input{}, fmt.Errorf("wework: closed")
	case <-ctx.Done():
		return transport.Input{}, ctx.Err()
	}
}

// Write sends a markdown message to the WeWork chat.
func (w *WeWork) Write(ctx context.Context, text string) error {
	// Strip image markdown — WeWork markdown does not render inline images.
	text = stripImageMarkdown(text)

	w.stateMu.Lock()
	chatID := w.lastChatID
	chatType := w.lastChatType
	reqID := w.lastReqID
	w.stateMu.Unlock()

	if chatID == "" {
		return fmt.Errorf("wework: no chat session available")
	}

	// Use aibot_respond_msg when replying to a callback, aibot_send_msg for proactive pushes.
	cmd := "aibot_send_msg"
	respReqID := reqID
	if respReqID != "" {
		cmd = "aibot_respond_msg"
	} else {
		respReqID = fmt.Sprintf("send_%d", time.Now().UnixNano())
	}

	payload := map[string]any{
		"cmd": cmd,
		"headers": map[string]string{
			"req_id": respReqID,
		},
		"body": map[string]any{
			"msgtype": "markdown",
			"markdown": map[string]string{
				"content": text,
			},
		},
	}

	if cmd == "aibot_send_msg" {
		body := payload["body"].(map[string]any)
		body["chatid"] = chatID
		body["chat_type"] = chatType
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	w.connMu.Lock()
	conn := w.conn
	w.connMu.Unlock()
	if conn == nil {
		return fmt.Errorf("wework: not connected")
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}

// stripImageMarkdown removes ![alt](url) syntax since WeWork markdown does not support inline images.
func stripImageMarkdown(text string) string {
	var result strings.Builder
	result.Grow(len(text))
	for i := 0; i < len(text); i++ {
		if text[i] == '!' && i+1 < len(text) && text[i+1] == '[' {
			end := strings.Index(text[i+2:], "](http")
			if end >= 0 {
				closeParen := strings.IndexByte(text[i+2+end+7:], ')')
				if closeParen >= 0 {
					i += 2 + end + 7 + closeParen + 1
					continue
				}
			}
			end = strings.Index(text[i+2:], "](")
			if end >= 0 {
				closeParen := strings.IndexByte(text[i+2+end+2:], ')')
				if closeParen >= 0 {
					i += 2 + end + 2 + closeParen + 1
					continue
				}
			}
		}
		result.WriteByte(text[i])
	}
	return result.String()
}

// Flush is a no-op for WeWork.
func (w *WeWork) Flush() error { return nil }

// Close shuts down the transport.
func (w *WeWork) Close() error {
	select {
	case <-w.closeCh:
		return nil
	default:
		close(w.closeCh)
	}

	w.connMu.Lock()
	if w.conn != nil {
		_ = w.conn.Close()
		w.conn = nil
	}
	w.connMu.Unlock()

	w.wg.Wait()
	return nil
}

func (w *WeWork) Capability() transport.Capability {
	return transport.Capability{
		Interactive:        false,
		Streamable:         false,
		NestRead:           false,
		RenderTextMarkdown: "markdown",
	}
}

func (w *WeWork) RequestPermission(_ context.Context, _ string) (transport.PermissionResult, error) {
	return transport.PermissionDenied, fmt.Errorf("%s", i18n.T("wework.no_interactive"))
}

func (w *WeWork) Confirm(_ context.Context, _ string) (bool, error) {
	return false, fmt.Errorf("%s", i18n.T("wework.no_interactive"))
}

// UserID returns the WeWork user ID of the most recent message sender.
func (w *WeWork) UserID() string {
	w.stateMu.Lock()
	defer w.stateMu.Unlock()
	return w.lastUserID
}

// UserNick is not available in Smart Bot callbacks.
func (w *WeWork) UserNick() string { return "" }

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

// Ensure WeWork implements transport.IO.
var _ transport.IO = (*WeWork)(nil)
