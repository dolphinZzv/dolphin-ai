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
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/xid"
	"go.uber.org/zap"

	"dolphin/internal/common"
	"dolphin/internal/i18n"
	"dolphin/internal/transport"
	pandamcp "dolphin/internal/transport/panda/mcp"
	"dolphin/internal/types"
)

const (
	pandaReconnectBase = 1 * time.Second
	pandaReconnectMax  = 30 * time.Second
	pandaWriteTimeout  = 10 * time.Second
	pandaPingInterval  = 30 * time.Second
	pandaReadTimeout   = 90 * time.Second
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
			AllowConvs: valOr(cfg, "allow_convs", ""),
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
	AllowConvs string // comma-separated allowed conversation IDs; empty = allow all
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
	allowConvs []string // conversation ID glob patterns; nil = allow all

	// track from incoming MsgPush for reply routing
	lastSenderID string
	lastConvID   string
	// AgentTimeline root message tracking
	timelineRootMsgID int64

	clientSeq    int64 // incrementing sequence for agent timeline sends
	connectedAt  int64 // unix millis; filters out messages older than this after reconnect
	firstConnect bool

	timelineEntries []AgentTimelineEntry
	timelineMu      sync.Mutex
	thinkingSent    bool

	// Buffering between first send and ack arrival, to avoid split bubbles.
	firstSendDone  bool
	pendingEntries []AgentTimelineEntry
	pendingStatus  string
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

	var allowConvs []string
	if cfg.AllowConvs != "" {
		for c := range strings.SplitSeq(cfg.AllowConvs, ",") {
			c = strings.TrimSpace(c)
			if c != "" {
				allowConvs = append(allowConvs, c)
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
		allowConvs:    allowConvs,
		firstConnect:  true,
	}
}

func (p *Panda) ID() string { return p.id }

func (p *Panda) Token() string { return p.token }

func (p *Panda) Context() string {
	return "Current message is from panda-ai IM. " +
		"To share an image, write the local file path on its own line, or use markdown syntax: ![desc](path.png) or [desc](path.png). " +
		"The server uploads and replaces the path automatically — the user never sees it. " +
		"Use MESSAGE tool to send text/markdown messages proactively."
}

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

// Write sends the final response.
// If the turn involved agent features (thinking, tool calls), it sends an
// AgentTimeline (contentType=9) with status "completed".
// For simple commands with no agent activity, it sends a plain text message
// (contentType=0) using WriteContent.
func (p *Panda) Write(ctx context.Context, text string) error {
	if p.token != "" {
		text = p.autoUploadImages(ctx, text)
	} else {
		p.logger.Warn("panda: Write skipping autoUploadImages — no token")
	}

	// Determine whether this turn had agent activity (thinking, tool calls).
	p.mu.Lock()
	firstDone := p.firstSendDone
	p.mu.Unlock()
	p.timelineMu.Lock()
	hasAgentActivity := firstDone || len(p.timelineEntries) > 0
	var thinkEntry *AgentTimelineEntry
	if !p.thinkingSent && len(p.timelineEntries) > 0 && p.timelineEntries[len(p.timelineEntries)-1].Type == TimelineEntryThinking {
		e := p.timelineEntries[len(p.timelineEntries)-1]
		thinkEntry = &e
	}
	p.timelineEntries = nil
	p.thinkingSent = false
	p.timelineMu.Unlock()

	defer func() {
		p.mu.Lock()
		p.timelineRootMsgID = 0
		p.firstSendDone = false
		p.pendingEntries = nil
		p.pendingStatus = ""
		p.mu.Unlock()
	}()

	if !hasAgentActivity {
		// Simple command: no thinking, no tool calls — send as plain text.
		return p.WriteContent(ctx, text, 0)
	}

	// Agent interaction: send as AgentTimeline.
	var entries []AgentTimelineEntry
	if thinkEntry != nil {
		entries = append(entries, *thinkEntry)
	}
	entries = append(entries, AgentTimelineEntry{
		ID:        xid.New().String(),
		Type:      TimelineEntryResponse,
		Content:   text,
		Timestamp: time.Now().UnixMilli(),
	})

	return p.sendTimelineNoBuffer(ctx, entries, "completed")
}

// sendTimeline serializes and sends an AgentTimelineBody message.
// The first send of a turn uses parentMsgID=0 to create a new bubble.
// Subsequent sends use the msgID from the server's MsgSendAck as parentMsgID
// to append to the existing bubble.
func (p *Panda) sendTimeline(ctx context.Context, entries []AgentTimelineEntry, status string) error {
	convID := p.cfg.ConvID

	p.mu.Lock()
	if convID == "" {
		convID = p.lastConvID
	}
	lastConvID := convID
	parentMsgID := p.timelineRootMsgID
	firstDone := p.firstSendDone
	// If we've already sent the first frame but ack hasn't arrived yet,
	// buffer these entries to avoid creating a second bubble.
	if parentMsgID == 0 && firstDone {
		p.pendingEntries = append(p.pendingEntries, entries...)
		p.pendingStatus = status
		p.mu.Unlock()
		p.logger.Info("panda: buffering entries waiting for ack",
			zap.Int("pending", len(p.pendingEntries)),
		)
		return nil
	}
	p.mu.Unlock()

	p.logger.Info("panda: sendTimeline",
		zap.String("conv_id", lastConvID),
		zap.Int64("parent_msg_id", parentMsgID),
		zap.String("status", status),
		zap.Int("entries", len(entries)),
	)

	if lastConvID == "" {
		return fmt.Errorf("panda: no conv_id configured and no incoming conversation to reply to")
	}

	body := AgentTimelineBody{
		Entries:     entries,
		Status:      status,
		ParentMsgID: parentMsgID,
	}
	bodyJSON, _ := json.Marshal(body)

	p.mu.Lock()
	clientSeq := p.clientSeq
	p.clientSeq++
	p.mu.Unlock()

	payload := msgSendPayload{
		ConvID:      lastConvID,
		ContentType: 9,
		Body:        string(bodyJSON),
		ReplyTo:     0,
		ClientSeq:   clientSeq,
		Mention:     []string{},
	}
	payloadData, _ := json.Marshal(payload)

	frame := frame{
		Type:    msgTypeSend,
		ID:      xid.New().String(),
		Payload: payloadData,
	}

	if err := p.writeFrame(frame); err != nil {
		return err
	}
	// Track that the first send has gone out, so subsequent sends before
	// the ack arrives are buffered instead of creating a second bubble.
	if parentMsgID == 0 {
		p.mu.Lock()
		p.firstSendDone = true
		p.mu.Unlock()
	}
	return nil
}

// sendTimelineNoBuffer is like sendTimeline but never buffers entries.
// It sends directly — if ack hasn't arrived, parentMsgID will be 0 which
// creates a standalone bubble. Used for the final "completed" send to
// avoid racing with the deferred cleanup in Write().
func (p *Panda) sendTimelineNoBuffer(ctx context.Context, entries []AgentTimelineEntry, status string) error {
	convID := p.cfg.ConvID

	p.mu.Lock()
	if convID == "" {
		convID = p.lastConvID
	}
	lastConvID := convID
	parentMsgID := p.timelineRootMsgID
	clientSeq := p.clientSeq
	p.clientSeq++
	p.mu.Unlock()

	if lastConvID == "" {
		return fmt.Errorf("panda: no conv_id configured and no incoming conversation to reply to")
	}

	p.logger.Info("panda: sendTimeline (final)",
		zap.String("conv_id", lastConvID),
		zap.Int64("parent_msg_id", parentMsgID),
		zap.String("status", status),
		zap.Int("entries", len(entries)),
	)

	body := AgentTimelineBody{
		Entries:     entries,
		Status:      status,
		ParentMsgID: parentMsgID,
	}
	bodyJSON, _ := json.Marshal(body)

	payload := msgSendPayload{
		ConvID:      lastConvID,
		ContentType: 9,
		Body:        string(bodyJSON),
		ReplyTo:     0,
		ClientSeq:   clientSeq,
		Mention:     []string{},
	}
	payloadData, _ := json.Marshal(payload)

	frame := frame{
		Type:    msgTypeSend,
		ID:      xid.New().String(),
		Payload: payloadData,
	}

	return p.writeFrame(frame)
}

// WriteThinking accumulates thinking text and sends a running timeline update.
func (p *Panda) WriteThinking(ctx context.Context, text string) error {
	p.timelineMu.Lock()
	defer p.timelineMu.Unlock()
	// Append to the last entry if it's a thinking entry, otherwise create new.
	// We only accumulate locally — thinking is flushed once when the next event
	// (tool call or response) arrives, avoiding duplicate entries in the client.
	if n := len(p.timelineEntries); n > 0 && p.timelineEntries[n-1].Type == TimelineEntryThinking {
		p.timelineEntries[n-1].Content += text
	} else {
		p.timelineEntries = append(p.timelineEntries, AgentTimelineEntry{
			ID:        xid.New().String(),
			Type:      TimelineEntryThinking,
			Content:   text,
			Timestamp: time.Now().UnixMilli(),
		})
		// New thinking phase starts — will be flushed with the next event.
		p.thinkingSent = false
	}
	return nil
}

// WriteToolCall records a tool call and sends a running timeline update.
func (p *Panda) WriteToolCall(ctx context.Context, call types.ToolCall) error {
	p.timelineMu.Lock()
	// Build entries to send: flush accumulated thinking, then the tool call.
	var entries []AgentTimelineEntry
	if !p.thinkingSent && len(p.timelineEntries) > 0 && p.timelineEntries[len(p.timelineEntries)-1].Type == TimelineEntryThinking {
		entries = append(entries, p.timelineEntries[len(p.timelineEntries)-1])
		p.thinkingSent = true
	}
	tc := AgentTimelineEntry{
		ID:        xid.New().String(),
		Type:      TimelineEntryToolCall,
		Content:   call.Name,
		ToolName:  call.Name,
		ToolInput: call.Arguments,
		Timestamp: time.Now().UnixMilli(),
	}
	p.timelineEntries = append(p.timelineEntries, tc)
	entries = append(entries, tc)
	p.timelineMu.Unlock()

	return p.sendTimeline(ctx, entries, "running")
}

// WriteToolResult records a tool result and sends a running timeline update.
func (p *Panda) WriteToolResult(ctx context.Context, result types.ToolResult) error {
	p.timelineMu.Lock()
	status := "success"
	if result.IsError {
		status = "error"
	}
	p.timelineEntries = append(p.timelineEntries, AgentTimelineEntry{
		ID:        xid.New().String(),
		Type:      TimelineEntryToolResult,
		Content:   result.Content,
		Status:    status,
		Timestamp: time.Now().UnixMilli(),
	})
	entry := p.timelineEntries[len(p.timelineEntries)-1]
	p.timelineMu.Unlock()

	return p.sendTimeline(ctx, []AgentTimelineEntry{entry}, "running")
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

	_ = conn.SetWriteDeadline(time.Now().Add(pandaWriteTimeout))
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

// mdLinkRe matches markdown links/image references with a local image path.
// e.g. [alt](path.png) or ![alt](path.png)
var mdLinkRe = regexp.MustCompile(`(!?)\[([^\]]*)\]\(([^)]+\.(?:png|jpg|jpeg|gif|webp|bmp))\)`)

// bareImgRe matches a path that starts with . / or \ and ends with an image extension.
var bareImgRe = regexp.MustCompile(`([./\\]\S+\.(?:png|jpg|jpeg|gif|webp|bmp))`)

// autoUploadImages scans text for local image file paths, uploads them to the
// panda-ai server, and replaces each path with a markdown image link.
func (p *Panda) autoUploadImages(ctx context.Context, text string) string {
	if !strings.ContainsAny(text, "/\\.") {
		return text
	}

	// Pass 1: handle markdown link/image syntax [alt](path.png) or ![alt](path.png)
	text = p.replaceMarkdownLinks(ctx, text)
	// Pass 2: handle bare paths like /tmp/img.png or ./img.png
	text = p.replaceBarePaths(ctx, text)

	return text
}

func (p *Panda) replaceMarkdownLinks(ctx context.Context, text string) string {
	matches := mdLinkRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text
	}

	var buf strings.Builder
	lastEnd := 0
	for _, m := range matches {
		fullStart, fullEnd := m[0], m[1] // entire match: [alt](path.png) or ![alt](path.png)
		altStart, altEnd := m[4], m[5]   // alt text
		pathStart, pathEnd := m[6], m[7] // file path

		filePath := text[pathStart:pathEnd]
		if strings.HasPrefix(filePath, "http://") || strings.HasPrefix(filePath, "https://") {
			continue
		}

		p.logger.Info("panda: autoUploadImages found md link", zap.String("path", filePath))

		info, err := os.Stat(filePath)
		if err != nil || info.IsDir() || info.Size() == 0 {
			p.logger.Warn("panda: autoUploadImages stat failed", zap.String("path", filePath), zap.Error(err))
			continue
		}

		uploaded, err := p.uploadImage(ctx, filePath)
		if err != nil || uploaded == "" {
			p.logger.Error("panda: autoUploadImages upload failed", zap.String("path", filePath), zap.Error(err))
			continue
		}

		p.logger.Info("panda: autoUploadImages replaced md link", zap.String("path", filePath), zap.String("url", uploaded))
		buf.WriteString(text[lastEnd:fullStart])
		alt := text[altStart:altEnd]
		fmt.Fprintf(&buf, "![%s](%s)", alt, uploaded)
		lastEnd = fullEnd
	}
	buf.WriteString(text[lastEnd:])
	return buf.String()
}

func (p *Panda) replaceBarePaths(ctx context.Context, text string) string {
	if !strings.ContainsAny(text, "/\\") {
		return text
	}

	matches := bareImgRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text
	}

	var buf strings.Builder
	lastEnd := 0
	for _, m := range matches {
		start, end := m[2], m[3] // submatch 1
		filePath := text[start:end]

		if strings.HasPrefix(filePath, "http://") || strings.HasPrefix(filePath, "https://") {
			continue
		}
		filePath = strings.TrimRight(filePath, "'\"(),.;!?[]")

		p.logger.Info("panda: autoUploadImages found bare path", zap.String("path", filePath))

		info, err := os.Stat(filePath)
		if err != nil || info.IsDir() || info.Size() == 0 {
			p.logger.Warn("panda: autoUploadImages stat failed", zap.String("path", filePath), zap.Error(err))
			continue
		}

		uploaded, err := p.uploadImage(ctx, filePath)
		if err != nil || uploaded == "" {
			p.logger.Error("panda: autoUploadImages upload failed", zap.String("path", filePath), zap.Error(err))
			continue
		}

		p.logger.Info("panda: autoUploadImages replaced bare path", zap.String("path", filePath), zap.String("url", uploaded))
		buf.WriteString(text[lastEnd:start])
		fileName := filepath.Base(filePath)
		fmt.Fprintf(&buf, "![%s](%s)", fileName, uploaded)
		lastEnd = end
	}
	buf.WriteString(text[lastEnd:])
	return buf.String()
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
	_ = w.WriteField("file_type", fmt.Sprintf("%d", fileType))
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

func (p *Panda) Flush() error {
	p.mu.Lock()
	hasTimeline := p.timelineRootMsgID != 0
	p.mu.Unlock()

	var err error
	if hasTimeline {
		entry := AgentTimelineEntry{
			ID:        xid.New().String(),
			Type:      TimelineEntryResponse,
			Content:   "",
			Timestamp: time.Now().UnixMilli(),
		}
		err = p.sendTimeline(context.Background(), []AgentTimelineEntry{entry}, "completed")
	}

	// Always reset timeline state between turns, even when no frames were
	// sent (e.g. thinking accumulated but no tool calls reached sendTimeline).
	// Otherwise thinking entries leak into the next turn and get appended.
	p.timelineMu.Lock()
	p.timelineEntries = nil
	p.thinkingSent = false
	p.timelineMu.Unlock()
	p.mu.Lock()
	p.timelineRootMsgID = 0
	p.firstSendDone = false
	p.pendingEntries = nil
	p.pendingStatus = ""
	p.mu.Unlock()
	return err
}

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
	reqData, err := json.Marshal(body) //nolint:gosec // G117: password is a login credential field, not a hardcoded secret
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
	Mention     []string `json:"mention"`
	Timestamp   int64    `json:"timestamp"`
	ConvSeq     int64    `json:"conv_seq"`
}

type msgSendPayload struct {
	ConvID      string   `json:"conv_id"`
	ContentType int      `json:"content_type"`
	Body        string   `json:"body"`
	ReplyTo     int64    `json:"reply_to"`
	ClientSeq   int64    `json:"client_seq"`
	Mention     []string `json:"mention"`
}

type msgSendAckPayload struct {
	MsgID     int64 `json:"msg_id"`
	Timestamp int64 `json:"timestamp"`
	ClientSeq int64 `json:"client_seq"`
	Status    int   `json:"status"`
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

		// Send both protocol-level and application-level pings for keepalive.
		// Protocol-level ping (opcode 9) keeps TCP alive at the WebSocket
		// layer; the server's library auto-responds with pong.
		// Application-level ping (type 61) is part of the panda protocol.
		pingTicker := time.NewTicker(pandaPingInterval)
		pingDone := make(chan struct{})
		go func() {
			for {
				select {
				case <-pingTicker.C:
					p.connMu.Lock()
					conn := p.conn
					p.connMu.Unlock()
					if conn != nil {
						_ = conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(pandaWriteTimeout))
					}
					_ = p.writeFrame(frame{Type: msgTypePing})
				case <-pingDone:
					pingTicker.Stop()
					return
				}
			}
		}()

		p.readLoop()

		close(pingDone)

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

	dialer := &websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	conn, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		if p.isClosed() {
			return false
		}
		p.logger.Warn("panda: ws dial failed", zap.Error(err))
		return false
	}
	if resp != nil {
		resp.Body.Close()
	}

	// Set initial read deadline; ping/pong handlers extend it.
	_ = conn.SetReadDeadline(time.Now().Add(pandaReadTimeout))
	conn.SetPingHandler(func(data string) error {
		_ = conn.SetReadDeadline(time.Now().Add(pandaReadTimeout))
		return conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(pandaWriteTimeout))
	})
	conn.SetPongHandler(func(data string) error {
		_ = conn.SetReadDeadline(time.Now().Add(pandaReadTimeout))
		return nil
	})

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
		// Extend read deadline after each successful read.
		_ = p.conn.SetReadDeadline(time.Now().Add(pandaReadTimeout))

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
	switch f.Type { //nolint:exhaustive // unhandled frame types are intentionally ignored
	case msgTypePing:
		return p.writeFrame(frame{Type: msgTypePong})

	case msgTypePong:
		// ignore

	case msgTypePush:
		return p.handleMsgPush(f.Payload)

	case msgTypeSendAck:
		var ack msgSendAckPayload
		if err := json.Unmarshal(f.Payload, &ack); err == nil {
			p.mu.Lock()
			// Only accept ack when we expect one (firstSendDone means a first send is in flight).
			// If firstSendDone is false, the ack belongs to a previous turn whose state has
			// already been reset — using it would leak a stale parentMsgID into the next turn.
			if p.timelineRootMsgID == 0 && p.firstSendDone {
				p.timelineRootMsgID = ack.MsgID
				// Flush any entries buffered while waiting for the first ack.
				if len(p.pendingEntries) > 0 {
					pending := p.pendingEntries
					p.pendingEntries = nil
					status := p.pendingStatus
					p.mu.Unlock()
					// Send buffered entries outside the lock to avoid deadlock.
					for _, e := range pending {
						_ = p.sendTimeline(context.Background(), []AgentTimelineEntry{e}, status)
					}
					return nil
				}
			}
			p.mu.Unlock()
		}

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

	// Conversation allowlist check
	if !p.isConvAllowed(push.ConvID) {
		p.logger.Debug("panda: conv not allowed", zap.String("conv_id", push.ConvID))
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

	p.logger.Info("panda: received message",
		zap.String("conv_id", push.ConvID),
		zap.Int64("msg_id", push.MsgID),
		zap.String("sender", push.SenderID),
	)

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

// isConvAllowed checks if a conversation ID matches the conv allowlist.
// Empty allowConvs means allow all conversations.
func (p *Panda) isConvAllowed(convID string) bool {
	if len(p.allowConvs) == 0 {
		return true
	}
	for _, pattern := range p.allowConvs {
		if ok, _ := path.Match(pattern, convID); ok {
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
