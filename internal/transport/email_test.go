package transport

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"dolphin/internal/config"
)

// startTestSMTPServer starts a minimal SMTP server that captures the message body.
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

		// Server sends its banner
		conn.Write([]byte("220 localhost ESMTP test\r\n"))

		// Helper to read a line
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
				// Read body until \r\n.\r\n or \n.\n
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

func TestEmailTransportName(t *testing.T) {
	tp := &EmailTransport{}
	if n := tp.Name(); n != "email" {
		t.Errorf("Name() = %q", n)
	}
}

func TestEmailTransportClose(t *testing.T) {
	tp := NewEmailTransport(&config.EmailConfig{})
	if err := tp.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}
	// Second close should be safe (idempotent)
	if err := tp.Close(); err != nil {
		t.Errorf("second Close() error: %v", err)
	}
}

func TestEmailTransportReadLine(t *testing.T) {
	tp := NewEmailTransport(&config.EmailConfig{})
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
	tp := NewEmailTransport(&config.EmailConfig{})
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
	tp := NewEmailTransport(cfg)
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
	tp := NewEmailTransport(cfg)
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
	tp := NewEmailTransport(cfg)
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

// portFromAddr extracts the port from a host:port string.
func portFromAddr(addr string) int {
	_, port, _ := net.SplitHostPort(addr)
	var p int
	fmt.Sscanf(port, "%d", &p)
	return p
}

func TestEmailTransportSendMailSMTPPortDefault(t *testing.T) {
	// When SMTPPort is 0, it defaults to 587
	// We don't have a server on 587, so this should fail gracefully
	cfg := &config.EmailConfig{
		SMTPHost: "localhost",
		SMTPPort: 0,
		Username: "test@example.com",
		Password: "pass",
		From:     "test@example.com",
		UseTLS:   false,
	}
	tp := NewEmailTransport(cfg)
	err := tp.sendMail("test")
	if err == nil {
		t.Error("expected error when connecting to port 587")
	}
}

func TestEmailTransportSendMailTLSNotUsed(t *testing.T) {
	// With UseTLS=false and SMTPPort=587, sendMailPlain is used
	// This test verifies the non-TLS path works with a mock server
	addr, gotMsg := startTestSMTPServer(t)
	cfg := &config.EmailConfig{
		SMTPHost: "localhost",
		SMTPPort: portFromAddr(addr),
		Username: "test@example.com",
		Password: "pass",
		From:     "test@example.com",
		UseTLS:   false,
	}
	tp := NewEmailTransport(cfg)
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
