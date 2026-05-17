package email

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/mail"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"dolphin/internal/config"
)

// ── Helpers ────────────────────────────────────────────────────────────

func emailConfig(addr string) *config.Config {
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	return &config.Config{
		Transport: config.TransportConfig{
			Email: config.EmailConfig{
				Username:      "test@example.com",
				Password:      "secret",
				From:          "test@example.com",
				SMTPHost:      host,
				SMTPPort:      port,
				IMAPHost:      host,
				IMAPPort:      port,
				SkipTLSVerify: true,
			},
		},
		MCP: config.MCPConfig{
			Email: config.EmailMCPConfig{
				Enabled:  true,
				Priority: 500,
			},
		},
	}
}

func emailToolForTest(t *testing.T, addr string) *Tool {
	return New(emailConfig(addr))
}

// ── Mock TCP servers ───────────────────────────────────────────────────

// testTLSConfig returns a TLS config with a self-signed cert for use in tests.
func testTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	cert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}
	return &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
	}
}

// startPOP3Server starts a minimal POP3+TLS server that never deletes messages.
// Returns address and a control channel for tests to push responses.
func startPOP3Server(t *testing.T, messages []pop3Message) string {
	ln, err := tls.Listen("tcp", "127.0.0.1:0", testTLSConfig(t))
	if err != nil {
		t.Fatalf("POP3 listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))

		brw := newBufioReadWriter(conn)

		// Greeting
		brw.WriteLine("+OK POP3 mock server ready")

		for {
			line, err := brw.ReadLine()
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)

			switch {
			case strings.HasPrefix(line, "USER"):
				brw.WriteLine("+OK")
			case strings.HasPrefix(line, "PASS"):
				brw.WriteLine("+OK")
			case line == "STAT":
				brw.WriteLine(fmt.Sprintf("+OK %d %d", len(messages), len(messages)*100))
			case strings.HasPrefix(line, "TOP"):
				if idx := parsePOP3Seq(line, "TOP"); idx >= 0 && idx < len(messages) {
					brw.WriteLine("+OK")
					brw.WriteMultiline(messages[idx].header)
				} else {
					brw.WriteLine("-ERR no such message")
				}
			case strings.HasPrefix(line, "RETR"):
				if idx := parsePOP3Seq(line, "RETR"); idx >= 0 && idx < len(messages) {
					brw.WriteLine("+OK")
					brw.WriteMultiline(messages[idx].header + "\n" + messages[idx].body)
				} else {
					brw.WriteLine("-ERR no such message")
				}
			case line == "QUIT":
				brw.WriteLine("+OK bye")
				return
			default:
				brw.WriteLine("-ERR unknown command")
			}
		}
	}()

	return ln.Addr().String()
}

type pop3Message struct {
	header string
	body   string
}

func parsePOP3Seq(line, cmd string) int {
	rest := strings.TrimSpace(strings.TrimPrefix(line, cmd))
	// Take only the first space-delimited token (TOP has "msg lines")
	if i := strings.IndexAny(rest, " \t"); i >= 0 {
		rest = rest[:i]
	}
	n, err := strconv.Atoi(rest)
	if err != nil {
		return -1
	}
	return n - 1 // 0-indexed
}

// startIMAPServer starts a minimal IMAP server for search/fetch testing.
func startIMAPServer(t *testing.T, msgs []imapMessage) string {
	ln, err := tls.Listen("tcp", "127.0.0.1:0", testTLSConfig(t))
	if err != nil {
		t.Fatalf("IMAP listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))

		brw := newBufioReadWriter(conn)

		// Greeting (untagged)
		brw.WriteLine("* OK [CAPABILITY IMAP4rev1 AUTH=PLAIN] IMAP mock ready")

		for {
			line, err := brw.ReadLine()
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			tag := ""
			if fields := strings.Fields(line); len(fields) > 0 {
				tag = fields[0]
			}
			rest := strings.TrimSpace(strings.TrimPrefix(line, tag))

			switch {
			case strings.HasPrefix(rest, "LOGIN"):
				brw.WriteLine(tag + " OK LOGIN completed")
			case strings.HasPrefix(rest, "SELECT"):
				brw.WriteLine("* " + strconv.Itoa(len(msgs)) + " EXISTS")
				brw.WriteLine("* OK [UIDNEXT 1]")
				brw.WriteLine(tag + " OK SELECT completed")
			case strings.HasPrefix(rest, "SEARCH"):
				// Return sequence numbers of all messages (filtering not implemented in mock)
				var seqs []string
				for i := range msgs {
					seqs = append(seqs, strconv.Itoa(i+1))
				}
				brw.WriteLine("* SEARCH " + strings.Join(seqs, " "))
				brw.WriteLine(tag + " OK SEARCH completed")
			case strings.HasPrefix(rest, "FETCH"):
				// Parse seq numbers: "FETCH 1:2 (ENVELOPE RFC822)"
				parts := strings.Fields(rest)
				if len(parts) >= 2 {
					seqStr := parts[1]
					// Parse range "1:2" -> start and end
					seqStart, seqEnd := 1, len(msgs)
					if idx := strings.IndexByte(seqStr, ':'); idx >= 0 {
						seqStart, _ = strconv.Atoi(seqStr[:idx])
						seqEnd, _ = strconv.Atoi(seqStr[idx+1:])
					} else if idx := strings.IndexByte(seqStr, ','); idx >= 0 {
						seqStart, _ = strconv.Atoi(seqStr[:idx])
						seqEnd = seqStart
					} else {
						seqStart, _ = strconv.Atoi(seqStr)
						seqEnd = seqStart
					}
					for seq := seqStart; seq >= 1 && seq <= len(msgs) && seq <= seqEnd; seq++ {
						m := msgs[seq-1]
						mailbox, host := m.mailbox, m.host
						if mailbox == "" {
							mailbox = "user"
						}
						if host == "" {
							host = "test.com"
						}
						raw := fmt.Sprintf("From: %s\r\nSubject: %s\r\nDate: %s\r\n\r\n%s",
							m.from, m.subject, m.date, m.body)
						// RFC822 literal
						brw.WriteRaw(fmt.Sprintf("* %d FETCH (RFC822 {%d}\r\n", seq, len(raw)))
						brw.WriteRaw(raw)
						// ENVELOPE after literal
						env := fmt.Sprintf(` ENVELOPE ("%s" "%s" (("%s" NIL "%s" "%s")) (("%s" NIL "%s" "%s")) (("%s" NIL "%s" "%s")) NIL NIL NIL NIL ""))`,
							m.date, m.subject,
							m.from, mailbox, host,
							m.from, mailbox, host,
							m.from, mailbox, host)
						brw.WriteLine(env)
					}
				}
				brw.WriteLine(tag + " OK FETCH completed")
			case strings.HasPrefix(rest, "LOGOUT"):
				brw.WriteLine("* BYE IMAP mock closing")
				brw.WriteLine(tag + " OK LOGOUT completed")
				return
			case strings.HasPrefix(rest, "NOOP"):
				brw.WriteLine(tag + " OK NOOP completed")
			default:
				brw.WriteLine(tag + " OK completed")
			}
		}
	}()

	return ln.Addr().String()
}

type imapMessage struct {
	from    string
	subject string
	date    string
	body    string
	mailbox string // envelope mailbox name (left of @)
	host    string // envelope host (right of @)
}

// simple buffered read-write helper for mock servers.
type bufioReadWriter struct {
	conn net.Conn
	buf  []byte // read buffer
}

func newBufioReadWriter(conn net.Conn) *bufioReadWriter {
	return &bufioReadWriter{conn: conn, buf: make([]byte, 0, 4096)}
}

func (b *bufioReadWriter) ReadLine() (string, error) {
	var line strings.Builder
	for {
		if len(b.buf) == 0 {
			n, err := b.conn.Read(b.buf[:cap(b.buf)])
			if err != nil {
				return "", err
			}
			b.buf = b.buf[:n]
		}
		for i, c := range b.buf {
			if c == '\n' {
				before := string(b.buf[:i])
				rest := b.buf[i+1:]
				if len(rest) > 0 {
					b.buf = rest
				} else {
					b.buf = b.buf[:0]
				}
				return strings.TrimRight(line.String()+before, "\r"), nil
			}
		}
		line.Write(b.buf)
		b.buf = b.buf[:0]
	}
}

func (b *bufioReadWriter) WriteLine(s string) {
	b.conn.Write([]byte(s + "\r\n"))
}

func (b *bufioReadWriter) WriteMultiline(s string) {
	// POP3 multi-line: dot-stuffed, terminated by \r\n.\r\n
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimRight(line, "\r")
		if strings.HasPrefix(trimmed, ".") {
			b.conn.Write([]byte("."))
		}
		b.conn.Write([]byte(trimmed + "\r\n"))
	}
	b.conn.Write([]byte(".\r\n"))
}

func (b *bufioReadWriter) WriteRaw(s string) {
	b.conn.Write([]byte(s))
}

// ── Mock SMTP Server ───────────────────────────────────────────────────

// startTestSMTPServer starts a minimal SMTP server that captures the message body.
func startTestSMTPServer(t *testing.T) (addr string, gotMsg chan string) {
	gotMsg = make(chan string, 1)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("SMTP listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))

		brw := newBufioReadWriter(conn)
		brw.WriteLine("220 localhost ESMTP test")

		for {
			line, err := brw.ReadLine()
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "EHLO") || strings.HasPrefix(line, "HELO"):
				brw.WriteLine("250-localhost")
				brw.WriteLine("250 AUTH PLAIN")
			case strings.HasPrefix(line, "AUTH"):
				brw.WriteLine("235 2.7.0 Authentication successful")
			case strings.HasPrefix(line, "MAIL FROM"):
				brw.WriteLine("250 2.1.0 Ok")
			case strings.HasPrefix(line, "RCPT TO"):
				brw.WriteLine("250 2.1.5 Ok")
			case strings.HasPrefix(line, "DATA"):
				brw.WriteLine("354 End data with <CR><LF>.<CR><LF>")
				var body strings.Builder
				for {
					b, err := brw.ReadLine()
					if err != nil {
						return
					}
					if b == "." {
						break
					}
					body.WriteString(b + "\n")
				}
				gotMsg <- body.String()
				brw.WriteLine("250 2.0.0 Ok: queued")
			case strings.HasPrefix(line, "QUIT"):
				brw.WriteLine("221 2.0.0 Bye")
				return
			default:
				brw.WriteLine("250 Ok")
			}
		}
	}()

	return ln.Addr().String(), gotMsg
}

func portFromAddr(addr string) int {
	_, port, _ := net.SplitHostPort(addr)
	var p int
	fmt.Sscanf(port, "%d", &p)
	return p
}

// ── Tests ──────────────────────────────────────────────────────────────

func TestEmailToolDefinition(t *testing.T) {
	tool := New(config.DefaultConfig())
	def := tool.Definition()
	if def.Name != "email" {
		t.Errorf("Name = %q, want %q", def.Name, "email")
	}
	if def.Source != "built-in" {
		t.Errorf("Source = %q, want %q", def.Source, "built-in")
	}
}

func TestEmailExecuteInvalidJSON(t *testing.T) {
	tool := New(config.DefaultConfig())
	result, err := tool.Execute(context.Background(), json.RawMessage(`{invalid}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
}

func TestEmailExecuteUnknownAction(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Transport.Email.Username = "user"
	cfg.Transport.Email.Password = "pass"
	tool := New(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"unknown"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for unknown action")
	}
}

func TestEmailExecuteNoConfig(t *testing.T) {
	tool := New(config.DefaultConfig())
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"send","to":"a@b.com","subject":"hi","body":"hello"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error when email not configured")
	}
}

func TestEmailSendMissingFields(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Transport.Email.Username = "user"
	cfg.Transport.Email.Password = "pass"
	tool := New(cfg)

	t.Run("missing to", func(t *testing.T) {
		result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"send","subject":"hi","body":"hello"}`))
		if err != nil {
			t.Fatalf("Execute error: %v", err)
		}
		if !result.IsError {
			t.Error("expected error for missing 'to'")
		}
	})

	t.Run("missing subject", func(t *testing.T) {
		result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"send","to":"a@b.com","body":"hello"}`))
		if err != nil {
			t.Fatalf("Execute error: %v", err)
		}
		if !result.IsError {
			t.Error("expected error for missing 'subject'")
		}
	})
}

func TestEmailSendViaMockSMTP(t *testing.T) {
	addr, gotMsg := startTestSMTPServer(t)
	cfg := emailConfig(addr)
	cfg.Transport.Email.UseTLS = false
	cfg.Transport.Email.SMTPPort = portFromAddr(addr)

	tool := New(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"send","to":"recipient@example.com","subject":"Test Subject","body":"Hello, this is a test."}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	select {
	case msg := <-gotMsg:
		if !strings.Contains(msg, "Test Subject") {
			t.Errorf("expected 'Test Subject' in message, got: %q", msg)
		}
		if !strings.Contains(msg, "Hello, this is a test.") {
			t.Errorf("expected body in message, got: %q", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for SMTP server")
	}
}

func TestEmailSendConnectionFailure(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Transport.Email.Username = "user"
	cfg.Transport.Email.Password = "pass"
	cfg.Transport.Email.SMTPHost = "127.0.0.1"
	cfg.Transport.Email.SMTPPort = 1 // nothing listening here
	cfg.Transport.Email.UseTLS = false

	tool := New(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"send","to":"a@b.com","subject":"hi","body":"hello"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for connection failure")
	}
}

func TestEmailFetchMissingSeq(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Transport.Email.Username = "user"
	cfg.Transport.Email.Password = "pass"
	tool := New(cfg)

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"fetch"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for missing seq")
	}
}

func TestEmailFetchIMAP(t *testing.T) {
	msgs := []imapMessage{
		{from: "alice@test.com", subject: "Hello IMAP", date: "Mon, 10 Mar 2025 10:00:00 +0000", body: "IMAP body content", mailbox: "alice", host: "test.com"},
	}
	addr := startIMAPServer(t, msgs)
	cfg := emailConfig(addr)

	tool := New(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"fetch","seq":1}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Hello IMAP") {
		t.Errorf("expected 'Hello IMAP' in content, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "IMAP body content") {
		t.Errorf("expected body in content, got: %s", result.Content)
	}
}

func TestEmailSearchIMAP(t *testing.T) {
	msgs := []imapMessage{
		{from: "alice@test.com", subject: "Meeting tomorrow", date: "Mon, 10 Mar 2025 10:00:00 +0000", body: "Let's meet", mailbox: "alice", host: "test.com"},
		{from: "bob@test.com", subject: "Hello World", date: "Tue, 11 Mar 2025 11:00:00 +0000", body: "Test body", mailbox: "bob", host: "test.com"},
	}
	addr := startIMAPServer(t, msgs)
	cfg := emailConfig(addr)

	tool := New(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"search","query":"hello","max_results":5}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Hello World") {
		t.Errorf("expected 'Hello World' in search results, got: %s", result.Content)
	}
}

// ── POP3 Tests ─────────────────────────────────────────────────────────

func TestEmailFetchPOP3(t *testing.T) {
	msgs := []pop3Message{
		{
			header: "From: alice@test.com\r\nSubject: Hello POP3\r\nDate: Mon, 10 Mar 2025 10:00:00 +0000",
			body:   "POP3 body line 1\nPOP3 body line 2",
		},
	}
	addr := startPOP3Server(t, msgs)
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)

	cfg := config.DefaultConfig()
	cfg.Transport.Email.Username = "test@example.com"
	cfg.Transport.Email.Password = "secret"
	cfg.Transport.Email.Protocol = "pop3"
	cfg.Transport.Email.POP3Host = host
	cfg.Transport.Email.POP3Port = port
	cfg.Transport.Email.IMAPHost = host
	cfg.Transport.Email.SkipTLSVerify = true
	cfg.Transport.Email.SkipTLSVerify = true

	tool := New(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"fetch","seq":1}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Hello POP3") {
		t.Errorf("expected 'Hello POP3' in content, got: %s", result.Content)
	}
}

func TestEmailSearchPOP3(t *testing.T) {
	msgs := []pop3Message{
		{
			header: "From: alice@test.com\r\nSubject: First Message\r\nDate: Mon, 10 Mar 2025 10:00:00 +0000",
			body:   "first body",
		},
		{
			header: "From: bob@test.com\r\nSubject: Second Message\r\nDate: Tue, 11 Mar 2025 11:00:00 +0000",
			body:   "second body",
		},
	}
	addr := startPOP3Server(t, msgs)
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)

	cfg := config.DefaultConfig()
	cfg.Transport.Email.Username = "test@example.com"
	cfg.Transport.Email.Password = "secret"
	cfg.Transport.Email.Protocol = "pop3"
	cfg.Transport.Email.POP3Host = host
	cfg.Transport.Email.POP3Port = port
	cfg.Transport.Email.IMAPHost = host
	cfg.Transport.Email.SkipTLSVerify = true

	tool := New(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"search","query":"second","max_results":5}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Second") {
		t.Errorf("expected 'Second' in results, got: %s", result.Content)
	}
}

func TestEmailSearchPOP3EmptyMailbox(t *testing.T) {
	addr := startPOP3Server(t, nil)
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)

	cfg := config.DefaultConfig()
	cfg.Transport.Email.Username = "test@example.com"
	cfg.Transport.Email.Password = "secret"
	cfg.Transport.Email.Protocol = "pop3"
	cfg.Transport.Email.POP3Host = host
	cfg.Transport.Email.POP3Port = port
	cfg.Transport.Email.IMAPHost = host
	cfg.Transport.Email.SkipTLSVerify = true

	tool := New(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"search","query":"anything","max_results":5}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "No messages") {
		t.Errorf("expected 'No messages' for empty mailbox, got: %s", result.Content)
	}
}

func TestEmailSearchPOP3QueryFilter(t *testing.T) {
	msgs := []pop3Message{
		{
			header: "From: alice@test.com\r\nSubject: Alpha Report\r\nDate: Mon, 10 Mar 2025 10:00:00 +0000",
			body:   "alpha data",
		},
		{
			header: "From: bob@test.com\r\nSubject: Beta Report\r\nDate: Tue, 11 Mar 2025 11:00:00 +0000",
			body:   "beta data",
		},
		{
			header: "From: charlie@test.com\r\nSubject: Gamma Report\r\nDate: Wed, 12 Mar 2025 12:00:00 +0000",
			body:   "gamma data",
		},
	}
	addr := startPOP3Server(t, msgs)
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)

	cfg := config.DefaultConfig()
	cfg.Transport.Email.Username = "test@example.com"
	cfg.Transport.Email.Password = "secret"
	cfg.Transport.Email.Protocol = "pop3"
	cfg.Transport.Email.POP3Host = host
	cfg.Transport.Email.POP3Port = port
	cfg.Transport.Email.IMAPHost = host
	cfg.Transport.Email.SkipTLSVerify = true

	tool := New(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"search","query":"beta","max_results":5}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if strings.Contains(result.Content, "Alpha") {
		t.Errorf("query 'beta' should not match 'Alpha', got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Beta") {
		t.Errorf("query 'beta' should match 'Beta', got: %s", result.Content)
	}
}

func TestEmailSearchPOP3MaxResults(t *testing.T) {
	msgs := make([]pop3Message, 20)
	for i := 0; i < 20; i++ {
		msgs[i] = pop3Message{
			header: fmt.Sprintf("From: user%d@test.com\r\nSubject: Message %d\r\nDate: Mon, 10 Mar 2025 10:00:00 +0000", i, i),
			body:   fmt.Sprintf("body %d", i),
		}
	}
	addr := startPOP3Server(t, msgs)
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)

	cfg := config.DefaultConfig()
	cfg.Transport.Email.Username = "test@example.com"
	cfg.Transport.Email.Password = "secret"
	cfg.Transport.Email.Protocol = "pop3"
	cfg.Transport.Email.POP3Host = host
	cfg.Transport.Email.POP3Port = port
	cfg.Transport.Email.IMAPHost = host
	cfg.Transport.Email.SkipTLSVerify = true

	tool := New(cfg)

	// max_results = 3, should scan only last 3 of 20 messages
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"search","query":"message","max_results":3}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	// Should find Message 17, 18, 19 (most recent 3)
	if strings.Contains(result.Content, "Message 0") {
		t.Errorf("max_results=3 should not scan message 0, got: %s", result.Content)
	}
}

func TestEmailPOP3NoDelete(t *testing.T) {
	// Verify no DELE command is ever sent by using a server that rejects DELE
	deleReceived := false
	ln, err := tls.Listen("tcp", "127.0.0.1:0", testTLSConfig(t))
	if err != nil {
		t.Fatalf("POP3 listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))

		brw := newBufioReadWriter(conn)
		brw.WriteLine("+OK POP3 mock")

		for {
			line, err := brw.ReadLine()
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "USER"):
				brw.WriteLine("+OK")
			case strings.HasPrefix(line, "PASS"):
				brw.WriteLine("+OK")
			case line == "STAT":
				brw.WriteLine("+OK 1 100")
			case strings.HasPrefix(line, "TOP"):
				brw.WriteLine("+OK")
				brw.WriteMultiline("From: test@test.com\r\nSubject: Test\r\nDate: Mon, 10 Mar 2025 10:00:00 +0000")
			case strings.HasPrefix(line, "DELE"):
				deleReceived = true
				brw.WriteLine("-ERR not supported in test")
			case line == "QUIT":
				brw.WriteLine("+OK bye")
				return
			default:
				brw.WriteLine("-ERR unknown")
			}
		}
	}()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	cfg := config.DefaultConfig()
	cfg.Transport.Email.Username = "test@example.com"
	cfg.Transport.Email.Password = "secret"
	cfg.Transport.Email.Protocol = "pop3"
	cfg.Transport.Email.POP3Host = host
	cfg.Transport.Email.POP3Port = port
	cfg.Transport.Email.IMAPHost = host
	cfg.Transport.Email.SkipTLSVerify = true

	tool := New(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"search","query":"test","max_results":5}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if deleReceived {
		t.Error("POP3 client issued DELE command, violating constraint that remote messages must not be deleted")
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
}

func TestEmailPOP3ConnectionFailure(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Transport.Email.Username = "test@example.com"
	cfg.Transport.Email.Password = "secret"
	cfg.Transport.Email.Protocol = "pop3"
	cfg.Transport.Email.POP3Host = "127.0.0.1"
	cfg.Transport.Email.POP3Port = 1

	tool := New(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"search","query":"test","max_results":5}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Error("expected error for POP3 connection failure")
	}
}

func TestEmailToolSchema(t *testing.T) {
	tool := New(config.DefaultConfig())
	def := tool.Definition()

	var schema map[string]any
	if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema missing properties")
	}

	action, ok := props["action"].(map[string]any)
	if !ok {
		t.Fatal("schema missing action property")
	}

	enum, ok := action["enum"].([]any)
	if !ok {
		t.Fatal("action missing enum")
	}

	actions := make([]string, len(enum))
	for i, v := range enum {
		actions[i] = v.(string)
	}

	expected := []string{"send", "search", "fetch"}
	for _, e := range expected {
		found := false
		for _, a := range actions {
			if a == e {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("schema enum missing action %q, got %v", e, actions)
		}
	}

	// Verify attachments field in schema
	if _, ok := props["attachments"]; !ok {
		t.Error("schema missing attachments property")
	}
}

// ── Attachment tests ────────────────────────────────────────────────────

func TestEmailSendWithAttachment(t *testing.T) {
	// Create a temp file to attach
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(filePath, []byte("attachment content here"), 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	addr, gotMsg := startTestSMTPServer(t)
	cfg := emailConfig(addr)
	cfg.Transport.Email.UseTLS = false
	cfg.Transport.Email.SMTPPort = portFromAddr(addr)

	tool := New(cfg)
	input := fmt.Sprintf(`{"action":"send","to":"r@x.com","subject":"With Attachment","body":"See attached.","attachments":[{"file_path":"%s"}]}`, filePath)
	result, err := tool.Execute(context.Background(), json.RawMessage(input))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	select {
	case msg := <-gotMsg:
		if !strings.Contains(msg, "Content-Type: multipart/mixed") {
			t.Errorf("expected multipart/mixed content type, got: %q", msg)
		}
		if !strings.Contains(msg, "Content-Disposition: attachment; filename=\"test.txt\"") {
			t.Errorf("expected attachment disposition, got: %q", msg)
		}
		if !strings.Contains(msg, "See attached.") {
			t.Errorf("expected text body in multipart, got: %q", msg)
		}
		if !strings.Contains(msg, "attachment content here") {
			t.Errorf("expected attachment content, got: %q", msg)
		}
		if !strings.Contains(result.Content, "attachments: test.txt") {
			t.Errorf("expected attachment name in result, got: %q", result.Content)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for SMTP server")
	}
}

func TestEmailSendWithMultipleAttachments(t *testing.T) {
	tmpDir := t.TempDir()
	files := []string{"a.txt", "b.pdf"}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(name+" content"), 0600); err != nil {
			t.Fatalf("write temp file %s: %v", name, err)
		}
	}

	addr, gotMsg := startTestSMTPServer(t)
	cfg := emailConfig(addr)
	cfg.Transport.Email.UseTLS = false
	cfg.Transport.Email.SMTPPort = portFromAddr(addr)

	tool := New(cfg)
	input := fmt.Sprintf(`{"action":"send","to":"r@x.com","subject":"Two Files","body":"Here are two files.","attachments":[{"file_path":"%s"},{"file_path":"%s"}]}`,
		filepath.Join(tmpDir, "a.txt"), filepath.Join(tmpDir, "b.pdf"))
	result, err := tool.Execute(context.Background(), json.RawMessage(input))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "attachments: a.txt, b.pdf") {
		t.Errorf("expected both attachment names, got: %q", result.Content)
	}

	select {
	case msg := <-gotMsg:
		if !strings.Contains(msg, `attachment; filename="a.txt"`) {
			t.Errorf("missing a.txt attachment, got: %q", msg)
		}
		if !strings.Contains(msg, `attachment; filename="b.pdf"`) {
			t.Errorf("missing b.pdf attachment, got: %q", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for SMTP server")
	}
}

func TestEmailSendAttachmentNotFound(t *testing.T) {
	addr, _ := startTestSMTPServer(t)
	cfg := emailConfig(addr)
	cfg.Transport.Email.UseTLS = false
	cfg.Transport.Email.SMTPPort = portFromAddr(addr)

	tool := New(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"send","to":"r@x.com","subject":"Bad Attach","body":"test","attachments":[{"file_path":"/nonexistent/file.txt"}]}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for missing attachment file")
	}
	if !strings.Contains(result.Content, "Cannot read attachment") {
		t.Errorf("expected 'Cannot read attachment' error, got: %q", result.Content)
	}
}

func TestEmailSendAttachmentEmptyPath(t *testing.T) {
	addr, gotMsg := startTestSMTPServer(t)
	cfg := emailConfig(addr)
	cfg.Transport.Email.UseTLS = false
	cfg.Transport.Email.SMTPPort = portFromAddr(addr)

	tool := New(cfg)
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"send","to":"r@x.com","subject":"Empty Path","body":"test","attachments":[{"file_path":""}]}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	// Should have sent as plain text (empty file_path is skipped)
	select {
	case msg := <-gotMsg:
		if strings.Contains(msg, "multipart/mixed") {
			t.Error("empty file_path should not produce multipart")
		}
		if !strings.Contains(msg, "Content-Type: text/plain") {
			t.Errorf("expected text/plain for no-attach send, got: %q", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for SMTP server")
	}
}

// ── Multipart parse tests ───────────────────────────────────────────────

func TestFormatParsedMessageMultipart(t *testing.T) {
	// Build a multipart/mixed message similar to what our send() produces
	var raw strings.Builder
	raw.WriteString("From: sender@test.com\r\n")
	raw.WriteString("To: recipient@test.com\r\n")
	raw.WriteString("Subject: Multipart Test\r\n")
	raw.WriteString("Date: Mon, 10 Mar 2025 10:00:00 +0000\r\n")
	raw.WriteString("MIME-Version: 1.0\r\n")
	raw.WriteString("Content-Type: multipart/mixed; boundary=\"testboundary\"\r\n")
	raw.WriteString("\r\n")
	raw.WriteString("--testboundary\r\n")
	raw.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	raw.WriteString("\r\n")
	raw.WriteString("This is the text body.\r\n")
	raw.WriteString("--testboundary\r\n")
	raw.WriteString("Content-Type: application/pdf\r\n")
	raw.WriteString("Content-Disposition: attachment; filename=\"report.pdf\"\r\n")
	raw.WriteString("Content-Transfer-Encoding: base64\r\n")
	raw.WriteString("\r\n")
	raw.WriteString("fakebase64data\r\n")
	raw.WriteString("--testboundary--\r\n")

	result := formatParsedMessage(raw.String(), 1, func(hdr mail.Header) string {
		return fmt.Sprintf("From: %s\nSubject: %s\nSeq: 1", hdr.Get("From"), hdr.Get("Subject"))
	})

	if !strings.Contains(result, "Multipart Test") {
		t.Errorf("expected subject in result, got: %q", result)
	}
	if !strings.Contains(result, "This is the text body.") {
		t.Errorf("expected text body in result, got: %q", result)
	}
	if !strings.Contains(result, "Attachments:") {
		t.Errorf("expected Attachments section, got: %q", result)
	}
	if !strings.Contains(result, "report.pdf") {
		t.Errorf("expected report.pdf attachment, got: %q", result)
	}
	if !strings.Contains(result, "application/pdf") {
		t.Errorf("expected application/pdf mime type, got: %q", result)
	}
}

func TestFormatParsedMessagePlainText(t *testing.T) {
	raw := "From: sender@test.com\r\nSubject: Plain Text\r\nDate: Mon, 10 Mar 2025 10:00:00 +0000\r\n\r\nPlain body here."

	result := formatParsedMessage(raw, 1, func(hdr mail.Header) string {
		return fmt.Sprintf("From: %s\nSubject: %s\nSeq: 1", hdr.Get("From"), hdr.Get("Subject"))
	})

	if !strings.Contains(result, "Plain Text") {
		t.Errorf("expected subject, got: %q", result)
	}
	if !strings.Contains(result, "Plain body here.") {
		t.Errorf("expected body, got: %q", result)
	}
	if strings.Contains(result, "Attachments:") {
		t.Error("plain text should not have Attachments section")
	}
}

func TestFormatParsedMessageParseFailure(t *testing.T) {
	raw := "not a valid email message"

	result := formatParsedMessage(raw, 5, func(hdr mail.Header) string {
		return "should not reach"
	})

	if !strings.Contains(result, "parse failed") {
		t.Errorf("expected parse failure note, got: %q", result)
	}
	if !strings.Contains(result, "Seq: 5") {
		t.Errorf("expected seq number, got: %q", result)
	}
}
