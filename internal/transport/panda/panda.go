package panda

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dolphin/internal/common"
	"dolphin/internal/i18n"
	"dolphin/internal/transport"
	pandamcp "dolphin/internal/transport/panda/mcp"

	"github.com/gorilla/websocket"
	"github.com/rs/xid"
	"go.uber.org/zap"
)

const (
	pandaReconnectBase = 1 * time.Second
	pandaReconnectMax  = 30 * time.Second
	pandaWriteTimeout  = 10 * time.Second
)

func init() {
	transport.Register("panda", func(ctx context.Context, cfg map[string]any) (transport.IO, error) {
		logger, _ := cfg["logger"].(*zap.Logger)
		agentName, _ := cfg["agent_name"].(string)
		return NewPanda(PandaConfig{
			Server:     valOr(cfg, "server", "http://127.0.0.1:8080"),
			Account:    valOr(cfg, "account", ""),
			Password:   valOr(cfg, "password", ""),
			ConvID:     valOr(cfg, "conv_id", ""),
			AllowUsers: valOr(cfg, "allow_users", ""),
		}, logger, agentName), nil
	})
}

func valOr(cfg map[string]any, key, def string) string {
	if v, ok := cfg[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return def
}

// PandaConfig holds panda-ai server connection configuration.
type PandaConfig struct {
	Server     string // HTTP base URL (e.g. http://127.0.0.1:8080)
	Account    string // login account
	Password   string // login password
	ConvID     string // optional: fixed conversation to send/receive; empty = auto-reply to incoming conv
	AllowUsers string // comma-separated allowed sender user IDs; empty = deny all
}

// Panda is a transport that connects to a panda-ai IM server via WebSocket.
type Panda struct {
	*transport.SessionHolder
	id        string
	cfg       PandaConfig
	logger    *zap.Logger
	agentName string

	httpClient *http.Client
	token      string
	userID     string

	conn   *websocket.Conn
	connMu sync.Mutex

	writeMu sync.Mutex

	msgChan chan string
	closeCh chan struct{}
	wg      sync.WaitGroup

	mu         sync.Mutex
	closed     bool
	allowUsers []string // user ID glob patterns; nil = deny all

	// track from incoming MsgPush for reply routing
	lastSenderID string
	lastConvID   string

	connectedAt  int64 // unix millis; filters out messages older than this after reconnect
	firstConnect bool
}

// NewPanda creates a new panda transport.
func NewPanda(cfg PandaConfig, logger *zap.Logger, agentName string) *Panda {
	if logger == nil {
		logger, _ = zap.NewProduction()
	}

	var allowUsers []string
	if cfg.AllowUsers != "" {
		for u := range strings.SplitSeq(cfg.AllowUsers, ",") {
			u = strings.TrimSpace(u)
			if u != "" {
				allowUsers = append(allowUsers, u)
			}
		}
	}

	return &Panda{
		SessionHolder: transport.NewSessionHolder(nil),
		id:            "panda",
		cfg:           cfg,
		logger:        logger,
		agentName:     agentName,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
		msgChan:       make(chan string, 100),
		closeCh:       make(chan struct{}),
		allowUsers:    allowUsers,
		firstConnect:  true,
	}
}

func (p *Panda) ID() string { return p.id }

func (p *Panda) Token() string { return p.token }

func (p *Panda) Context() string { return i18n.T("panda.context") }

func (p *Panda) Tools() []common.ToolDesc {
	return []common.ToolDesc{
		{
			Name: "panda_mcp",
			Executor: pandamcp.NewFileUploadSource(
				p.cfg.Server,
				p.Token,
				p.Write,
				p.WriteContent,
				p.logger,
			),
		},
	}
}

func (p *Panda) Start(ctx context.Context) error {
	if err := p.login(ctx); err != nil {
		return fmt.Errorf("panda: login: %w", err)
	}
	p.wg.Add(1)
	go p.run()
	return nil
}

// Read blocks until a message is received.
func (p *Panda) Read(ctx context.Context) (string, error) {
	select {
	case msg := <-p.msgChan:
		return msg, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Write sends a text message. If conv_id is configured, uses that;
// otherwise replies to the conversation the last incoming message came from.
// Automatically uploads local image paths found in the text.
func (p *Panda) Write(ctx context.Context, text string) error {
	if p.token != "" {
		text = p.autoUploadImages(ctx, text)
	}

	convID := p.cfg.ConvID

	p.mu.Lock()
	if convID == "" {
		convID = p.lastConvID
	}
	lastConvID := convID
	p.mu.Unlock()

	if lastConvID == "" {
		return fmt.Errorf("panda: no conv_id configured and no incoming conversation to reply to")
	}

	payload := msgSendPayload{
		ConvID:      lastConvID,
		ContentType: 0,
		Body:        text,
		ClientSeq:   time.Now().UnixMilli(),
	}
	payloadData, _ := json.Marshal(payload)

	frame := frame{
		Type:    msgTypeSend,
		ID:      xid.New().String(),
		Payload: payloadData,
	}

	return p.writeFrame(frame)
}

// writeFrame sends a frame over WebSocket with mutex protection.
func (p *Panda) writeFrame(f frame) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	p.connMu.Lock()
	conn := p.conn
	p.connMu.Unlock()

	if conn == nil {
		return fmt.Errorf("panda: not connected")
	}

	conn.SetWriteDeadline(time.Now().Add(pandaWriteTimeout))
	return conn.WriteJSON(f)
}

// WriteContent sends a message with the given content type.
// contentType 0 = text, 1 = image, 2 = audio, 3 = video.
func (p *Panda) WriteContent(ctx context.Context, text string, contentType int) error {
	convID := p.cfg.ConvID

	p.mu.Lock()
	if convID == "" {
		convID = p.lastConvID
	}
	lastConvID := convID
	p.mu.Unlock()

	if lastConvID == "" {
		return fmt.Errorf("panda: no conv_id configured and no incoming conversation to reply to")
	}

	payload := msgSendPayload{
		ConvID:      lastConvID,
		ContentType: contentType,
		Body:        text,
		ClientSeq:   time.Now().UnixMilli(),
	}
	payloadData, _ := json.Marshal(payload)

	frame := frame{
		Type:    msgTypeSend,
		ID:      xid.New().String(),
		Payload: payloadData,
	}

	return p.writeFrame(frame)
}

// isImageExt returns true if the file extension is a common image format.
func isImageExt(ext string) bool {
	ext = strings.ToLower(ext)
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return true
	}
	return false
}

// autoUploadImages scans text for local image file paths, uploads them to the
// panda-ai server, and replaces each path with a markdown image link.
func (p *Panda) autoUploadImages(ctx context.Context, text string) string {
	// Quick check for common path patterns — avoids scanning when unnecessary
	if !strings.ContainsAny(text, "/\\") {
		return text
	}

	// Check each whitespace-separated token
	var result []string
	for _, token := range strings.Fields(text) {
		trimmed := strings.Trim(token, "'\"(),.;!?")
		if isImageExt(filepath.Ext(trimmed)) {
			if info, err := os.Stat(trimmed); err == nil && !info.IsDir() && info.Size() > 0 {
				uploaded, err := p.uploadImage(ctx, trimmed)
				if err == nil && uploaded != "" {
					fileName := filepath.Base(trimmed)
					result = append(result, fmt.Sprintf("![%s](%s)", fileName, uploaded))
					continue
				}
			}
		}
		result = append(result, token)
	}

	return strings.Join(result, " ")
}

// uploadImage uploads a local image file to the panda-ai server and returns the URL.
func (p *Panda) uploadImage(ctx context.Context, filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	ext := strings.ToLower(filepath.Ext(filePath))
	fileType := 1 // default: file
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		fileType = 0 // image
	case ".mp3", ".wav", ".ogg", ".aac", ".m4a", ".amr":
		fileType = 2 // audio
	case ".mp4", ".avi", ".mov", ".wmv", ".flv":
		fileType = 3 // video
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(fw, f); err != nil {
		return "", fmt.Errorf("copy file: %w", err)
	}
	w.WriteField("file_type", fmt.Sprintf("%d", fileType))
	w.Close()

	serverURL := strings.TrimRight(p.cfg.Server, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL+"/api/v1/files/upload", &buf)
	if err != nil {
		return "", fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+p.token)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload request: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("upload failed (status %d)", resp.StatusCode)
	}

	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(respData, &envelope); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if envelope.Code != 0 {
		return "", fmt.Errorf("upload rejected (code %d)", envelope.Code)
	}

	var result struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(envelope.Data, &result); err != nil {
		return "", fmt.Errorf("parse upload data: %w", err)
	}
	return result.URL, nil
}

func (p *Panda) Flush() error { return nil }

func (p *Panda) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	close(p.closeCh)
	p.mu.Unlock()

	p.connMu.Lock()
	if p.conn != nil {
		p.conn.Close()
		p.conn = nil
	}
	p.connMu.Unlock()

	p.wg.Wait()
	return nil
}

func (p *Panda) Capability() transport.Capability {
	return transport.Capability{
		Interactive:        false,
		Streamable:         false,
		NestRead:           false,
		RenderTextMarkdown: "markdown",
	}
}

func (p *Panda) RequestPermission(_ context.Context, _ string) (transport.PermissionResult, error) {
	return transport.PermissionDenied, fmt.Errorf("%s", i18n.T("panda.no_interactive"))
}

// ---------------------------------------------------------------------------
// Authentication
// ---------------------------------------------------------------------------

type loginReq struct {
	Account  string `json:"account"`
	Password string `json:"password"`
}

type loginRes struct {
	UserID       string `json:"user_id"`
	Account      string `json:"account"`
	Name         string `json:"name"`
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
}

// apiResponse is the standard panda-ai server envelope.
type apiResponse struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func (p *Panda) login(ctx context.Context) error {
	serverURL := strings.TrimRight(p.cfg.Server, "/")
	body := loginReq{Account: p.cfg.Account, Password: p.cfg.Password}
	reqData, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal login: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL+"/api/v1/users/login", bytes.NewReader(reqData))
	if err != nil {
		return fmt.Errorf("create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("login request: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read login response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login failed (status %d): %s", resp.StatusCode, string(respData))
	}

	// Unwrap the standard API envelope {code, msg, data}
	var envelope apiResponse
	if err := json.Unmarshal(respData, &envelope); err != nil {
		return fmt.Errorf("parse api envelope: %w", err)
	}
	if envelope.Code != 0 {
		return fmt.Errorf("login rejected (code %d): %s", envelope.Code, envelope.Msg)
	}

	var result loginRes
	if err := json.Unmarshal(envelope.Data, &result); err != nil {
		return fmt.Errorf("parse login data: %w", err)
	}

	if result.Token == "" {
		return fmt.Errorf("login returned empty token")
	}

	p.token = result.Token
	p.userID = result.UserID
	p.logger.Info("panda login success", zap.String("user_id", p.userID))
	return nil
}

// ---------------------------------------------------------------------------
// WebSocket protocol types (mirrors panda_ai protocol)
// ---------------------------------------------------------------------------

type messageType int

const (
	msgTypeSend          messageType = 1
	msgTypeSendAck       messageType = 2
	msgTypePush          messageType = 11
	msgTypeSyncReq       messageType = 21
	msgTypeSyncRes       messageType = 22
	msgTypeSessionRecov  messageType = 43
	msgTypeSessionRecAck messageType = 44
	msgTypePing          messageType = 61
	msgTypePong          messageType = 62
	msgTypeError         messageType = 71
)

type frame struct {
	Type    messageType     `json:"type"`
	ID      string          `json:"id"`
	Payload json.RawMessage `json:"payload"`
}

type msgPushPayload struct {
	MsgID       int64    `json:"msg_id"`
	ConvID      string   `json:"conv_id"`
	SenderID    string   `json:"sender_id"`
	ContentType int      `json:"content_type"`
	Body        string   `json:"body"`
	ReplyTo     int64    `json:"reply_to"`
	Mention     []string `json:"mention,omitempty"`
	Timestamp   int64    `json:"timestamp"`
	ConvSeq     int64    `json:"conv_seq"`
}

type msgSendPayload struct {
	ConvID      string   `json:"conv_id"`
	ContentType int      `json:"content_type"`
	Body        string   `json:"body"`
	ReplyTo     int64    `json:"reply_to"`
	ClientSeq   int64    `json:"client_seq"`
	Mention     []string `json:"mention,omitempty"`
}

type errorPayload struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ---------------------------------------------------------------------------
// WebSocket connection & read loop
// ---------------------------------------------------------------------------

// run connects to the WebSocket and enters the read loop with reconnection.
func (p *Panda) run() {
	defer p.wg.Done()

	for {
		connected := p.connect()
		if !connected {
			return
		}

		p.readLoop()

		p.connMu.Lock()
		if p.conn != nil {
			p.conn.Close()
			p.conn = nil
		}
		p.connMu.Unlock()

		select {
		case <-p.closeCh:
			return
		default:
		}

		if !p.reconnect() {
			return
		}
	}
}

// reconnect performs re-login and exponential-backoff reconnection.
// Returns true on successful reconnect, false if closed.
func (p *Panda) reconnect() bool {
	if err := p.login(context.Background()); err != nil {
		p.logger.Warn("panda: re-login failed", zap.Error(err))
	}

	backoff := pandaReconnectBase
	for {
		p.logger.Info("panda: reconnecting", zap.Duration("backoff", backoff))
		select {
		case <-p.closeCh:
			return false
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > pandaReconnectMax {
			backoff = pandaReconnectMax
		}
		if p.connect() {
			return true
		}
	}
}

// connect establishes the WebSocket connection.
func (p *Panda) connect() bool {
	wsURL := p.makeWSURL()
	p.logger.Info("panda: connecting", zap.String("url", wsURL))

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		if p.isClosed() {
			return false
		}
		p.logger.Warn("panda: ws dial failed", zap.Error(err))
		return false
	}

	p.connMu.Lock()
	p.conn = conn
	p.connMu.Unlock()

	if p.firstConnect {
		p.firstConnect = false
	} else {
		p.connectedAt = time.Now().UnixMilli()
	}

	p.logger.Info("panda: connected", zap.String("user_id", p.userID))
	return true
}

// makeWSURL builds the WebSocket URL from the configured server URL.
func (p *Panda) makeWSURL() string {
	serverURL := strings.TrimRight(p.cfg.Server, "/")
	wsScheme := "ws"
	if strings.HasPrefix(serverURL, "https") {
		wsScheme = "wss"
	}
	serverURL = strings.TrimPrefix(serverURL, "http://")
	serverURL = strings.TrimPrefix(serverURL, "https://")

	u := url.URL{
		Scheme:   wsScheme,
		Host:     serverURL,
		Path:     "/ws",
		RawQuery: "token=" + url.QueryEscape(p.token),
	}
	return u.String()
}

// readLoop reads frames from the WebSocket connection.
func (p *Panda) readLoop() {
	for {
		_, message, err := p.conn.ReadMessage()
		if err != nil {
			if p.isClosed() {
				return
			}
			p.logger.Warn("panda: read error", zap.Error(err))
			return
		}

		var f frame
		if err := json.Unmarshal(message, &f); err != nil {
			p.logger.Warn("panda: unmarshal frame", zap.Error(err))
			continue
		}

		if err := p.handleFrame(f); err != nil {
			p.logger.Warn("panda: handle frame", zap.Int("type", int(f.Type)), zap.Error(err))
		}
	}
}

// handleFrame dispatches a received frame.
func (p *Panda) handleFrame(f frame) error {
	switch f.Type {
	case msgTypePing:
		return p.writeFrame(frame{Type: msgTypePong})

	case msgTypePong:
		// ignore

	case msgTypePush:
		return p.handleMsgPush(f.Payload)

	case msgTypeError:
		var errPayload errorPayload
		if err := json.Unmarshal(f.Payload, &errPayload); err == nil {
			p.logger.Warn("panda: server error", zap.Int("code", errPayload.Code), zap.String("message", errPayload.Message))
		}

	case msgTypeSessionRecAck:
		// welcome frame on connect, ignore

	default:
		p.logger.Debug("panda: unhandled frame type", zap.Int("type", int(f.Type)))
	}
	return nil
}

// handleMsgPush processes an incoming message push.
func (p *Panda) handleMsgPush(data json.RawMessage) error {
	var push msgPushPayload
	if err := json.Unmarshal(data, &push); err != nil {
		return fmt.Errorf("unmarshal push: %w", err)
	}

	// Skip messages from ourselves
	if push.SenderID == p.userID {
		return nil
	}

	// Filter by configured conversation
	if p.cfg.ConvID != "" && push.ConvID != p.cfg.ConvID {
		return nil
	}

	// Sender allowlist check
	if !p.isSenderAllowed(push.SenderID) {
		p.logger.Debug("panda: sender not allowed", zap.String("sender_id", push.SenderID))
		return nil
	}

	// Skip historical messages pushed after reconnect
	if p.connectedAt > 0 && push.Timestamp > 0 && push.Timestamp < p.connectedAt {
		return nil
	}

	// Only handle text messages
	if push.ContentType != 0 {
		return nil
	}

	p.mu.Lock()
	p.lastSenderID = push.SenderID
	p.lastConvID = push.ConvID
	p.mu.Unlock()

	msg := push.Body
	select {
	case p.msgChan <- msg:
	default:
		p.logger.Warn("panda: msgChan full, dropping message")
	}

	return nil
}

// isSenderAllowed checks if a sender user ID matches the allowlist.
// Empty allowUsers means deny all.
func (p *Panda) isSenderAllowed(userID string) bool {
	if len(p.allowUsers) == 0 {
		return false
	}
	for _, pattern := range p.allowUsers {
		if ok, _ := path.Match(pattern, userID); ok {
			return true
		}
	}
	return false
}

func (p *Panda) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}
