// Package ssh provides SSH server transport.
package ssh

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dolphin/internal/config"
	transport "dolphin/internal/transport"

	"github.com/charmbracelet/glamour"
	"go.uber.org/zap"
	gossh "golang.org/x/crypto/ssh"
)

func init() { transport.Register("ssh", New) }

// SSHTransport provides SSH server transport.
type SSHTransport struct {
	cfg      *config.SSHConfig
	config   *gossh.ServerConfig
	listener net.Listener
	mu       sync.Mutex
	handler  func(context.Context, transport.UserIO)
}

func New(cfg *config.Config) (transport.Transport, error) {
	serverCfg, err := buildServerConfig(cfg.Transport.SSH)
	if err != nil {
		return nil, err
	}
	return &SSHTransport{
		cfg:    &cfg.Transport.SSH,
		config: serverCfg,
	}, nil
}

// buildServerConfig creates a gossh.ServerConfig from SSH config, including
// password and public key auth callbacks that capture the current config values.
func buildServerConfig(sshCfg config.SSHConfig) (*gossh.ServerConfig, error) {
	serverCfg := &gossh.ServerConfig{}

	// Password auth: check against the configured password (if set).
	serverCfg.PasswordCallback = func(conn gossh.ConnMetadata, password []byte) (*gossh.Permissions, error) {
		zap.S().Debugw("ssh password auth attempt", "user", conn.User(), "remote", conn.RemoteAddr())

		allowed := sshCfg.AllowedUsers
		if len(allowed) == 0 && sshCfg.Username != "" {
			allowed = []string{sshCfg.Username}
		}
		if !isUserAllowed(conn.User(), allowed) {
			return nil, fmt.Errorf("ssh: unauthorized user: %s", conn.User())
		}
		if sshCfg.Password == "" {
			return nil, fmt.Errorf("ssh: password auth not configured")
		}
		if subtle.ConstantTimeCompare(password, []byte(sshCfg.Password)) != 1 {
			return nil, fmt.Errorf("ssh: invalid password for %s", conn.User())
		}

		zap.S().Infow("ssh password auth succeeded", "user", conn.User())
		return &gossh.Permissions{}, nil
	}

	// Public key auth.
	serverCfg.PublicKeyCallback = func(conn gossh.ConnMetadata, key gossh.PublicKey) (*gossh.Permissions, error) {
		zap.S().Debugw("ssh pubkey auth attempt", "user", conn.User(), "remote", conn.RemoteAddr())

		if len(sshCfg.AllowedUsers) > 0 && !isUserAllowed(conn.User(), sshCfg.AllowedUsers) {
			return nil, fmt.Errorf("ssh: unauthorized user: %s", conn.User())
		}
		if err := checkAuthorizedKey(conn.User(), key, sshCfg.AuthorizedKeys); err != nil {
			return nil, fmt.Errorf("ssh: pubkey auth failed for %s: %w", conn.User(), err)
		}

		zap.S().Infow("ssh pubkey auth succeeded", "user", conn.User())
		return &gossh.Permissions{}, nil
	}

	// Resolve host key path.
	hostKey := sshCfg.HostKey
	if hostKey == "" {
		home, _ := os.UserHomeDir()
		hostKey = filepath.Join(home, ".dolphin", "ssh_host_key")
	}

	signer, err := loadHostKey(hostKey)
	if err != nil {
		zap.S().Infow("generating persistent SSH host key", "path", hostKey)
		signer, err = genAndSaveKey(hostKey)
		if err != nil {
			zap.S().Warnw("failed to persist host key, using ephemeral key; known_hosts will change each restart", "error", err)
			signer, err = genEphemeralKey()
			if err != nil {
				return nil, fmt.Errorf("generate host key: %w", err)
			}
		}
	}
	serverCfg.AddHostKey(signer)

	return serverCfg, nil
}

// OnConfigChange handles SSH config hot-reload.
// SSH can hot-reload entirely in-place: auth rules, host key, and listener.
// Existing connections continue with the rules from when they connected.
func (t *SSHTransport) OnConfigChange(oldCfg, newCfg *config.Config) error {
	oldSSH := oldCfg.Transport.SSH
	newSSH := newCfg.Transport.SSH

	// Check if anything relevant changed
	if oldSSH.Addr == newSSH.Addr && oldSSH.Password == newSSH.Password &&
		oldSSH.HostKey == newSSH.HostKey && eqStringSlice(oldSSH.AllowedUsers, newSSH.AllowedUsers) &&
		oldSSH.AuthorizedKeys == newSSH.AuthorizedKeys && oldSSH.Username == newSSH.Username {
		return transport.ErrUnchanged
	}

	// Rebuild gossh.ServerConfig with fresh closures.
	newServerCfg, err := buildServerConfig(newSSH)
	if err != nil {
		return fmt.Errorf("ssh reload: rebuild server config: %w", err)
	}

	// Swap listener first if address changed, then server config.
	if oldSSH.Addr != newSSH.Addr {
		newListener, err := net.Listen("tcp", newSSH.Addr)
		if err != nil {
			return fmt.Errorf("ssh reload listen: %w", err)
		}
		t.mu.Lock()
		oldListener := t.listener
		t.listener = newListener
		t.config = newServerCfg
		t.cfg = &newSSH
		t.mu.Unlock()
		if oldListener != nil {
			go oldListener.Close() // drain
		}
	} else {
		t.mu.Lock()
		t.config = newServerCfg
		t.cfg = &newSSH
		t.mu.Unlock()
	}

	zap.S().Infow("ssh config reloaded", "addr", newSSH.Addr)
	return nil
}

func eqStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// checkAuthorizedKey checks if the given public key matches a line in the
// user's authorized_keys file. If cfgPath is non-empty, it is used directly;
// if empty the function resolves the user's home directory and checks
// .ssh/authorized_keys and .ssh/authorized_keys2.
func checkAuthorizedKey(username string, presentedKey gossh.PublicKey, cfgPath string) error {
	var paths []string

	if cfgPath != "" {
		// Custom path — resolve tilde with the user's home dir if available,
		// or fall back to os.UserHomeDir for relative expansion.
		home := homeDirFor(username)
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		paths = []string{expandTilde(cfgPath, home)}
	} else {
		// Default: look up the user to find their .ssh directory.
		home := homeDirFor(username)
		if home == "" {
			return fmt.Errorf("unknown user: %s", username)
		}
		paths = []string{
			filepath.Join(home, ".ssh", "authorized_keys"),
			filepath.Join(home, ".ssh", "authorized_keys2"),
		}
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			pub, _, _, _, err := gossh.ParseAuthorizedKey([]byte(line))
			if err != nil {
				continue
			}
			// Compare marshaled bytes since KeysEqual may be unavailable.
			if bytes.Equal(pub.Marshal(), presentedKey.Marshal()) {
				return nil
			}
		}
	}
	return fmt.Errorf("no matching authorized key found")
}

// isUserAllowed checks that the connecting user is in the allowed list.
// An empty list means no restriction (allows any authenticated user).
func isUserAllowed(user string, allowedUsers []string) bool {
	if len(allowedUsers) == 0 {
		return true
	}
	for _, u := range allowedUsers {
		if u == user {
			return true
		}
	}
	return false
}

// homeDirFor returns the home directory for the given username, or "" if the
// user cannot be looked up.
func homeDirFor(username string) string {
	u, err := user.Lookup(username)
	if err != nil {
		return ""
	}
	return u.HomeDir
}

// expandTilde replaces a leading "~" with the given home directory.
func expandTilde(path, homeDir string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir, path[2:])
	}
	if path == "~" {
		return homeDir
	}
	return path
}

// SetSessionHandler sets the handler for incoming SSH sessions.
func (t *SSHTransport) SetSessionHandler(h func(context.Context, transport.UserIO)) {
	t.handler = h
}

func (t *SSHTransport) Name() string { return "ssh" }

func (t *SSHTransport) Banner() string {
	addr := t.cfg.Addr
	if addr == "" {
		addr = ":2222"
	}
	user := t.cfg.Username
	if user == "" {
		user = "<user>"
	}
	return fmt.Sprintf("  SSH server: %s (ssh %s@<host> -p %s)\n", addr, user, addr[1:])
}

func (t *SSHTransport) Start(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	transport.ActiveConnections.Add(1)
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
			md = transport.NewMarkdownRenderer(t.cfg.MarkdownStyle)
		}
		session := NewSSHSession(ch, conn, sshConn.RemoteAddr().String(), sshConn.User(), md)
		t.handler(ctx, session)
		ch.Close()
	}
}

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
		transport.ActiveConnections.Add(-1)
		return ln.Close()
	}
	return nil
}

// SSHSession wraps an SSH channel as a UserIO with readline-like editing.
type SSHSession struct {
	ch      gossh.Channel
	conn    net.Conn
	reader  *bufio.Reader
	history []string
	histIdx int
	remote  string
	user    string
	md      *glamour.TermRenderer
	mdBuf   strings.Builder
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

// defaultReadTimeout is the default SSH read deadline duration.
const defaultReadTimeout = 5 * time.Minute

func (s *SSHSession) redraw(line []byte, pos int) {
	fmt.Fprint(s.ch, "\rDolphin > ", string(line), "\x1b[K")
	back := len(line) - pos
	for i := 0; i < back; i++ {
		fmt.Fprint(s.ch, "\b")
	}
}

func (s *SSHSession) ReadLine() (string, error) {
	if s.conn != nil {
		s.conn.SetReadDeadline(time.Now().Add(defaultReadTimeout))
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
			transport.MsgsReceived.Inc()
			return lineStr, nil

		case '\b', 0x7f:
			if pos > 0 {
				line = append(line[:pos-1], line[pos:]...)
				pos--
				s.redraw(line, pos)
			}

		case 0x03:
			fmt.Fprint(s.ch, "^C\r\n")
			return "", nil

		case 0x04:
			fmt.Fprint(s.ch, "\r\n")
			return "", fmt.Errorf("EOF")

		case 0x01:
			for pos > 0 {
				fmt.Fprint(s.ch, "\b")
				pos--
			}

		case 0x05:
			for pos < len(line) {
				fmt.Fprint(s.ch, string(line[pos]))
				pos++
			}

		case 0x09:
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

		case 0x1b:
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
				case 'A':
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
				case 'B':
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
				case 'C':
					if pos < len(line) {
						fmt.Fprint(s.ch, string(line[pos]))
						pos++
					}
				case 'D':
					if pos > 0 {
						fmt.Fprint(s.ch, "\b")
						pos--
					}
				case '3':
					if b2, err := s.reader.ReadByte(); err == nil && b2 == '~' {
						if pos < len(line) {
							line = append(line[:pos], line[pos+1:]...)
							s.redraw(line, pos)
						}
					}
				case 'H':
					for pos > 0 {
						fmt.Fprint(s.ch, "\b")
						pos--
					}
				case 'F':
					for pos < len(line) {
						fmt.Fprint(s.ch, string(line[pos]))
						pos++
					}
				case '1':
					if b2, err := s.reader.ReadByte(); err == nil && b2 == '~' {
						for pos > 0 {
							fmt.Fprint(s.ch, "\b")
							pos--
						}
					}
				case '4':
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

func (s *SSHSession) Capabilities() transport.Capabilities {
	return transport.Capabilities{Streaming: true, ShowToolDetails: true}
}

func (s *SSHSession) WriteLine(text string) error {
	transport.MsgsSent.Inc()
	if s.md != nil && s.mdBuf.Len() > 0 {
		s.mdBuf.WriteString("\n")
		rendered, err := s.md.Render(s.mdBuf.String())
		s.mdBuf.Reset()
		if err == nil {
			fmt.Fprint(s.ch, strings.ReplaceAll(rendered, "\n", "\r\n"))
			return nil
		}
	}
	_, err := fmt.Fprint(s.ch, strings.ReplaceAll(text, "\n", "\r\n"), "\r\n")
	return err
}

func (s *SSHSession) WriteString(text string) error {
	transport.MsgsSent.Inc()
	if s.md != nil {
		s.mdBuf.WriteString(text)
		content := s.mdBuf.String()
		if idx := strings.LastIndex(content, "\n\n"); idx >= 0 {
			block := content[:idx+2]
			s.mdBuf.Reset()
			s.mdBuf.WriteString(content[idx+2:])
			rendered, err := s.md.Render(block)
			if err == nil {
				_, err = fmt.Fprint(s.ch, strings.ReplaceAll(rendered, "\n", "\r\n"))
				return err
			}
		}
		return nil
	}
	_, err := fmt.Fprint(s.ch, strings.ReplaceAll(text, "\n", "\r\n"))
	return err
}

func (s *SSHSession) Flush() error {
	_, err := fmt.Fprint(s.ch, "\r\n----------------------------------------\r\n")
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
	// Expand leading tilde to home directory, matching loadHostKey behavior.
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Clean(home + path[1:])
	}
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
