package email

import (
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"dolphin/internal/config"
)

func newEmailTransportWithSender(cfg *config.EmailConfig, sender string) *EmailTransport {
	tp := &EmailTransport{
		cfg:       cfg,
		msgCh:     make(chan string, 1024),
		closeCh:   make(chan struct{}),
		startTime: time.Now(),
	}
	tp.lastSender = sender
	return tp
}

func startTestSMTPServer(t *testing.T) (addr string, gotMsg chan string) {
	gotMsg = make(chan string, 1)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))

		conn.Write([]byte("220 localhost ESMTP test\r\n"))

		readLine := func() (string, error) {
			var buf strings.Builder
			tmp := make([]byte, 1)
			for {
				n, err := conn.Read(tmp)
				if err != nil {
					return "", err
				}
				if n == 0 {
					continue
				}
				if tmp[0] == '\n' {
					return strings.TrimRight(buf.String(), "\r"), nil
				}
				buf.WriteByte(tmp[0])
			}
		}

		reply := func(code string) {
			conn.Write([]byte(code + "\r\n"))
		}

		for {
			line, err := readLine()
			if err != nil {
				return
			}
			if strings.HasPrefix(line, "EHLO") || strings.HasPrefix(line, "HELO") {
				reply("250-localhost")
				reply("250 AUTH PLAIN")
			} else if strings.HasPrefix(line, "AUTH") {
				reply("235 2.7.0 Authentication successful")
			} else if strings.HasPrefix(line, "MAIL FROM") {
				reply("250 2.1.0 Ok")
			} else if strings.HasPrefix(line, "RCPT TO") {
				reply("250 2.1.5 Ok")
			} else if strings.HasPrefix(line, "DATA") {
				reply("354 End data with <CR><LF>.<CR><LF>")
				var body strings.Builder
				for {
					b, err := readLine()
					if err != nil {
						return
					}
					if b == "." {
						break
					}
					body.WriteString(b + "\n")
				}
				gotMsg <- body.String()
				reply("250 2.0.0 Ok: queued")
			} else if strings.HasPrefix(line, "QUIT") {
				reply("221 2.0.0 Bye")
				return
			}
		}
	}()

	return ln.Addr().String(), gotMsg
}

func TestEmailOnConfigChangeUpdatesCfg(t *testing.T) {
	tp := &EmailTransport{
		cfg:     &config.EmailConfig{IMAPHost: "old.host", PollInterval: "30s"},
		msgCh:   make(chan string, 1024),
		closeCh: make(chan struct{}),
	}

	oldCfg := &config.Config{}
	oldCfg.Transport.Email = config.EmailConfig{IMAPHost: "old.host", PollInterval: "30s"}

	newCfg := &config.Config{}
	newCfg.Transport.Email = config.EmailConfig{IMAPHost: "new.host", PollInterval: "10s"}

	tp.OnConfigChange(oldCfg, newCfg)

	if tp.cfg.IMAPHost != "new.host" {
		t.Errorf("cfg.IMAPHost = %q, want new.host", tp.cfg.IMAPHost)
	}
	if tp.cfg.PollInterval != "10s" {
		t.Errorf("cfg.PollInterval = %q, want 10s", tp.cfg.PollInterval)
	}
}

func TestEmailTransportName(t *testing.T) {
	tp := &EmailTransport{}
	if n := tp.Name(); n != "email" {
		t.Errorf("Name() = %q", n)
	}
}

func TestEmailTransportCapabilities(t *testing.T) {
	tp := &EmailTransport{}
	caps := tp.Capabilities()
	if caps.Streaming {
		t.Errorf("expected Streaming=false for email")
	}
	if caps.ConfirmExit {
		t.Errorf("expected ConfirmExit=false for email")
	}
}

func TestEmailTransportSendMailWithPort(t *testing.T) {
	addr, gotMsg := startTestSMTPServer(t)

	cfg := &config.EmailConfig{
		SMTPHost: "localhost",
		SMTPPort: portFromAddr(addr),
		Username: "test@example.com",
		Password: "pass",
		From:     "test@example.com",
		UseTLS:   false,
	}
	tp := newEmailTransportWithSender(cfg, "recipient@example.com")
	err := tp.WriteLine("test with explicit port")
	if err != nil {
		t.Fatalf("WriteLine() error: %v", err)
	}

	select {
	case msg := <-gotMsg:
		if !strings.Contains(msg, "test with explicit port") {
			t.Errorf("expected body to contain message, got: %q", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for SMTP server")
	}
}

func TestEmailTransportFromFallback(t *testing.T) {
	addr, gotMsg := startTestSMTPServer(t)

	cfg := &config.EmailConfig{
		SMTPHost: "localhost",
		SMTPPort: portFromAddr(addr),
		Username: "user@example.com",
		Password: "pass",
		From:     "",
		UseTLS:   false,
	}
	tp := newEmailTransportWithSender(cfg, "recipient@example.com")
	err := tp.WriteLine("test from fallback")
	if err != nil {
		t.Fatalf("WriteLine() error: %v", err)
	}

	select {
	case <-gotMsg:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for SMTP server")
	}
}

func TestEmailTransportReadLine(t *testing.T) {
	tp := &EmailTransport{
		msgCh:   make(chan string, 1024),
		closeCh: make(chan struct{}),
	}
	tp.msgCh <- "hello command"
	got, err := tp.ReadLine()
	if err != nil {
		t.Fatalf("ReadLine() error: %v", err)
	}
	if got != "hello command" {
		t.Errorf("ReadLine() = %q, want %q", got, "hello command")
	}
}

func TestEmailTransportReadLineClosed(t *testing.T) {
	tp := &EmailTransport{
		msgCh:   make(chan string, 1024),
		closeCh: make(chan struct{}),
	}
	tp.Close()
	_, err := tp.ReadLine()
	if err == nil {
		t.Error("expected error after close")
	}
}

func TestEmailTransportWriteLine(t *testing.T) {
	addr, gotMsg := startTestSMTPServer(t)

	cfg := &config.EmailConfig{
		SMTPHost: "localhost",
		SMTPPort: portFromAddr(addr),
		Username: "test@example.com",
		Password: "pass",
		From:     "test@example.com",
		UseTLS:   false,
	}
	tp := newEmailTransportWithSender(cfg, "recipient@example.com")
	err := tp.WriteLine("hello response")
	if err != nil {
		t.Fatalf("WriteLine() error: %v", err)
	}

	select {
	case msg := <-gotMsg:
		if !strings.Contains(msg, "hello response") {
			t.Errorf("expected body to contain 'hello response', got: %q", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for SMTP server to receive message")
	}
}

func TestEmailTransportWriteString(t *testing.T) {
	addr, gotMsg := startTestSMTPServer(t)

	cfg := &config.EmailConfig{
		SMTPHost: "localhost",
		SMTPPort: portFromAddr(addr),
		Username: "test@example.com",
		Password: "pass",
		From:     "test@example.com",
		UseTLS:   false,
	}
	tp := newEmailTransportWithSender(cfg, "recipient@example.com")
	err := tp.WriteString("hello world")
	if err != nil {
		t.Fatalf("WriteString() error: %v", err)
	}

	select {
	case msg := <-gotMsg:
		if !strings.Contains(msg, "hello world") {
			t.Errorf("expected body to contain 'hello world', got: %q", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for SMTP server to receive message")
	}
}

func TestEmailTransportSendMailPlain(t *testing.T) {
	addr, gotMsg := startTestSMTPServer(t)

	cfg := &config.EmailConfig{
		SMTPHost: "localhost",
		SMTPPort: portFromAddr(addr),
		Username: "test@example.com",
		Password: "pass",
		From:     "test@example.com",
		UseTLS:   false,
	}
	tp := newEmailTransportWithSender(cfg, "recipient@example.com")
	err := tp.sendMail("test body")
	if err != nil {
		t.Fatalf("sendMail() error: %v", err)
	}

	select {
	case msg := <-gotMsg:
		if !strings.Contains(msg, "test body") {
			t.Errorf("expected 'test body' in message, got: %q", msg)
		}
		if !strings.Contains(msg, "Subject: Re: dolphin Agent") {
			t.Errorf("expected Subject header")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for SMTP server")
	}
}

func portFromAddr(addr string) int {
	_, port, _ := net.SplitHostPort(addr)
	var p int
	fmt.Sscanf(port, "%d", &p)
	return p
}

func TestEmailTransportSendMailSMTPPortDefault(t *testing.T) {
	cfg := &config.EmailConfig{
		SMTPHost: "localhost",
		SMTPPort: 0,
		Username: "test@example.com",
		Password: "pass",
		From:     "test@example.com",
		UseTLS:   false,
	}
	tp := newEmailTransportWithSender(cfg, "recipient@example.com")
	err := tp.sendMail("test")
	if err == nil {
		t.Error("expected error when connecting to port 587")
	}
}

func TestEmailTransportSendMailTLSNotUsed(t *testing.T) {
	addr, gotMsg := startTestSMTPServer(t)
	cfg := &config.EmailConfig{
		SMTPHost: "localhost",
		SMTPPort: portFromAddr(addr),
		Username: "test@example.com",
		Password: "pass",
		From:     "test@example.com",
		UseTLS:   false,
	}
	tp := newEmailTransportWithSender(cfg, "recipient@example.com")
	err := tp.WriteString("non-tls test")
	if err != nil {
		t.Fatalf("WriteString error: %v", err)
	}
	select {
	case msg := <-gotMsg:
		if !strings.Contains(msg, "non-tls test") {
			t.Errorf("expected body, got: %q", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
}

func TestDecodeMIMEBody_Empty(t *testing.T) {
	result := decodeMIMEBody(nil)
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
	result = decodeMIMEBody([]byte("  \n  "))
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestDecodeMIMEBody_PlainText(t *testing.T) {
	input := []byte("hello world\nwhat time is it?")
	result := decodeMIMEBody(input)
	if result != "hello world\nwhat time is it?" {
		t.Errorf("expected plain text pass-through, got %q", result)
	}
}

func TestDecodeMIMEBody_Base64TextPlain(t *testing.T) {
	body := base64.StdEncoding.EncodeToString([]byte("现在几点了？"))
	input := fmt.Sprintf("Content-Type: text/plain; charset=\"utf-8\"\r\nContent-Transfer-Encoding: base64\r\n\r\n%s", body)
	result := decodeMIMEBody([]byte(input))
	if !strings.Contains(result, "现在几点了") {
		t.Errorf("expected decoded text, got %q", result)
	}
}

func TestDecodeMIMEBody_QuotedPrintable(t *testing.T) {
	input := []byte("Content-Type: text/plain; charset=\"utf-8\"\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nhello=20world=21")
	result := decodeMIMEBody(input)
	if result != "hello world!" {
		t.Errorf("expected 'hello world!', got %q", result)
	}
}

func TestDecodeMIMEBody_MultipartPreferPlain(t *testing.T) {
	boundary := "----=_NextPart_001"
	textPlain := "这是纯文本"
	htmlB64 := base64.StdEncoding.EncodeToString([]byte("<div>这是HTML</div>"))

	input := fmt.Sprintf(`Content-Type: multipart/alternative; boundary="%s"

--%s
Content-Type: text/html; charset="utf-8"
Content-Transfer-Encoding: base64

%s
--%s
Content-Type: text/plain; charset="utf-8"

%s
--%s--
`, boundary, boundary, htmlB64, boundary, textPlain, boundary)

	result := decodeMIMEBody([]byte(input))
	if !strings.Contains(result, textPlain) {
		t.Errorf("expected plain text preferred, got %q", result)
	}
	if strings.Contains(result, "HTML") || strings.Contains(result, "<div>") {
		t.Errorf("should not contain HTML content, got %q", result)
	}
}

func TestDecodeMIMEBody_MultipartHTMLOnly(t *testing.T) {
	boundary := "----=_NextPart_002"
	htmlContent := "<div>Hello <b>World</b></div><br><div><!--emptysign--></div>"

	input := fmt.Sprintf(`Content-Type: multipart/alternative; boundary="%s"

--%s
Content-Type: text/html; charset="utf-8"

%s
--%s--
`, boundary, boundary, htmlContent, boundary)

	result := decodeMIMEBody([]byte(input))
	if !strings.Contains(result, "Hello World") {
		t.Errorf("expected stripped HTML text, got %q", result)
	}
	if strings.Contains(result, "<div>") || strings.Contains(result, "emptysign") {
		t.Errorf("HTML tags and comments should be stripped, got %q", result)
	}
}

func TestDecodeMIMEBody_GB18030Charset(t *testing.T) {
	gb18030Text := []byte{0xb2, 0xe2, 0xca, 0xd4, 0xd3, 0xca, 0xbc, 0xfe}
	body := base64.StdEncoding.EncodeToString(gb18030Text)
	input := fmt.Sprintf("Content-Type: text/plain; charset=\"gb18030\"\r\nContent-Transfer-Encoding: base64\r\n\r\n%s", body)
	result := decodeMIMEBody([]byte(input))
	if !strings.Contains(result, "测试邮件") {
		t.Errorf("expected GB18030 decoded text '测试邮件', got %q", result)
	}
}

func TestDecodeMIMEBody_EmptyEmailBug(t *testing.T) {
	boundary := "----=_NextPart_6A096321_00A669A0_1DCD0CEC"
	emptyHTML := base64.StdEncoding.EncodeToString([]byte("<div><br></div><div><!--emptysign--></div>"))

	input := fmt.Sprintf(`Content-Type: multipart/alternative; boundary="%s"

--%s
Content-Type: text/plain; charset="gb18030"
Content-Transfer-Encoding: base64


--%s
Content-Type: text/html; charset="gb18030"
Content-Transfer-Encoding: base64

%s
--%s--
`, boundary, boundary, boundary, emptyHTML, boundary)

	result := decodeMIMEBody([]byte(input))
	result = strings.TrimSpace(result)
	t.Logf("empty email decoded to: %q", result)
	if strings.Contains(result, "Content-Type:") {
		t.Errorf("MIME headers leaked into result: %q", result)
	}
	if strings.Contains(result, "base64") {
		t.Errorf("base64 text leaked into result: %q", result)
	}
	if strings.Contains(result, "emptysign") {
		t.Errorf("HTML comment leaked into result: %q", result)
	}
}

func TestStripQuotedReply_NoQuoting(t *testing.T) {
	input := "hello, what time is it?"
	result := stripQuotedReply(input)
	if result != input {
		t.Errorf("expected no change, got %q", result)
	}
}

func TestStripQuotedReply_OnWrote(t *testing.T) {
	input := "my new message\n\nOn Mon, Jan 1 2026, someone@example.com wrote:\n> old reply\n> more old"
	result := stripQuotedReply(input)
	if !strings.HasPrefix(result, "my new message") {
		t.Errorf("expected only new message, got %q", result)
	}
	if strings.Contains(result, ">") {
		t.Errorf("quoted text not stripped, got %q", result)
	}
}

func TestStripQuotedReply_QuotedLineOnly(t *testing.T) {
	input := "> This is a quoted reply"
	result := stripQuotedReply(input)
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestStripQuotedReply_ChineseReply(t *testing.T) {
	input := "今天日期是2026年5月17日\n\n在 2026年5月17日，dolphin@siciv.space 写道：\n> 这是引用的内容"
	result := stripQuotedReply(input)
	if !strings.Contains(result, "今天日期") {
		t.Errorf("expected new message retained, got %q", result)
	}
	if strings.Contains(result, ">") || strings.Contains(result, "写道") {
		t.Errorf("quoted text not stripped, got %q", result)
	}
}

func TestDecodeMIMEBody_PreambleMultipart(t *testing.T) {
	boundary := "----=_NextPart_6A0980D1_DAC6FD60_1CEFE28F"
	textPlainB64 := base64.StdEncoding.EncodeToString([]byte("今天日期是多少？"))
	input := fmt.Sprintf(`This is a multi-part message in MIME format.

--%s
Content-Type: text/plain;
	charset="utf-8"
Content-Transfer-Encoding: base64

%s
--%s--
`, boundary, textPlainB64, boundary)
	result := decodeMIMEBody([]byte(input))
	if !strings.Contains(result, "今天日期是多少") {
		t.Errorf("expected decoded text '今天日期是多少？', got %q", result)
	}
	if strings.Contains(result, "This is a multi-part") {
		t.Errorf("preamble leaked into result: %q", result)
	}
	if strings.Contains(result, "Content-Type:") {
		t.Errorf("MIME headers leaked into result: %q", result)
	}
}
