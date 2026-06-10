package main

import (
	"bytes"
	"encoding/base64"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

func TestConfigLoad(t *testing.T) {
	cfg := &Config{
		IMAPServer: "imap.example.com",
		IMAPPort:   "993",
		SMTPServer: "smtp.example.com",
		SMTPPort:   "465",
		Email:      "user@example.com",
		Password:   "secret",
	}
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.IMAPPort != "993" {
		t.Errorf("IMAPPort = %q", cfg.IMAPPort)
	}
	if cfg.SMTPPort != "465" {
		t.Errorf("SMTPPort = %q", cfg.SMTPPort)
	}
}

func TestConfigLoadMissing(t *testing.T) {
	cfg := &Config{}
	err := cfg.Load()
	if err == nil {
		t.Fatal("Load() expected error for missing config")
	}
	if !strings.Contains(err.Error(), "imap-server") {
		t.Errorf("error missing imap-server: %v", err)
	}
	if !strings.Contains(err.Error(), "email") {
		t.Errorf("error missing email: %v", err)
	}
	if !strings.Contains(err.Error(), "password") {
		t.Errorf("error missing password: %v", err)
	}
}

func TestConfigLoadEnv(t *testing.T) {
	t.Setenv("MAIL_IMAP_SERVER", "imap.env.test")
	t.Setenv("MAIL_SMTP_SERVER", "smtp.env.test")
	t.Setenv("MAIL_EMAIL", "env@test.com")
	t.Setenv("MAIL_PASSWORD", "envpass")

	cfg := &Config{IMAPPort: "993", SMTPPort: "465"}
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.IMAPServer != "imap.env.test" {
		t.Errorf("IMAPServer = %q, want %q", cfg.IMAPServer, "imap.env.test")
	}
	if cfg.SMTPServer != "smtp.env.test" {
		t.Errorf("SMTPServer = %q, want %q", cfg.SMTPServer, "smtp.env.test")
	}
	if cfg.Email != "env@test.com" {
		t.Errorf("Email = %q, want %q", cfg.Email, "env@test.com")
	}
	if cfg.Password != "envpass" {
		t.Errorf("Password = %q, want %q", cfg.Password, "envpass")
	}
}

func TestConfigSMTPServerFallback(t *testing.T) {
	cfg := &Config{
		IMAPServer: "imap.example.com",
		SMTPServer: "",
		Email:      "x@y.com",
		Password:   "p",
	}
	if err := cfg.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.SMTPServer != "imap.example.com" {
		t.Errorf("SMTPServer = %q, want imap fallback", cfg.SMTPServer)
	}
}

func TestConfigAddr(t *testing.T) {
	cfg := &Config{
		IMAPServer: "imap.test",
		IMAPPort:   "993",
		SMTPServer: "smtp.test",
		SMTPPort:   "587",
	}
	if cfg.IMAPAddr() != "imap.test:993" {
		t.Errorf("IMAPAddr() = %q", cfg.IMAPAddr())
	}
	if cfg.SMTPAddr() != "smtp.test:587" {
		t.Errorf("SMTPAddr() = %q", cfg.SMTPAddr())
	}
}

// ---------------------------------------------------------------------------
// buildMessage tests
// ---------------------------------------------------------------------------

func TestBuildMessagePlain(t *testing.T) {
	msg := buildMessage("a@x.com", "b@y.com", "Hello", "plain text body")

	if !strings.Contains(msg, "From: a@x.com") {
		t.Error("missing From header")
	}
	if !strings.Contains(msg, "To: b@y.com") {
		t.Error("missing To header")
	}
	if !strings.Contains(msg, "Subject: Hello") {
		t.Error("missing Subject header")
	}
	if !strings.Contains(msg, "text/plain") {
		t.Error("missing text/plain content type")
	}
	if !strings.Contains(msg, "plain text body") {
		t.Error("missing body text")
	}
}

func TestBuildMessageMarkdown(t *testing.T) {
	body := "# Title\n\n**bold** text"
	msg := buildMessage("a@x.com", "b@y.com", "MD", body)

	if !strings.Contains(msg, "multipart/alternative") {
		t.Error("expected multipart/alternative for markdown body")
	}
	if !strings.Contains(msg, "text/html") {
		t.Error("expected HTML part for markdown body")
	}
	if !strings.Contains(msg, "<h1>") {
		t.Error("expected rendered HTML heading")
	}
	if !strings.Contains(msg, "<strong>") {
		t.Error("expected rendered bold tag")
	}
	// Plain text part should still be present
	if !strings.Contains(msg, "# Title") {
		t.Error("missing plain text part")
	}
}

func TestBuildMessageEmptySubject(t *testing.T) {
	msg := buildMessage("a@x.com", "b@y.com", "", "body")
	if !strings.Contains(msg, "Subject: ") {
		t.Error("missing Subject header")
	}
}

func TestBuildMessageCRLF(t *testing.T) {
	msg := buildMessage("a@x.com", "b@y.com", "Test", "body")
	// SMTP requires CRLF line endings — verify first line ends with \r\n.
	if !strings.HasPrefix(msg, "From:") {
		t.Error("should start with From:")
	}
	if !strings.Contains(msg, "\r\n") {
		t.Error("missing CRLF line endings")
	}
}

// ---------------------------------------------------------------------------
// addrString tests
// ---------------------------------------------------------------------------

func TestAddrString(t *testing.T) {
	tests := []struct {
		name  string
		addrs []imap.Address
		want  string
	}{
		{"empty", nil, ""},
		{"no host", []imap.Address{{Mailbox: "user"}}, "user"},
		{"full", []imap.Address{{Mailbox: "user", Host: "example.com"}}, "user@example.com"},
		{"multiple takes first", []imap.Address{
			{Mailbox: "first", Host: "a.com"},
			{Mailbox: "second", Host: "b.com"},
		}, "first@a.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := addrString(tt.addrs); got != tt.want {
				t.Errorf("addrString() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// dateString tests
// ---------------------------------------------------------------------------

func TestDateString(t *testing.T) {
	if got := dateString(time.Time{}); got != "" {
		t.Errorf("dateString(zero) = %q, want empty", got)
	}
	now := time.Date(2025, 6, 2, 14, 30, 0, 0, time.UTC)
	if got := dateString(now); got != "2025-06-02 14:30" {
		t.Errorf("dateString() = %q", got)
	}
}

// ---------------------------------------------------------------------------
// decodeTransfer tests
// ---------------------------------------------------------------------------

func TestDecodeTransfer(t *testing.T) {
	tests := []struct {
		name string
		enc  string
		text string
		want string
	}{
		{"none", "", "hello", "hello"},
		{"base64", "base64", base64.StdEncoding.EncodeToString([]byte("decoded")), "decoded"},
		{"base64 mixed case", "Base64", base64.StdEncoding.EncodeToString([]byte("ok")), "ok"},
		{"base64 invalid", "base64", "!!!invalid!!!", "!!!invalid!!!"},
		{"quoted-printable", "quoted-printable", "=48=65=6c=6c=6f", "Hello"},
		{"qp case", "Quoted-Printable", "=48=65", "He"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decodeTransfer(tt.enc, tt.text); got != tt.want {
				t.Errorf("decodeTransfer() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// mimeText tests
// ---------------------------------------------------------------------------

func TestMimeTextEmpty(t *testing.T) {
	if got := mimeText(""); got != "" {
		t.Errorf("mimeText('') = %q, want empty", got)
	}
	if got := mimeText("  "); got != "" {
		t.Errorf("mimeText('  ') = %q, want empty", got)
	}
}

func TestMimeTextPlain(t *testing.T) {
	body := "Hello World"
	if got := mimeText(body); got != "Hello World" {
		t.Errorf("mimeText() = %q", got)
	}
}

func TestMimeTextBase64(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("base64 content"))
	if got := mimeText(encoded); got != "base64 content" {
		t.Errorf("mimeText(base64) = %q", got)
	}
}

func TestMimeTextQuotedPrintable(t *testing.T) {
	// quoted-printable inside multipart.
	body := "--boundary\r\n" +
		"Content-Type: text/plain; charset=\"UTF-8\"\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n" +
		"\r\n" +
		"=48=65=6c=6c=6f\r\n" +
		"--boundary--\r\n"
	if got := mimeText(body); got != "Hello" {
		t.Errorf("mimeText(qp) = %q", got)
	}
}

func TestMimeTextMultipart(t *testing.T) {
	body := "--boundary\r\n" +
		"Content-Type: text/plain; charset=\"UTF-8\"\r\n" +
		"\r\n" +
		"plain part\r\n" +
		"--boundary\r\n" +
		"Content-Type: text/html; charset=\"UTF-8\"\r\n" +
		"\r\n" +
		"<html>html part</html>\r\n" +
		"--boundary--\r\n"

	got := mimeText(body)
	if got != "plain part" {
		t.Errorf("mimeText(multipart) = %q, want %q", got, "plain part")
	}
}

func TestMimeTextMultipartBase64(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("base64 decoded"))
	body := "--boundary\r\n" +
		"Content-Type: text/plain; charset=\"UTF-8\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n" +
		"\r\n" +
		encoded + "\r\n" +
		"--boundary--\r\n"

	if got := mimeText(body); got != "base64 decoded" {
		t.Errorf("mimeText(base64 multipart) = %q", got)
	}
}

func TestMimeTextMultipartNoText(t *testing.T) {
	// Only non-text parts — should return empty.
	body := "--boundary\r\n" +
		"Content-Type: application/octet-stream\r\n" +
		"\r\n" +
		"binary\r\n" +
		"--boundary--\r\n"

	if got := mimeText(body); got != "" {
		t.Errorf("mimeText(no text) = %q, want empty", got)
	}
}

func TestMimeTextInvalidMultipart(t *testing.T) {
	body := "--\n"
	got := mimeText(body)
	// TrimSpace strips the trailing newline, so we get "--".
	if got != "--" {
		t.Errorf("mimeText(invalid multipart) = %q, want %q", got, "--")
	}
}

// ---------------------------------------------------------------------------
// readMailsWithClient tests (integration via in-memory IMAP server)
// ---------------------------------------------------------------------------

const testUsername = "test@example.com"
const testPassword = "pass"

func newTestIMAPServer(t *testing.T) (net.Listener, *imapserver.Server) {
	t.Helper()
	memServer := imapmemserver.New()
	user := imapmemserver.NewUser(testUsername, testPassword)
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatalf("Create INBOX: %v", err)
	}
	memServer.AddUser(user)

	srv := imapserver.New(&imapserver.Options{
		NewSession: func(conn *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return memServer.NewSession(), nil, nil
		},
		InsecureAuth: true,
		Caps: imap.CapSet{
			imap.CapIMAP4rev1: {},
			imap.CapIMAP4rev2: {},
		},
	})

	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	go func() {
		if err := srv.Serve(ln); err != nil {
			t.Logf("IMAP server exited: %v", err)
		}
	}()
	return ln, srv
}

func dialTestIMAP(t *testing.T, ln net.Listener) *imapclient.Client {
	t.Helper()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	client := imapclient.New(conn, nil)
	if err := client.Login(testUsername, testPassword).Wait(); err != nil {
		t.Fatalf("Login: %v", err)
	}
	return client
}

func appendMsg(t *testing.T, client *imapclient.Client, raw string) {
	t.Helper()
	appendCmd := client.Append("INBOX", int64(len(raw)), nil)
	_, err := appendCmd.Write([]byte(raw))
	if err != nil {
		t.Fatalf("Append Write: %v", err)
	}
	if err := appendCmd.Close(); err != nil {
		t.Fatalf("Append Close: %v", err)
	}
	if _, err := appendCmd.Wait(); err != nil {
		t.Fatalf("Append Wait: %v", err)
	}
}

const testMsg1 = "From: alice@example.com\r\nSubject: Hello\r\nDate: Mon, 02 Jun 2025 10:00:00 +0000\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nFirst message body\r\n"
const testMsg2 = "From: bob@example.com\r\nSubject: Meeting\r\nDate: Tue, 03 Jun 2025 14:30:00 +0000\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nMeeting at 3pm\r\n"

func TestReadMailsWithClient_EmptyInbox(t *testing.T) {
	ln, srv := newTestIMAPServer(t)
	defer srv.Close()

	client := dialTestIMAP(t, ln)
	var buf bytes.Buffer
	err := readMailsWithClient(&buf, client, 10)
	if err != nil {
		t.Fatalf("readMailsWithClient: %v", err)
	}
	if !strings.Contains(buf.String(), "inbox is empty") {
		t.Errorf("expected 'inbox is empty', got: %q", buf.String())
	}
	_ = client.Logout().Wait()
	_ = client.Close()
}

func TestReadMailsWithClient_SingleMessage(t *testing.T) {
	ln, srv := newTestIMAPServer(t)
	defer srv.Close()

	client := dialTestIMAP(t, ln)
	appendMsg(t, client, testMsg1)

	var buf bytes.Buffer
	err := readMailsWithClient(&buf, client, 10)
	if err != nil {
		t.Fatalf("readMailsWithClient: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Hello") {
		t.Errorf("output missing subject 'Hello': %q", output)
	}
	if !strings.Contains(output, "alice@example.com") {
		t.Errorf("output missing sender: %q", output)
	}
	if !strings.Contains(output, "ID") {
		t.Errorf("output missing table header: %q", output)
	}
	if !strings.Contains(output, "Seen") {
		t.Errorf("output missing Seen column: %q", output)
	}
	_ = client.Logout().Wait()
	_ = client.Close()
}

func TestReadMailsWithClient_MultipleMessages(t *testing.T) {
	ln, srv := newTestIMAPServer(t)
	defer srv.Close()

	client := dialTestIMAP(t, ln)
	appendMsg(t, client, testMsg1)
	appendMsg(t, client, testMsg2)

	var buf bytes.Buffer
	err := readMailsWithClient(&buf, client, 10)
	if err != nil {
		t.Fatalf("readMailsWithClient: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Hello") {
		t.Errorf("output missing first subject: %q", output)
	}
	if !strings.Contains(output, "Meeting") {
		t.Errorf("output missing second subject: %q", output)
	}
	if !strings.Contains(output, "alice@example.com") {
		t.Error("table missing alice@example.com")
	}
	if !strings.Contains(output, "bob@example.com") {
		t.Error("table missing bob@example.com")
	}
	_ = client.Logout().Wait()
	_ = client.Close()
}

func TestReadMailsWithClient_Limit(t *testing.T) {
	ln, srv := newTestIMAPServer(t)
	defer srv.Close()

	client := dialTestIMAP(t, ln)
	appendMsg(t, client, testMsg1)
	appendMsg(t, client, testMsg2)

	var buf bytes.Buffer
	// Limit 1 — only the newest (last appended) message should appear.
	err := readMailsWithClient(&buf, client, 1)
	if err != nil {
		t.Fatalf("readMailsWithClient: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Meeting") {
		t.Errorf("output missing newest subject: %q", output)
	}
	if strings.Contains(output, "First message body") {
		t.Errorf("limit=1 should exclude old message body, got: %q", output)
	}
	_ = client.Logout().Wait()
	_ = client.Close()
}

func TestReadMailsWithClient_SeenMessage(t *testing.T) {
	ln, srv := newTestIMAPServer(t)
	defer srv.Close()

	client := dialTestIMAP(t, ln)
	const seenMsg = "From: charlie@example.com\r\nSubject: Seen One\r\nDate: Mon, 02 Jun 2025 08:00:00 +0000\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nThis is seen\r\n"
	appendCmd := client.Append("INBOX", int64(len(seenMsg)), &imap.AppendOptions{
		Flags: []imap.Flag{imap.FlagSeen},
	})
	_, _ = appendCmd.Write([]byte(seenMsg))
	_ = appendCmd.Close()
	if _, err := appendCmd.Wait(); err != nil {
		t.Fatalf("Append Wait: %v", err)
	}
	appendMsg(t, client, testMsg1)

	var buf bytes.Buffer
	err := readMailsWithClient(&buf, client, 10)
	if err != nil {
		t.Fatalf("readMailsWithClient: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Seen One") {
		t.Errorf("output missing seen subject: %q", output)
	}
	_ = client.Logout().Wait()
	_ = client.Close()
}

func TestReadMailsWithClient_SelectError(t *testing.T) {
	memServer := imapmemserver.New()
	user := imapmemserver.NewUser(testUsername, testPassword)
	memServer.AddUser(user)

	srv := imapserver.New(&imapserver.Options{
		NewSession: func(conn *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return memServer.NewSession(), nil, nil
		},
		InsecureAuth: true,
		Caps: imap.CapSet{imap.CapIMAP4rev1: {}},
	})
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	conn, _ := net.Dial("tcp", ln.Addr().String())
	client := imapclient.New(conn, nil)
	if err := client.Login(testUsername, testPassword).Wait(); err != nil {
		t.Fatalf("Login: %v", err)
	}

	var buf bytes.Buffer
	err = readMailsWithClient(&buf, client, 10)
	if err == nil {
		t.Fatal("expected error selecting non-existent mailbox")
	}
	if !strings.Contains(err.Error(), "imap select") {
		t.Errorf("error should mention imap select, got: %v", err)
	}
	_ = client.Logout().Wait()
	_ = client.Close()
}
