package ssh

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	transport "dolphin/internal/transport"

	"dolphin/internal/config"

	gossh "golang.org/x/crypto/ssh"
)

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
	sess := NewSSHSession(ch, nil, "test@127.0.0.1", "testuser", nil)

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
	sess := NewSSHSession(ch, nil, "test@127.0.0.1", "testuser", nil)

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
	sess := NewSSHSession(ch, nil, "test@127.0.0.1", "testuser", nil)

	if err := sess.WriteString("hello"); err != nil {
		t.Fatalf("WriteString error: %v", err)
	}
	if buf.String() != "hello" {
		t.Errorf("got %q, want hello", buf.String())
	}
}

func TestSSHSessionImplementsUserIO(t *testing.T) {
	var _ transport.UserIO = (*SSHSession)(nil)
}

func TestSSHSessionContext(t *testing.T) {
	buf := bytes.NewBufferString("")
	ch := &mockChannel{Buffer: buf}
	sess := NewSSHSession(ch, nil, "192.168.1.5:54321", "admin", nil)
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

func TestNewSSHTransportWithPassword(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Transport.SSH.Enabled = true
	cfg.Transport.SSH.Username = "testuser"
	cfg.Transport.SSH.Password = "testpass"

	tr, err := New(cfg)
	if err != nil {
		t.Fatalf("New with valid config should not error: %v", err)
	}
	st := tr.(*SSHTransport)
	st.SetSessionHandler(func(_ context.Context, _ transport.UserIO) {})
}

func TestNewSSHTransportWithEphemeralKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Transport.SSH.Enabled = true
	cfg.Transport.SSH.HostKey = "/nonexistent/path/key"
	cfg.Transport.SSH.Password = "test-password"

	tr, err := New(cfg)
	if err != nil {
		t.Fatalf("New should fall back to ephemeral key: %v", err)
	}
	st := tr.(*SSHTransport)
	st.SetSessionHandler(func(_ context.Context, _ transport.UserIO) {})
}

func TestSSHAuthValidCredentials(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Transport.SSH.Enabled = true
	cfg.Transport.SSH.Username = "dolphin"
	cfg.Transport.SSH.Password = "secret"
	cfg.Transport.SSH.HostKey = "/nonexistent/key"

	connected := make(chan struct{}, 1)
	handler := func(_ context.Context, _ transport.UserIO) {
		connected <- struct{}{}
	}

	tr, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	trans := tr.(*SSHTransport)
	trans.SetSessionHandler(handler)

	cfg.Transport.SSH.Addr = "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- trans.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	trans.mu.Lock()
	addr := trans.listener.Addr().String()
	trans.mu.Unlock()

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

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer session.Close()

	select {
	case <-connected:
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

	handler := func(_ context.Context, _ transport.UserIO) {}
	tr, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	trans := tr.(*SSHTransport)
	trans.SetSessionHandler(handler)

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

	handler := func(_ context.Context, _ transport.UserIO) {}
	tr, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	trans := tr.(*SSHTransport)
	trans.SetSessionHandler(handler)

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

	handler := func(ctx context.Context, _ transport.UserIO) {
		<-ctx.Done()
	}

	tr, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	trans := tr.(*SSHTransport)
	trans.SetSessionHandler(handler)

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

	if ok, err := sess.SendRequest("shell", true, nil); err != nil {
		t.Fatalf("shell request error: %v", err)
	} else if !ok {
		t.Fatal("shell request was rejected")
	}

	if ok, err := sess.SendRequest("pty-req", true, nil); err != nil {
		t.Fatalf("pty-req request error: %v", err)
	} else if !ok {
		t.Fatal("pty-req request was rejected")
	}

	cancel()
	time.Sleep(50 * time.Millisecond)
}
