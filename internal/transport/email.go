package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"dolphinzZ/internal/config"

	goimap "github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"go.uber.org/zap"
)

// EmailTransport provides email-based I/O via SMTP (send) and IMAP (receive).
type EmailTransport struct {
	cfg        *config.EmailConfig
	msgCh      chan string
	closeCh    chan struct{}
	closeOnce  sync.Once
	pollTicker *time.Ticker
	closeMu    sync.Mutex
	startTime  time.Time
}

func NewEmailTransport(cfg *config.EmailConfig) *EmailTransport {
	return &EmailTransport{
		cfg:       cfg,
		msgCh:     make(chan string, 1024),
		closeCh:   make(chan struct{}),
		startTime: time.Now(),
	}
}

func (t *EmailTransport) Name() string { return "email" }

func (t *EmailTransport) Capabilities() Capabilities {
	return Capabilities{Streaming: false, Flushable: true}
}

// Start begins IMAP polling and blocks until context is cancelled.
func (t *EmailTransport) Start(ctx context.Context) error {
	interval, _ := time.ParseDuration(t.cfg.PollInterval)
	if interval <= 0 {
		interval = 10 * time.Second
	}
	t.pollTicker = time.NewTicker(interval)
	t.poll()
	for {
		select {
		case <-ctx.Done():
			return t.Close()
		case <-t.pollTicker.C:
			t.poll()
		}
	}
}

// ReadLine blocks until a new email command arrives or the transport is closed.
func (t *EmailTransport) ReadLine() (string, error) {
	select {
	case msg, ok := <-t.msgCh:
		if !ok {
			return "", fmt.Errorf("email transport closed")
		}
		msgsReceived.Inc()
		return msg, nil
	case <-t.closeCh:
		return "", fmt.Errorf("email transport closed")
	}
}

// WriteLine sends an email response via SMTP.
func (t *EmailTransport) WriteLine(s string) error {
	return t.sendMail(s + "\n")
}

// WriteString sends an email response via SMTP.
func (t *EmailTransport) WriteString(s string) error {
	return t.sendMail(s)
}

func (t *EmailTransport) sendMail(body string) error {
	msgsSent.Inc()
	host := t.cfg.SMTPHost
	port := t.cfg.SMTPPort
	if port <= 0 {
		port = 587
	}
	addr := fmt.Sprintf("%s:%d", host, port)
	from := t.cfg.From
	if from == "" {
		from = t.cfg.Username
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("From: %s\r\n", from))
	sb.WriteString(fmt.Sprintf("To: %s\r\n", from))
	sb.WriteString(fmt.Sprintf("Subject: Re: DolphinzZ Agent\r\n"))
	sb.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().Format(time.RFC1123Z)))
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(body)

	if t.cfg.UseTLS && t.cfg.SMTPPort == 465 {
		return t.sendMailTLS(addr, host, sb.String())
	}
	return t.sendMailPlain(addr, sb.String())
}

func (t *EmailTransport) sendMailTLS(addr, host, msg string) error {
	tconn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return fmt.Errorf("tls connect: %w", err)
	}
	defer tconn.Close()

	sc, err := smtp.NewClient(tconn, host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer sc.Close()

	auth := smtp.PlainAuth("", t.cfg.Username, t.cfg.Password, host)
	if err := sc.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	sc.Mail(t.cfg.From)
	sc.Rcpt(t.cfg.From)
	w, err := sc.Data()
	if err != nil {
		return err
	}
	w.Write([]byte(msg))
	return w.Close()
}

func (t *EmailTransport) sendMailPlain(addr, msg string) error {
	auth := smtp.PlainAuth("", t.cfg.Username, t.cfg.Password, t.cfg.SMTPHost)
	return smtp.SendMail(addr, auth, t.cfg.From, []string{t.cfg.From}, []byte(msg))
}

func (t *EmailTransport) poll() {
	host := t.cfg.IMAPHost
	if host == "" {
		host = t.cfg.SMTPHost
	}
	port := t.cfg.IMAPPort
	if port <= 0 {
		port = 993
	}
	addr := fmt.Sprintf("%s:%d", host, port)

	c, err := client.DialTLS(addr, nil)
	if err != nil {
		zap.S().Warnw("email imap connect failed", "error", err)
		return
	}
	defer c.Logout()

	if err := c.Login(t.cfg.Username, t.cfg.Password); err != nil {
		zap.S().Warnw("email imap login failed", "error", err)
		return
	}

	mbox, err := c.Select("INBOX", false)
	if err != nil {
		zap.S().Warnw("email imap select inbox failed", "error", err)
		return
	}
	if mbox.Messages == 0 {
		return
	}

	criteria := goimap.NewSearchCriteria()
	criteria.WithoutFlags = []string{"\\Seen"}
	seqNums, err := c.Search(criteria)
	if err != nil {
		zap.S().Debugw("email imap search failed", "error", err)
		return
	}
	if len(seqNums) == 0 {
		return
	}

	// Mark all unseen as read first
	allUnseen := new(goimap.SeqSet)
	allUnseen.AddNum(seqNums...)
	c.Store(allUnseen, goimap.AddFlags, []interface{}{"\\Seen"}, nil)

	// Only process the newest message
	latest := seqNums[len(seqNums)-1]
	seqset := new(goimap.SeqSet)
	seqset.AddNum(latest)

	messages := make(chan *goimap.Message, 1)
	if err := c.Fetch(seqset, []goimap.FetchItem{goimap.FetchEnvelope}, messages); err != nil {
		zap.S().Debugw("email imap fetch failed", "error", err)
		return
	}

	msg := <-messages
	if msg == nil || msg.Envelope == nil {
		return
	}

	// Skip messages sent before agent started
	if !msg.Envelope.Date.IsZero() && msg.Envelope.Date.Before(t.startTime) {
		return
	}

	subject := msg.Envelope.Subject
	if subject == "" {
		return
	}

	zap.S().Infow("email received", "subject", truncate(subject, 80))

	select {
	case t.msgCh <- subject:
	default:
		zap.S().Warnw("email message dropped, channel full")
	}
}

func (t *EmailTransport) Close() error {
	t.closeOnce.Do(func() {
		t.closeMu.Lock()
		if t.pollTicker != nil {
			t.pollTicker.Stop()
		}
		t.closeMu.Unlock()
		close(t.closeCh)
	})
	return nil
}
