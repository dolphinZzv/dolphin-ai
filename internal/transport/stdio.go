package transport

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"dolphin/internal/common"
	"dolphin/internal/i18n"

	"github.com/chzyer/readline"
)

func init() {
	Register("stdio", func(ctx context.Context, cfg map[string]any) (IO, error) {
		user := os.Getenv("USER")
		if user == "" {
			user = "unknown"
		}
		return NewStdio(user), nil
	})
}

type Stdio struct {
	*SessionHolder
	id         string
	rl         *readline.Instance
	reader     *bufio.Reader
	mu         sync.Mutex
	user       string
	ctx        context.Context
	cancel     context.CancelFunc
	permResult chan string // non-nil when a permission request is pending
}

func NewStdio(user string) *Stdio {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Stdio{
		SessionHolder: NewSessionHolder(nil),
		id:            "stdio",
		reader:        bufio.NewReader(os.Stdin),
		user:          user,
		ctx:           ctx,
		cancel:        cancel,
	}

	host := s.user
	if host == "" {
		host = "unknown"
	}
	prompt := host + "> "

	rl, err := readline.NewEx(&readline.Config{
		Prompt:          prompt,
		HistoryFile:     "/tmp/dolphin_history",
		InterruptPrompt: "^C",
	})
	if err == nil {
		s.rl = rl
	}
	return s
}

func (s *Stdio) ID() string { return s.id }

func (s *Stdio) Context() string                 { return "" }
func (s *Stdio) Tools() []common.ToolDesc        { return nil }
func (s *Stdio) Start(ctx context.Context) error { return nil }

func (s *Stdio) Read(ctx context.Context) (string, error) {
	if s.rl != nil {
		lineCh := make(chan string, 1)
		errCh := make(chan error, 1)

		go func() {
			for {
				line, err := s.rl.Readline()
				if err != nil {
					errCh <- err
					return
				}
				if line == "" {
					continue
				}
				// Check if a permission request is pending.
				s.mu.Lock()
				permCh := s.permResult
				s.mu.Unlock()
				if permCh != nil {
					permCh <- line
					continue // permission response consumed, wait for next input
				}
				line = s.maybeExit(line)
				if line == "" {
					continue
				}
				lineCh <- line
				return
			}
		}()

		select {
		case line := <-lineCh:
			return line, nil
		case err := <-errCh:
			return "", err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	// Fallback: read from buffered stdin.
	lineCh := make(chan string, 1)
	errCh := make(chan error, 1)

	go func() {
		for {
			line, err := s.reader.ReadString('\n')
			if err != nil {
				errCh <- err
				return
			}
			line = line[:len(line)-1] // trim trailing newline
			if line == "" {
				continue
			}
			s.mu.Lock()
			permCh := s.permResult
			s.mu.Unlock()
			if permCh != nil {
				permCh <- line
				continue
			}
			line = s.maybeExit(line)
			if line == "" {
				continue
			}
			lineCh <- line
			return
		}
	}()

	select {
	case line := <-lineCh:
		return line, nil
	case err := <-errCh:
		return "", err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (s *Stdio) maybeExit(line string) string {
	switch strings.TrimSpace(strings.ToLower(line)) {
	case "exit", "quit", "/exit", "/quit":
		fmt.Fprint(os.Stdout, i18n.T("transport.stdio_exit_confirm"))
		reply, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		reply = strings.TrimSpace(strings.ToLower(reply))
		if reply == "y" || reply == "yes" {
			fmt.Fprintln(os.Stdout, i18n.T("transport.stdio_bye"))
			os.Exit(0)
		}
		return ""
	}
	return line
}

func (s *Stdio) Write(ctx context.Context, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := fmt.Fprint(os.Stdout, text)
	return err
}

func (s *Stdio) Flush() error {
	_, err := fmt.Fprint(os.Stdout, "\n")
	return err
}

func (s *Stdio) Close() error {
	s.cancel()
	if s.rl != nil {
		return s.rl.Close()
	}
	return nil
}

// RunInteractive suspends readline and runs a command connected to the real terminal.
func (s *Stdio) RunInteractive(ctx context.Context, name string, args ...string) error {
	if s.rl != nil {
		s.rl.Close()
		s.rl = nil
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	// Re-create readline instance.
	host := s.user
	if host == "" {
		host = "unknown"
	}
	rl, rlErr := readline.NewEx(&readline.Config{
		Prompt:          host + "> ",
		HistoryFile:     "/tmp/dolphin_history",
		InterruptPrompt: "^C",
	})
	if rlErr == nil {
		s.rl = rl
	} else {
		fmt.Fprintf(os.Stderr, "failed to re-init readline: %v\n", rlErr)
	}
	return err
}

func (s *Stdio) Capability() Capability {
	return Capability{
		Interactive:        true,
		Streamable:         true,
		NestRead:           true,
		RenderTextMarkdown: "none",
	}
}

func (s *Stdio) RequestPermission(_ context.Context, prompt string) (PermissionResult, error) {
	fmt.Fprint(os.Stdout, prompt+i18n.T("transport.stdio_permission_menu"))

	var reply string
	if s.rl != nil {
		// Route through Read() via permResult channel to avoid competing with
		// readline for stdin (readline owns stdin in raw mode).
		ch := make(chan string, 1)
		s.mu.Lock()
		s.permResult = ch
		s.mu.Unlock()

		select {
		case reply = <-ch:
		case <-s.ctx.Done():
			s.mu.Lock()
			s.permResult = nil
			s.mu.Unlock()
			return PermissionDenied, s.ctx.Err()
		}

		s.mu.Lock()
		s.permResult = nil
		s.mu.Unlock()
	} else {
		line, err := s.reader.ReadString('\n')
		if err != nil {
			return PermissionDenied, nil
		}
		reply = line
	}

	reply = strings.TrimSpace(strings.ToLower(reply))
	switch reply {
	case "1", "y", "yes", "once":
		return PermissionOnce, nil
	case "2", "always":
		return PermissionAlways, nil
	default:
		return PermissionDenied, nil
	}
}

// Ensure Stdio implements IO.
var _ IO = (*Stdio)(nil)

// NullTransport is a no-op transport for testing.
type NullTransport struct {
	*SessionHolder
	id      string
	readBuf []string
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewNullTransport(id string) *NullTransport {
	ctx, cancel := context.WithCancel(context.Background())
	return &NullTransport{
		SessionHolder: NewSessionHolder(nil),
		id:            id,
		ctx:           ctx,
		cancel:        cancel,
	}
}

func (n *NullTransport) ID() string { return n.id }

func (n *NullTransport) Context() string                 { return "" }
func (n *NullTransport) Tools() []common.ToolDesc        { return nil }
func (n *NullTransport) Start(ctx context.Context) error { return nil }

func (n *NullTransport) Read(ctx context.Context) (string, error) {
	if len(n.readBuf) == 0 {
		return "", io.EOF
	}
	s := n.readBuf[0]
	n.readBuf = n.readBuf[1:]
	return s, nil
}

func (n *NullTransport) Write(ctx context.Context, text string) error { return nil }
func (n *NullTransport) Flush() error                                 { return nil }
func (n *NullTransport) Close() error                                 { n.cancel(); return nil }
func (n *NullTransport) RequestPermission(_ context.Context, _ string) (PermissionResult, error) {
	return PermissionDenied, nil
}
func (n *NullTransport) Capability() Capability {
	return Capability{Interactive: false, Streamable: false, NestRead: false, RenderTextMarkdown: "none"}
}

var _ IO = (*NullTransport)(nil)
