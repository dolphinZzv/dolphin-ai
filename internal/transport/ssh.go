package transport

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"dolphin/internal/config"

	"time"

	"github.com/charmbracelet/glamour"
	"go.uber.org/zap"
	gossh "golang.org/x/crypto/ssh"
)

// SSHTransport provides SSH server transport.
type SSHTransport struct {
	cfg      *config.SSHConfig
	config   *gossh.ServerConfig
	listener net.Listener
	mu       sync.Mutex
	handler  func(context.Context, UserIO)
}

func NewSSHTransport(cfg *config.Config, handler func(context.Context, UserIO)) (*SSHTransport, error) {
	sshCfg := cfg.Transport.SSH
	if sshCfg.Password == "" {
		return nil, fmt.Errorf("ssh password is empty — set transport.ssh.password in config or ensure auto-generation succeeds")
	}
	serverCfg := &gossh.ServerConfig{
		PasswordCallback: func(conn gossh.ConnMetadata, password []byte) (*gossh.Permissions, error) {
			zap.S().Debugw("ssh password auth", "user", conn.User())
			if conn.User() != sshCfg.Username {
				return nil, fmt.Errorf("unauthorized user: %s", conn.User())
			}
			if subtle.ConstantTimeCompare(password, []byte(sshCfg.Password)) != 1 {
				return nil, fmt.Errorf("invalid password")
			}
			return &gossh.Permissions{}, nil
		},
	}

	hostKey := cfg.Transport.SSH.HostKey
	if hostKey == "" {
		hostKey = "~/.ssh/id_ed25519"
	}

	signer, err := loadHostKey(hostKey)
	if err != nil {
		// Try persistent auto-generated key
		home, _ := os.UserHomeDir()
		autoKeyPath := filepath.Join(home, ".dolphin", "ssh_host_key")
		signer, err = loadHostKey(autoKeyPath)
		if err != nil {
			zap.S().Infow("generating persistent SSH host key", "path", autoKeyPath)
			signer, err = genAndSaveKey(autoKeyPath)
			if err != nil {
				zap.S().Warnw("falling back to ephemeral SSH host key", "error", err)
				signer, err = genEphemeralKey()
				if err != nil {
					return nil, fmt.Errorf("generate host key: %w", err)
				}
			}
		}
	}
	serverCfg.AddHostKey(signer)

	return &SSHTransport{
		cfg:     &cfg.Transport.SSH,
		config:  serverCfg,
		handler: handler,
	}, nil
}

func (t *SSHTransport) Name() string { return "ssh" }

func (t *SSHTransport) Start(ctx context.Context) error {
	// Early exit if context is already cancelled
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	activeConnections.Add(1)
	addr := t.cfg.Addr
	if addr == "" {
		addr = ":2222"
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("ssh listen: %w", err)
	}
	t.mu.Lock()
	t.listener = listener
	t.mu.Unlock()

	zap.S().Infow("ssh server listening", "addr", addr)

	go func() {
		<-ctx.Done()
		t.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				zap.S().Errorw("ssh accept error", "error", err)
				continue
			}
		}
		go t.handleConn(ctx, conn)
	}
}

func (t *SSHTransport) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	sshConn, chans, reqs, err := gossh.NewServerConn(conn, t.config)
	if err != nil {
		zap.S().Debugw("ssh handshake failed", "error", err)
		return
	}
	defer sshConn.Close()

	zap.S().Infow("ssh connection established",
		"user", sshConn.User(),
		"remote", sshConn.RemoteAddr().String(),
	)

	go gossh.DiscardRequests(reqs)

	// Close sshConn when context is cancelled to unblock chans (P1#8)
	go func() {
		<-ctx.Done()
		sshConn.Close()
	}()

	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			newCh.Reject(gossh.UnknownChannelType, "unsupported")
			continue
		}
		ch, reqs, err := newCh.Accept()
		if err != nil {
			continue
		}
		go handleChannelRequests(reqs)

		var md *glamour.TermRenderer
		if t.cfg.MarkdownRender {
			md, _ = glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(0))
		}
		session := NewSSHSession(ch, conn, sshConn.RemoteAddr().String(), sshConn.User(), md)
		t.handler(ctx, session)
		ch.Close()
	}
}

// handleChannelRequests handles SSH channel requests.
// We accept all requests since we handle I/O ourselves.
func handleChannelRequests(reqs <-chan *gossh.Request) {
	for req := range reqs {
		if req.WantReply {
			req.Reply(true, nil)
		}
	}
}

func (t *SSHTransport) Close() error {
	t.mu.Lock()
	ln := t.listener
	t.listener = nil
	t.mu.Unlock()
	if ln != nil {
		activeConnections.Add(-1)
		return ln.Close()
	}
	return nil
}

// SSHSession wraps an SSH channel as a UserIO with readline-like editing.
type SSHSession struct {
	ch      gossh.Channel
	conn    net.Conn // underlying TCP connection for read deadlines
	reader  *bufio.Reader
	history []string
	histIdx int
	remote  string // remote address
	user    string // SSH user
	md      *glamour.TermRenderer // markdown renderer, nil when disabled
}

func NewSSHSession(ch gossh.Channel, conn net.Conn, remote, user string, md *glamour.TermRenderer) *SSHSession {
	return &SSHSession{
		ch:      ch,
		conn:    conn,
		reader:  bufio.NewReader(ch),
		history: make([]string, 0, 20),
		histIdx: -1,
		remote:  remote,
		user:    user,
		md:      md,
	}
}

var sshCompletions = []string{"/exit", "/quit", "/help"}

// redraw writes the line buffer to the channel starting from column 0,
// then moves the cursor to the current position.
func (s *SSHSession) redraw(line []byte, pos int) {
	fmt.Fprint(s.ch, "Dolphin > ", string(line), "\x1b[K")
	// If cursor isn't at end, move it back
	back := len(line) - pos
	for i := 0; i < back; i++ {
		fmt.Fprint(s.ch, "\b")
	}
}

func (s *SSHSession) ReadLine() (string, error) {
	// Set a 5-minute read deadline to prevent indefinite blocking
	if s.conn != nil {
		s.conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
	}
	fmt.Fprint(s.ch, "Dolphin > ")
	line := make([]byte, 0, 256)
	pos := 0
	s.histIdx = -1

	for {
		b, err := s.reader.ReadByte()
		if err != nil {
			return "", err
		}

		switch b {
		case '\r', '\n':
			fmt.Fprint(s.ch, "\r\n")
			lineStr := string(line)
			if lineStr != "" && lineStr != "/exit" && lineStr != "/quit" {
				s.history = append(s.history, lineStr)
			}
			msgsReceived.Inc()
			return lineStr, nil

		case '\b', 0x7f: // backspace
			if pos > 0 {
				// Shift content left
				line = append(line[:pos-1], line[pos:]...)
				pos--
				s.redraw(line, pos)
			}

		case 0x03: // Ctrl+C
			fmt.Fprint(s.ch, "^C\r\n")
			return "", nil

		case 0x04: // Ctrl+D
			fmt.Fprint(s.ch, "\r\n")
			return "", fmt.Errorf("EOF")

		case 0x01: // Ctrl+A — home
			for pos > 0 {
				fmt.Fprint(s.ch, "\b")
				pos--
			}

		case 0x05: // Ctrl+E — end
			for pos < len(line) {
				fmt.Fprint(s.ch, string(line[pos]))
				pos++
			}

		case 0x09: // Tab
			prefix := string(line)
			match := ""
			for _, c := range sshCompletions {
				if len(c) >= len(prefix) && c[:len(prefix)] == prefix {
					if match != "" {
						match = ""
						break
					}
					match = c
				}
			}
			if match != "" {
				line = []byte(match)
				pos = len(line)
				s.redraw(line, pos)
			}

		case 0x1b: // Escape sequences
			b1, err := s.reader.ReadByte()
			if err != nil {
				continue
			}
			if b1 == '[' {
				dir, err := s.reader.ReadByte()
				if err != nil {
					continue
				}
				switch dir {
				case 'A': // Up
					if len(s.history) > 0 && s.histIdx != 0 {
						if s.histIdx == -1 {
							s.histIdx = len(s.history) - 1
						} else {
							s.histIdx--
						}
						line = []byte(s.history[s.histIdx])
						pos = len(line)
						s.redraw(line, pos)
					}
				case 'B': // Down
					if s.histIdx >= 0 {
						s.histIdx++
						if s.histIdx >= len(s.history) {
							s.histIdx = len(s.history)
							line = line[:0]
							pos = 0
						} else {
							line = []byte(s.history[s.histIdx])
							pos = len(line)
						}
						s.redraw(line, pos)
					}
				case 'C': // Right
					if pos < len(line) {
						fmt.Fprint(s.ch, string(line[pos]))
						pos++
					}
				case 'D': // Left
					if pos > 0 {
						fmt.Fprint(s.ch, "\b")
						pos--
					}
				case '3': // Delete (Del) - ESC [ 3 ~
					if b2, err := s.reader.ReadByte(); err == nil && b2 == '~' {
						if pos < len(line) {
							line = append(line[:pos], line[pos+1:]...)
							s.redraw(line, pos)
						}
					}
				case 'H': // Home
					for pos > 0 {
						fmt.Fprint(s.ch, "\b")
						pos--
					}
				case 'F': // End
					for pos < len(line) {
						fmt.Fprint(s.ch, string(line[pos]))
						pos++
					}
				case '1': // Home (ESC[1~)
					if b2, err := s.reader.ReadByte(); err == nil && b2 == '~' {
						for pos > 0 {
							fmt.Fprint(s.ch, "\b")
							pos--
						}
					}
				case '4': // End (ESC[4~)
					if b2, err := s.reader.ReadByte(); err == nil && b2 == '~' {
						for pos < len(line) {
							fmt.Fprint(s.ch, string(line[pos]))
							pos++
						}
					}
				}
			}

		default:
			if b >= 32 {
				// Insert at cursor position
				line = append(line, 0)
				copy(line[pos+1:], line[pos:])
				line[pos] = b
				pos++
				s.redraw(line, pos)
			}
		}
	}
}

func (s *SSHSession) Name() string { return "ssh" }

func (s *SSHSession) Context() string {
	return fmt.Sprintf("Connected via SSH from %s as user %s.", s.remote, s.user)
}

func (s *SSHSession) Capabilities() Capabilities {
	return Capabilities{Streaming: true, Flushable: false, ShowToolDetails: true}
}

func (s *SSHSession) WriteLine(text string) error {
	msgsSent.Inc()
	if s.md != nil && text != "" {
		rendered, err := s.md.Render(text)
		if err == nil {
			_, err = fmt.Fprint(s.ch, strings.ReplaceAll(rendered, "\n", "\r\n"))
			return err
		}
	}
	_, err := fmt.Fprint(s.ch, strings.ReplaceAll(text, "\n", "\r\n"), "\r\n")
	return err
}

func (s *SSHSession) WriteString(text string) error {
	msgsSent.Inc()
	// Don't add trailing \r\n, but ensure embedded newlines are CRLF
	_, err := fmt.Fprint(s.ch, strings.ReplaceAll(text, "\n", "\r\n"))
	return err
}

func loadHostKey(path string) (gossh.Signer, error) {
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Clean(home + path[1:])
	}
	keyData, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	return gossh.ParsePrivateKey(keyData)
}

func genEphemeralKey() (gossh.Signer, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return gossh.NewSignerFromKey(priv)
}

func genAndSaveKey(path string) (gossh.Signer, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	os.MkdirAll(filepath.Dir(path), 0700)
	pemBlock, err := gossh.MarshalPrivateKey(priv, "dolphin-host-key")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(pemBlock), 0600); err != nil {
		return nil, err
	}
	return gossh.NewSignerFromKey(priv)
}
