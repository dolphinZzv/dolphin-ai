// Package email provides IMAP/SMTP email transport.
package email

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"dolphin/internal/config"
	transport "dolphin/internal/transport"

	goimap "github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"go.uber.org/zap"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

func init() { transport.Register("email", New) }

type EmailMessage struct {
	From      string
	Subject   string
	Body      string
	MessageID string
}

type emailHistoryEntry struct {
	From    string
	Subject string
	Body    string
	Date    time.Time
}

// EmailTransport provides email-based I/O via SMTP (send) and IMAP (receive).
type EmailTransport struct {
	cfg                 *config.EmailConfig
	msgCh               chan string
	closeCh             chan struct{}
	closeOnce           sync.Once
	pollTicker          *time.Ticker
	closeMu             sync.Mutex
	startTime           time.Time
	lastSender          string
	lastMsgID           string
	lastSubject         string
	conversationHistory []emailHistoryEntry
	historyMu           sync.Mutex
	senderMu            sync.RWMutex
	allowSelfSent       bool
	allowedSendersOv    []string
}

func New(cfg *config.Config) (transport.Transport, error) {
	return &EmailTransport{
		cfg:       &cfg.Transport.Email,
		msgCh:     make(chan string, 1024),
		closeCh:   make(chan struct{}),
		startTime: time.Now(),
	}, nil
}

func (t *EmailTransport) SetLastSender(to string) {
	t.senderMu.Lock()
	t.lastSender = to
	t.senderMu.Unlock()
}

func (t *EmailTransport) SetStartTime(st time.Time) { t.startTime = st }

func (t *EmailTransport) SetAllowSelfSent(v bool) { t.allowSelfSent = v }

func (t *EmailTransport) SetAllowedSenders(senders []string) { t.allowedSendersOv = senders }

func (t *EmailTransport) InjectMessage(msg string) {
	select {
	case t.msgCh <- msg:
	default:
	}
}

func (t *EmailTransport) PollIMAP() *EmailMessage {
	return t.pollOnce()
}

func (t *EmailTransport) FetchLatestUnseen() *EmailMessage {
	return t.pollOnce()
}

func (t *EmailTransport) OnConfigChange(oldCfg, newCfg *config.Config) {
	oldInterval := t.cfg.PollInterval
	t.cfg = &newCfg.Transport.Email

	if t.cfg.PollInterval != oldInterval && t.pollTicker != nil {
		interval, _ := time.ParseDuration(t.cfg.PollInterval)
		if interval <= 0 {
			interval = 10 * time.Second
		}
		t.pollTicker.Reset(interval)
		zap.S().Infow("email poll interval updated", "interval", t.cfg.PollInterval)
	}
}

func (t *EmailTransport) Name() string { return "email" }

func (t *EmailTransport) Banner() string {
	return fmt.Sprintf("  Email IMAP: %s:%d, SMTP: %s:%d, from: %s\n", t.cfg.IMAPHost, t.cfg.IMAPPort, t.cfg.SMTPHost, t.cfg.SMTPPort, t.cfg.From)
}

func (t *EmailTransport) Context() string {
	return fmt.Sprintf("Connected via email (IMAP: %s:%d, SMTP: %s:%d). User replies may be delayed by minutes. Be patient, self-contained, and thorough — each response is a separate email.",
		t.cfg.IMAPHost, t.cfg.IMAPPort, t.cfg.SMTPHost, t.cfg.SMTPPort)
}

func (t *EmailTransport) Capabilities() transport.Capabilities {
	return transport.Capabilities{Streaming: false}
}

func (t *EmailTransport) Start(ctx context.Context) error {
	transport.ActiveConnections.Add(1)
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

func (t *EmailTransport) ReadLine() (string, error) {
	select {
	case msg, ok := <-t.msgCh:
		if !ok {
			return "", fmt.Errorf("email transport closed")
		}
		transport.MsgsReceived.Inc()
		return msg, nil
	case <-t.closeCh:
		return "", fmt.Errorf("email transport closed")
	case <-time.After(5 * time.Minute):
		return "", fmt.Errorf("email transport: read timeout (5m)")
	}
}

func (t *EmailTransport) WriteLine(s string) error {
	return t.sendMail(s + "\n")
}

func (t *EmailTransport) WriteString(s string) error {
	return t.sendMail(s)
}

func (t *EmailTransport) Flush() error { return nil }

func (t *EmailTransport) sendMail(body string) error {
	transport.MsgsSent.Inc()
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

	t.senderMu.RLock()
	to := t.lastSender
	msgID := t.lastMsgID
	subject := t.lastSubject
	t.senderMu.RUnlock()
	if to == "" {
		return fmt.Errorf("email: no sender yet — wait for an incoming message")
	}

	if decoded, err := (&mime.WordDecoder{}).DecodeHeader(subject); err == nil {
		subject = decoded
	}
	if subject == "" {
		subject = "dolphin Agent"
	}
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "From: %s\r\n", from)
	fmt.Fprintf(&sb, "To: %s\r\n", to)
	fmt.Fprintf(&sb, "Subject: %s\r\n", subject)
	fmt.Fprintf(&sb, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	if msgID != "" {
		fmt.Fprintf(&sb, "In-Reply-To: <%s>\r\n", msgID)
		fmt.Fprintf(&sb, "References: <%s>\r\n", msgID)
	}
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(body)

	t.historyMu.Lock()
	history := make([]emailHistoryEntry, len(t.conversationHistory))
	copy(history, t.conversationHistory)
	t.historyMu.Unlock()
	for i := len(history) - 1; i >= 0; i-- {
		h := history[i]
		sb.WriteString("\r\n\r\n")
		fmt.Fprintf(&sb, "On %s, %s wrote:\r\n", h.Date.Format(time.RFC1123Z), h.From)
		for _, line := range strings.Split(h.Body, "\n") {
			sb.WriteString("> " + line + "\r\n")
		}
	}

	if t.cfg.UseTLS && t.cfg.SMTPPort == 465 {
		return t.sendMailTLS(addr, host, sb.String(), to)
	}
	return t.sendMailPlain(addr, sb.String(), to)
}

func (t *EmailTransport) sendMailTLS(addr, host, msg, to string) error {
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
	//nolint:govet
	if err := sc.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	sc.Mail(t.cfg.From)
	sc.Rcpt(to)
	w, err := sc.Data()
	if err != nil {
		return err
	}
	w.Write([]byte(msg))
	return w.Close()
}

func (t *EmailTransport) sendMailPlain(addr, msg, to string) error {
	auth := smtp.PlainAuth("", t.cfg.Username, t.cfg.Password, t.cfg.SMTPHost)
	return smtp.SendMail(addr, auth, t.cfg.From, []string{to}, []byte(msg))
}

func (t *EmailTransport) poll() {
	msg := t.pollOnce()
	if msg == nil {
		return
	}
	select {
	case t.msgCh <- msg.Body:
	default:
		zap.S().Warnw("email message dropped, channel full")
	}
}

func (t *EmailTransport) pollOnce() *EmailMessage {
	host := t.cfg.IMAPHost
	if host == "" {
		host = t.cfg.SMTPHost
	}
	port := t.cfg.IMAPPort
	if port <= 0 {
		port = 993
	}
	addr := fmt.Sprintf("%s:%d", host, port)

	d := &net.Dialer{Timeout: emailDialTimeout(t.cfg.DialTimeout)}
	tlsConn, err := tls.DialWithDialer(d, "tcp", addr, nil)
	if err != nil {
		zap.S().Warnw("email imap connect failed", "error", err)
		return nil
	}
	c, err := client.New(tlsConn)
	if err != nil {
		zap.S().Warnw("email imap connect failed", "error", err)
		return nil
	}
	defer c.Logout()

	//nolint:govet
	if err := c.Login(t.cfg.Username, t.cfg.Password); err != nil {
		zap.S().Warnw("email imap login failed", "error", err)
		return nil
	}

	mbox, err := c.Select("INBOX", false)
	if err != nil {
		zap.S().Warnw("email imap select inbox failed", "error", err)
		return nil
	}
	if mbox.Messages == 0 {
		return nil
	}

	criteria := goimap.NewSearchCriteria()
	criteria.WithoutFlags = []string{"\\Seen"}
	seqNums, err := c.Search(criteria)
	if err != nil {
		zap.S().Debugw("email imap search failed", "error", err)
		return nil
	}
	if len(seqNums) == 0 {
		return nil
	}

	allUnseen := new(goimap.SeqSet)
	allUnseen.AddNum(seqNums...)
	c.Store(allUnseen, goimap.AddFlags, []interface{}{"\\Seen"}, nil)

	latest := seqNums[len(seqNums)-1]
	seqset := new(goimap.SeqSet)
	seqset.AddNum(latest)

	messages := make(chan *goimap.Message, 1)
	if err := c.Fetch(seqset, []goimap.FetchItem{
		goimap.FetchEnvelope,
		goimap.FetchItem("BODY[TEXT]"),
	}, messages); err != nil {
		zap.S().Debugw("email imap fetch failed", "error", err)
		return nil
	}

	msg := <-messages
	if msg == nil || msg.Envelope == nil {
		return nil
	}

	if !msg.Envelope.Date.IsZero() && msg.Envelope.Date.Before(t.startTime) {
		return nil
	}

	if !t.allowSelfSent && isOwnAddress(msg.Envelope.From, t.cfg.From, t.cfg.Username) {
		return nil
	}

	allowed := t.cfg.AllowedSenders
	if t.allowedSendersOv != nil {
		allowed = t.allowedSendersOv
	}
	if len(allowed) > 0 && !isOwnAddress(msg.Envelope.From, allowed...) {
		zap.S().Debugw("email from non-allowed sender, skipped",
			"from", formatAddresses(msg.Envelope.From))
		return nil
	}

	rawSubject := msg.Envelope.Subject
	decSubject := rawSubject
	if d, err := (&mime.WordDecoder{}).DecodeHeader(rawSubject); err == nil {
		decSubject = d
	}

	var bodyText string
	for _, lit := range msg.Body {
		data, _ := io.ReadAll(lit)
		bodyText = decodeMIMEBody(data)
		break
	}

	bodyText = stripQuotedReply(bodyText)

	if bodyText == "" {
		bodyText = stripReplyPrefixes(decSubject)
	}
	if bodyText == "" {
		return nil
	}

	var fromAddr, msgID string

	if len(msg.Envelope.From) > 0 && msg.Envelope.From[0] != nil {
		fromAddr = msg.Envelope.From[0].Address()
		msgID = msg.Envelope.MessageId
		t.senderMu.Lock()
		t.lastSender = fromAddr
		t.lastMsgID = msgID
		t.lastSubject = rawSubject
		t.senderMu.Unlock()
		t.historyMu.Lock()
		t.conversationHistory = append(t.conversationHistory, emailHistoryEntry{
			From:    fromAddr,
			Subject: rawSubject,
			Body:    bodyText,
			Date:    msg.Envelope.Date,
		})
		if len(t.conversationHistory) > 10 {
			t.conversationHistory = t.conversationHistory[len(t.conversationHistory)-10:]
		}
		t.historyMu.Unlock()
	}
	zap.S().Infow("email received", "from", fromAddr, "subject", transport.Truncate(decSubject, 80))

	return &EmailMessage{
		From:      fromAddr,
		Subject:   decSubject,
		Body:      bodyText,
		MessageID: msgID,
	}
}

func stripQuotedReply(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ">") {
			if i > 0 {
				return strings.TrimSpace(strings.Join(lines[:i], "\n"))
			}
			return ""
		}
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "wrote:") || strings.Contains(lower, "写道：") {
			if i > 0 {
				return strings.TrimSpace(strings.Join(lines[:i], "\n"))
			}
			return ""
		}
	}
	return strings.TrimSpace(s)
}

func stripReplyPrefixes(s string) string {
	lower := strings.ToLower(s)
	for {
		stripped := false
		for _, p := range []string{"re:", "fwd:", "回复:", "转发:"} {
			if strings.HasPrefix(lower, p) {
				s = strings.TrimSpace(s[len(p):])
				lower = strings.ToLower(s)
				stripped = true
			}
		}
		if !stripped {
			break
		}
	}
	return s
}

func formatAddresses(from []*goimap.Address) string {
	var parts []string
	for _, addr := range from {
		if addr != nil {
			parts = append(parts, addr.Address())
		}
	}
	return strings.Join(parts, ", ")
}

func isOwnAddress(from []*goimap.Address, refs ...string) bool {
	for _, addr := range from {
		if addr == nil {
			continue
		}
		addrStr := strings.ToLower(addr.Address())
		for _, ref := range refs {
			if ref == "" {
				continue
			}
			ref = strings.ToLower(ref)
			if strings.HasPrefix(ref, "@") {
				if strings.HasSuffix(addrStr, ref) {
					return true
				}
			} else if addrStr == ref {
				return true
			}
		}
	}
	return false
}

func decodeMIMEBody(bodyData []byte) string {
	bodyData = bytes.TrimSpace(bodyData)
	if len(bodyData) == 0 {
		return ""
	}

	ct, params, cte, hasCT := parseMIMEHeaders(bodyData)
	boundary := params["boundary"]

	if !hasCT || boundary == "" {
		if b := detectBoundary(bodyData); b != "" {
			bodyData = findMultipartStart(bodyData, b)
			bodyData = normalizeCRLF(bodyData)
			reader := multipart.NewReader(bytes.NewReader(bodyData), b)
			var textParts, htmlParts []string
			for {
				part, err := reader.NextPart()
				if err != nil {
					break
				}
				partCT := part.Header.Get("Content-Type")
				partCTE := part.Header.Get("Content-Transfer-Encoding")
				mediaType, _, _ := mime.ParseMediaType(partCT)
				data, _ := io.ReadAll(part)
				decoded := decodeContent(string(data), partCT, partCTE)
				if mediaType == "text/plain" {
					textParts = append(textParts, decoded)
				} else if mediaType == "text/html" {
					htmlParts = append(htmlParts, decoded)
				}
			}
			if len(textParts) > 0 {
				return strings.TrimSpace(strings.Join(textParts, "\n"))
			}
			if len(htmlParts) > 0 {
				return strings.TrimSpace(stripHTML(strings.Join(htmlParts, "\n")))
			}
			return strings.TrimSpace(string(bodyData))
		}
		if !hasCT {
			return decodeContent(string(bodyData), "", "")
		}
	}

	if boundary == "" {
		ctFull := ct
		for k, v := range params {
			ctFull += "; " + k + "=\"" + v + "\""
		}
		return decodeContent(string(extractAfterHeaders(bodyData)), ctFull, cte)
	}

	bodyData = findMultipartStart(bodyData, boundary)
	bodyData = normalizeCRLF(bodyData)
	reader := multipart.NewReader(bytes.NewReader(bodyData), boundary)

	var textParts []string
	var htmlParts []string

	for {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		partCT := part.Header.Get("Content-Type")
		partCTE := part.Header.Get("Content-Transfer-Encoding")
		mediaType, _, _ := mime.ParseMediaType(partCT)

		data, _ := io.ReadAll(part)
		decoded := decodeContent(string(data), partCT, partCTE)

		if mediaType == "text/plain" {
			textParts = append(textParts, decoded)
		} else if mediaType == "text/html" {
			htmlParts = append(htmlParts, decoded)
		}
	}

	if len(textParts) > 0 {
		return strings.TrimSpace(strings.Join(textParts, "\n"))
	}
	if len(htmlParts) > 0 {
		return strings.TrimSpace(stripHTML(strings.Join(htmlParts, "\n")))
	}

	return strings.TrimSpace(string(bodyData))
}

func parseMIMEHeaders(data []byte) (string, map[string]string, string, bool) {
	limit := min(len(data), 2048)
	head := data[:limit]

	idx := bytes.Index(head, []byte("\n\n"))
	if idx < 0 {
		idx = bytes.Index(head, []byte("\r\n\r\n"))
	}
	if idx < 0 {
		nl := bytes.IndexByte(head, '\n')
		line := head
		if nl > 0 {
			line = head[:nl]
		}
		var ct string
		var params map[string]string
		var cte string
		if bytes.HasPrefix(line, []byte("Content-Type:")) {
			val := string(bytes.TrimSpace(line[len("Content-Type:"):]))
			mt, p, err := mime.ParseMediaType(val)
			if err != nil {
				return "", nil, "", false
			}
			ct = mt
			params = p
		}
		if ct == "" {
			return "", nil, "", false
		}
		if nl > 0 {
			rest := head[nl+1:]
			if nl2 := bytes.IndexByte(rest, '\n'); nl2 > 0 {
				line2 := bytes.TrimSpace(rest[:nl2])
				if bytes.HasPrefix(line2, []byte("Content-Transfer-Encoding:")) {
					cte = string(bytes.TrimSpace(line2[len("Content-Transfer-Encoding:"):]))
				}
			}
		}
		return ct, params, cte, true
	}

	headerBlock := head[:idx]
	lines := bytes.Split(headerBlock, []byte("\n"))
	var ct string
	var params map[string]string
	var cte string
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if bytes.HasPrefix(line, []byte("Content-Type:")) {
			val := string(bytes.TrimSpace(line[len("Content-Type:"):]))
			mt, p, err := mime.ParseMediaType(val)
			if err == nil {
				ct = mt
				params = p
			}
		}
		if bytes.HasPrefix(line, []byte("Content-Transfer-Encoding:")) {
			cte = string(bytes.TrimSpace(line[len("Content-Transfer-Encoding:"):]))
		}
	}
	if ct == "" {
		return "", nil, "", false
	}
	return ct, params, cte, true
}

func detectBoundary(data []byte) string {
	lines := bytes.Split(data, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if bytes.HasPrefix(line, []byte("--")) && len(line) > 2 {
			candidate := string(line[2:])
			candidate = strings.TrimSuffix(candidate, "--")
			if candidate != "" && len(candidate) >= 4 {
				if bytes.Count(data, []byte(candidate)) >= 2 {
					return candidate
				}
			}
		}
	}
	return ""
}

func normalizeCRLF(data []byte) []byte {
	var out []byte
	for i, b := range data {
		if b == '\n' && (i == 0 || data[i-1] != '\r') {
			out = append(out, '\r', '\n')
		} else {
			out = append(out, b)
		}
	}
	return out
}

func findMultipartStart(data []byte, boundary string) []byte {
	dash := []byte("--" + boundary)
	nlDash := append([]byte("\n"), dash...)
	if idx := bytes.Index(data, nlDash); idx >= 0 {
		return data[idx:]
	}
	rnlDash := append([]byte("\r\n"), dash...)
	if idx := bytes.Index(data, rnlDash); idx >= 0 {
		return data[idx:]
	}
	return data
}

func extractAfterHeaders(data []byte) []byte {
	idx := bytes.Index(data, []byte("\n\n"))
	if idx >= 0 {
		return data[idx+2:]
	}
	idx = bytes.Index(data, []byte("\r\n\r\n"))
	if idx >= 0 {
		return data[idx+4:]
	}
	return data
}

func decodeContent(body, mediaType, transferEncoding string) string {
	switch strings.ToLower(transferEncoding) {
	case "base64":
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(body))
		if err != nil {
			return body
		}
		return decodeCharset(string(decoded), mediaType)
	case "quoted-printable":
		reader := quotedprintable.NewReader(strings.NewReader(body))
		decoded, err := io.ReadAll(reader)
		if err != nil {
			return body
		}
		return decodeCharset(string(decoded), mediaType)
	default:
		return decodeCharset(body, mediaType)
	}
}

func decodeCharset(body, mediaType string) string {
	_, params, err := mime.ParseMediaType(mediaType)
	if err != nil {
		return body
	}
	charset := strings.ToLower(params["charset"])
	switch charset {
	case "gb18030", "gbk", "gb2312":
		return decodeGB18030([]byte(body))
	default:
		return body
	}
}

func decodeGB18030(data []byte) string {
	decoder := simplifiedchinese.GB18030.NewDecoder()
	decoded, err := io.ReadAll(transform.NewReader(bytes.NewReader(data), decoder))
	if err != nil {
		return string(data)
	}
	return string(decoded)
}

func stripHTML(html string) string {
	var buf strings.Builder
	inTag := false
	for i := 0; i < len(html); i++ {
		ch := html[i]
		if ch == '<' {
			inTag = true
			continue
		}
		if ch == '>' {
			inTag = false
			continue
		}
		if !inTag {
			buf.WriteByte(ch)
		}
	}
	s := buf.String()
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	return strings.TrimSpace(s)
}

func emailDialTimeout(s string) time.Duration {
	if s != "" {
		d, err := time.ParseDuration(s)
		if err == nil && d > 0 {
			return d
		}
	}
	return 30 * time.Second
}

func (t *EmailTransport) Close() error {
	t.closeOnce.Do(func() {
		transport.ActiveConnections.Add(-1)
		t.closeMu.Lock()
		if t.pollTicker != nil {
			t.pollTicker.Stop()
		}
		t.closeMu.Unlock()
		close(t.closeCh)
	})
	return nil
}
