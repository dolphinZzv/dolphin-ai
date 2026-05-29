package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"path"
	"strings"
	"sync"
	"time"

	"dolphin/internal/common"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"go.uber.org/zap"
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
	if v, ok := cfg[key].(string); ok && v != "" {
		return v
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
	lastFrom    string
	lastSubject string

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
		id:           "email",
		cfg:          cfg,
		logger:       logger,
		sendOnly:     sendOnly,
		agentName:    agentName,
		closeCh:      make(chan struct{}),
		allowSenders: allowSenders,
	}
}

func (e *Email) ID() string { return e.id }

func (e *Email) Context() string          { return "当前消息来自邮件" }
func (e *Email) Tools() []common.ToolDesc { return nil }

// Read polls IMAP inbox for unseen messages and blocks until one arrives.
// In send-only mode, Read returns ErrSendOnly.
func (e *Email) Read(ctx context.Context) (string, error) {
	if e.sendOnly {
		select {
		case <-e.closeCh:
			return "", fmt.Errorf("email: closed")
		case <-ctx.Done():
			return "", ctx.Err()
		default:
			return "", ErrSendOnly
		}
	}

	for {
		select {
		case <-e.closeCh:
			return "", fmt.Errorf("email: closed")
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		content, from, subject, err := e.fetchUnseen(ctx)
		if err != nil {
			e.logger.Warn("email imap fetch error", zap.Error(err))
			select {
			case <-e.closeCh:
				return "", fmt.Errorf("email: closed")
			case <-time.After(10 * time.Second):
			}
			continue
		}
		if content == "" {
			select {
			case <-e.closeCh:
				return "", fmt.Errorf("email: closed")
			case <-time.After(5 * time.Second):
			}
			continue
		}

		if !e.isSenderAllowed(from) {
			e.logger.Debug("email: skipped message from non-allowed sender",
				zap.String("from", from),
			)
			e.rejectMessage(context.Background(), from, subject)
			select {
			case <-e.closeCh:
				return "", fmt.Errorf("email: closed")
			case <-time.After(time.Second):
			}
			continue
		}

		e.mu.Lock()
		e.lastFrom = from
		e.lastSubject = subject
		e.mu.Unlock()

		return content, nil
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

	e.pendingMsg = e.buildMessage(to, subj, text)
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
	return e.sendSMTP(context.Background(), to, msg)
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
		Interactive: false,
		Streamable:  false,
		NestRead:    false,
	}
}

// rejectMessage sends a rejection email to the sender via SMTP.
func (e *Email) rejectMessage(ctx context.Context, to, subject string) {
	rejectSubj := "Re: " + subject
	body := "抱歉，您没有权限向此邮箱发送消息，您的邮件已被忽略。"
	if len(e.allowSenders) == 0 {
		body = "机器人暂未配置白名单，请联系管理员配置后使用"
	}
	msg := e.buildMessage(to, rejectSubj, body)
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
func (e *Email) buildMessage(to, subject, body string) string {
	fromHeader := e.cfg.EmailAddress
	if e.agentName != "" {
		fromHeader = fmt.Sprintf("%s <%s>", e.agentName, e.cfg.EmailAddress)
	}

	var buf bytes.Buffer
	buf.WriteString("From: " + fromHeader + "\r\n")
	buf.WriteString("To: " + to + "\r\n")
	buf.WriteString("Subject: " + subject + "\r\n")
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n")
	buf.WriteString("\r\n")
	buf.WriteString(body)
	return buf.String()
}

// ---------------------------------------------------------------------------
// IMAP
// ---------------------------------------------------------------------------

func (e *Email) fetchUnseen(ctx context.Context) (content, from, subject string, err error) {
	client, err := e.getIMAPClient(ctx)
	if err != nil {
		return "", "", "", err
	}

	_, err = client.Select("INBOX", nil).Wait()
	if err != nil {
		e.closeIMAP()
		return "", "", "", fmt.Errorf("imap select: %w", err)
	}

	searchRes, err := client.Search(&imap.SearchCriteria{
		NotFlag: []imap.Flag{imap.FlagSeen},
	}, nil).Wait()
	if err != nil {
		return "", "", "", fmt.Errorf("imap search: %w", err)
	}

	seqNums := searchRes.AllSeqNums()
	if len(seqNums) == 0 {
		return "", "", "", nil
	}

	seqSet := imap.SeqSetNum(seqNums[0])

	fetchRes, err := client.Fetch(seqSet, &imap.FetchOptions{
		Envelope:    true,
		BodySection: []*imap.FetchItemBodySection{{}},
	}).Collect()
	if err != nil {
		return "", "", "", fmt.Errorf("imap fetch: %w", err)
	}

	if len(fetchRes) > 0 {
		msg := fetchRes[0]
		if msg.Envelope != nil {
			if len(msg.Envelope.From) > 0 {
				from = msg.Envelope.From[0].Mailbox + "@" + msg.Envelope.From[0].Host
			}
			subject = msg.Envelope.Subject
		}
		for _, bs := range msg.BodySection {
			content += string(bs.Bytes)
		}
	}

	return content, from, subject, nil
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
	addr := net.JoinHostPort(e.cfg.SMTPServer, e.cfg.SMTPPort)

	tlsCfg := &tls.Config{ServerName: e.cfg.SMTPServer}
	conn, err := tls.Dial("tcp", addr, tlsCfg)
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
	if err := client.Rcpt(to); err != nil {
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
