package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"net/textproto"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/yuin/goldmark"
	"go.uber.org/zap"

	"dolphin/internal/common"
	"dolphin/internal/i18n"
)

var ErrSendOnly = fmt.Errorf("email: send-only mode, cannot receive")

func init() {
	Register("email", func(ctx context.Context, cfg map[string]any) (IO, error) {
		logger, _ := cfg["logger"].(*zap.Logger)
		agentName, _ := cfg["agent_name"].(string)
		return NewEmail(EmailConfig{
			IMAPServer:   valOr(cfg, "imap_server", "imap.exmail.qq.com"),
			IMAPPort:     valOr(cfg, "imap_port", "993"),
			SMTPServer:   valOr(cfg, "smtp_server", "smtp.exmail.qq.com"),
			SMTPPort:     valOr(cfg, "smtp_port", "465"),
			EmailAddress: valOr(cfg, "email_address", ""),
			Password:     valOr(cfg, "password", ""),
			AllowSenders: valOr(cfg, "allow_senders", ""),
		}, logger, agentName), nil
	})
}

func valOr(cfg map[string]any, key, def string) string {
	if v, ok := cfg[key]; ok {
		switch val := v.(type) {
		case string:
			if val != "" {
				return val
			}
		case []any:
			var parts []string
			for _, item := range val {
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

// EmailConfig holds email transport configuration.
type EmailConfig struct {
	IMAPServer   string
	IMAPPort     string
	SMTPServer   string
	SMTPPort     string
	EmailAddress string
	Password     string
	Key          string // send-only key
	AllowSenders string // comma-separated list of allowed sender emails; empty = allow all
}

// Email is a chunk-mode transport that reads via IMAP and writes via SMTP.
type Email struct {
	*SessionHolder
	id        string
	cfg       EmailConfig
	logger    *zap.Logger
	sendOnly  bool
	agentName string
	closeCh   chan struct{}
	wg        sync.WaitGroup
	mu        sync.Mutex
	closed    bool

	// Last sender info — populated on Read, used on Write/Flush to reply.
	lastFrom      string
	lastSubject   string
	lastMessageID string
	lastBody      string

	// Flush-buffered message — written by Write, sent by Flush.
	pendingMsg string
	pendingTo  string

	// Persistent IMAP connection — reused across Read polls.
	imapClient *imapclient.Client

	allowSenders []string // glob patterns for allowed sender emails; empty = deny all
}

func NewEmail(cfg EmailConfig, logger *zap.Logger, agentName string) *Email {
	if logger == nil {
		logger, _ = zap.NewProduction()
	}
	sendOnly := cfg.Key != "" || cfg.IMAPServer == ""

	var allowSenders []string
	if cfg.AllowSenders != "" {
		for _, s := range strings.Split(cfg.AllowSenders, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				allowSenders = append(allowSenders, s)
			}
		}
	}

	return &Email{
		SessionHolder: NewSessionHolder(nil),
		id:            "email",
		cfg:           cfg,
		logger:        logger,
		sendOnly:      sendOnly,
		agentName:     agentName,
		closeCh:       make(chan struct{}),
		allowSenders:  allowSenders,
	}
}

func (e *Email) ID() string { return e.id }

func (e *Email) Context() string                 { return i18n.T("transport.context_email") }
func (e *Email) Tools() []common.ToolDesc        { return nil }
func (e *Email) Start(ctx context.Context) error { return nil }

// Read polls IMAP inbox for unseen messages and blocks until one arrives.
// In send-only mode, Read returns ErrSendOnly.
func (e *Email) Read(ctx context.Context) (Input, error) {
	if e.sendOnly {
		select {
		case <-e.closeCh:
			return Input{}, fmt.Errorf("email: closed")
		case <-ctx.Done():
			return Input{}, ctx.Err()
		default:
			return Input{}, ErrSendOnly
		}
	}

	for {
		select {
		case <-e.closeCh:
			return Input{}, fmt.Errorf("email: closed")
		case <-ctx.Done():
			return Input{}, ctx.Err()
		default:
		}

		content, from, subject, messageID, err := e.fetchUnseen(ctx)
		if err != nil {
			e.logger.Warn("email imap fetch error", zap.Error(err))
			select {
			case <-e.closeCh:
				return Input{}, fmt.Errorf("email: closed")
			case <-time.After(10 * time.Second):
			}
			continue
		}
		if content == "" {
			select {
			case <-e.closeCh:
				return Input{}, fmt.Errorf("email: closed")
			case <-time.After(5 * time.Second):
			}
			continue
		}

		if !e.isSenderAllowed(from) {
			e.logger.Debug("email: skipped message from non-allowed sender",
				zap.String("from", from),
			)
			e.rejectMessage(context.Background(), from, subject, messageID)
			select {
			case <-e.closeCh:
				return Input{}, fmt.Errorf("email: closed")
			case <-time.After(time.Second):
			}
			continue
		}

		e.mu.Lock()
		e.lastFrom = from
		e.lastSubject = subject
		e.lastMessageID = messageID
		e.lastBody = content
		e.mu.Unlock()

		// Return clean format: subject + body.
		if subject != "" {
			content = i18n.T("transport.email_subject") + subject + "\n" + content
		}
		return Input{Text: strings.TrimSpace(content)}, nil
	}
}

// Write prepares an email reply. The message is buffered until Flush is called.
func (e *Email) Write(ctx context.Context, text string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	to := e.lastFrom
	subj := e.lastSubject
	if to == "" {
		return fmt.Errorf("email: no sender to reply to")
	}
	if subj != "" && !strings.HasPrefix(strings.ToLower(subj), "re:") {
		subj = "Re: " + subj
	}

	e.pendingMsg = e.buildMessage(to, subj, text, e.lastMessageID, e.lastBody)
	e.pendingTo = to
	return nil
}

// Flush sends the buffered email reply via SMTP.
func (e *Email) Flush() error {
	e.mu.Lock()
	msg := e.pendingMsg
	to := e.pendingTo
	e.pendingMsg = ""
	e.pendingTo = ""
	e.mu.Unlock()

	if msg == "" {
		return nil
	}
	err := e.sendSMTP(context.Background(), to, msg)
	e.ResetSession()
	return err
}

func (e *Email) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	close(e.closeCh)
	e.closeIMAP()
	e.wg.Wait()
	return nil
}

func (e *Email) Capability() Capability {
	return Capability{
		Interactive:        false,
		Streamable:         false,
		NestRead:           false,
		RenderTextMarkdown: "markdown",
	}
}

func (e *Email) RequestPermission(_ context.Context, _ string) (PermissionResult, error) {
	return PermissionDenied, fmt.Errorf("%s", i18n.T("transport.email_no_interactive"))
}

func (e *Email) Confirm(_ context.Context, _ string) (bool, error) {
	return false, fmt.Errorf("%s", i18n.T("transport.email_no_interactive"))
}

// rejectMessage sends a rejection email to the sender via SMTP.
func (e *Email) rejectMessage(ctx context.Context, to, subject, messageId string) {
	rejectSubj := "Re: " + subject
	body := i18n.T("transport.email_denied")
	if len(e.allowSenders) == 0 {
		body = i18n.T("transport.email_no_whitelist")
	}
	msg := e.buildMessage(to, rejectSubj, body, messageId, "")
	_ = e.sendSMTP(ctx, to, msg)
}

// isSenderAllowed checks whether a sender email matches any whitelist pattern.
// Patterns support glob wildcards (*, ?). Empty list means deny all.
func (e *Email) isSenderAllowed(from string) bool {
	if len(e.allowSenders) == 0 {
		return false
	}
	for _, pattern := range e.allowSenders {
		if ok, _ := path.Match(pattern, from); ok {
			return true
		}
	}
	return false
}

// buildMessage constructs the raw SMTP message with agent name as sender.
// Renders markdown body as both text/plain and text/html (multipart/alternative).
// messageID is the Message-ID of the original email being replied to
// (for In-Reply-To / References headers); empty for new messages.
func (e *Email) buildMessage(to, subject, body, messageID string, originalBody string) string {
	fromHeader := e.cfg.EmailAddress
	if e.agentName != "" {
		fromHeader = fmt.Sprintf("%s <%s>", e.agentName, e.cfg.EmailAddress)
	}

	var buf bytes.Buffer
	buf.WriteString("From: " + fromHeader + "\r\n")
	buf.WriteString("To: " + to + "\r\n")
	buf.WriteString("Subject: " + subject + "\r\n")
	if messageID != "" {
		buf.WriteString("In-Reply-To: <" + messageID + ">\r\n")
		buf.WriteString("References: <" + messageID + ">\r\n")
	}

	// Render markdown body to HTML.
	htmlBuf := new(bytes.Buffer)
	if err := goldmark.Convert([]byte(body), htmlBuf); err != nil {
		e.logger.Warn("email: goldmark convert error, falling back to plain text", zap.Error(err))
		buf.WriteString("MIME-Version: 1.0\r\n")
		buf.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n")
		buf.WriteString("\r\n")
		buf.WriteString(body)
		return buf.String()
	}

	// Multipart/alternative with text/plain + text/html.
	mw := multipart.NewWriter(&buf)
	boundary := mw.Boundary()
	buf.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&buf, "Content-Type: multipart/alternative; boundary=%q\r\n", boundary)
	buf.WriteString("\r\n")

	// text/plain part
	p, _ := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type": {"text/plain; charset=\"UTF-8\""},
	})
	// Append quoted original message.
	finalBody := body
	if originalBody != "" {
		finalBody += "\r\n\r\n"
		for _, line := range strings.Split(strings.TrimRight(originalBody, "\n"), "\n") {
			finalBody += "> " + line + "\n"
		}
	}
	_, _ = p.Write([]byte(finalBody + "\r\n"))

	// text/html part
	// Append quoted original as blockquote in HTML.
	htmlBody := htmlBuf.String()
	if originalBody != "" {
		htmlBody += "\n<br>\n<blockquote style=\"border-left:2px solid #ccc;margin:0;padding:0 1em;color:#888;\">\n"
		for _, line := range strings.Split(strings.TrimRight(originalBody, "\n"), "\n") {
			htmlBody += html.EscapeString(line) + "<br>\n"
		}
		htmlBody += "</blockquote>\n"
	}
	h := "<html><body>\n" + htmlBody + "\n</body></html>"
	p, _ = mw.CreatePart(textproto.MIMEHeader{
		"Content-Type": {"text/html; charset=\"UTF-8\""},
	})
	_, _ = p.Write([]byte(h + "\r\n"))

	mw.Close()
	return buf.String()
}

// ---------------------------------------------------------------------------
// MIME text extraction
// ---------------------------------------------------------------------------

// extractText parses MIME body content and returns the first text/plain
// or text/html part content, handling base64/quoted-printable encoding.
func extractText(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}

	// Detect multipart: body starts with --boundary
	if strings.HasPrefix(s, "--") {
		idx := strings.Index(s, "\n")
		if idx <= 2 {
			return s
		}
		boundary := strings.TrimRight(s[2:idx], "\r")
		if boundary == "" {
			return s
		}
		mr := multipart.NewReader(strings.NewReader(s), boundary)
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			ct := part.Header.Get("Content-Type")
			if !strings.HasPrefix(ct, "text/plain") && !strings.HasPrefix(ct, "text/html") {
				continue
			}
			raw, _ := io.ReadAll(part)
			text := string(raw)

			switch strings.ToLower(part.Header.Get("Content-Transfer-Encoding")) {
			case "base64":
				if decoded, err := base64.StdEncoding.DecodeString(text); err == nil {
					text = string(decoded)
				}
			case "quoted-printable":
				reader := quotedprintable.NewReader(strings.NewReader(text))
				if decoded, err := io.ReadAll(reader); err == nil {
					text = string(decoded)
				}
			}
			return strings.TrimSpace(text)
		}
		return ""
	}

	// Single part: try base64 decode, fall back to raw
	if decoded, err := base64.StdEncoding.DecodeString(s); err == nil {
		return strings.TrimSpace(string(decoded))
	}
	return s
}

// ---------------------------------------------------------------------------
// IMAP
// ---------------------------------------------------------------------------

func (e *Email) fetchUnseen(ctx context.Context) (content, from, subject, messageID string, err error) {
	client, err := e.getIMAPClient(ctx)
	if err != nil {
		return "", "", "", "", err
	}

	_, err = client.Select("INBOX", nil).Wait()
	if err != nil {
		e.closeIMAP()
		return "", "", "", "", fmt.Errorf("imap select: %w", err)
	}

	searchRes, err := client.Search(&imap.SearchCriteria{
		NotFlag: []imap.Flag{imap.FlagSeen},
	}, nil).Wait()
	if err != nil {
		return "", "", "", "", fmt.Errorf("imap search: %w", err)
	}

	seqNums := searchRes.AllSeqNums()
	if len(seqNums) == 0 {
		return "", "", "", "", nil
	}

	seqSet := imap.SeqSetNum(seqNums[0])

	fetchRes, err := client.Fetch(seqSet, &imap.FetchOptions{
		Envelope:    true,
		BodySection: []*imap.FetchItemBodySection{{}},
	}).Collect()
	if err != nil {
		return "", "", "", "", fmt.Errorf("imap fetch: %w", err)
	}

	if len(fetchRes) > 0 {
		msg := fetchRes[0]
		if msg.Envelope != nil {
			if len(msg.Envelope.From) > 0 {
				addr := msg.Envelope.From[0]
				if addr.Host != "" {
					from = addr.Mailbox + "@" + addr.Host
				} else if addr.Mailbox != "" {
					from = addr.Mailbox
				}
				e.logger.Debug("email: envelope from",
					zap.String("mailbox", addr.Mailbox),
					zap.String("host", addr.Host),
					zap.String("from", from),
				)
			}
			subject = msg.Envelope.Subject
			messageID = msg.Envelope.MessageID
		}
		for _, bs := range msg.BodySection {
			content += string(bs.Bytes)
		}

		// Mark as seen to avoid re-fetching on next poll.
		_, _ = client.Store(seqSet, &imap.StoreFlags{
			Op:     imap.StoreFlagsAdd,
			Flags:  []imap.Flag{imap.FlagSeen},
			Silent: true,
		}, nil).Collect()
	}

	return content, from, subject, messageID, nil
}

func (e *Email) getIMAPClient(ctx context.Context) (*imapclient.Client, error) {
	if e.imapClient != nil {
		return e.imapClient, nil
	}

	addr := net.JoinHostPort(e.cfg.IMAPServer, e.cfg.IMAPPort)
	tlsCfg := &tls.Config{ServerName: e.cfg.IMAPServer}
	client, err := imapclient.DialTLS(addr, &imapclient.Options{TLSConfig: tlsCfg})
	if err != nil {
		return nil, fmt.Errorf("imap dial: %w", err)
	}

	if err := client.Login(e.cfg.EmailAddress, e.cfg.Password).Wait(); err != nil {
		client.Close()
		return nil, fmt.Errorf("imap login: %w", err)
	}

	e.imapClient = client
	return client, nil
}

func (e *Email) closeIMAP() {
	if e.imapClient != nil {
		e.imapClient.Close()
		e.imapClient = nil
	}
}

// ---------------------------------------------------------------------------
// SMTP
// ---------------------------------------------------------------------------

func (e *Email) sendSMTP(ctx context.Context, to, msg string) error {
	// Reject recipients containing line breaks to prevent SMTP header/command
	// injection (gosec G707). A valid address is a single line.
	if strings.ContainsAny(to, "\r\n") {
		return fmt.Errorf("smtp: invalid recipient (contains line break)")
	}
	addr := net.JoinHostPort(e.cfg.SMTPServer, e.cfg.SMTPPort)

	tlsCfg := &tls.Config{ServerName: e.cfg.SMTPServer}
	conn, err := (&tls.Dialer{Config: tlsCfg}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp tls dial: %w", err)
	}

	client, err := smtp.NewClient(conn, e.cfg.SMTPServer)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	auth := smtp.PlainAuth("", e.cfg.EmailAddress, e.cfg.Password, e.cfg.SMTPServer)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}

	if err := client.Mail(e.cfg.EmailAddress); err != nil {
		return fmt.Errorf("smtp mail: %w", err)
	}
	if err := client.Rcpt(to); err != nil { //nolint:gosec // G707: `to` validated for line breaks at function entry
		return fmt.Errorf("smtp rcpt: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	_, err = io.WriteString(w, msg)
	if err != nil {
		w.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	return w.Close()
}

// Ensure Email implements IO.
var _ IO = (*Email)(nil)
