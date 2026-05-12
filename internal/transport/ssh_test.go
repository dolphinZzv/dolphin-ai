package transport

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"dolphin/internal/config"

	gossh "golang.org/x/crypto/ssh"
)

// mockChannel implements gossh.Channel for testing.
type mockChannel struct {
	*bytes.Buffer
}

func (m *mockChannel) Read(data []byte) (int, error) {
	return m.Buffer.Read(data)
}

func (m *mockChannel) Write(data []byte) (int, error) {
	return m.Buffer.Write(data)
}

func (m *mockChannel) Close() error      { return nil }
func (m *mockChannel) CloseWrite() error { return nil }
func (m *mockChannel) SendRequest(name string, wantReply bool, payload []byte) (bool, error) {
	return true, nil
}
func (m *mockChannel) Stderr() io.ReadWriter { return nil }

func TestSSHSessionReadLine(t *testing.T) {
	buf := bytes.NewBufferString("hello\n")
	ch := &mockChannel{Buffer: buf}
	sess := NewSSHSession(ch, nil, "test@127.0.0.1", "testuser")

	line, err := sess.ReadLine()
	if err != nil {
		t.Fatalf("ReadLine error: %v", err)
	}
	if line != "hello" {
		t.Errorf("got %q, want hello", line)
	}
}

func TestSSHSessionWriteLine(t *testing.T) {
	var buf bytes.Buffer
	ch := &mockChannel{Buffer: &buf}
	sess := NewSSHSession(ch, nil, "test@127.0.0.1", "testuser")

	if err := sess.WriteLine("test output"); err != nil {
		t.Fatalf("WriteLine error: %v", err)
	}
	if !strings.HasPrefix(buf.String(), "test output") {
		t.Errorf("got %q, want test output", buf.String())
	}
}

func TestSSHSessionWriteString(t *testing.T) {
	var buf bytes.Buffer
	ch := &mockChannel{Buffer: &buf}
	sess := NewSSHSession(ch, nil, "test@127.0.0.1", "testuser")

	if err := sess.WriteString("hello"); err != nil {
		t.Fatalf("WriteString error: %v", err)
	}
	if buf.String() != "hello" {
		t.Errorf("got %q, want hello", buf.String())
	}
}

func TestStdioTransportImplementsUserIO(t *testing.T) {
	var _ UserIO = (*StdioTransport)(nil)
}

func TestSSHSessionImplementsUserIO(t *testing.T) {
	var _ UserIO = (*SSHSession)(nil)
}

func TestSSHSessionContext(t *testing.T) {
	buf := bytes.NewBufferString("")
	ch := &mockChannel{Buffer: buf}
	sess := NewSSHSession(ch, nil, "192.168.1.5:54321", "admin")
	ctx := sess.Context()
	if !strings.Contains(ctx, "192.168.1.5") {
		t.Errorf("expected remote addr in context, got: %s", ctx)
	}
	if !strings.Contains(ctx, "admin") {
		t.Errorf("expected user in context, got: %s", ctx)
	}
	if !strings.Contains(ctx, "SSH") {
		t.Errorf("expected SSH in context, got: %s", ctx)
	}
}

func TestStdioTransportContext(t *testing.T) {
	tp := NewStdioTransport()
	ctx := tp.Context()
	if !strings.Contains(ctx, "terminal") {
		t.Errorf("expected 'terminal' in stdio context, got: %s", ctx)
	}
}

func TestMQTTTransportContext(t *testing.T) {
	cfg := config.DefaultConfig()
	tp := NewMQTTTransport(cfg)
	ctx := tp.Context()
	if !strings.Contains(ctx, "MQTT") {
		t.Errorf("expected MQTT in context, got: %s", ctx)
	}
	if !strings.Contains(ctx, cfg.Transport.MQTT.Broker) {
		t.Errorf("expected broker in context, got: %s", ctx)
	}
}

func TestEmailTransportContext(t *testing.T) {
	cfg := &config.EmailConfig{
		IMAPHost: "imap.example.com",
		IMAPPort: 993,
		SMTPHost: "smtp.example.com",
		SMTPPort: 587,
	}
	tp := NewEmailTransport(cfg)
	ctx := tp.Context()
	if !strings.Contains(ctx, "email") {
		t.Errorf("expected 'email' in context, got: %s", ctx)
	}
	if !strings.Contains(ctx, "imap.example.com") {
		t.Errorf("expected IMAP host in context, got: %s", ctx)
	}
}

func TestNewSSHTransportWithPassword(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Transport.SSH.Enabled = true
	cfg.Transport.SSH.Username = "testuser"
	cfg.Transport.SSH.Password = "testpass"

	handler := func(_ context.Context, _ UserIO) {}
	_, err := NewSSHTransport(cfg, handler)
	if err != nil {
		t.Fatalf("NewSSHTransport with valid config should not error: %v", err)
	}
}

func TestNewSSHTransportWithEphemeralKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Transport.SSH.Enabled = true
	cfg.Transport.SSH.HostKey = "/nonexistent/path/key"

	handler := func(_ context.Context, _ UserIO) {}
	_, err := NewSSHTransport(cfg, handler)
	if err != nil {
		t.Fatalf("NewSSHTransport should fall back to ephemeral key: %v", err)
	}
}

func TestSSHAuthValidCredentials(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Transport.SSH.Enabled = true
	cfg.Transport.SSH.Username = "dolphin"
	cfg.Transport.SSH.Password = "secret"
	cfg.Transport.SSH.HostKey = "/nonexistent/key"

	connected := make(chan struct{}, 1)
	handler := func(_ context.Context, _ UserIO) {
		connected <- struct{}{}
	}

	trans, err := NewSSHTransport(cfg, handler)
	if err != nil {
		t.Fatalf("NewSSHTransport: %v", err)
	}

	// Start on random port
	cfg.Transport.SSH.Addr = "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- trans.Start(ctx)
	}()

	// Give it time to start
	time.Sleep(100 * time.Millisecond)

	// Find the actual addr
	trans.mu.Lock()
	addr := trans.listener.Addr().String()
	trans.mu.Unlock()

	// Generate client config with valid credentials
	clientCfg := &gossh.ClientConfig{
		User:            "dolphin",
		Auth:            []gossh.AuthMethod{gossh.Password("secret")},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         2 * time.Second,
	}

	client, err := gossh.Dial("tcp", addr, clientCfg)
	if err != nil {
		t.Fatalf("SSH dial with valid creds should succeed: %v", err)
	}
	defer client.Close()

	// Open a session so handler runs
	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer session.Close()

	select {
	case <-connected:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("handler was not called after connection")
	}
}

func TestSSHAuthInvalidPassword(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Transport.SSH.Enabled = true
	cfg.Transport.SSH.Username = "dolphin"
	cfg.Transport.SSH.Password = "correctpass"
	cfg.Transport.SSH.HostKey = "/nonexistent/key"

	handler := func(_ context.Context, _ UserIO) {}
	trans, err := NewSSHTransport(cfg, handler)
	if err != nil {
		t.Fatalf("NewSSHTransport: %v", err)
	}

	cfg.Transport.SSH.Addr = "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		trans.Start(ctx)
	}()
	time.Sleep(100 * time.Millisecond)

	trans.mu.Lock()
	addr := trans.listener.Addr().String()
	trans.mu.Unlock()

	clientCfg := &gossh.ClientConfig{
		User:            "dolphin",
		Auth:            []gossh.AuthMethod{gossh.Password("wrongpass")},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         2 * time.Second,
	}

	_, err = gossh.Dial("tcp", addr, clientCfg)
	if err == nil {
		t.Fatal("SSH dial with wrong password should fail")
	}
}

func TestSSHAuthInvalidUser(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Transport.SSH.Enabled = true
	cfg.Transport.SSH.Username = "dolphin"
	cfg.Transport.SSH.Password = "pass"
	cfg.Transport.SSH.HostKey = "/nonexistent/key"

	handler := func(_ context.Context, _ UserIO) {}
	trans, err := NewSSHTransport(cfg, handler)
	if err != nil {
		t.Fatalf("NewSSHTransport: %v", err)
	}

	cfg.Transport.SSH.Addr = "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		trans.Start(ctx)
	}()
	time.Sleep(100 * time.Millisecond)

	trans.mu.Lock()
	addr := trans.listener.Addr().String()
	trans.mu.Unlock()

	clientCfg := &gossh.ClientConfig{
		User:            "wronguser",
		Auth:            []gossh.AuthMethod{gossh.Password("pass")},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         2 * time.Second,
	}

	_, err = gossh.Dial("tcp", addr, clientCfg)
	if err == nil {
		t.Fatal("SSH dial with wrong username should fail")
	}
}

func TestSSHChannelRequestHandling(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Transport.SSH.Enabled = true
	cfg.Transport.SSH.Username = "test"
	cfg.Transport.SSH.Password = "test"
	cfg.Transport.SSH.HostKey = "/nonexistent/key"

	handler := func(ctx context.Context, _ UserIO) {
		<-ctx.Done()
	}

	trans, err := NewSSHTransport(cfg, handler)
	if err != nil {
		t.Fatalf("NewSSHTransport: %v", err)
	}

	cfg.Transport.SSH.Addr = "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go trans.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	trans.mu.Lock()
	addr := trans.listener.Addr().String()
	trans.mu.Unlock()

	clientCfg := &gossh.ClientConfig{
		User:            "test",
		Auth:            []gossh.AuthMethod{gossh.Password("test")},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         2 * time.Second,
	}

	client, err := gossh.Dial("tcp", addr, clientCfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()

	// Send "shell" request directly - should succeed
	if ok, err := sess.SendRequest("shell", true, nil); err != nil {
		t.Fatalf("shell request error: %v", err)
	} else if !ok {
		t.Fatal("shell request was rejected")
	}

	// Send "pty-req" request - should succeed
	if ok, err := sess.SendRequest("pty-req", true, nil); err != nil {
		t.Fatalf("pty-req request error: %v", err)
	} else if !ok {
		t.Fatal("pty-req request was rejected")
	}

	cancel()
	time.Sleep(50 * time.Millisecond)
}
