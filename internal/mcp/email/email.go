package email

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"dolphin/internal/config"
	"dolphin/internal/mcp"

	goimap "github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

// Tool provides SMTP send and IMAP/POP3 search/fetch as a built-in MCP tool.
type Tool struct {
	cfg    *config.Config
	schema json.RawMessage
}

func New(cfg *config.Config) *Tool {
	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"send", "search", "fetch"},
				"description": "send: send an email; search: search mailbox; fetch: read a specific email body",
			},
			"to": map[string]any{
				"type":        "string",
				"description": "recipient email address (required for send action)",
			},
			"subject": map[string]any{
				"type":        "string",
				"description": "email subject (required for send action)",
			},
			"body": map[string]any{
				"type":        "string",
				"description": "email body text (required for send action)",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "search text to match in subject or sender (search action). Prefix with 'from:' or 'subject:' for field-specific search (IMAP only); POP3 searches all text.",
			},
			"max_results": map[string]any{
				"type":        "integer",
				"description": "max results to return (search only, default 10, max 50)",
			},
			"seq": map[string]any{
				"type":        "integer",
				"description": "message sequence number (1 = oldest, required for fetch action). Returned by search action.",
			},
			"unread_only": map[string]any{
				"type":        "boolean",
				"description": "only search unread messages (IMAP only, default false)",
			},
			"attachments": map[string]any{
				"type":        "array",
				"description": "Optional file attachments (send only). Each item must have an absolute file_path.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"file_path": map[string]any{
							"type":        "string",
							"description": "Absolute path to the file to attach",
						},
					},
				},
			},
		},
		"required": []string{"action"},
	})
	return &Tool{cfg: cfg, schema: schema}
}

func (e *Tool) Definition() mcp.ToolDefinition {
	return mcp.ToolDefinition{
		Name:        "email",
		Description: "Send and receive emails via SMTP/IMAP/POP3. Actions: send (send an email), search (search mailbox messages), fetch (read a specific email body by sequence number). Supports IMAP and POP3 (see transport.email.protocol). Requires transport.email to be configured.",
		InputSchema: e.schema,
		Priority:    e.cfg.MCP.Email.Priority,
		Source:      "built-in",
	}
}

func (e *Tool) Execute(ctx context.Context, input json.RawMessage) (*mcp.ToolResult, error) {
	var params struct {
		Action      string `json:"action"`
		To          string `json:"to,omitempty"`
		Subject     string `json:"subject,omitempty"`
		Body        string `json:"body,omitempty"`
		Query       string `json:"query,omitempty"`
		MaxResults  int    `json:"max_results,omitempty"`
		Seq         int    `json:"seq,omitempty"`
		UnreadOnly  bool   `json:"unread_only,omitempty"`
		Attachments []struct {
			FilePath string `json:"file_path"`
		} `json:"attachments,omitempty"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return &mcp.ToolResult{Content: "Invalid input: " + err.Error(), IsError: true}, nil
	}

	ecfg := &e.cfg.Transport.Email
	if ecfg.Username == "" || ecfg.Password == "" {
		return &mcp.ToolResult{Content: "Email not configured. Set transport.email.username and transport.email.password in config.", IsError: true}, nil
	}

	switch params.Action {
	case "send":
		return e.send(ecfg, params.To, params.Subject, params.Body, params.Attachments)
	case "search":
		if params.MaxResults <= 0 || params.MaxResults > 50 {
			params.MaxResults = 10
		}
		return e.search(ecfg, params.Query, params.UnreadOnly, params.MaxResults)
	case "fetch":
		if params.Seq <= 0 {
			return &mcp.ToolResult{Content: "seq (message sequence number, 1 = oldest) is required for fetch action.", IsError: true}, nil
		}
		return e.fetch(ecfg, params.Seq)
	default:
		return &mcp.ToolResult{Content: fmt.Sprintf("Unknown action: %q. Available: send, search, fetch.", params.Action), IsError: true}, nil
	}
}

func (e *Tool) send(ecfg *config.EmailConfig, to, subject, body string, attachments []struct {
	FilePath string `json:"file_path"`
}) (*mcp.ToolResult, error) {
	if to == "" {
		return &mcp.ToolResult{Content: "Missing required field: to.", IsError: true}, nil
	}
	if subject == "" {
		return &mcp.ToolResult{Content: "Missing required field: subject.", IsError: true}, nil
	}

	from := ecfg.From
	if from == "" {
		from = ecfg.Username
	}

	host := ecfg.SMTPHost
	port := ecfg.SMTPPort
	if port <= 0 {
		port = 587
	}
	addr := fmt.Sprintf("%s:%d", host, port)

	type attachFile struct {
		name     string
		data     []byte
		mimeType string
	}
	var files []attachFile
	const maxFileSize = 10 * 1024 * 1024
	for _, a := range attachments {
		if a.FilePath == "" {
			continue
		}
		absPath, err := filepath.Abs(a.FilePath)
		if err != nil {
			return &mcp.ToolResult{Content: fmt.Sprintf("Invalid file path %q: %v", a.FilePath, err), IsError: true}, nil
		}
		data, err := os.ReadFile(absPath)
		if err != nil {
			return &mcp.ToolResult{Content: fmt.Sprintf("Cannot read attachment %q: %v", absPath, err), IsError: true}, nil
		}
		if len(data) > maxFileSize {
			return &mcp.ToolResult{Content: fmt.Sprintf("Attachment %q too large (%d bytes, max %d)", absPath, len(data), maxFileSize), IsError: true}, nil
		}
		mimeType := mime.TypeByExtension(filepath.Ext(absPath))
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		files = append(files, attachFile{
			name:     filepath.Base(absPath),
			data:     data,
			mimeType: mimeType,
		})
	}

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("From: %s\r\n", from))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", to))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().Format(time.RFC1123Z)))
	msg.WriteString("MIME-Version: 1.0\r\n")

	if len(files) == 0 {
		msg.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
		msg.WriteString("\r\n")
		msg.WriteString(body)
	} else {
		mw := multipart.NewWriter(&msg)
		msg.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\n", mw.Boundary()))
		msg.WriteString("\r\n")

		textPart, _ := mw.CreatePart(map[string][]string{
			"Content-Type": {"text/plain; charset=\"utf-8\""},
		})
		textPart.Write([]byte(body))

		for _, f := range files {
			part, err := mw.CreatePart(map[string][]string{
				"Content-Type":              {f.mimeType},
				"Content-Disposition":       {fmt.Sprintf("attachment; filename=\"%s\"", f.name)},
				"Content-Transfer-Encoding": {"base64"},
			})
			if err != nil {
				return &mcp.ToolResult{Content: fmt.Sprintf("Failed to create attachment part: %v", err), IsError: true}, nil
			}
			part.Write(f.data)
		}
		mw.Close()
	}

	rawMsg := msg.String()

	if ecfg.UseTLS && ecfg.SMTPPort == 465 {
		if err := sendTLS(addr, host, from, []string{to}, rawMsg, ecfg.Username, ecfg.Password); err != nil {
			return &mcp.ToolResult{Content: fmt.Sprintf("Send failed (TLS): %s", err.Error()), IsError: true}, nil
		}
	} else {
		if err := sendPlain(addr, host, from, []string{to}, rawMsg, ecfg.Username, ecfg.Password); err != nil {
			return &mcp.ToolResult{Content: fmt.Sprintf("Send failed: %s", err.Error()), IsError: true}, nil
		}
	}

	if len(files) > 0 {
		var names []string
		for _, f := range files {
			names = append(names, f.name)
		}
		return &mcp.ToolResult{Content: fmt.Sprintf("Email sent to %s: %s (attachments: %s)", to, subject, strings.Join(names, ", "))}, nil
	}
	return &mcp.ToolResult{Content: fmt.Sprintf("Email sent to %s: %s", to, subject)}, nil
}

func sendTLS(addr, host, from string, to []string, msg, user, pass string) error {
	tconn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer tconn.Close()

	sc, err := smtp.NewClient(tconn, host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer sc.Close()

	auth := smtp.PlainAuth("", user, pass, host)
	if err := sc.Auth(auth); err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	if err := sc.Mail(from); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	for _, rcpt := range to {
		if err := sc.Rcpt(rcpt); err != nil {
			return fmt.Errorf("rcpt %s: %w", rcpt, err)
		}
	}
	w, err := sc.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return w.Close()
}

func sendPlain(addr, host, from string, to []string, msg, user, pass string) error {
	auth := smtp.PlainAuth("", user, pass, host)
	return smtp.SendMail(addr, auth, from, to, []byte(msg))
}

func (e *Tool) search(ecfg *config.EmailConfig, query string, unreadOnly bool, maxResults int) (*mcp.ToolResult, error) {
	protocol := strings.ToLower(ecfg.Protocol)
	if protocol == "pop3" {
		return e.searchPOP3(ecfg, query, maxResults)
	}
	return e.searchIMAP(ecfg, query, unreadOnly, maxResults)
}

func (e *Tool) searchIMAP(ecfg *config.EmailConfig, query string, unreadOnly bool, maxResults int) (*mcp.ToolResult, error) {
	c, err := dialIMAP(ecfg)
	if err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("IMAP connection failed: %s", err.Error()), IsError: true}, nil
	}
	defer c.Logout()

	mbox, err := c.Select("INBOX", false)
	if err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Failed to select INBOX: %s", err.Error()), IsError: true}, nil
	}
	if mbox.Messages == 0 {
		return &mcp.ToolResult{Content: "No messages in inbox."}, nil
	}

	criteria := goimap.NewSearchCriteria()
	if unreadOnly {
		criteria.WithoutFlags = []string{"\\Seen"}
	}
	if query != "" {
		if strings.HasPrefix(strings.ToLower(query), "from:") {
			criteria.Header = map[string][]string{"From": {strings.TrimSpace(query[5:])}}
		} else if strings.HasPrefix(strings.ToLower(query), "subject:") {
			criteria.Header = map[string][]string{"Subject": {strings.TrimSpace(query[8:])}}
		} else {
			criteria.Text = []string{query}
		}
	}

	seqNums, err := c.Search(criteria)
	if err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Search failed: %s", err.Error()), IsError: true}, nil
	}
	if len(seqNums) == 0 {
		return &mcp.ToolResult{Content: "No matching emails found."}, nil
	}

	start := 0
	if len(seqNums) > maxResults {
		start = len(seqNums) - maxResults
	}
	latest := seqNums[start:]
	seqset := new(goimap.SeqSet)
	seqset.AddNum(latest...)

	messages := make(chan *goimap.Message, len(latest))
	if err := c.Fetch(seqset, []goimap.FetchItem{goimap.FetchEnvelope}, messages); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Fetch failed: %s", err.Error()), IsError: true}, nil
	}

	var results []string
	for msg := range messages {
		if msg == nil || msg.Envelope == nil {
			continue
		}
		results = append(results, formatEnvelope(int(msg.SeqNum), msg.Envelope))
	}

	if len(results) == 0 {
		return &mcp.ToolResult{Content: "No matching emails found."}, nil
	}

	return &mcp.ToolResult{Content: fmt.Sprintf("Found %d matching emails (showing %d):\n\n%s",
		len(seqNums), len(results), strings.Join(results, "\n"))}, nil
}

func (e *Tool) searchPOP3(ecfg *config.EmailConfig, query string, maxResults int) (*mcp.ToolResult, error) {
	p, err := dialPOP3(ecfg)
	if err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("POP3 connection failed: %s", err.Error()), IsError: true}, nil
	}
	defer p.quit()

	count := p.messageCount()
	if count == 0 {
		return &mcp.ToolResult{Content: "No messages in mailbox."}, nil
	}

	start := 1
	if count > maxResults {
		start = count - maxResults + 1
	}

	var results []string
	for i := start; i <= count; i++ {
		raw, err := p.retrHeaders(i)
		if err != nil {
			continue
		}
		if parsed, err := mail.ReadMessage(strings.NewReader(raw)); err == nil {
			subj := parsed.Header.Get("Subject")
			from := parsed.Header.Get("From")
			date := parsed.Header.Get("Date")

			if query != "" {
				subjLower := strings.ToLower(subj)
				fromLower := strings.ToLower(from)
				qLower := strings.ToLower(query)
				if !strings.Contains(subjLower, qLower) && !strings.Contains(fromLower, qLower) {
					continue
				}
			}

			results = append(results, fmt.Sprintf("Seq:%d | %s | %s | %s",
				i, formatDate(date), truncateStr(from, 40), truncateStr(subj, 60)))
		}
	}

	if len(results) == 0 {
		return &mcp.ToolResult{Content: "No matching emails found."}, nil
	}

	return &mcp.ToolResult{Content: strings.Join(results, "\n")}, nil
}

func (e *Tool) fetch(ecfg *config.EmailConfig, seq int) (*mcp.ToolResult, error) {
	protocol := strings.ToLower(ecfg.Protocol)
	if protocol == "pop3" {
		return e.fetchPOP3(ecfg, seq)
	}
	return e.fetchIMAP(ecfg, uint32(seq))
}

func (e *Tool) fetchIMAP(ecfg *config.EmailConfig, seq uint32) (*mcp.ToolResult, error) {
	c, err := dialIMAP(ecfg)
	if err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("IMAP connection failed: %s", err.Error()), IsError: true}, nil
	}
	defer c.Logout()

	if _, err := c.Select("INBOX", true); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Failed to select INBOX: %s", err.Error()), IsError: true}, nil
	}

	seqset := new(goimap.SeqSet)
	seqset.AddNum(seq)

	messages := make(chan *goimap.Message, 1)
	items := []goimap.FetchItem{goimap.FetchEnvelope, goimap.FetchRFC822}
	if err := c.Fetch(seqset, items, messages); err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Fetch failed: %s", err.Error()), IsError: true}, nil
	}

	msg := <-messages
	if msg == nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Message seq %d not found.", seq), IsError: true}, nil
	}

	return formatMessageBody(msg.Body, int(seq)), nil
}

func (e *Tool) fetchPOP3(ecfg *config.EmailConfig, seq int) (*mcp.ToolResult, error) {
	p, err := dialPOP3(ecfg)
	if err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("POP3 connection failed: %s", err.Error()), IsError: true}, nil
	}
	defer p.quit()

	count := p.messageCount()
	if seq < 1 || seq > count {
		return &mcp.ToolResult{Content: fmt.Sprintf("Invalid seq %d. Mailbox has %d messages (1-%d).", seq, count, count), IsError: true}, nil
	}

	raw, err := p.retr(seq)
	if err != nil {
		return &mcp.ToolResult{Content: fmt.Sprintf("Failed to fetch message %d: %s", seq, err.Error()), IsError: true}, nil
	}

	content := formatParsedMessage(raw, seq, func(hdr mail.Header) string {
		return fmt.Sprintf("From: %s\nTo: %s\nSubject: %s\nDate: %s\nSeq: %d",
			hdr.Get("From"), hdr.Get("To"), hdr.Get("Subject"), hdr.Get("Date"), seq)
	})
	return &mcp.ToolResult{Content: content}, nil
}

func tlsConfigForEmail(ecfg *config.EmailConfig) *tls.Config {
	return &tls.Config{InsecureSkipVerify: ecfg.SkipTLSVerify}
}

func dialIMAP(ecfg *config.EmailConfig) (*client.Client, error) {
	host := ecfg.IMAPHost
	if host == "" {
		host = ecfg.SMTPHost
	}
	port := ecfg.IMAPPort
	if port <= 0 {
		port = 993
	}

	d := &net.Dialer{Timeout: 30 * time.Second}
	tlsConn, err := tls.DialWithDialer(d, "tcp", fmt.Sprintf("%s:%d", host, port), tlsConfigForEmail(ecfg))
	if err != nil {
		return nil, err
	}
	c, err := client.New(tlsConn)
	if err != nil {
		tlsConn.Close()
		return nil, err
	}
	if err := c.Login(ecfg.Username, ecfg.Password); err != nil {
		c.Logout()
		return nil, err
	}
	return c, nil
}

func formatEnvelope(seq int, env *goimap.Envelope) string {
	from := ""
	if len(env.From) > 0 {
		from = env.From[0].PersonalName
		if from == "" {
			from = env.From[0].MailboxName + "@" + env.From[0].HostName
		}
	}
	return fmt.Sprintf("Seq:%d | %s | %s | %s",
		seq,
		env.Date.Format("2006-01-02 15:04"),
		truncateStr(from, 40),
		truncateStr(env.Subject, 60))
}

func formatMessageBody(body map[*goimap.BodySectionName]goimap.Literal, seq int) *mcp.ToolResult {
	var raw string
	for _, literal := range body {
		data, err := io.ReadAll(literal)
		if err != nil {
			continue
		}
		raw = string(data)
		break
	}
	if raw == "" {
		return &mcp.ToolResult{Content: "No body content found."}
	}

	content := formatParsedMessage(raw, seq, func(hdr mail.Header) string {
		return fmt.Sprintf("From: %s\nTo: %s\nSubject: %s\nDate: %s\nSeq: %d",
			hdr.Get("From"), hdr.Get("To"), hdr.Get("Subject"), hdr.Get("Date"), seq)
	})
	return &mcp.ToolResult{Content: content}
}

func formatParsedMessage(raw string, seq int, headerFn func(mail.Header) string) string {
	parsed, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		return fmt.Sprintf("Seq: %d\n(raw, parse failed)\n\n%s", seq, truncateStr(raw, 2000))
	}

	header := parsed.Header
	contentType := header.Get("Content-Type")

	bodyBytes, _ := io.ReadAll(parsed.Body)
	bodyStr := string(bodyBytes)

	if strings.HasPrefix(contentType, "multipart/") {
		_, params, _ := mime.ParseMediaType(contentType)
		boundary := params["boundary"]
		if boundary != "" {
			mpReader := multipart.NewReader(strings.NewReader(bodyStr), boundary)

			var textParts []string
			var attachInfos []string

			for {
				part, err := mpReader.NextPart()
				if err != nil {
					break
				}
				partCT := part.Header.Get("Content-Type")
				partData, _ := io.ReadAll(part)

				if strings.HasPrefix(partCT, "text/plain") || strings.HasPrefix(partCT, "text/html") {
					textParts = append(textParts, string(partData))
				} else {
					filename := ""
					disp := part.Header.Get("Content-Disposition")
					if disp != "" {
						_, dispParams, _ := mime.ParseMediaType(disp)
						filename = dispParams["filename"]
					}
					if filename == "" {
						filename = "unnamed"
					}
					attachInfos = append(attachInfos, fmt.Sprintf("  [%d] %s (%s, %d bytes)",
						len(attachInfos)+1, filename, partCT, len(partData)))
				}
			}

			result := headerFn(header) + "\n"
			if len(textParts) > 0 {
				result += "\n" + truncateStr(strings.Join(textParts, "\n---\n"), 8000)
			}
			if len(attachInfos) > 0 {
				result += "\n\nAttachments:\n" + strings.Join(attachInfos, "\n")
			}
			return result
		}
	}

	return fmt.Sprintf("%s\n\n%s", headerFn(header), truncateStr(bodyStr, 10000))
}

type pop3Conn struct {
	conn   net.Conn
	rw     *bufio.ReadWriter
	count  int
	logged bool
}

func dialPOP3(ecfg *config.EmailConfig) (*pop3Conn, error) {
	host := ecfg.POP3Host
	if host == "" {
		host = ecfg.IMAPHost
	}
	if host == "" {
		host = ecfg.SMTPHost
	}
	port := ecfg.POP3Port
	if port <= 0 {
		port = 995
	}

	d := &net.Dialer{Timeout: 30 * time.Second}
	tlsConn, err := tls.DialWithDialer(d, "tcp", fmt.Sprintf("%s:%d", host, port), tlsConfigForEmail(ecfg))
	if err != nil {
		plainConn, err2 := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, 110), 30*time.Second)
		if err2 != nil {
			return nil, fmt.Errorf("pop3 connect failed (TLS %s:%d and plain :110): %w / %s", host, port, err, err2)
		}
		tlsConn = tls.Client(plainConn, &tls.Config{ServerName: host, InsecureSkipVerify: ecfg.SkipTLSVerify})
	}

	p := &pop3Conn{
		conn: tlsConn,
		rw:   bufio.NewReadWriter(bufio.NewReader(tlsConn), bufio.NewWriter(tlsConn)),
	}

	line, err := p.readLine()
	if err != nil {
		tlsConn.Close()
		return nil, fmt.Errorf("pop3 greeting: %w", err)
	}
	if !strings.HasPrefix(line, "+OK") {
		tlsConn.Close()
		return nil, fmt.Errorf("pop3 unexpected greeting: %s", line)
	}

	if _, err := p.cmd("USER %s", ecfg.Username); err != nil {
		tlsConn.Close()
		return nil, fmt.Errorf("pop3 user: %w", err)
	}
	if _, err := p.cmd("PASS %s", ecfg.Password); err != nil {
		tlsConn.Close()
		return nil, fmt.Errorf("pop3 pass: %w", err)
	}
	p.logged = true

	statLine, err := p.cmd("STAT")
	if err != nil {
		p.quit()
		return nil, fmt.Errorf("pop3 stat: %w", err)
	}
	parts := strings.Fields(statLine)
	if len(parts) >= 2 {
		p.count, _ = strconv.Atoi(parts[1])
	}

	return p, nil
}

func (p *pop3Conn) cmd(format string, args ...any) (string, error) {
	msg := fmt.Sprintf(format, args...)
	if _, err := p.rw.WriteString(msg + "\r\n"); err != nil {
		return "", err
	}
	if err := p.rw.Flush(); err != nil {
		return "", err
	}
	line, err := p.readLine()
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(line, "+OK") {
		return "", fmt.Errorf("POP3 error: %s", line)
	}
	return line, nil
}

func (p *pop3Conn) readLine() (string, error) {
	line, err := p.rw.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func (p *pop3Conn) messageCount() int {
	return p.count
}

func (p *pop3Conn) retrHeaders(n int) (string, error) {
	topCmd := fmt.Sprintf("TOP %d 0", n)
	if _, err := p.rw.WriteString(topCmd + "\r\n"); err != nil {
		return "", err
	}
	if err := p.rw.Flush(); err != nil {
		return "", err
	}

	line, err := p.readLine()
	if err != nil {
		return "", err
	}

	if strings.HasPrefix(line, "+OK") {
		return p.readMultiline(), nil
	}

	return p.retr(n)
}

func (p *pop3Conn) retr(n int) (string, error) {
	if _, err := p.cmd("RETR %d", n); err != nil {
		return "", err
	}
	return p.readMultiline(), nil
}

func (p *pop3Conn) readMultiline() string {
	var lines []string
	for {
		line, err := p.readLine()
		if err != nil {
			break
		}
		if line == "." {
			break
		}
		if strings.HasPrefix(line, "..") {
			line = line[1:]
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (p *pop3Conn) quit() {
	if !p.logged {
		return
	}
	p.rw.WriteString("QUIT\r\n")
	p.rw.Flush()
	p.conn.Close()
	p.logged = false
}

func formatDate(s string) string {
	t, err := time.Parse(time.RFC1123Z, s)
	if err == nil {
		return t.Format("2006-01-02 15:04")
	}
	t, err = time.Parse(time.RFC1123, s)
	if err == nil {
		return t.Format("2006-01-02 15:04")
	}
	return s
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
